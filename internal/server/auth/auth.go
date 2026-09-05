package auth

import (
	"crypto/rand"
	"encoding/base64"
	"math/big"
	"os"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/golang-jwt/jwt/v5"
)

var (
	jwtSecretOnce sync.Once
	jwtSecretKey  []byte
)

// maxJWTLifetime 服务端封顶的 token 最长有效期，客户端传入的超长/负数
// 有效期一律按此上限收敛，杜绝"永不过期"的 token。
const maxJWTLifetime = 30 * 24 * time.Hour

// tokenClaims 在标准声明之外携带 token 版本（ver）：
// 改密/改名会递增 op 侧的用户 token 版本，旧 token 因版本不匹配而失效。
// 签名密钥（jwt_secret）同时是存量密文的 AEAD 数据密钥（op/secret.go），
// 不可轮换，因此撤销语义完全由版本承担。
type tokenClaims struct {
	TokenVersion int `json:"ver"`
	jwt.RegisteredClaims
}

// getJWTSecret returns the JWT signing key, generating and persisting one if needed.
func getJWTSecret() []byte {
	jwtSecretOnce.Do(func() {
		secret, err := op.SettingGetString(model.SettingKeyJWTSecret)
		if err == nil && secret != "" {
			jwtSecretKey = []byte(secret)
			return
		}

		// Generate a random 32-byte secret
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			// CRITICAL: crypto/rand failure means the system cannot generate secure keys.
			// This should never happen on properly configured systems. Refusing to start
			// with a weak fallback is safer than running with predictable secrets.
			log.Errorf("CRITICAL: Failed to generate JWT secret using crypto/rand: %v. "+
				"System cannot start securely. Check OS entropy source (/dev/urandom).", err)
			os.Exit(1)
		}
		generated := base64.RawURLEncoding.EncodeToString(b)
		if err := op.SettingSetString(model.SettingKeyJWTSecret, generated); err != nil {
			// If we can't persist, still use the generated key for this session
			log.Warnf("failed to persist JWT secret: %s", err.Error())
		}
		jwtSecretKey = []byte(generated)
	})
	return jwtSecretKey
}

func GenerateJWTToken(expiresMin int) (string, string, error) {
	now := time.Now()
	claims := &tokenClaims{
		TokenVersion: op.UserTokenVersion(),
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    conf.APP_NAME,
		},
	}
	if expiresMin == 0 {
		claims.ExpiresAt = jwt.NewNumericDate(now.Add(time.Duration(15) * time.Minute))
	} else if expiresMin > 0 {
		lifetime := time.Duration(expiresMin) * time.Minute
		if lifetime > maxJWTLifetime {
			lifetime = maxJWTLifetime
		}
		claims.ExpiresAt = jwt.NewNumericDate(now.Add(lifetime))
	} else {
		// -1（"记住我"）与其他任意负值统一按 30 天封顶，
		// 不再允许无过期时间的 token
		claims.ExpiresAt = jwt.NewNumericDate(now.Add(maxJWTLifetime))
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(getJWTSecret())
	if err != nil {
		return "", "", err
	}
	return token, claims.ExpiresAt.Format(time.RFC3339), nil
}

func VerifyJWTToken(token string) bool {
	parsed, err := jwt.ParseWithClaims(token, &tokenClaims{}, func(token *jwt.Token) (interface{}, error) {
		return getJWTSecret(), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !parsed.Valid {
		return false
	}
	claims, ok := parsed.Claims.(*tokenClaims)
	if !ok {
		return false
	}
	// 版本不匹配（含升级前签发、无 ver claim 的旧 token，其版本为零值 0）：
	// 只要用户改过密/改过名（版本 > 0），一律拒绝。
	if claims.TokenVersion != op.UserTokenVersion() {
		return false
	}
	return true
}

func GenerateAPIKey() string {
	const keyChars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := make([]byte, 48)
	maxI := big.NewInt(int64(len(keyChars)))
	for i := range b {
		n, err := rand.Int(rand.Reader, maxI)
		if err != nil {
			return ""
		}
		b[i] = keyChars[n.Int64()]
	}
	return "sk-" + conf.APP_NAME + "-" + string(b)
}
