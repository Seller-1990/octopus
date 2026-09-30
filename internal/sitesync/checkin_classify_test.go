package sitesync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

// 签到结果分类回归（v1.8.7 用户反馈 + ocr 复审裁决）：
// - 站点关闭签到（固定文案「签到功能未启用」）→ skipped 中性，不计失败
// - 404/405（站点无可用签到接口）→ skipped
// - 顺序固化：success / 已签到 → 优先于 disabled；非白名单业务失败仍 failed
func TestCheckinAccountStateClassification(t *testing.T) {
	ctx := setupProjectTestDB(t)
	if err := op.SettingRefreshCache(ctx); err != nil {
		t.Fatalf("refresh setting cache: %v", err)
	}

	const accessToken = "classify-access-token"
	newSiteWithAccount := func(t *testing.T, name string) (model.Site, model.SiteAccount) {
		t.Helper()
		site := model.Site{
			Name:     name,
			Platform: model.SitePlatformNewAPI,
			BaseURL:  "https://classify.invalid",
			Enabled:  true,
		}
		if err := dbpkg.GetDB().WithContext(ctx).Create(&site).Error; err != nil {
			t.Fatalf("create site: %v", err)
		}
		account := model.SiteAccount{
			SiteID:         site.ID,
			Name:           name + "-account",
			CredentialType: model.SiteCredentialTypeAccessToken,
			AccessToken:    accessToken,
			Enabled:        true,
		}
		if err := dbpkg.GetDB().WithContext(ctx).Create(&account).Error; err != nil {
			t.Fatalf("create account: %v", err)
		}
		return site, account
	}

	runAgainst := func(t *testing.T, site *model.Site, account *model.SiteAccount, handler http.HandlerFunc) (*model.SiteCheckinResult, error) {
		t.Helper()
		server := httptest.NewServer(handler)
		defer server.Close()
		site.BaseURL = server.URL
		result, _, err := checkinAccountState(context.Background(), site, account)
		return result, err
	}

	t.Run("disabled message maps to skipped", func(t *testing.T) {
		site, account := newSiteWithAccount(t, "classify-disabled")
		result, err := runAgainst(t, &site, &account, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":false,"message":"签到功能未启用"}`))
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Status != model.SiteExecutionStatusSkipped {
			t.Fatalf("disabled checkin should be skipped, got %s (%s)", result.Status, result.Message)
		}
	})

	t.Run("already checked in still success before disabled", func(t *testing.T) {
		site, account := newSiteWithAccount(t, "classify-order")
		result, err := runAgainst(t, &site, &account, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":false,"message":"今日已签到"}`))
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Status != model.SiteExecutionStatusSuccess {
			t.Fatalf("already-checked-in must stay success, got %s", result.Status)
		}
	})

	t.Run("non-whitelist business failure stays failed", func(t *testing.T) {
		site, account := newSiteWithAccount(t, "classify-failed")
		result, err := runAgainst(t, &site, &account, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":false,"message":"账号已禁用"}`))
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Status != model.SiteExecutionStatusFailed {
			t.Fatalf("non-whitelist failure must stay failed, got %s", result.Status)
		}
	})

	t.Run("405 maps to skipped", func(t *testing.T) {
		site, account := newSiteWithAccount(t, "classify-405")
		result, err := runAgainst(t, &site, &account, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = w.Write([]byte(`{"message":"method not allowed"}`))
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Status != model.SiteExecutionStatusSkipped {
			t.Fatalf("405 should be skipped, got %s (%s)", result.Status, result.Message)
		}
	})

	t.Run("404 stays skipped", func(t *testing.T) {
		site, account := newSiteWithAccount(t, "classify-404")
		result, err := runAgainst(t, &site, &account, func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Status != model.SiteExecutionStatusSkipped {
			t.Fatalf("404 should stay skipped, got %s", result.Status)
		}
	})

	t.Run("whitelist is exact not substring", func(t *testing.T) {
		if !isCheckinDisabledMessage("  签到功能未启用  ") {
			t.Fatal("trimmed exact message should match")
		}
		if isCheckinDisabledMessage("账号已禁用") {
			t.Fatal("account-disabled must not match")
		}
		if isCheckinDisabledMessage("今日已签到后签到功能未启用提示") {
			t.Fatal("substring must not match (exact whitelist)")
		}
		if isCheckinDisabledMessage("") {
			t.Fatal("empty must not match")
		}
	})
}
