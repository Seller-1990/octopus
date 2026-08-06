package sitesync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func TestEnsureFreshSub2APIAccessTokenReturnsProactiveRefreshFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"message":"temporarily unavailable"}`))
	}))
	defer server.Close()

	account := &model.SiteAccount{
		AccessToken:    "stale-access-token",
		RefreshToken:   "preserved-refresh-token",
		TokenExpiresAt: time.Now().UnixMilli(),
	}
	_, err := ensureFreshSub2APIAccessToken(context.Background(), &model.Site{BaseURL: server.URL}, account, false)
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("expected proactive refresh failure, got %v", err)
	}
	if account.AccessToken != "stale-access-token" || account.RefreshToken != "preserved-refresh-token" {
		t.Fatalf("failed refresh changed credentials: %+v", account)
	}
}

func TestEnsureFreshSub2APIAccessTokenCoalescesConcurrentRefresh(t *testing.T) {
	var refreshCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		refreshCalls.Add(1)
		time.Sleep(20 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"access_token":"fresh-access-token","expires_in":3600}}`))
	}))
	defer server.Close()

	account := &model.SiteAccount{
		AccessToken:    "stale-access-token",
		RefreshToken:   "preserved-refresh-token",
		TokenExpiresAt: time.Now().UnixMilli(),
	}
	const callers = 100
	results := make(chan string, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	wg.Add(callers)
	for range callers {
		go func() {
			defer wg.Done()
			token, err := ensureFreshSub2APIAccessToken(context.Background(), &model.Site{BaseURL: server.URL}, account, false)
			results <- token
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent refresh returned error: %v", err)
		}
	}
	for token := range results {
		if token != "fresh-access-token" {
			t.Fatalf("expected shared fresh token, got %q", token)
		}
	}
	if refreshCalls.Load() != 1 {
		t.Fatalf("expected one upstream refresh, got %d", refreshCalls.Load())
	}
	if account.RefreshToken != "preserved-refresh-token" {
		t.Fatalf("omitted rotated token cleared refresh credential: %q", account.RefreshToken)
	}
}

