package sitesync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestRefreshSitePricingUsesBrowserSessionWithoutExtractedToken(t *testing.T) {
	ctx := setupProjectTestDB(t)
	var directCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		directCalls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	site := model.Site{Name: "Browser Pricing Site", Platform: model.SitePlatformNewAPI, BaseURL: server.URL, Enabled: true}
	if err := op.SiteCreate(&site, ctx); err != nil {
		t.Fatalf("create site: %v", err)
	}
	account := model.SiteAccount{
		SiteID: site.ID, Name: "Browser Pricing Account",
		CredentialType: model.SiteCredentialTypeUsernamePassword,
		Username:       "browser-user", Password: "password", Enabled: true,
	}
	if err := op.SiteAccountCreate(&account, ctx); err != nil {
		t.Fatalf("create account: %v", err)
	}

	var browserCalls atomic.Int32
	browserCtx := withVerificationBrowserTransport(
		ctx,
		op.VerificationBrowserBinding{PairingID: 27, TaskID: 28, SessionID: 29, TargetURL: server.URL},
		func(_ context.Context, request op.VerificationBrowserRequestInput) (*op.VerificationBrowserResponse, error) {
			browserCalls.Add(1)
			if request.URL != server.URL+"/api/pricing" {
				t.Fatalf("unexpected browser pricing URL: %s", request.URL)
			}
			return &op.VerificationBrowserResponse{
				Status:  http.StatusOK,
				Headers: map[string]string{"content-type": "application/json"},
				Body:    `{"group_ratio":{},"data":[]}`,
			}, nil
		},
	)

	if err := refreshSitePricingQuotes(browserCtx, &site, &account, ""); err != nil {
		t.Fatalf("refresh pricing through browser session: %v", err)
	}
	if browserCalls.Load() != 1 || directCalls.Load() != 0 {
		t.Fatalf("pricing did not stay on browser transport: browser=%d direct=%d", browserCalls.Load(), directCalls.Load())
	}
}

func TestParseSitePricingQuotesAppliesGroupMultiplierOnlyInResolver(t *testing.T) {
	payload := map[string]any{
		"group_ratio": map[string]any{"premium": 2.0},
		"data": []any{
			map[string]any{
				"model_name":    "gpt-price-test",
				"enable_groups": []any{"premium"},
				"token_price_usd_per_million": map[string]any{
					"input":       1.25,
					"output":      3.5,
					"cache_read":  0.2,
					"cache_write": 0.8,
				},
			},
		},
	}

	quotes := parseSitePricingQuotes(11, 22, payload)
	if len(quotes) != 1 {
		t.Fatalf("got %d quotes, want 1", len(quotes))
	}
	quote := quotes[0]
	if quote.Input != 1.25 || quote.Output != 3.5 || quote.CacheRead != 0.2 || quote.CacheWrite != 0.8 {
		t.Fatalf("parser multiplied base prices prematurely: %+v", quote)
	}
	if quote.GroupMultiplier != 2 {
		t.Fatalf("group multiplier = %f, want 2", quote.GroupMultiplier)
	}
	if quote.Source != model.PriceQuoteSourceSiteExact ||
		quote.Unit != model.PriceUnitPerMillionTokens ||
		quote.Currency != "USD" ||
		quote.ExchangeRateToUSD != 1 {
		t.Fatalf("unexpected quote metadata: %+v", quote)
	}
}

func TestParseSitePricingQuotesPreservesExplicitZeroGroupMultiplier(t *testing.T) {
	payload := map[string]any{
		"group_ratio": map[string]any{"complimentary": 0.0},
		"data": []any{
			map[string]any{
				"model_name":    "free-model",
				"enable_groups": []any{"complimentary"},
				"input_price":   1.0,
			},
		},
	}

	quotes := parseSitePricingQuotes(11, 22, payload)
	if len(quotes) != 1 {
		t.Fatalf("got %d quotes, want 1", len(quotes))
	}
	if quotes[0].GroupMultiplier != 0 || !quotes[0].GroupMultiplierKnown {
		t.Fatalf("explicit zero group multiplier was replaced: %+v", quotes[0])
	}
}

