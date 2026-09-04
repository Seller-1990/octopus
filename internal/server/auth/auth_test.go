package auth

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/conf"
	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/golang-jwt/jwt/v5"
)

// TestTokenRevocationOnCredentialChange 验证 P0-1 端到端语义：
// 签发 → 有效；改密（token 版本 +1）→ 旧 token 立即失效；重新登录 → 有效；
// 改名同样吊销全部存量 token；无 ver claim 的"升级前旧格式 token"
// 在版本 > 0 后必须被拒绝（版本零值 0 与当前版本 2 不匹配）。
func TestTokenRevocationOnCredentialChange(t *testing.T) {
	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}
	dbPath := filepath.Join(t.TempDir(), "octopus-auth-test.db")
	if err := dbpkg.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() {
		_ = dbpkg.Close()
	})

	prevPass := conf.AppConfig.Bootstrap.Password
	conf.AppConfig.Bootstrap.Password = "bootstrap123"
	t.Cleanup(func() {
		conf.AppConfig.Bootstrap.Password = prevPass
	})

	if err := op.UserInit(); err != nil {
		t.Fatalf("UserInit: %v", err)
	}

	token1, _, err := GenerateJWTToken(15)
	if err != nil {
		t.Fatalf("GenerateJWTToken: %v", err)
	}
	if !VerifyJWTToken(token1) {
		t.Fatal("token1 should verify before any credential change")
	}

	if err := op.UserChangePassword("bootstrap123", "newpass456"); err != nil {
		t.Fatalf("UserChangePassword: %v", err)
	}
	if VerifyJWTToken(token1) {
		t.Fatal("token1 must be revoked after password change (version bumped)")
	}

	token2, _, err := GenerateJWTToken(15)
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if !VerifyJWTToken(token2) {
		t.Fatal("token2 (issued at version 1) should verify")
	}

	if err := op.UserChangeUsername("admin2"); err != nil {
		t.Fatalf("UserChangeUsername: %v", err)
	}
	if VerifyJWTToken(token2) {
		t.Fatal("token2 must be revoked after username change")
	}

	token3, _, err := GenerateJWTToken(0)
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if !VerifyJWTToken(token3) {
		t.Fatal("token3 (issued at version 2) should verify")
	}

	// 升级前旧格式 token：RegisteredClaims 无 ver claim → 版本零值 0 ≠ 2。
	now := time.Now()
	legacy := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		Issuer:    conf.APP_NAME,
		ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
	})
	legacyStr, err := legacy.SignedString(getJWTSecret())
	if err != nil {
		t.Fatalf("sign legacy token: %v", err)
	}
	if VerifyJWTToken(legacyStr) {
		t.Fatal("legacy token without ver claim must be rejected once token version > 0")
	}
}

// TestExpireCapEnforced 覆盖 F03 收尾：客户端可控的 expire 参数服务端封顶
// 30 天——超长正值（如 100 年）与任意负值（-2 等）一律收敛，杜绝永不过期。
func TestExpireCapEnforced(t *testing.T) {
	cases := []struct {
		name       string
		expiresMin int
		minDays    float64
		maxDays    float64
	}{
		{"huge-positive-capped", 52_560_000, 29.9, 30.1}, // 100 年 → 30 天
		{"negative-capped", -2, 29.9, 30.1},              // 未定义负值 → 30 天
		{"remember-me", -1, 29.9, 30.1},                  // 语义保持 30 天
		{"fifteen-min", 0, 0.009, 0.02},                  // 默认 15 分钟不回归
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token, _, err := GenerateJWTToken(tc.expiresMin)
			if err != nil {
				t.Fatalf("GenerateJWTToken: %v", err)
			}
			if !VerifyJWTToken(token) {
				t.Fatal("issued token must verify")
			}
			parsed, err := jwt.ParseWithClaims(token, &tokenClaims{}, func(token *jwt.Token) (interface{}, error) {
				return getJWTSecret(), nil
			}, jwt.WithValidMethods([]string{"HS256"}))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			claims := parsed.Claims.(*tokenClaims)
			if claims.ExpiresAt == nil {
				t.Fatal("ExpiresAt must always be set (never issue non-expiring tokens)")
			}
			days := time.Until(claims.ExpiresAt.Time).Hours() / 24
			if days < tc.minDays || days > tc.maxDays {
				t.Fatalf("lifetime = %.3f days, want [%.3f, %.3f]", days, tc.minDays, tc.maxDays)
			}
		})
	}
}