func TestEnsureFreshSub2APIAccessTokenReloadsJoinedAccountCopies(t *testing.T) {
	ctx := setupProjectTestDB(t)
	var refreshCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		refreshCalls.Add(1)
		time.Sleep(20 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"access_token":"fresh-copy-token","refresh_token":"rotated-copy-token","expires_in":3600}}`))
	}))
	defer server.Close()

	siteRecord := model.Site{Name: "copy-refresh", Platform: model.SitePlatformSub2API, BaseURL: server.URL, Enabled: true}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&siteRecord).Error; err != nil {
		t.Fatalf("create site: %v", err)
	}
	persisted := model.SiteAccount{
		SiteID:         siteRecord.ID,
		Name:           "copy-account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "stale-copy-token",
		RefreshToken:   "old-copy-refresh",
		TokenExpiresAt: time.Now().UnixMilli(),
		Enabled:        true,
		AutoSync:       true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&persisted).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}

	const callers = 20
	accounts := make([]model.SiteAccount, callers)
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for index := range accounts {
		accounts[index] = persisted
		wg.Add(1)
		go func(account *model.SiteAccount) {
			defer wg.Done()
			_, err := ensureFreshSub2APIAccessToken(ctx, &siteRecord, account, false)
			errs <- err
		}(&accounts[index])
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("joined refresh returned error: %v", err)
		}
	}
	if refreshCalls.Load() != 1 {
		t.Fatalf("expected one upstream refresh, got %d", refreshCalls.Load())
	}
	for index := range accounts {
		if accounts[index].AccessToken != "fresh-copy-token" ||
			accounts[index].RefreshToken != "rotated-copy-token" ||
			accounts[index].CredentialRevision != 1 {
			t.Fatalf("account copy %d stayed stale: %+v", index, accounts[index])
		}
	}
}

func TestEnsureFreshSub2APIAccessTokenForceRequiresRefreshToken(t *testing.T) {
	account := &model.SiteAccount{AccessToken: "rejected-token"}
	_, err := ensureFreshSub2APIAccessToken(context.Background(), &model.Site{BaseURL: "https://sub2.example"}, account, true)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "refresh token") {
		t.Fatalf("expected actionable missing refresh token error, got %v", err)
	}
}

func TestEnsureFreshSub2APIAccessTokenForceRetriesAfterJoinedShortCircuit(t *testing.T) {
	var refreshCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		refreshCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"access_token":"forced-fresh-token","expires_in":3600}}`))
	}))
	defer server.Close()

	siteRecord := &model.Site{BaseURL: server.URL}
	account := &model.SiteAccount{
		AccessToken:    "still-valid-token",
		RefreshToken:   "force-refresh-token",
		TokenExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
	}
	key := sub2APIRefreshKey(siteRecord, account)
	leaderStarted := make(chan struct{})
	releaseLeader := make(chan struct{})
	leaderDone := make(chan struct{})
	go func() {
		defer close(leaderDone)
		coordinateSub2APIRefresh(context.Background(), key, func() sub2APIRefreshResult {
			close(leaderStarted)
			<-releaseLeader
			return sub2APIRefreshResult{token: account.AccessToken}
		})
	}()
	<-leaderStarted

	result := make(chan error, 1)
	go func() {
		token, err := ensureFreshSub2APIAccessToken(context.Background(), siteRecord, account, true)
		if err == nil && token != "forced-fresh-token" {
			err = fmt.Errorf("unexpected forced token %q", token)
		}
		result <- err
	}()
	select {
	case err := <-result:
		t.Fatalf("force refresh returned before joined call completed: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	close(releaseLeader)
	<-leaderDone
	if err := <-result; err != nil {
		t.Fatalf("force refresh after joined short circuit: %v", err)
	}
	if refreshCalls.Load() != 1 {
		t.Fatalf("expected one forced upstream refresh, got %d", refreshCalls.Load())
	}
}

func TestSub2APICredentialColumnsMigrateAdditively(t *testing.T) {
	setupProjectTestDB(t)
	for _, field := range []string{
		"SessionCookieEncrypted",
		"CredentialRevision",
		"LastAuthFailureClass",
		"LastAuthFailureAt",
	} {
		if !dbpkg.GetDB().Migrator().HasColumn(&model.SiteAccount{}, field) {
			t.Fatalf("expected additive site account column for %s", field)
		}
	}
	if err := dbpkg.GetDB().AutoMigrate(&model.SiteAccount{}); err != nil {
		t.Fatalf("rerun site account migration: %v", err)
	}
}

func TestSub2APIRefreshCASPreservesNewerCredentials(t *testing.T) {
	ctx := setupProjectTestDB(t)
	var accountID int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		result := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteAccount{}).
			Where("id = ? AND credential_revision = ?", accountID, 0).
			Updates(map[string]any{
				"access_token":        "newer-access-token",
				"refresh_token":       "newer-refresh-token",
				"token_expires_at":    time.Now().Add(time.Hour).UnixMilli(),
				"credential_revision": 1,
			})
		if result.Error != nil || result.RowsAffected != 1 {
			t.Errorf("install newer credentials: rows=%d err=%v", result.RowsAffected, result.Error)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"access_token":"stale-response-token","refresh_token":"stale-response-refresh","expires_in":3600}}`))
	}))
	defer server.Close()

	siteRecord := model.Site{Name: "sub2-cas", Platform: model.SitePlatformSub2API, BaseURL: server.URL, Enabled: true}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&siteRecord).Error; err != nil {
		t.Fatalf("create site: %v", err)
	}
	persisted := model.SiteAccount{
		SiteID:             siteRecord.ID,
		Name:               "sub2-account",
		CredentialType:     model.SiteCredentialTypeAccessToken,
		AccessToken:        "old-access-token",
		RefreshToken:       "old-refresh-token",
		TokenExpiresAt:     time.Now().UnixMilli(),
		Enabled:            true,
		AutoSync:           true,
		CredentialRevision: 0,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&persisted).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}
	accountID = persisted.ID

	callerCopy := persisted
	token, err := ensureFreshSub2APIAccessToken(ctx, &siteRecord, &callerCopy, true)
	if err != nil {
		t.Fatalf("refresh with CAS returned error: %v", err)
	}
	if token != "newer-access-token" || callerCopy.RefreshToken != "newer-refresh-token" || callerCopy.CredentialRevision != 1 {
		t.Fatalf("expected newer credentials to win CAS, got token=%q account=%+v", token, callerCopy)
	}
	var reloaded model.SiteAccount
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloaded, persisted.ID).Error; err != nil {
		t.Fatalf("reload account: %v", err)
	}
	if reloaded.AccessToken != "newer-access-token" || reloaded.RefreshToken != "newer-refresh-token" {
		t.Fatalf("stale refresh overwrote newer credentials: %+v", reloaded)
	}
}

func TestRefreshDueSub2APIAccountsBoundsConcurrencyAndIsolatesFailure(t *testing.T) {
	ctx := setupProjectTestDB(t)
	var calls atomic.Int32
	var active atomic.Int32
	var maxActive atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		current := active.Add(1)
		defer active.Add(-1)
		for {
			observed := maxActive.Load()
			if current <= observed || maxActive.CompareAndSwap(observed, current) {
				break
			}
		}
		var body struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode refresh request: %v", err)
		}
		time.Sleep(15 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		if body.RefreshToken == "refresh-fail" {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"message":"temporary failure"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"access_token":"fresh-token","expires_in":3600}}`))
	}))
	defer server.Close()

	siteRecord := model.Site{Name: "scheduled-sub2", Platform: model.SitePlatformSub2API, BaseURL: server.URL, Enabled: true}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&siteRecord).Error; err != nil {
		t.Fatalf("create site: %v", err)
	}
	accounts := make([]model.SiteAccount, 6)
	for index := range accounts {
		refreshToken := "refresh-ok"
		if index == 0 {
			refreshToken = "refresh-fail"
		}
		accounts[index] = model.SiteAccount{
			SiteID:         siteRecord.ID,
			Name:           fmt.Sprintf("scheduled-%d", index),
			CredentialType: model.SiteCredentialTypeAccessToken,
			AccessToken:    "stale-token",
			RefreshToken:   refreshToken,
			TokenExpiresAt: time.Now().UnixMilli(),
			Enabled:        true,
			AutoSync:       true,
		}
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&accounts).Error; err != nil {
		t.Fatalf("create accounts: %v", err)
	}

	summary, err := RefreshDueSub2APIAccounts(ctx, 2)
	if err != nil {
		t.Fatalf("RefreshDueSub2APIAccounts: %v", err)
	}
	if summary.Selected != 6 || summary.Succeeded != 5 || summary.Failed != 1 {
		t.Fatalf("unexpected refresh summary: %+v", summary)
	}
	if calls.Load() != 7 || maxActive.Load() > 2 {
		t.Fatalf("unexpected scheduler concurrency: calls=%d max=%d", calls.Load(), maxActive.Load())
	}
	var failed model.SiteAccount
	if err := dbpkg.GetDB().WithContext(ctx).Where("name = ?", "scheduled-0").First(&failed).Error; err != nil {
		t.Fatalf("reload failed account: %v", err)
	}
	if failed.LastAuthFailureClass != "upstream_5xx" || failed.RefreshToken != "refresh-fail" {
		t.Fatalf("failure was not isolated and classified: %+v", failed)
	}
}

