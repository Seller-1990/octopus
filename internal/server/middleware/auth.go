package middleware

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/auth"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/gin-gonic/gin"
)

func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if token == "" {
			resp.Unauthorized(c)
			c.Abort()
			return
		}
		if !auth.VerifyJWTToken(strings.TrimPrefix(token, "Bearer ")) {
			resp.InvalidToken(c)
			c.Abort()
			return
		}
		c.Next()
	}
}

func APIKeyAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		var apiKey string
		var requestType string

		if key := c.Request.Header.Get("x-api-key"); key != "" {
			apiKey = key
			requestType = "anthropic"
		} else if auth := c.Request.Header.Get("Authorization"); auth != "" {
			apiKey = strings.TrimPrefix(auth, "Bearer ")
			requestType = "openai"
		}

		if apiKey == "" {
			resp.APIKeyMissing(c)
			c.Abort()
			return
		}

		if !strings.HasPrefix(apiKey, "sk-"+conf.APP_NAME+"-") {
			resp.InvalidToken(c)
			c.Abort()
			return
		}
		apiKeyObj, err := op.APIKeyGetByAPIKey(apiKey, c.Request.Context())
		if err != nil {
			resp.InvalidToken(c)
			c.Abort()
			return
		}
		if !apiKeyObj.Enabled {
			resp.ErrorWithAppError(c, http.StatusUnauthorized, apperror.New(apperror.CodeAuthAPIKeyDisabled, "API key is disabled").WithStatus(http.StatusUnauthorized))
			c.Abort()
			return
		}
		if apiKeyObj.ExpireAt > 0 && apiKeyObj.ExpireAt < time.Now().Unix() {
			resp.APIKeyExpired(c)
			c.Abort()
			return
		}
		statsAPIKey := op.StatsAPIKeyGet(apiKeyObj.ID)
		if apiKeyObj.MaxCost > 0 && apiKeyObj.MaxCost < statsAPIKey.StatsMetrics.OutputCost+statsAPIKey.StatsMetrics.InputCost {
			resp.ErrorWithAppError(c, http.StatusUnauthorized, apperror.New(apperror.CodeAuthAPIKeyCostExceeded, "API key has reached the max cost").WithStatus(http.StatusUnauthorized))
			c.Abort()
			return
		}
		if apiKeyObj.MaxRPM > 0 {
			allowed, retryAfter := op.RateLimitCheck(apiKeyObj.ID, apiKeyObj.MaxRPM)
			if !allowed {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
				resp.ErrorWithAppError(c, http.StatusTooManyRequests, apperror.New(apperror.CodeAuthAPIKeyRateLimited, "API key has exceeded the rate limit").WithStatus(http.StatusTooManyRequests))
				c.Abort()
				return
			}
		}
		if apiKeyObj.QuotaLimit > 0 {
			now := time.Now()
			if apiKeyObj.QuotaResetAt == 0 || now.Unix() >= apiKeyObj.QuotaResetAt {
				// 条件重置:op 层锁内重检到期快照,并发/管理员改动时不重复清零;
				// 返回值是实际状态,不得假设「已重置且 used=0」。
				current, err := op.APIKeyResetQuota(c.Request.Context(), apiKeyObj.ID, apiKeyObj.QuotaResetAt, now)
				if err != nil {
					resp.Error(c, http.StatusInternalServerError, err.Error())
					c.Abort()
					return
				}
				apiKeyObj.QuotaUsed = current.QuotaUsed
			}
			if apiKeyObj.QuotaUsed >= apiKeyObj.QuotaLimit {
				c.JSON(http.StatusTooManyRequests, gin.H{
					"error": map[string]any{
						"message": "API key quota exceeded",
						"type":    "quota_exceeded",
						"code":    "quota_exceeded",
					},
				})
				c.Abort()
				return
			}
		}
		c.Set("request_type", requestType)
		c.Set("supported_models", apiKeyObj.SupportedModels)
		c.Set("api_key_id", apiKeyObj.ID)
		c.Next()
	}
}
