package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/auth"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/gin-gonic/gin"
)

func init() {
	router.NewGroupRouter("/api/v1/user").
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/login", http.MethodPost).
				Handle(login),
		)
	router.NewGroupRouter("/api/v1/user").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/change-password", http.MethodPost).
				Handle(changePassword),
		).
		AddRoute(
			router.NewRoute("/change-username", http.MethodPost).
				Handle(changeUsername),
		).
		AddRoute(
			router.NewRoute("/status", http.MethodGet).
				Handle(status),
		)
}

func login(c *gin.Context) {
	var user model.UserLogin
	if err := c.ShouldBindJSON(&user); err != nil {
		resp.InvalidJSON(c)
		return
	}
	ip := c.ClientIP()
	if allowed, retryAfter, atCapacity := loginThrottleAttempt(ip, time.Now()); !allowed {
		// 容量拒绝已在限流器内全局限频记录;锁定拒绝逐条记录,由每 IP 的尝试预算天然约束。
		if !atCapacity {
			log.Warnf("SECURITY AUDIT: login throttled for IP %s, retry after %s", ip, retryAfter.Round(time.Second))
		}
		c.Header("Retry-After", strconv.FormatInt(int64((retryAfter+time.Second-1)/time.Second), 10))
		resp.ErrorWithCode(c, http.StatusTooManyRequests, "", "too many login attempts, retry later")
		return
	}
	if err := op.UserVerify(user.Username, user.Password); err != nil {
		log.Warnf("SECURITY AUDIT: failed login attempt for user %q from IP %s", user.Username, ip)
		resp.InvalidCredentials(c)
		return
	}
	loginThrottleSuccess(ip)
	token, expire, err := auth.GenerateJWTToken(user.Expire)
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, model.UserLoginResponse{Token: token, ExpireAt: expire})
}

func changePassword(c *gin.Context) {
	var user model.UserChangePassword
	if err := c.ShouldBindJSON(&user); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := op.UserChangePassword(user.OldPassword, user.NewPassword); err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, err)
		return
	}
	// 改密在 op 层原子递增用户 token 版本（与密码同事务落库），
	// 全部存量 JWT 因 ver claim 不匹配而立即失效；前端改密成功后会强制重新登录。
	// 不轮换签名密钥：jwt_secret 兼任存量密文的 AEAD 数据密钥（op/secret.go）。
	log.Warnf("SECURITY AUDIT: password changed from IP %s, all existing tokens revoked", c.ClientIP())
	resp.Success(c, "password changed successfully")
}

func changeUsername(c *gin.Context) {
	var user model.UserChangeUsername
	if err := c.ShouldBindJSON(&user); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := op.UserChangeUsername(user.NewUsername); err != nil {
		log.Errorf("failed to change username: %v", err)
		resp.InternalError(c)
		return
	}
	// 与改密同策略：op 层递增 token 版本吊销全部存量 token（不轮换签名密钥）
	log.Warnf("SECURITY AUDIT: username changed from IP %s, all existing tokens revoked", c.ClientIP())
	resp.Success(c, "username changed successfully")
}

func status(c *gin.Context) {
	resp.Success(c, "ok")
}