func TestParseSitePricingGroupMultipliersDoesNotRequireModelQuotes(t *testing.T) {
	payload := map[string]any{
		"group_ratio": map[string]any{
			"default": 1.0,
			"vip":     0.25,
		},
		"data": []any{},
	}

	multipliers := parseSitePricingGroupMultipliers(payload)
	if len(multipliers) != 2 || multipliers["default"].Value != 1 || multipliers["vip"].Value != 0.25 ||
		!multipliers["default"].Known || !multipliers["vip"].Known {
		t.Fatalf("unexpected group multipliers: %+v", multipliers)
	}
}

func TestParseSitePricingGroupMultipliersDefaultsExplicitUnconfiguredGroup(t *testing.T) {
	payload := map[string]any{
		"group_ratio": map[string]any{
			"GPT-Pro-正价": 0.31,
		},
		"data": []any{
			map[string]any{
				"model_name":    "gpt-5.6-sol",
				"enable_groups": []any{"GPT-Plus-正价", "GPT-Pro-正价"},
				"model_ratio":   2.5,
			},
		},
	}

	multipliers := parseSitePricingGroupMultipliers(payload)
	// 阶段 2 v2 X10：S2 修正后——group_ratio 有值（GPT-Pro）known=true；
	// enable_groups 缺省补 1x（GPT-Plus）known=false（原断言「全部 known」反转）
	if len(multipliers) != 2 || multipliers["GPT-Plus-正价"].Value != 1 || multipliers["GPT-Plus-正价"].Known ||
		multipliers["GPT-Pro-正价"].Value != 0.31 || !multipliers["GPT-Pro-正价"].Known {
		t.Fatalf("unexpected group multipliers: %+v", multipliers)
	}

	quotes := parseSitePricingQuotes(11, 22, payload)
	if len(quotes) != 2 {
		t.Fatalf("got %d quotes, want 2", len(quotes))
	}
	knownByGroup := map[string]bool{}
	for _, quote := range quotes {
		knownByGroup[quote.GroupKey] = quote.GroupMultiplierKnown
	}
	if !knownByGroup["GPT-Pro-正价"] || knownByGroup["GPT-Plus-正价"] {
		t.Fatalf("unexpected quote known states: %+v", knownByGroup)
	}
}

func TestParseSitePricingQuotesPreservesPerRequestAndSiteCreditUnits(t *testing.T) {
	payload := map[string]any{
		"currency":    "credits",
		"group_ratio": map[string]any{"default": 3.0},
		"data": []any{
			map[string]any{
				"model_name":  "per-request-model",
				"quota_type":  1.0,
				"model_price": 0.5,
			},
			map[string]any{
				"model_name":   "credit-model",
				"unit":         "site_credit",
				"input_price":  2.0,
				"output_price": 4.0,
			},
			map[string]any{
				"model_name":  "invalid-unit-model",
				"unit":        "per_second",
				"input_price": 1.0,
			},
		},
	}

	quotes := parseSitePricingQuotes(11, 22, payload)
	if len(quotes) != 3 {
		t.Fatalf("got %d quotes, want 3", len(quotes))
	}
	byModel := make(map[string]model.SiteModelPriceQuote, len(quotes))
	for _, quote := range quotes {
		byModel[quote.ModelName] = quote
	}

	perRequest := byModel["per-request-model"]
	if perRequest.Unit != model.PriceUnitPerRequest ||
		perRequest.Currency != "SITE_CREDIT" ||
		perRequest.PerRequest != 0.5 ||
		perRequest.GroupMultiplier != 3 {
		t.Fatalf("per-request quote mismatch: %+v", perRequest)
	}
	credit := byModel["credit-model"]
	if credit.Unit != model.PriceUnitSiteCredit ||
		credit.Currency != "SITE_CREDIT" ||
		credit.Input != 2 ||
		credit.Output != 4 {
		t.Fatalf("site-credit quote mismatch: %+v", credit)
	}
	if got := byModel["invalid-unit-model"].Unit; got != model.PriceUnit("per_second") {
		t.Fatalf("unknown unit %q was not preserved for rejection", got)
	}
}

