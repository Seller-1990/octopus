package op

import (
	"context"
	"path/filepath"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

// setupUserOpTestDB 为 op/user 测试初始化独立 sqlite 库。
func setupUserOpTestDB(t *testing.T) context.Context {
	t.Helper()
	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}
	dbPath := filepath.Join(t.TempDir(), "octopus-user-test.db")
	if err := dbpkg.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	clearSiteChannelBindingCache()
	clearHeaderPolicyCache()
	t.Cleanup(func() {
		_ = dbpkg.Close()
	})
	return context.Background()
}

// TestTokenVersionBumpsOnCredentialChange 验证 P0-1 核心语义：
// 改密/改名在单次 DB 更新中原子递增 token 版本，版本与密码/用户名同库生效；
// 失败路径（旧密码错误、同名改名）不递增版本。
func TestTokenVersionBumpsOnCredentialChange(t *testing.T) {
	_ = setupUserOpTestDB(t)

	user := model.User{Username: "admin", Password: "oldpass123", TokenVersion: 0}
	if err := user.HashPassword(); err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := dbpkg.GetDB().Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	prev := userCache
	userCacheMu.Lock()
	userCache = user
	userCacheMu.Unlock()
	t.Cleanup(func() {
		userCacheMu.Lock()
		userCache = prev
		userCacheMu.Unlock()
	})

	if v := UserTokenVersion(); v != 0 {
		t.Fatalf("initial version = %d, want 0", v)
	}

	// 旧密码错误 → 不递增
	if err := UserChangePassword("wrongpass", "newpass456"); err == nil {
		t.Fatal("expected error for wrong old password")
	}
	if v := UserTokenVersion(); v != 0 {
		t.Fatalf("version bumped on failed password change: %d", v)
	}

	// 正常改密 → 版本 +1，且密码已换
	if err := UserChangePassword("oldpass123", "newpass456"); err != nil {
		t.Fatalf("change password: %v", err)
	}
	if v := UserTokenVersion(); v != 1 {
		t.Fatalf("version after password change = %d, want 1", v)
	}
	if err := UserVerify("admin", "newpass456"); err != nil {
		t.Fatalf("new password should verify: %v", err)
	}
	if err := UserVerify("admin", "oldpass123"); err == nil {
		t.Fatal("old password should no longer verify")
	}
	var row model.User
	if err := dbpkg.GetDB().First(&row).Error; err != nil {
		t.Fatalf("load user row: %v", err)
	}
	if row.TokenVersion != 1 {
		t.Fatalf("DB token_version = %d, want 1", row.TokenVersion)
	}

	// 改同名 → 报错且不递增
	if err := UserChangeUsername("admin"); err == nil {
		t.Fatal("expected error for unchanged username")
	}
	if v := UserTokenVersion(); v != 1 {
		t.Fatalf("version bumped on no-op username change: %d", v)
	}

	// 正常改名 → 版本 +1，DB 同步
	if err := UserChangeUsername("admin2"); err != nil {
		t.Fatalf("change username: %v", err)
	}
	if v := UserTokenVersion(); v != 2 {
		t.Fatalf("version after username change = %d, want 2", v)
	}
	if err := dbpkg.GetDB().First(&row).Error; err != nil {
		t.Fatalf("reload user row: %v", err)
	}
	if row.TokenVersion != 2 || row.Username != "admin2" {
		t.Fatalf("DB row = %+v, want username admin2 version 2", row)
	}
	if err := UserVerify("admin2", "newpass456"); err != nil {
		t.Fatalf("verify after username change: %v", err)
	}
}
