package sitesync

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

// 同步成功后必须对账签到状态：今天的签到存在失败记录时，同步成功即补一次
// 签到（F：同步完成但签到状态整天异常）。补签经 checkinAccount 正常写回
// last_checkin_*；签到已成功或从未签过（idle）的账号不触发补签。
func TestSyncAccountReconcilesFailedCheckin(t *testing.T) {
	ctx := setupProjectTestDB(t)
	if err := op.SettingRefreshCache(ctx); err != nil {
		t.Fatalf("refresh setting cache: %v", err)
	}

	const accessToken = "reconcile-access-token"
	checkinCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-reconcile"}]}`))
		case "/external-checkin":
			checkinCalls++
			_, _ = w.Write([]byte(`{"success":true,"message":"checkin reconciled"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site := model.Site{
		Name:     "Reconcile Site",
		Platform: model.SitePlatformAPI,
		BaseURL:  server.URL,
		Enabled:  true,
	}
	externalCheckinURL := server.URL + "/external-checkin"
	site.ExternalCheckinURL = &externalCheckinURL
	if err := op.SiteCreate(&site, ctx); err != nil {
		t.Fatalf("create site: %v", err)
	}

	newAccount := func(name string) model.SiteAccount {
		return model.SiteAccount{
			SiteID:         site.ID,
			Name:           name,
			CredentialType: model.SiteCredentialTypeAccessToken,
			AccessToken:    accessToken,
			Enabled:        true,
			AutoSync:       true,
			AutoCheckin:    true,
		}
	}

	// 场景 1：上次签到失败 → 同步成功后补签，签到状态翻转为 success。
	failedAccount := newAccount("reconcile-failed")
	if err := op.SiteAccountCreate(&failedAccount, ctx); err != nil {
		t.Fatalf("create failed account: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteAccount{}).
		Where("id = ?", failedAccount.ID).
		Update("last_checkin_status", model.SiteExecutionStatusFailed).Error; err != nil {
		t.Fatalf("mark failed checkin: %v", err)
	}

	if _, err := SyncAccount(ctx, failedAccount.ID); err != nil {
		t.Fatalf("SyncAccount: %v", err)
	}
	if checkinCalls != 1 {
		t.Fatalf("expected one post-sync checkin, got %d", checkinCalls)
	}
	var reloaded model.SiteAccount
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloaded, failedAccount.ID).Error; err != nil {
		t.Fatalf("reload failed account: %v", err)
	}
	if reloaded.LastCheckinStatus != model.SiteExecutionStatusSuccess {
		t.Fatalf("last_checkin_status = %q, want success after reconcile", reloaded.LastCheckinStatus)
	}

	// 场景 2：签到已成功（或从未签过）→ 同步不触发补签。
	okAccount := newAccount("reconcile-ok")
	if err := op.SiteAccountCreate(&okAccount, ctx); err != nil {
		t.Fatalf("create ok account: %v", err)
	}
	if _, err := SyncAccount(ctx, okAccount.ID); err != nil {
		t.Fatalf("SyncAccount (ok): %v", err)
	}
	if checkinCalls != 1 {
		t.Fatalf("unexpected extra checkin for non-failed account, got %d", checkinCalls)
	}
}

// 补签门禁回归：最小间隔、连续失败上限、陈旧快照下的并发成功、当日成功
// 不被迟到失败覆写（对抗审查 R1 修复项）。
func TestSyncAccountReconcileGates(t *testing.T) {
	ctx := setupProjectTestDB(t)
	if err := op.SettingRefreshCache(ctx); err != nil {
		t.Fatalf("refresh setting cache: %v", err)
	}

	const accessToken = "reconcile-gate-token"
	checkinCalls := 0
	// 并发成功注入：置为账号 ID 后，下一次同步读模型时该账号被标记为
	// 当日已成功——模拟「同步进行到一半时用户手动签到成功」。
	var concurrentSuccessID atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			if id := concurrentSuccessID.Load(); id != 0 {
				now := time.Now()
				if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteAccount{}).
					Where("id = ?", id).
					Updates(map[string]any{
						"last_checkin_status": model.SiteExecutionStatusSuccess,
						"last_checkin_at":     &now,
					}).Error; err != nil {
					t.Errorf("inject concurrent success: %v", err)
				}
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-gate"}]}`))
		case "/external-checkin":
			checkinCalls++
			_, _ = w.Write([]byte(`{"success":true,"message":"gate checkin"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site := model.Site{
		Name:     "Reconcile Gate Site",
		Platform: model.SitePlatformAPI,
		BaseURL:  server.URL,
		Enabled:  true,
	}
	externalCheckinURL := server.URL + "/external-checkin"
	site.ExternalCheckinURL = &externalCheckinURL
	if err := op.SiteCreate(&site, ctx); err != nil {
		t.Fatalf("create site: %v", err)
	}

	markCheckin := func(t *testing.T, accountID int, status model.SiteExecutionStatus, lastAt time.Time, streak int) {
		t.Helper()
		if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteAccount{}).
			Where("id = ?", accountID).
			Updates(map[string]any{
				"last_checkin_status": status,
				"last_checkin_at":     &lastAt,
				"checkin_fail_streak": streak,
			}).Error; err != nil {
			t.Fatalf("mark checkin state: %v", err)
		}
	}

	newAccount := func(name string) model.SiteAccount {
		account := model.SiteAccount{
			SiteID:         site.ID,
			Name:           name,
			CredentialType: model.SiteCredentialTypeAccessToken,
			AccessToken:    accessToken,
			Enabled:        true,
			AutoSync:       true,
			AutoCheckin:    true,
		}
		if err := op.SiteAccountCreate(&account, ctx); err != nil {
			t.Fatalf("create account %s: %v", name, err)
		}
		return account
	}

	// 场景 A：失败发生在 10 分钟前（< checkinReconcileMinInterval）→ 不补签。
	recentFailure := newAccount("gate-recent")
	markCheckin(t, recentFailure.ID, model.SiteExecutionStatusFailed, time.Now().Add(-10*time.Minute), 1)
	if _, err := SyncAccount(ctx, recentFailure.ID); err != nil {
		t.Fatalf("SyncAccount (recent): %v", err)
	}
	if checkinCalls != 0 {
		t.Fatalf("reconcile must respect the min interval, got %d checkin calls", checkinCalls)
	}

	// 场景 B：连续失败达上限 → 不补签（交还正常退避调度）。
	cappedFailure := newAccount("gate-capped")
	markCheckin(t, cappedFailure.ID, model.SiteExecutionStatusFailed, time.Now().Add(-2*time.Hour), checkinReconcileMaxFailStreak)
	if _, err := SyncAccount(ctx, cappedFailure.ID); err != nil {
		t.Fatalf("SyncAccount (capped): %v", err)
	}
	if checkinCalls != 0 {
		t.Fatalf("reconcile must stop after the fail-streak cap, got %d checkin calls", checkinCalls)
	}

	// 场景 C：间隔充足且未达上限 → 补签。
	eligible := newAccount("gate-eligible")
	markCheckin(t, eligible.ID, model.SiteExecutionStatusFailed, time.Now().Add(-2*time.Hour), 1)
	if _, err := SyncAccount(ctx, eligible.ID); err != nil {
		t.Fatalf("SyncAccount (eligible): %v", err)
	}
	if checkinCalls != 1 {
		t.Fatalf("expected one reconcile checkin, got %d", checkinCalls)
	}

	// 场景 D：同步期间账号被并发手动签到成功 → 重读门禁看到当日成功，
	// 不再补签（同步开始时的快照仍显示 failed）。
	concurrentSuccess := newAccount("gate-concurrent")
	markCheckin(t, concurrentSuccess.ID, model.SiteExecutionStatusFailed, time.Now().Add(-2*time.Hour), 0)
	concurrentSuccessID.Store(int64(concurrentSuccess.ID))
	if _, err := SyncAccount(ctx, concurrentSuccess.ID); err != nil {
		t.Fatalf("SyncAccount (concurrent): %v", err)
	}
	concurrentSuccessID.Store(0)
	if checkinCalls != 1 {
		t.Fatalf("reconcile must skip when a concurrent same-day success exists, got %d checkin calls", checkinCalls)
	}
	var concurrentReloaded model.SiteAccount
	if err := dbpkg.GetDB().WithContext(ctx).First(&concurrentReloaded, concurrentSuccess.ID).Error; err != nil {
		t.Fatalf("reload concurrent account: %v", err)
	}
	if concurrentReloaded.LastCheckinStatus != model.SiteExecutionStatusSuccess {
		t.Fatalf("concurrent same-day success must survive the sync: %q", concurrentReloaded.LastCheckinStatus)
	}
}

// 当日成功写回护栏：迟到的失败结果不得把当日 success 覆写为 failed。
func TestUpdateAccountCheckinStateProtectsSameDaySuccess(t *testing.T) {
	ctx := setupProjectTestDB(t)
	site := model.Site{Name: "guard-site", Platform: model.SitePlatformAPI, BaseURL: "https://site.example"}
	if err := op.SiteCreate(&site, ctx); err != nil {
		t.Fatalf("create site: %v", err)
	}
	account := model.SiteAccount{
		SiteID:         site.ID,
		Name:           "guard-account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "guard-token",
		Enabled:        true,
		AutoCheckin:    true,
	}
	if err := op.SiteAccountCreate(&account, ctx); err != nil {
		t.Fatalf("create account: %v", err)
	}
	now := time.Now()
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteAccount{}).
		Where("id = ?", account.ID).
		Updates(map[string]any{
			"last_checkin_status": model.SiteExecutionStatusSuccess,
			"last_checkin_at":     &now,
		}).Error; err != nil {
		t.Fatalf("seed same-day success: %v", err)
	}

	stale := account
	stale.LastCheckinStatus = model.SiteExecutionStatusFailed
	stale.LastCheckinAt = nil
	stale.CheckinFailStreak = 2
	if err := updateAccountCheckinState(ctx, &stale, model.SiteExecutionStatusFailed, "late timeout", false, ""); err != nil {
		t.Fatalf("late failure write: %v", err)
	}

	var reloaded model.SiteAccount
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloaded, account.ID).Error; err != nil {
		t.Fatalf("reload account: %v", err)
	}
	if reloaded.LastCheckinStatus != model.SiteExecutionStatusSuccess {
		t.Fatalf("same-day success was overwritten by a late failure: %q", reloaded.LastCheckinStatus)
	}

	// 隔日失败不受护栏限制（正常更新）。
	yesterday := now.Add(-24 * time.Hour)
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteAccount{}).
		Where("id = ?", account.ID).
		Update("last_checkin_at", &yesterday).Error; err != nil {
		t.Fatalf("backdate checkin: %v", err)
	}
	stale.LastCheckinStatus = model.SiteExecutionStatusFailed
	if err := updateAccountCheckinState(ctx, &stale, model.SiteExecutionStatusFailed, "today failure", false, ""); err != nil {
		t.Fatalf("next-day failure write: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloaded, account.ID).Error; err != nil {
		t.Fatalf("reload account: %v", err)
	}
	if reloaded.LastCheckinStatus != model.SiteExecutionStatusFailed {
		t.Fatalf("failure on a non-success day must be recorded: %q", reloaded.LastCheckinStatus)
	}
}
