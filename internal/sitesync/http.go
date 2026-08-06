package sitesync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/client"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
)

const (
	siteSafeReadMaxAttempts    = 3
	siteSafeReadBaseRetryDelay = 25 * time.Millisecond
	siteSafeReadMaxRetryDelay  = 2 * time.Second
)

func siteHTTPClient(ctx context.Context, siteRecord *model.Site, accounts ...*model.SiteAccount) (*http.Client, error) {
	if siteRecord == nil {
		return nil, fmt.Errorf("site is nil")
	}
	proxyMode, proxyConfigID := resolveSiteAccountProxy(siteRecord, accounts...)
	switch proxyMode {
	case "", model.ProxyUsageModeDirect:
		return client.GetHTTPClientSystemProxy(false)
	case model.ProxyUsageModeSystem:
		return client.GetHTTPClientSystemProxy(true)
	case model.ProxyUsageModePool:
		if proxyConfigID == nil || *proxyConfigID <= 0 {
			return nil, fmt.Errorf("proxy config id is required when proxy mode is pool")
		}
		proxyURL, err := op.ProxyURLForConfig(*proxyConfigID, ctx)
		if err != nil {
			return nil, err
		}
		return client.GetHTTPClientCustomProxy(proxyURL)
	default:
		return nil, fmt.Errorf("unsupported proxy mode: %s", proxyMode)
	}
}

func resolveSiteAccountProxy(siteRecord *model.Site, accounts ...*model.SiteAccount) (model.ProxyUsageMode, *int) {
	if len(accounts) > 0 && accounts[0] != nil && accounts[0].ProxyMode != "" && accounts[0].ProxyMode != model.ProxyUsageModeInherit {
		return accounts[0].ProxyMode, accounts[0].ProxyConfigID
	}
	if siteRecord == nil {
		return model.ProxyUsageModeDirect, nil
	}
	if siteRecord.ProxyMode == "" {
		return model.ProxyUsageModeDirect, nil
	}
	return siteRecord.ProxyMode, siteRecord.ProxyConfigID
}

func requestJSON(ctx context.Context, siteRecord *model.Site, method string, requestURL string, body any, headers map[string]string, accounts ...*model.SiteAccount) (map[string]any, error) {
	if siteRecord == nil {
		return nil, fmt.Errorf("site is nil")
	}
	var payloadBytes []byte
	var err error
	if body != nil {
		payloadBytes, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}

	account := firstSiteAccount(accounts...)
	policy := resolveSiteRequestHeaderPolicy(ctx, siteRecord, account)
	verificationCookie, verificationUserAgent := siteVerificationHeaders(ctx, siteRecord, account)
	buildRequest := func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(
			ctx,
			method,
			requestURL,
			bytes.NewReader(payloadBytes),
		)
		if err != nil {
			return nil, err
		}
		applyDefaultSiteRequestHeaders(req, body != nil)
		op.ApplyHeaderPolicy(req.Header, nil, siteRecord.CustomHeader, policy)
		applyTrustedSiteRequestHeaders(req.Header, headers, verificationCookie, verificationUserAgent)
		return req, nil
	}

	if transport, ok := verificationBrowserTransportFromContext(ctx); ok {
		for attempt := 0; attempt < siteSafeReadMaxAttempts; attempt++ {
			req, err := buildRequest()
			if err != nil {
				return nil, err
			}
			response, err := transport.request(ctx, op.VerificationBrowserRequestInput{
				Binding: transport.binding,
				Method:  req.Method,
				URL:     req.URL.String(),
				Headers: verificationBrowserHeaders(req.Header),
				Body:    string(payloadBytes),
			})
			if err != nil {
				if !shouldRetrySiteReadTransport(ctx, method, attempt, err) {
					return nil, err
				}
				if err := waitForSiteReadRetry(ctx, attempt, nil); err != nil {
					return nil, err
				}
				continue
			}
			responseHeader := verificationBrowserResponseHeader(response.Headers)
			responseBody := []byte(response.Body)
			if shouldRetrySiteReadResponse(method, attempt, response.Status, responseHeader, responseBody) {
				if err := waitForSiteReadRetry(ctx, attempt, responseHeader); err != nil {
					return nil, err
				}
				continue
			}
			return parseSiteJSONResponse(response.Status, responseHeader, responseBody)
		}
		return nil, fmt.Errorf("site read retry attempts exhausted")
	}

	httpClient, err := siteHTTPClient(ctx, siteRecord, accounts...)
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < siteSafeReadMaxAttempts; attempt++ {
		req, err := buildRequest()
		if err != nil {
			return nil, err
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			if !shouldRetrySiteReadTransport(ctx, method, attempt, err) {
				return nil, err
			}
			if err := waitForSiteReadRetry(ctx, attempt, nil); err != nil {
				return nil, err
			}
			continue
		}

		bodyBytes, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			if !shouldRetrySiteReadTransport(ctx, method, attempt, readErr) {
				return nil, readErr
			}
			if err := waitForSiteReadRetry(ctx, attempt, nil); err != nil {
				return nil, err
			}
			continue
		}
		if shouldRetrySiteReadResponse(method, attempt, resp.StatusCode, resp.Header, bodyBytes) {
			if err := waitForSiteReadRetry(ctx, attempt, resp.Header); err != nil {
				return nil, err
			}
			continue
		}
		return parseSiteJSONResponse(resp.StatusCode, resp.Header, bodyBytes)
	}
	return nil, fmt.Errorf("site read retry attempts exhausted")
}

