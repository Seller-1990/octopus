package sitesync

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
