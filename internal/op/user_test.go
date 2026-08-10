package op

import (
	"testing"

	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

// setupUserTest 复用 op 包 DB 基建（独立 sqlite，测试后关闭）。
// userCache 是包级变量，测试间需复位——否则残留的 ID=1/admin 会让后续
// First 因主键命中而错误跳过 bootstrap（P2 测试隔离）。
func setupUserTest(t *testing.T) {
	t.Helper()
	_ = setupCatalogProvisionTest(t) // 独立 DB + channel cache reset
	t.Cleanup(func() { userCache = model.User{} })
}

// TestUserInitRequiresBootstrapPassword F01 回归：空库无 bootstrap 密码必须拒绝启动（不创建固定 admin/admin）。
func TestUserInitRequiresBootstrapPassword(t *testing.T) {
	setupUserTest(t)
	oldPwd := conf.AppConfig.Bootstrap.Password
	conf.AppConfig.Bootstrap.Password = ""
	t.Cleanup(func() { conf.AppConfig.Bootstrap.Password = oldPwd })

	if err := UserInit(); err == nil {
		t.Fatal("UserInit must fail when bootstrap password is not set (no fixed admin/admin)")
	}
	var count int64
	if err := db.GetDB().Model(&model.User{}).Count(&count).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 0 {
		t.Fatalf("no user should be created without bootstrap password, got %d", count)
	}
}

// TestUserInitCreatesAdminWithBootstrapPassword F01 回归：有 bootstrap 密码 → 创建 admin 且密码可验证。
func TestUserInitCreatesAdminWithBootstrapPassword(t *testing.T) {
	setupUserTest(t)
	oldPwd := conf.AppConfig.Bootstrap.Password
	conf.AppConfig.Bootstrap.Password = "test-strong-pass-123"
	t.Cleanup(func() { conf.AppConfig.Bootstrap.Password = oldPwd })

	if err := UserInit(); err != nil {
		t.Fatalf("UserInit with bootstrap password should succeed: %v", err)
	}
	u := UserGet()
	if u.Username != "admin" {
		t.Fatalf("expected admin username, got %s", u.Username)
	}
	if err := UserVerify("admin", "test-strong-pass-123"); err != nil {
		t.Fatalf("bootstrap password must be verifiable: %v", err)
	}
	if err := UserVerify("admin", "wrong"); err == nil {
		t.Fatal("wrong password must fail verification")
	}
}

// TestUserInitRejectsShortBootstrapPassword P1 修复：bootstrap 密码最小 6 位。
func TestUserInitRejectsShortBootstrapPassword(t *testing.T) {
	setupUserTest(t)
	oldPwd := conf.AppConfig.Bootstrap.Password
	conf.AppConfig.Bootstrap.Password = "12345"
	t.Cleanup(func() { conf.AppConfig.Bootstrap.Password = oldPwd })

	if err := UserInit(); err == nil {
		t.Fatal("short bootstrap password must be rejected")
	}
}

// TestUserInitSkipsExistingUser F01 回归：旧库（已有用户）不受影响，不触发 bootstrap。
func TestUserInitSkipsExistingUser(t *testing.T) {
	setupUserTest(t)
	oldPwd := conf.AppConfig.Bootstrap.Password
	conf.AppConfig.Bootstrap.Password = ""
	t.Cleanup(func() { conf.AppConfig.Bootstrap.Password = oldPwd })

	// 先建用户（模拟旧库），再 UserInit（无密码也应跳过）
	conf.AppConfig.Bootstrap.Password = "first-pass"
	if err := UserInit(); err != nil {
		t.Fatalf("first init: %v", err)
	}
	conf.AppConfig.Bootstrap.Password = ""
	if err := UserInit(); err != nil {
		t.Fatalf("UserInit must skip when user exists (legacy upgrade): %v", err)
	}
	u := UserGet()
	if u.Username != "admin" {
		t.Fatalf("expected existing admin preserved, got %s", u.Username)
	}
}