func shouldRetrySiteReadTransport(ctx context.Context, method string, attempt int, err error) bool {
	return err != nil && method == http.MethodGet && attempt+1 < siteSafeReadMaxAttempts && ctx.Err() == nil
}

func shouldRetrySiteReadResponse(method string, attempt int, statusCode int, header http.Header, body []byte) bool {
	if method != http.MethodGet || attempt+1 >= siteSafeReadMaxAttempts {
		return false
	}
	if IsCloudflareProtectionResponse(statusCode, header, body) {
		return false
	}
	switch statusCode {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func waitForSiteReadRetry(ctx context.Context, attempt int, header http.Header) error {
	delay := time.Duration(0)
	if header != nil {
		delay = parseSiteRetryAfter(header.Get("Retry-After"))
	}
	if delay <= 0 {
		delay = siteSafeReadBaseRetryDelay * time.Duration(1<<attempt)
	}
	if delay > siteSafeReadMaxRetryDelay {
		delay = siteSafeReadMaxRetryDelay
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func parseSiteJSONResponse(
	statusCode int,
	header http.Header,
	bodyBytes []byte,
) (map[string]any, error) {
	if statusCode < 200 || statusCode >= 300 {
		return nil, formatSiteHTTPError(statusCode, header, bodyBytes)
	}
	if len(bodyBytes) == 0 {
		return map[string]any{}, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		if IsCloudflareProtectionResponse(statusCode, header, bodyBytes) {
			return nil, wrapCloudflareProtectionError(
				newCloudflareProtectionError(statusCode, header),
			)
		}
		return nil, formatSiteDecodeError(header.Get("Content-Type"), bodyBytes, err)
	}
	return payload, nil
}

func firstSiteAccount(accounts ...*model.SiteAccount) *model.SiteAccount {
	for _, account := range accounts {
		if account != nil {
			return account
		}
	}
	return nil
}

func resolveSiteRequestHeaderPolicy(
	ctx context.Context,
	siteRecord *model.Site,
	account *model.SiteAccount,
) model.ResolvedHeaderPolicy {
	if siteRecord == nil || siteRecord.ID <= 0 {
		return op.HeaderPolicyFailureFallback()
	}
	accountID := 0
	if account != nil {
		accountID = account.ID
	}
	policy, err := op.ResolveSiteHeaderPolicy(ctx, siteRecord.ID, accountID)
	if err == nil {
		return policy
	}
	// ctx 已超时/取消时失败是连锁反应而非独立故障，避免刷屏噪音
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return op.HeaderPolicyFailureFallback()
	}
	log.Warnf(
		"resolve site header policy failed (site=%d account=%d): %v",
		siteRecord.ID,
		accountID,
		err,
	)
	return op.HeaderPolicyFailureFallback()
}

func siteVerificationHeaders(
	ctx context.Context,
	siteRecord *model.Site,
	account *model.SiteAccount,
) (cookie string, userAgent string) {
	if siteRecord == nil || account == nil {
		return "", ""
	}
	_, proxyConfigID := resolveSiteAccountProxy(siteRecord, account)
	clashNode := account.PreferredClashNode
	if clashNode == "" {
		clashNode = siteRecord.PreferredClashNode
	}
	if path, ok := ctx.Value(recoveryPathContextKey{}).(siteRecoveryPath); ok {
		proxyConfigID = cloneInt(path.proxyConfigID)
		clashNode = path.clashNode
	}
	cookie, userAgent, ok := op.VerificationHeadersForAccount(account, proxyConfigID, clashNode)
	if !ok {
		return "", ""
	}
	return cookie, userAgent
}

func applyTrustedSiteRequestHeaders(
	target http.Header,
	headers map[string]string,
	verificationCookie string,
	verificationUserAgent string,
) {
	if target == nil {
		return
	}
	cookieHeader := target.Get("Cookie")
	for key, value := range headers {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		if strings.EqualFold(key, "Cookie") {
			cookieHeader = mergeCookieHeaderValues(cookieHeader, value)
			continue
		}
		target.Set(key, value)
	}
	cookieHeader = mergeCookieHeaderValues(cookieHeader, verificationCookie)
	if cookieHeader != "" {
		target.Set("Cookie", cookieHeader)
	}
	if verificationUserAgent != "" {
		target.Set("User-Agent", verificationUserAgent)
	}
}

func mergeCookieHeaderValues(values ...string) string {
	merged := ""
	for _, value := range values {
		for _, pair := range strings.Split(value, ";") {
			parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
			if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
				continue
			}
			merged = anyRouterUpsertCookie(merged, strings.TrimSpace(parts[0]), parts[1])
		}
	}
	return merged
}

func applyDefaultSiteRequestHeaders(req *http.Request, hasJSONBody bool) {
	if req == nil {
		return
	}
	if hasJSONBody {
		req.Header.Set("Content-Type", "application/json")
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", anyRouterUserAgent)
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json, text/plain, */*")
	}
	if req.Header.Get("Accept-Language") == "" {
		req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	}
}

func formatSiteHTTPError(statusCode int, header http.Header, bodyBytes []byte) error {
	if IsCloudflareProtectionResponse(statusCode, header, bodyBytes) {
		return wrapCloudflareProtectionError(newCloudflareProtectionError(statusCode, header))
	}
	if payload, ok := parseSiteJSONMap(bodyBytes); ok {
		if message := extractSiteResponseMessage(payload); message != "" {
			return newSiteHTTPErrorWithHeader(statusCode, message, header)
		}
	}
	if summary := extractSiteHTMLResponseSummary(header.Get("Content-Type"), bodyBytes); summary != "" {
		return newSiteHTTPErrorWithHeader(statusCode, summary, header)
	}
	return newSiteHTTPErrorWithHeader(statusCode, "上游返回非 JSON 响应，无法解析为接口响应", header)
}

// IsCloudflareProtectionResponse 判断一次上游响应是否为 Cloudflare 防护拦截。
// 供 sitesync 内部与被动离群退役（POR）门3 复用。
func IsCloudflareProtectionResponse(statusCode int, header http.Header, bodyBytes []byte) bool {
	switch statusCode {
	case http.StatusOK, http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable:
	default:
		return false
	}
	if json.Valid(bodyBytes) {
		payload, ok := parseSiteJSONMap(bodyBytes)
		if !ok {
			return false
		}
		return isCloudflareChallengeText(extractSiteResponseMessage(payload))
	}
	body := strings.ToLower(string(bodyBytes))
	if isCloudflareChallengeText(body) {
		return true
	}
	server := strings.ToLower(header.Get("Server"))
	hasCloudflareHeader := header.Get("CF-Ray") != "" || strings.Contains(server, "cloudflare")
	if !hasCloudflareHeader {
		return false
	}
	contentType := strings.ToLower(header.Get("Content-Type"))
	htmlLike := strings.Contains(contentType, "text/html") ||
		strings.HasPrefix(strings.TrimSpace(body), "<!doctype html") ||
		strings.HasPrefix(strings.TrimSpace(body), "<html")
	return statusCode != http.StatusOK || htmlLike
}

func isCloudflareChallengeText(value string) bool {
	value = strings.ToLower(value)
	return strings.Contains(value, "attention required") ||
		strings.Contains(value, "just a moment") ||
		strings.Contains(value, "cf-error-code") ||
		strings.Contains(value, "cloudflare ray id")
}

func formatSiteDecodeError(contentType string, bodyBytes []byte, err error) error {
	if summary := extractSiteHTMLResponseSummary(contentType, bodyBytes); summary != "" {
		return wrapSiteDecodeError(fmt.Sprintf("decode response failed: %s", summary), err)
	}
	return wrapSiteDecodeError(fmt.Sprintf("decode response failed: %v", err), err)
}

func parseSiteJSONMap(bodyBytes []byte) (map[string]any, bool) {
	if len(bodyBytes) == 0 {
		return map[string]any{}, true
	}
	var payload map[string]any
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		return nil, false
	}
	return payload, true
}

func extractSiteResponseMessage(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	return firstNonEmptyString(
		jsonString(payload["message"]),
		jsonString(nestedValue(payload, "error", "message")),
		jsonString(payload["msg"]),
	)
}

func extractSiteHTMLResponseSummary(contentType string, bodyBytes []byte) string {
	body := strings.TrimSpace(string(bodyBytes))
	if body == "" {
		return ""
	}
	if summary := extractHTMLErrorSummary(body); summary != "" {
		return summary
	}
	lowered := strings.ToLower(contentType + "\n" + body)
	if strings.Contains(lowered, "just a moment") {
		return "Just a moment..."
	}
	if strings.Contains(lowered, "cloudflare") {
		return "Cloudflare challenge"
	}
	return ""
}
func buildSiteURL(baseURL string, path string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return baseURL + path
}

func parseTokenItems(payload map[string]any) []map[string]any {
	return parseTokenItemsFromAny(payload)
}

func parseTokenItemsFromAny(value any) []map[string]any {
	for _, candidate := range itemSliceCandidates(value) {
		if items := normalizeItemSlice(candidate); len(items) > 0 {
			return items
		}
	}
	return nil
}

func itemSliceCandidates(value any) []any {
	payload, ok := value.(map[string]any)
	if !ok {
		return []any{value}
	}
	return []any{
		nestedValue(payload, "data", "items"),
		nestedValue(payload, "data", "list"),
		nestedValue(payload, "data", "records"),
		nestedValue(payload, "data", "rows"),
		nestedValue(payload, "data", "data"),
		payload["items"],
		payload["list"],
		payload["records"],
		payload["rows"],
		payload["data"],
	}
}

func parseGroupItems(payload map[string]any) []model.SiteUserGroup {
	return parseGroupItemsFromAny(payload)
}

func parseGroupItemsFromAny(value any) []model.SiteUserGroup {
	items := make([]model.SiteUserGroup, 0)
	for _, candidate := range groupItemCandidates(value) {
		items = parseGroupCandidate(candidate)
		if len(items) > 0 {
			break
		}
	}
	deduped := make(map[string]model.SiteUserGroup)
	for _, item := range items {
		key := model.NormalizeSiteGroupKey(item.GroupKey)
		item.GroupKey = key
		item.Name = model.NormalizeSiteGroupName(key, item.Name)
		deduped[key] = item
	}
	keys := make([]string, 0, len(deduped))
	for key := range deduped {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	result := make([]model.SiteUserGroup, 0, len(keys))
	for _, key := range keys {
		result = append(result, deduped[key])
	}
	return result
}

func groupItemCandidates(value any) []any {
	payload, ok := value.(map[string]any)
	if !ok {
		return []any{value}
	}
	return []any{
		nestedValue(payload, "data", "groups"),
		nestedValue(payload, "data", "items"),
		nestedValue(payload, "data", "list"),
		nestedValue(payload, "data", "records"),
		nestedValue(payload, "data", "rows"),
		nestedValue(payload, "data", "data"),
		payload["groups"],
		payload["items"],
		payload["list"],
		payload["records"],
		payload["rows"],
		payload["data"],
		payload,
	}
}

func parseGroupCandidate(candidate any) []model.SiteUserGroup {
	items := make([]model.SiteUserGroup, 0)
	switch value := candidate.(type) {
	case []any:
		for _, raw := range value {
			switch item := raw.(type) {
			case string:
				if trimmed := strings.TrimSpace(item); trimmed != "" {
					items = append(items, model.SiteUserGroup{GroupKey: trimmed, Name: trimmed})
				}
			case float64, int, int64:
				if groupKey := jsonString(item); groupKey != "" {
					items = append(items, model.SiteUserGroup{GroupKey: groupKey, Name: groupKey})
				}
			case map[string]any:
				if group, ok := parseGroupObject(item); ok {
					items = append(items, group)
				}
			}
		}
	case []string:
		for _, item := range value {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				items = append(items, model.SiteUserGroup{GroupKey: trimmed, Name: trimmed})
			}
		}
	case map[string]any:
		if group, ok := parseGroupObject(value); ok {
			return []model.SiteUserGroup{group}
		}
		for key, raw := range value {
			if isIgnorableGroupMapKey(key) {
				continue
			}
			name := key
			if value, ok := raw.(string); ok {
				name = firstNonEmptyString(value, key)
			} else if item, ok := raw.(map[string]any); ok {
				name = firstNonEmptyString(jsonString(item["name"]), jsonString(item["group_name"]), jsonString(item["groupName"]), jsonString(item["title"]), jsonString(item["label"]), key)
			}
			group := model.SiteUserGroup{GroupKey: key, Name: name}
			if item, ok := raw.(map[string]any); ok {
				group.Multiplier = parseOptionalSiteGroupMultiplier(
					item["rate_multiplier"], item["group_multiplier"], item["multiplier"], item["ratio"], item["rate"],
				)
			}
			items = append(items, group)
		}
	}
	return items
}

func parseGroupObject(item map[string]any) (model.SiteUserGroup, bool) {
	groupKey := firstNonEmptyString(
		jsonString(item["group_id"]),
		jsonString(item["groupId"]),
		jsonString(item["id"]),
		jsonString(item["value"]),
		jsonString(item["code"]),
		jsonString(item["name"]),
		jsonString(item["group_name"]),
		jsonString(item["groupName"]),
		jsonString(item["title"]),
		jsonString(item["label"]),
	)
	groupName := firstNonEmptyString(
		jsonString(item["name"]),
		jsonString(item["group_name"]),
		jsonString(item["groupName"]),
		jsonString(item["title"]),
		jsonString(item["label"]),
		groupKey,
	)
	if strings.TrimSpace(groupKey) == "" {
		return model.SiteUserGroup{}, false
	}
	return model.SiteUserGroup{
		GroupKey: strings.TrimSpace(groupKey),
		Name:     strings.TrimSpace(groupName),
		Multiplier: parseOptionalSiteGroupMultiplier(
			item["rate_multiplier"], item["group_multiplier"], item["multiplier"], item["ratio"], item["rate"],
		),
	}, true
}

func isIgnorableGroupMapKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "", "success", "message", "msg", "data", "code", "error", "errors", "groups", "items", "list", "records", "rows", "total", "page", "page_size", "pageSize":
		return true
	default:
		return false
	}
}

func normalizeItemSlice(value any) []map[string]any {
	switch rawItems := value.(type) {
	case []map[string]any:
		return rawItems
	case []any:
		items := make([]map[string]any, 0, len(rawItems))
		for _, raw := range rawItems {
			if item, ok := raw.(map[string]any); ok {
				items = append(items, item)
			}
		}
		return items
	default:
		return nil
	}
}

func normalizeModelNames(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	result := make([]string, 0, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	slices.Sort(result)
	return result
}

func parseEnabledFlag(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case float64:
		return int(typed) != 0
	case int:
		return typed != 0
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "", "enabled", "active", "1", "true", "on":
			return true
		case "disabled", "inactive", "0", "false", "off":
			return false
		default:
			return true
		}
	default:
		return true
	}
}

func ensureBearer(token string) string {
	token = strings.TrimSpace(token)
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		return token
	}
	return "Bearer " + token
}

func requestJSONWithManagedAccessToken(ctx context.Context, siteRecord *model.Site, method string, requestURL string, body any, accessToken string, accounts ...*model.SiteAccount) (map[string]any, error) {
	initialHeaders := managedUserIDHeaders(firstManagedPlatformUserID(accounts...))
	payload, err := requestJSONWithManagedHeaders(ctx, siteRecord, method, requestURL, body, accessToken, initialHeaders, accounts...)
	if err == nil || !siteRequiresManagedUserIDHeader(siteRecord) || !shouldRetryManagedRequestWithUserID(err) {
		return payload, err
	}

	userID, discoverErr := discoverManagedUserID(ctx, siteRecord, accessToken, accounts...)
	if discoverErr != nil {
		return nil, discoverErr
	}
	if userID <= 0 {
		return nil, err
	}
	rememberManagedPlatformUserID(userID, accounts...)

	userHeaders := managedUserIDHeaders(userID)
	return requestJSONWithManagedHeaders(ctx, siteRecord, method, requestURL, body, accessToken, userHeaders, accounts...)
}

func requestJSONWithManagedHeaders(ctx context.Context, siteRecord *model.Site, method string, requestURL string, body any, accessToken string, extraHeaders map[string]string, accounts ...*model.SiteAccount) (map[string]any, error) {
	var firstErr error
	for _, headers := range buildManagedAuthHeaders(accessToken, accounts...) {
		payload, err := requestJSON(ctx, siteRecord, method, requestURL, body, mergeHeaders(headers, extraHeaders), accounts...)
		if err == nil {
			return payload, nil
		}
		if firstErr == nil {
			firstErr = err
		}
		if !shouldTryAlternativeManagedAuth(err) {
			return nil, err
		}
	}
	return nil, firstErr
}

func buildManagedAuthHeaders(accessToken string, accounts ...*model.SiteAccount) []map[string]string {
	token := strings.TrimSpace(accessToken)
	if token == "" {
		return []map[string]string{{}}
	}
	if account := firstSiteAccount(accounts...); account != nil {
		switch account.CredentialType {
		case model.SiteCredentialTypeCookie:
			return []map[string]string{{"Cookie": token}}
		case model.SiteCredentialTypeAccessToken, model.SiteCredentialTypeAPIKey:
			return []map[string]string{{"Authorization": ensureBearer(token)}}
		}
	}

	candidates := make([]map[string]string, 0, 2)
	if looksLikeCookieToken(token) {
		candidates = append(candidates, map[string]string{"Cookie": token})
	}
	candidates = append(candidates, map[string]string{"Authorization": ensureBearer(token)})
	return candidates
}

func managedSessionRequestAvailable(ctx context.Context, accessToken string) bool {
	if strings.TrimSpace(accessToken) != "" {
		return true
	}
	_, ok := verificationBrowserTransportFromContext(ctx)
	return ok
}

func looksLikeCookieToken(token string) bool {
	return model.IsSiteCookieCredential(token)
}

func shouldTryAlternativeManagedAuth(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "http 400") || strings.Contains(message, "http 401") || strings.Contains(message, "http 403")
}

func siteRequiresManagedUserIDHeader(siteRecord *model.Site) bool {
	return siteRecord != nil && siteRecord.Platform == model.SitePlatformNewAPI
}

func shouldRetryManagedRequestWithUserID(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "new-api-user") ||
		strings.Contains(message, "missing user id") ||
		strings.Contains(message, "requires user id") ||
		strings.Contains(message, "invalid user id") ||
		strings.Contains(message, "wrong user id") ||
		strings.Contains(message, "未提供")
}

func discoverManagedUserID(ctx context.Context, siteRecord *model.Site, accessToken string, accounts ...*model.SiteAccount) (int, error) {
	requestURL := buildSiteURL(siteRecord.BaseURL, "/api/user/self")

	payload, err := requestJSONWithManagedHeaders(ctx, siteRecord, http.MethodGet, requestURL, nil, accessToken, nil, accounts...)
	if err == nil {
		if userID := anyRouterExtractUserID(payload); userID > 0 {
			return userID, nil
		}
	}

	var firstErr error
	if err != nil {
		firstErr = err
	}

	for _, userID := range anyRouterBuildUserIDProbeCandidates(accessToken) {
		userHeaders := map[string]string{}
		anyRouterAddUserIDHeaders(userHeaders, userID)
		payload, probeErr := requestJSONWithManagedHeaders(ctx, siteRecord, http.MethodGet, requestURL, nil, accessToken, userHeaders, accounts...)
		if probeErr != nil {
			if firstErr == nil {
				firstErr = probeErr
			}
			continue
		}
		if anyRouterExtractUserID(payload) > 0 {
			return userID, nil
		}
	}

	return 0, firstErr
}

func firstManagedPlatformUserID(accounts ...*model.SiteAccount) int {
	for _, account := range accounts {
		if account != nil && account.PlatformUserID != nil && *account.PlatformUserID > 0 {
			return *account.PlatformUserID
		}
	}
	return 0
}

func rememberManagedPlatformUserID(userID int, accounts ...*model.SiteAccount) {
	if userID <= 0 {
		return
	}
	for _, account := range accounts {
		if account == nil {
			continue
		}
		if account.PlatformUserID == nil || *account.PlatformUserID != userID {
			resolvedUserID := userID
			account.PlatformUserID = &resolvedUserID
		}
	}
}

func managedUserIDHeaders(userID int) map[string]string {
	if userID <= 0 {
		return nil
	}
	headers := map[string]string{}
	anyRouterAddUserIDHeaders(headers, userID)
	return headers
}

func mergeHeaders(base map[string]string, extra map[string]string) map[string]string {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	merged := make(map[string]string, len(base)+len(extra))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range extra {
		merged[key] = value
	}
	return merged
}

func jsonString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strings.TrimSpace(fmt.Sprintf("%.0f", typed))
	case int:
		return strings.TrimSpace(fmt.Sprintf("%d", typed))
	case int64:
		return strings.TrimSpace(fmt.Sprintf("%d", typed))
	default:
		return ""
	}
}

func jsonBool(value any) bool {
	typed, ok := value.(bool)
	if ok {
		return typed
	}
	return false
}

func nestedValue(payload map[string]any, keys ...string) any {
	var current any = payload
	for _, key := range keys {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = obj[key]
	}
	return current
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func marshalRawPayload(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(payload)
}
