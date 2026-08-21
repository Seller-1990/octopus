package sitesync

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

const (
	sub2APIAccessTokenRefreshLead  = 5 * time.Minute
	sub2APIAccessTokenSpreadWindow = 2 * time.Minute
	Sub2APIRefreshTaskInterval     = 30 * time.Second
)

type sub2APIRefreshedCredentials struct {
	AccessToken    string
	RefreshToken   string
	TokenExpiresAt int64
}

type sub2APIRefreshCall struct {
	done   chan struct{}
	result sub2APIRefreshResult
}

type sub2APIRefreshResult struct {
	token     string
	refreshed bool
	err       error
}

var sub2APIRefreshCalls = struct {
	sync.Mutex
	calls map[string]*sub2APIRefreshCall
}{calls: make(map[string]*sub2APIRefreshCall)}

func ensureFreshSub2APIAccessToken(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, forceRefresh bool) (string, error) {
	if account == nil {
		return "", fmt.Errorf("site account is nil")
	}
	key := sub2APIRefreshKey(siteRecord, account)
	for {
		result, joined := coordinateSub2APIRefresh(ctx, key, func() sub2APIRefreshResult {
			return ensureFreshSub2APIAccessTokenLocked(ctx, siteRecord, account, forceRefresh)
		})
		if result.err != nil {
			return "", result.err
		}
		if joined && account.ID > 0 {
			if err := reloadSub2APICredentials(ctx, account); err != nil {
				return "", err
			}
			result.token = stripBearerPrefix(account.AccessToken)
		}
		if forceRefresh && !result.refreshed {
			continue
		}
		return result.token, nil
	}
}

func sub2APIRefreshKey(siteRecord *model.Site, account *model.SiteAccount) string {
	if account.ID > 0 {
		return fmt.Sprintf("%s:id:%d", siteBaseURL(siteRecord), account.ID)
	}
	return fmt.Sprintf("%s:ptr:%p", siteBaseURL(siteRecord), account)
}

func ensureFreshSub2APIAccessTokenLocked(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, forceRefresh bool) sub2APIRefreshResult {
	if err := reloadSub2APICredentials(ctx, account); err != nil {
		return sub2APIRefreshResult{err: err}
	}
	accessToken := stripBearerPrefix(account.AccessToken)
	if accessToken == "" {
		return sub2APIRefreshResult{err: newAccessTokenRequiredError()}
	}
	if !forceRefresh && !shouldProactivelyRefreshSub2API(account) {
		return sub2APIRefreshResult{token: accessToken}
	}
	if strings.TrimSpace(account.RefreshToken) == "" {
		if forceRefresh {
			return sub2APIRefreshResult{err: fmt.Errorf("sub2api refresh token is required; reauthenticate the site account")}
		}
		return sub2APIRefreshResult{token: accessToken}
	}

	refreshed, err := refreshSub2APIManagedSession(ctx, siteRecord, account, accessToken)
	if err != nil {
		return sub2APIRefreshResult{err: err}
	}
	return sub2APIRefreshResult{token: refreshed, refreshed: true}
}

func reloadSub2APICredentials(ctx context.Context, account *model.SiteAccount) error {
	if account.ID <= 0 {
		return nil
	}
	var current model.SiteAccount
	if err := db.GetDB().WithContext(ctx).First(&current, account.ID).Error; err != nil {
		return fmt.Errorf("reload sub2api credentials: %w", err)
	}
	account.AccessToken = current.AccessToken
	account.RefreshToken = current.RefreshToken
	account.TokenExpiresAt = current.TokenExpiresAt
	account.CredentialRevision = current.CredentialRevision
	return nil
}

func coordinateSub2APIRefresh(ctx context.Context, key string, refresh func() sub2APIRefreshResult) (sub2APIRefreshResult, bool) {
	sub2APIRefreshCalls.Lock()
	if call := sub2APIRefreshCalls.calls[key]; call != nil {
		sub2APIRefreshCalls.Unlock()
		select {
		case <-ctx.Done():
			return sub2APIRefreshResult{err: ctx.Err()}, true
		case <-call.done:
			return call.result, true
		}
	}
	call := &sub2APIRefreshCall{done: make(chan struct{})}
	sub2APIRefreshCalls.calls[key] = call
	sub2APIRefreshCalls.Unlock()

	defer func() {
		sub2APIRefreshCalls.Lock()
		delete(sub2APIRefreshCalls.calls, key)
		close(call.done)
		sub2APIRefreshCalls.Unlock()
	}()

	call.result = refresh()
	return call.result, false
}

func siteBaseURL(siteRecord *model.Site) string {
	if siteRecord == nil {
		return ""
	}
	return siteRecord.BaseURL
}

func shouldProactivelyRefreshSub2API(account *model.SiteAccount) bool {
	return shouldProactivelyRefreshSub2APIAt(account, time.Now())
}

func shouldProactivelyRefreshSub2APIAt(account *model.SiteAccount, now time.Time) bool {
	if account == nil {
		return false
	}
	if strings.TrimSpace(account.RefreshToken) == "" {
		return false
	}
	if account.TokenExpiresAt <= 0 {
		return false
	}
	return !now.Before(sub2APIRefreshDueAt(account))
}

