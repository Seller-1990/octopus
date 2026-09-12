package op

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func setupPairingRotateTestDB(t *testing.T) context.Context {
	t.Helper()
	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}
	dbPath := filepath.Join(t.TempDir(), "octopus-pairing-rotate-test.db")
	if err := dbpkg.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() {
		_ = dbpkg.Close()
	})
	return context.Background()
}

// 一键同步的配对必须跨点击复用同一 pairing 记录并轮换令牌:
// id 稳定使已信任扩展可以自动重连,令牌轮换保证旧 fragment 立即失效。
func TestVerificationBridgePairingEnsureRotatedReusesPairing(t *testing.T) {
	ctx := setupPairingRotateTestDB(t)
	site := model.Site{Name: "rotate-site", Platform: "linuxdo", BaseURL: "https://site.example"}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&site).Error; err != nil {
		t.Fatalf("create site: %v", err)
	}
	account := model.SiteAccount{
		SiteID:         site.ID,
		Name:           "acc",
		CredentialType: model.SiteCredentialTypeCookie,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}

	first, err := VerificationBridgePairingEnsureRotated(ctx, "一键同步", account.ID)
	if err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	second, err := VerificationBridgePairingEnsureRotated(ctx, "一键同步", account.ID)
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	if first.Token == "" || second.Token == "" {
		t.Fatal("ensure must return a plaintext token")
	}
	if first.Token == second.Token {
		t.Fatal("second ensure must rotate the token")
	}
	if first.Pairing.ID != second.Pairing.ID {
		t.Fatalf("pairing id must stay stable across clicks: %d != %d", first.Pairing.ID, second.Pairing.ID)
	}
	var count int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.VerificationBridgePairing{}).
		Where("site_account_id = ?", account.ID).Count(&count).Error; err != nil {
		t.Fatalf("count pairings: %v", err)
	}
	if count != 1 {
		t.Fatalf("repeated browser sync must not pile up orphan pairings, got %d", count)
	}
}

// 撤销后再次一键同步应创建新配对,而不是复用已撤销记录。
func TestVerificationBridgePairingEnsureRotatedCreatesAfterRevoke(t *testing.T) {
	ctx := setupPairingRotateTestDB(t)
	site := model.Site{Name: "rotate-site-2", Platform: "linuxdo", BaseURL: "https://site.example"}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&site).Error; err != nil {
		t.Fatalf("create site: %v", err)
	}
	account := model.SiteAccount{
		SiteID:         site.ID,
		Name:           "acc-2",
		CredentialType: model.SiteCredentialTypeCookie,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}

	first, err := VerificationBridgePairingEnsureRotated(ctx, "一键同步", account.ID)
	if err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if err := VerificationBridgePairingRevoke(ctx, first.Pairing.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	second, err := VerificationBridgePairingEnsureRotated(ctx, "一键同步", account.ID)
	if err != nil {
		t.Fatalf("ensure after revoke: %v", err)
	}
	if second.Pairing.ID == first.Pairing.ID {
		t.Fatal("must not reuse a revoked pairing")
	}
}

// 查询失败不得被解释为「无已有配对」而静默新建：读取错误必须原样上抛，
// 否则重复触发会堆积多条有效配对并破坏固定 pairing id 契约（F04）。
func TestVerificationBridgePairingEnsureRotatedLookupErrorDoesNotCreate(t *testing.T) {
	ctx := setupPairingRotateTestDB(t)
	site := model.Site{Name: "rotate-site-3", Platform: "linuxdo", BaseURL: "https://site.example"}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&site).Error; err != nil {
		t.Fatalf("create site: %v", err)
	}
	account := model.SiteAccount{
		SiteID:         site.ID,
		Name:           "acc-3",
		CredentialType: model.SiteCredentialTypeCookie,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}

	first, err := VerificationBridgePairingEnsureRotated(ctx, "一键同步", account.ID)
	if err != nil {
		t.Fatalf("first ensure: %v", err)
	}

	// 一次性注入读取错误：下一次配对查询失败。
	var inject atomic.Bool
	inject.Store(true)
	callbackName := "test:pairing-lookup-error"
	if err := dbpkg.GetDB().Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*model.VerificationBridgePairing); !ok {
			return
		}
		if !inject.CompareAndSwap(true, false) {
			return
		}
		_ = tx.AddError(errors.New("injected pairing lookup failure"))
	}); err != nil {
		t.Fatalf("register lookup error callback: %v", err)
	}
	t.Cleanup(func() {
		_ = dbpkg.GetDB().Callback().Query().Remove(callbackName)
	})

	if _, err := VerificationBridgePairingEnsureRotated(ctx, "一键同步", account.ID); err == nil {
		t.Fatal("ensure must fail when the lookup errors, not fall through to creation")
	}

	var count int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.VerificationBridgePairing{}).
		Where("site_account_id = ?", account.ID).Count(&count).Error; err != nil {
		t.Fatalf("count pairings: %v", err)
	}
	if count != 1 {
		t.Fatalf("lookup failure must not create a duplicate pairing, got %d", count)
	}

	// 错误注入只命中一次：之后 ensure 正常轮换既有配对，id 保持稳定。
	retried, err := VerificationBridgePairingEnsureRotated(ctx, "一键同步", account.ID)
	if err != nil {
		t.Fatalf("ensure after transient lookup failure: %v", err)
	}
	if retried.Pairing.ID != first.Pairing.ID {
		t.Fatal("pairing id must stay stable after a transient lookup failure")
	}
}