func TestRefreshDueSub2APIAccountsReturnsCancellation(t *testing.T) {
	ctx := setupProjectTestDB(t)
	canceled, cancel := context.WithCancel(ctx)
	cancel()

	_, err := RefreshDueSub2APIAccounts(canceled, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestRecordSub2APIRefreshOutcomePersistsCancellation(t *testing.T) {
	ctx := setupProjectTestDB(t)
	_, account := createProjectionFixture(t, ctx)
	canceled, cancel := context.WithCancel(ctx)
	cancel()

	recordSub2APIRefreshOutcome(canceled, account.ID, context.Canceled)

	var reloaded model.SiteAccount
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloaded, account.ID).Error; err != nil {
		t.Fatalf("reload account: %v", err)
	}
	if reloaded.LastAuthFailureClass != "transport_canceled" || reloaded.LastAuthFailureAt == nil {
		t.Fatalf("canceled refresh outcome was not persisted: %+v", reloaded)
	}
}

func TestRefreshSub2APICandidatesReturnsRunningCancellation(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseResponse := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(requestStarted)
		<-releaseResponse
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		summary, err := refreshSub2APICandidates(ctx, []sub2APIRefreshCandidate{{
			site: &model.Site{BaseURL: server.URL},
			account: &model.SiteAccount{
				AccessToken:    "stale-token",
				RefreshToken:   "refresh-token",
				TokenExpiresAt: time.Now().UnixMilli(),
			},
		}}, 1)
		if summary.Selected != 1 {
			result <- fmt.Errorf("unexpected canceled summary: %+v", summary)
			return
		}
		result <- err
	}()
	<-requestStarted
	cancel()
	close(releaseResponse)
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected running cancellation, got %v", err)
	}
}