func TestSyncAccountBindsFirstPricingRefreshToProjectedCandidate(t *testing.T) {
	ctx := setupProjectTestDB(t)
	// 目录默认改为手动建组后，未选中的模型不会产生路由候选；
	// 本用例验证的是「有候选时价格必须绑定」，所以显式沿用自动建组。
	if err := op.SettingRefreshCache(ctx); err != nil {
		t.Fatalf("refresh setting cache: %v", err)
	}
	if err := op.SettingSetString(
		model.SettingKeyCatalogGroupProvisioning,
		string(model.CatalogGroupProvisioningAuto),
	); err != nil {
		t.Fatalf("enable auto catalog provisioning: %v", err)
	}
	const (
		accessToken = "sync-pricing-access-token"
		modelName   = "gpt-sync-pricing"
	)
	pricingCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+accessToken {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"unauthorized"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"` + modelName + `"}]}`))
		case "/api/pricing":
			pricingCalls++
			_, _ = w.Write([]byte(`{"group_ratio":{"default":0.25},"data":[{
				"model_name":"` + modelName + `",
				"enable_groups":["default"],
				"input_price":1.25,
				"output_price":3.5,
				"currency":"USD"
			}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site := model.Site{
		Name:     "Sync Pricing Site",
		Platform: model.SitePlatformAPI,
		BaseURL:  server.URL,
		Enabled:  true,
	}
	if err := op.SiteCreate(&site, ctx); err != nil {
		t.Fatalf("create site: %v", err)
	}
	account := model.SiteAccount{
		SiteID:         site.ID,
		Name:           "Sync Pricing Account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    accessToken,
		Enabled:        true,
		AutoSync:       true,
	}
	if err := op.SiteAccountCreate(&account, ctx); err != nil {
		t.Fatalf("create account: %v", err)
	}

	result, err := SyncAccount(ctx, account.ID)
	if err != nil {
		t.Fatalf("SyncAccount: %v", err)
	}
	if result.ModelCount != 1 || pricingCalls != 1 {
		t.Fatalf("unexpected sync/pricing result: result=%+v pricing_calls=%d", result, pricingCalls)
	}
	var quote model.SiteModelPriceQuote
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("site_account_id = ? AND model_name = ?", account.ID, modelName).
		First(&quote).Error; err != nil {
		t.Fatalf("load projected quote: %v", err)
	}
	if quote.RouteCandidateID == nil || *quote.RouteCandidateID <= 0 {
		t.Fatalf("first pricing refresh was not candidate-bound: %+v", quote)
	}
	var candidate model.RouteCandidate
	if err := dbpkg.GetDB().WithContext(ctx).First(&candidate, *quote.RouteCandidateID).Error; err != nil {
		t.Fatalf("load bound candidate: %v", err)
	}
	if candidate.SiteAccountID == nil || *candidate.SiteAccountID != account.ID ||
		candidate.UpstreamModelName != modelName {
		t.Fatalf("quote bound to wrong candidate: quote=%+v candidate=%+v", quote, candidate)
	}
	var unboundCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteModelPriceQuote{}).
		Where("site_account_id = ? AND model_name = ? AND route_candidate_id IS NULL", account.ID, modelName).
		Count(&unboundCount).Error; err != nil {
		t.Fatalf("count unbound quotes: %v", err)
	}
	if unboundCount != 0 {
		t.Fatalf("first sync left %d unbound pricing rows", unboundCount)
	}
	var group model.SiteUserGroup
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("site_account_id = ? AND group_key = ?", account.ID, model.SiteDefaultGroupKey).
		First(&group).Error; err != nil {
		t.Fatalf("load synced group: %v", err)
	}
	if group.Multiplier == nil || *group.Multiplier != 0.25 {
		t.Fatalf("pricing group multiplier was not persisted: %+v", group)
	}
}