func sub2APIRefreshDueAt(account *model.SiteAccount) time.Time {
	if account == nil || account.TokenExpiresAt <= 0 {
		return time.Time{}
	}
	dueAt := time.UnixMilli(account.TokenExpiresAt).Add(-sub2APIAccessTokenRefreshLead)
	if account.ID <= 0 || sub2APIAccessTokenSpreadWindow <= 0 {
		return dueAt
	}
	spreadMillis := sub2APIAccessTokenSpreadWindow.Milliseconds()
	offsetMillis := int64((uint64(account.ID) * 11400714819323198485) % uint64(spreadMillis))
	return dueAt.Add(time.Duration(offsetMillis) * time.Millisecond)
}

func shouldRetrySub2APIAfterRefresh(err error, account *model.SiteAccount) bool {
	if err == nil || account == nil || strings.TrimSpace(account.RefreshToken) == "" {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	if text == "" {
		return false
	}
	status := anyToInt64(apperror.Params(err)["statusCode"])
	return status == 401 || status == 403 ||
		strings.Contains(text, "unauthorized") ||
		strings.Contains(text, "forbidden") ||
		strings.Contains(text, "expired") ||
		strings.Contains(text, "invalid token") ||
		strings.Contains(text, "access token")
}

func refreshSub2APIManagedSession(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount, currentAccessToken string) (string, error) {
	if siteRecord == nil || account == nil {
		return "", fmt.Errorf("site or account is nil")
	}
	refreshToken := strings.TrimSpace(account.RefreshToken)
	if refreshToken == "" {
		return "", fmt.Errorf("sub2api managed refresh token missing")
	}

	headers := map[string]string{
		"Content-Type": "application/json",
	}
	if currentAccessToken = stripBearerPrefix(currentAccessToken); currentAccessToken != "" {
		headers["Authorization"] = ensureBearer(currentAccessToken)
	}

	var payload map[string]any
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		payload, err = requestJSON(
			ctx,
			siteRecord,
			"POST",
			buildSiteURL(siteRecord.BaseURL, "/api/v1/auth/refresh"),
			map[string]any{"refresh_token": refreshToken},
			headers,
			account,
		)
		if err == nil || attempt > 0 || !isTransientSub2APIRefreshFailure(err) {
			break
		}
		retryDelay := siteErrorRetryAfter(err)
		if retryDelay <= 0 {
			retryDelay = 20 * time.Millisecond
		}
		timer := time.NewTimer(retryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}
	if err != nil {
		return "", fmt.Errorf("sub2api token refresh request failed: %w", err)
	}

	refreshed, ok := parseSub2APIRefreshPayload(payload)
	if !ok {
		return "", fmt.Errorf("sub2api token refresh failed")
	}

	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = refreshToken
	}
	account.AccessToken = refreshed.AccessToken
	account.RefreshToken = refreshed.RefreshToken
	account.TokenExpiresAt = refreshed.TokenExpiresAt

	if account.ID > 0 {
		result := db.GetDB().WithContext(ctx).
			Model(&model.SiteAccount{}).
			Where("id = ? AND credential_revision = ?", account.ID, account.CredentialRevision).
			Updates(map[string]any{
				"access_token":        refreshed.AccessToken,
				"refresh_token":       refreshed.RefreshToken,
				"token_expires_at":    refreshed.TokenExpiresAt,
				"credential_revision": account.CredentialRevision + 1,
			})
		if result.Error != nil {
			return "", fmt.Errorf("failed to persist sub2api refreshed session: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			if err := reloadSub2APICredentials(ctx, account); err != nil {
				return "", fmt.Errorf("reload sub2api credentials after concurrent update: %w", err)
			}
			return stripBearerPrefix(account.AccessToken), nil
		}
		account.CredentialRevision++
	}

	return refreshed.AccessToken, nil
}

func isTransientSub2APIRefreshFailure(err error) bool {
	if err == nil || IsCloudflareProtectionError(err) || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	status := anyToInt64(apperror.Params(err)["statusCode"])
	if status != 0 {
		return status == 429 || status == 502 || status == 503 || status == 504
	}
	return apperror.Code(err) == ""
}

func parseSub2APIRefreshPayload(payload map[string]any) (sub2APIRefreshedCredentials, bool) {
	if payload == nil {
		return sub2APIRefreshedCredentials{}, false
	}

	if rawCode, ok := payload["code"]; ok {
		code := anyToInt64(rawCode)
		if code != 0 {
			return sub2APIRefreshedCredentials{}, false
		}
	}

	data, ok := payload["data"].(map[string]any)
	if !ok {
		return sub2APIRefreshedCredentials{}, false
	}

	accessToken := stripBearerPrefix(jsonString(data["access_token"]))
	refreshToken := strings.TrimSpace(jsonString(data["refresh_token"]))
	expiresInSeconds := anyToInt64(data["expires_in"])
	if accessToken == "" || expiresInSeconds <= 0 {
		return sub2APIRefreshedCredentials{}, false
	}

	return sub2APIRefreshedCredentials{
		AccessToken:    accessToken,
		RefreshToken:   refreshToken,
		TokenExpiresAt: time.Now().Add(time.Duration(expiresInSeconds) * time.Second).UnixMilli(),
	}, true
}

func anyToInt64(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0
		}
		var parsed int64
		if _, err := fmt.Sscanf(trimmed, "%d", &parsed); err == nil {
			return parsed
		}
	}
	return 0
}