func TestSub2APIRefreshDueAtIsDeterministicAndSpread(t *testing.T) {
	expiresAt := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	first := &model.SiteAccount{ID: 11, TokenExpiresAt: expiresAt.UnixMilli()}
	second := &model.SiteAccount{ID: 12, TokenExpiresAt: expiresAt.UnixMilli()}
	firstDue := sub2APIRefreshDueAt(first)
	if repeated := sub2APIRefreshDueAt(first); !repeated.Equal(firstDue) {
		t.Fatalf("same account produced unstable due times: %s != %s", firstDue, repeated)
	}
	if secondDue := sub2APIRefreshDueAt(second); secondDue.Equal(firstDue) {
		t.Fatalf("different accounts were not spread: %s", firstDue)
	}
	windowStart := expiresAt.Add(-sub2APIAccessTokenRefreshLead)
	windowEnd := windowStart.Add(sub2APIAccessTokenSpreadWindow)
	if firstDue.Before(windowStart) || !firstDue.Before(windowEnd) {
		t.Fatalf("due time %s outside [%s, %s)", firstDue, windowStart, windowEnd)
	}
}

func TestRefreshDueSub2APIAccountsSkipsOverlappingPass(t *testing.T) {
	sub2APIRefreshPassRunning.Store(true)
	t.Cleanup(func() { sub2APIRefreshPassRunning.Store(false) })
	summary, err := RefreshDueSub2APIAccounts(context.Background(), 1)
	if err != nil || summary != (Sub2APIRefreshSummary{}) {
		t.Fatalf("overlapping pass was not skipped: summary=%+v err=%v", summary, err)
	}
}

func TestSyncSub2APIUsesManagedKeyAndAPIModelEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/v1/keys":
			if r.Header.Get("Authorization") != "Bearer sub2-session-token" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"message":"unauthorized"}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"items":[{"id":11,"name":"managed-key","key":"sub2-user-key","group_id":7,"group_name":"VIP 7","group":{"id":7,"name":"vip","rate_multiplier":0.2},"enabled":true}]}}`))
		case "/api/v1/groups/available":
			if r.Header.Get("Authorization") != "Bearer sub2-session-token" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"message":"unauthorized"}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"groups":[{"id":7,"name":"vip","rate_multiplier":0.2}]}}`))
		case "/v1/models":
			http.NotFound(w, r)
		case "/api/v1/models":
			if r.Header.Get("Authorization") != "Bearer sub2-user-key" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"items":[{"id":"gpt-4o-mini"},{"name":"claude-3-5-sonnet"}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	snapshot, err := syncSub2API(context.Background(), &model.Site{
		BaseURL:  server.URL,
		Platform: model.SitePlatformSub2API,
	}, &model.SiteAccount{
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "Bearer sub2-session-token",
	})
	if err != nil {
		t.Fatalf("syncSub2API returned error: %v", err)
	}
	if len(snapshot.tokens) != 1 {
		t.Fatalf("expected one managed token, got %+v", snapshot.tokens)
	}
	if snapshot.tokens[0].Token != "sub2-user-key" || snapshot.tokens[0].GroupKey != "7" {
		t.Fatalf("expected managed token with group 7, got %+v", snapshot.tokens[0])
	}
	if len(snapshot.groups) != 1 || snapshot.groups[0].GroupKey != "7" || snapshot.groups[0].Name != "vip" {
		t.Fatalf("expected parsed group 7/vip, got %+v", snapshot.groups)
	}
	if snapshot.groups[0].Multiplier == nil || *snapshot.groups[0].Multiplier != 0.2 {
		t.Fatalf("expected key group multiplier 0.2, got %+v", snapshot.groups[0])
	}
	if len(snapshot.models) != 2 {
		t.Fatalf("expected models discovered from /api/v1/models, got %+v", snapshot.models)
	}
}