func TestSyncAccountReturnsPartialResultWhenCatalogProjectionFails(t *testing.T) {
	ctx := setupProjectTestDB(t)
	const (
		accessToken = "sync-catalog-failure-token"
		modelName   = "gpt-sync-catalog-failure"
	)
	pricingCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+accessToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"` + modelName + `"}]}`))
		case "/api/pricing":
			pricingCalls++
			_, _ = w.Write([]byte(`{"data":[{"model_name":"` + modelName + `","input_price":1}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site := model.Site{
		Name: "Catalog Failure Site", Platform: model.SitePlatformAPI,
		BaseURL: server.URL, Enabled: true,
	}
	if err := op.SiteCreate(&site, ctx); err != nil {
		t.Fatalf("create site: %v", err)
	}
	account := model.SiteAccount{
		SiteID: site.ID, Name: "Catalog Failure Account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    accessToken, Enabled: true, AutoSync: true,
	}
	if err := op.SiteAccountCreate(&account, ctx); err != nil {
		t.Fatalf("create account: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).
		Migrator().DropTable(&model.CanonicalModel{}); err != nil {
		t.Fatalf("drop canonical model table: %v", err)
	}

	result, err := SyncAccount(ctx, account.ID)
	if err == nil {
		t.Fatal("catalog projection failure was not reported")
	}
	if result == nil || result.Status != model.SiteExecutionStatusPartial ||
		result.ChannelCount != 1 || result.ModelCount != 1 {
		t.Fatalf("persisted projection was hidden by catalog failure: %+v", result)
	}
	if pricingCalls != 0 {
		t.Fatalf("pricing refresh ran without candidate catalog: %d calls", pricingCalls)
	}
	var bindingCount int64
	if countErr := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteChannelBinding{}).
		Where("site_account_id = ?", account.ID).
		Count(&bindingCount).Error; countErr != nil {
		t.Fatalf("count persisted bindings: %v", countErr)
	}
	if bindingCount != 1 {
		t.Fatalf("catalog failure rolled back or hid projected binding: %d", bindingCount)
	}
	var reloaded model.SiteAccount
	if reloadErr := dbpkg.GetDB().WithContext(ctx).First(&reloaded, account.ID).Error; reloadErr != nil {
		t.Fatalf("reload account: %v", reloadErr)
	}
	if reloaded.LastSyncStatus != model.SiteExecutionStatusPartial {
		t.Fatalf("account sync state did not record partial projection: %+v", reloaded)
	}
}

// TestSyncAccountMarksProjectionFailureAsFailed F08 回归：ProjectAccount 失败时
// 必须把 persistSyncSnapshot 已写的 success 状态纠正为 failed + 标记投影 stale，
// 不能让 UI/调度误以为「同步成功」而路由仍是旧值。
// TestSyncAccountMarksProjectionFailureAsFailed F08 回归：ProjectAccount 失败时
// 必须把 persistSyncSnapshot 已写的 success 状态纠正为 failed + 标记投影 stale，
// 不能让 UI/调度误以为「同步成功」而路由仍是旧值。
// 实施后审查修正（P0）：原测试用 NewAPI 平台 + 预占同名普通 channel 均不成立——
// (a) NewAPI 走 fetchManagementTokens（mock 404）→ 命中 fetch-failure 旧分支而非 F08 分支；
// (b) 普通同名 channel 会被 reuseManagedChannelByName 收养而非冲突。现改为：
//   - SitePlatformAPI（syncOfficialPlatform，只需 /v1/models 即可走到 persist+projection）；
//   - 用 buildManagedChannelName 精确构造投影将创建的 channel 名，预建「绑定到另一账号」的
//     托管 channel，使 reuseManagedChannelByName 触发「already bound to another site account」。
func TestSyncAccountMarksProjectionFailureAsFailed(t *testing.T) {
	ctx := setupProjectTestDB(t)
	// 目录手动建组时未选中模型不产生候选；此处显式自动建组，让投影走完整链路
	if err := op.SettingRefreshCache(ctx); err != nil {
		t.Fatalf("refresh setting cache: %v", err)
	}
	if err := op.SettingSetString(
		model.SettingKeyCatalogGroupProvisioning,
		string(model.CatalogGroupProvisioningAuto),
	); err != nil {
		t.Fatalf("enable auto catalog provisioning: %v", err)
	}

	const (
		accessToken = "sync-projection-failure-token"
		modelName   = "gpt-sync-projection-failure"
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+accessToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"` + modelName + `"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site := model.Site{
		Name: "Projection Failure Site", Platform: model.SitePlatformAPI,
		BaseURL: server.URL, Enabled: true,
	}
	if err := op.SiteCreate(&site, ctx); err != nil {
		t.Fatalf("create site: %v", err)
	}
	account := model.SiteAccount{
		SiteID: site.ID, Name: "Projection Failure Account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    accessToken, Enabled: true, AutoSync: true,
	}
	if err := op.SiteAccountCreate(&account, ctx); err != nil {
		t.Fatalf("create account: %v", err)
	}
	// 「另一账号」：占用将被投影的 channel 名（该 channel 绑定到 otherAccount）
	otherAccount := model.SiteAccount{
		SiteID: site.ID, Name: "Other Account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "other-token", Enabled: true, AutoSync: true,
	}
	if err := op.SiteAccountCreate(&otherAccount, ctx); err != nil {
		t.Fatalf("create other account: %v", err)
	}

	// 用 buildManagedChannelName 精确构造 account 投影将创建的 channel 名
	group := model.SiteUserGroup{
		GroupKey: model.SiteDefaultGroupKey,
		Name:     model.SiteDefaultGroupName,
	}
	conflictName := buildManagedChannelName(&site, &account, group, outbound.OutboundTypeOpenAIChat)
	conflict := model.Channel{Name: conflictName, Model: modelName, Enabled: true}
	if err := op.ChannelCreate(&conflict, ctx); err != nil {
		t.Fatalf("pre-create conflicting managed channel: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&model.SiteChannelBinding{
		SiteID:        site.ID,
		SiteAccountID: otherAccount.ID,
		GroupKey:      model.SiteDefaultGroupKey,
		ChannelID:     conflict.ID,
	}).Error; err != nil {
		t.Fatalf("bind conflicting channel to other account: %v", err)
	}

	result, err := SyncAccount(ctx, account.ID)
	if err == nil {
		t.Fatalf("projection failure was not reported (result=%+v)", result)
	}
	// 必须命中 F08 分支（投影失败）而非 fetch-failure：消息应含「投影失败」
	if !strings.Contains(sanitizeSiteStatusText(err.Error()), "投影") &&
		!strings.Contains(err.Error(), "already bound") {
		t.Fatalf("expected projection failure (F08 branch), got: %v", err)
	}

	var reloaded model.SiteAccount
	if rerr := dbpkg.GetDB().WithContext(ctx).First(&reloaded, account.ID).Error; rerr != nil {
		t.Fatalf("reload account: %v", rerr)
	}
	// F08：状态必须被纠正为 failed（不能残留 success）
	if reloaded.LastSyncStatus != model.SiteExecutionStatusFailed {
		t.Fatalf("projection failure must mark account failed, got %q (message=%q)", reloaded.LastSyncStatus, reloaded.LastSyncMessage)
	}
	// stale 标记：模型组应被标记 stale（投影未生效，路由应降级）
	var staleCount int64
	if serr := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteUserGroup{}).
		Where("site_account_id = ? AND model_sync_status = ?", account.ID, model.SiteGroupModelSyncStatusStale).
		Count(&staleCount).Error; serr != nil {
		t.Fatalf("count stale groups: %v", serr)
	}
	if staleCount == 0 {
		t.Fatalf("projection failure must mark groups stale, got 0 stale")
	}
}