func TestSyncSub2APIRequiresRealAPIKeyWhenKeyListIsEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/v1/keys", "/api/v1/api-keys":
			_, _ = w.Write([]byte(`{"code":0,"data":[]}`))
		case "/api/v1/groups/available", "/api/v1/groups", "/api/v1/group":
			_, _ = w.Write([]byte(`{"code":0,"data":[{"id":7,"name":"vip"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := syncSub2API(context.Background(), &model.Site{
		BaseURL:  server.URL,
		Platform: model.SitePlatformSub2API,
	}, &model.SiteAccount{
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "sub2-session-token",
	})
	if err == nil {
		t.Fatalf("expected syncSub2API to require an API key")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "api key") {
		t.Fatalf("expected API key error, got %v", err)
	}
}

func TestFetchSub2APITokensReturnsEnvelopeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":401,"message":"token expired","data":null}`))
	}))
	defer server.Close()

	_, err := fetchSub2APITokens(context.Background(), &model.Site{BaseURL: server.URL}, &model.SiteAccount{}, "expired-token")
	if err == nil {
		t.Fatalf("expected envelope error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "expired") {
		t.Fatalf("expected token expired error, got %v", err)
	}
}

func TestBuildSub2APITokensCapturesNestedGroupMultiplier(t *testing.T) {
	tokens := buildSub2APITokensFromItems([]map[string]any{
		{
			"id":       1.0,
			"name":     "managed-key",
			"key":      "sub2-user-key",
			"group_id": 11.0,
			"group": map[string]any{
				"id":              11.0,
				"name":            "GPT",
				"rate_multiplier": 0.05,
			},
		},
	})

	if len(tokens) != 1 {
		t.Fatalf("expected one token, got %+v", tokens)
	}
	if tokens[0].GroupMultiplier == nil || *tokens[0].GroupMultiplier != 0.05 {
		t.Fatalf("nested key group multiplier was not captured: %+v", tokens[0])
	}
	groups := mergeSiteGroups(nil, tokens)
	if len(groups) != 1 || groups[0].Multiplier == nil || *groups[0].Multiplier != 0.05 {
		t.Fatalf("key multiplier was not projected to its group: %+v", groups)
	}
}

func TestBuildSub2APIModelEndpointURLsIncludesAntigravityV1(t *testing.T) {
	endpoints := buildSub2APIModelEndpointURLs(&model.Site{BaseURL: "https://example.com"})
	for _, endpoint := range endpoints {
		if endpoint == "https://example.com/antigravity/v1/models" {
			return
		}
	}
	t.Fatalf("expected antigravity v1 models endpoint, got %+v", endpoints)
}

func TestParseSub2APIModelNamesReturnsEnvelopeError(t *testing.T) {
	_, err := parseSub2APIModelNames(map[string]any{
		"code":    float64(401),
		"message": "expired key",
	})
	if err == nil {
		t.Fatalf("expected envelope error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "expired") {
		t.Fatalf("expected expired key error, got %v", err)
	}
}
