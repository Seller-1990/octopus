package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/auth"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

func init() {
	router.NewGroupRouter("/api/v1/apikey").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/create", http.MethodPost).
				Handle(createAPIKey),
		).
		AddRoute(
			router.NewRoute("/list", http.MethodGet).
				Handle(listAPIKey),
		).
		AddRoute(
			router.NewRoute("/update", http.MethodPost).
				Handle(updateAPIKey),
		).
		AddRoute(
			router.NewRoute("/delete/:id", http.MethodDelete).
				Handle(deleteAPIKey),
		)
	// reveal 不强制 JSON body，单独注册在 Auth 组
	router.NewGroupRouter("/api/v1/apikey").
		Use(middleware.Auth()).
		AddRoute(
			router.NewRoute("/reveal/:id", http.MethodGet).
				Handle(revealAPIKey),
		)
	router.NewGroupRouter("/api/v1/apikey").
		Use(middleware.APIKeyAuth()).
		AddRoute(
			router.NewRoute("/stats", http.MethodGet).
				Handle(getStatsAPIKeyById),
		).
		AddRoute(
			router.NewRoute("/login", http.MethodGet).
				Handle(loginAPIKey),
		)
}

func createAPIKey(c *gin.Context) {
	var req model.APIKey
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if req.MaxRPM < 0 {
		resp.ErrorWithAppError(c, http.StatusBadRequest, apperror.New(apperror.CodeCommonInvalidParam, "max_rpm must be non-negative").WithStatus(http.StatusBadRequest))
		return
	}
	if err := op.ValidateAPIKeyQuota(req.QuotaLimit, req.QuotaPeriod); err != nil {
		resp.ErrorWithAppError(c, http.StatusBadRequest, apperror.New(apperror.CodeCommonInvalidParam, err.Error()).WithStatus(http.StatusBadRequest))
		return
	}
	req.APIKey = auth.GenerateAPIKey()
	if err := op.APIKeyCreate(&req, c.Request.Context()); err != nil {
		log.Errorf("failed to create api key: %v", err)
		resp.InternalError(c)
		return
	}
	// 创建是获取完整 key 的唯一机会，响应不掩码
	resp.Success(c, req)
}

func listAPIKey(c *gin.Context) {
	apiKeys, err := op.APIKeyList(c.Request.Context())
	if err != nil {
		log.Errorf("failed to list api keys: %v", err)
		resp.InternalError(c)
		return
	}
	maskedKeys := make([]model.APIKey, len(apiKeys))
	for i, key := range apiKeys {
		key.APIKey = maskSecret(key.APIKey)
		maskedKeys[i] = key
	}
	resp.Success(c, maskedKeys)
}

// revealAPIKey 返回完整 API Key。列表接口只返回掩码值，
// 前端复制按钮按需调用本端点获取明文（本组路由受 JWT 保护）。
// 明文出库属于敏感动作，与备份导出同标准留痕。
func revealAPIKey(c *gin.Context) {
	idNum, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.InvalidParam(c)
		return
	}
	apiKey, err := op.APIKeyGet(idNum, c.Request.Context())
	if err != nil {
		log.Errorf("failed to get api key %d: %v", idNum, err)
		resp.NotFound(c)
		return
	}
	log.Warnf("SECURITY AUDIT: api key %d (%s) revealed from IP %s", apiKey.ID, apiKey.Name, c.ClientIP())
	resp.Success(c, apiKey.APIKey)
}

func updateAPIKey(c *gin.Context) {
	var req model.APIKey
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if req.MaxRPM < 0 {
		resp.ErrorWithAppError(c, http.StatusBadRequest, apperror.New(apperror.CodeCommonInvalidParam, "max_rpm must be non-negative").WithStatus(http.StatusBadRequest))
		return
	}
	if err := op.ValidateAPIKeyQuota(req.QuotaLimit, req.QuotaPeriod); err != nil {
		resp.ErrorWithAppError(c, http.StatusBadRequest, apperror.New(apperror.CodeCommonInvalidParam, err.Error()).WithStatus(http.StatusBadRequest))
		return
	}
	// 编辑表单/行内开关会用列表返回的掩码值回填 api_key，精确比对掩码形式
	// 后还原为原值；不采用子串嗅探，避免误伤真实含 **** 的值。
	// op.APIKeyUpdate 另有无条件恢复原 key 的兜底，此处保证请求自洽。
	if req.APIKey != "" {
		if existing, err := op.APIKeyGet(req.ID, c.Request.Context()); err == nil {
			if masked := maskSecret(existing.APIKey); masked != "" && req.APIKey == masked {
				req.APIKey = existing.APIKey
			}
		}
	}
	if err := op.APIKeyUpdate(&req, c.Request.Context()); err != nil {
		log.Errorf("failed to update api key: %v", err)
		resp.InternalError(c)
		return
	}
	req.APIKey = maskSecret(req.APIKey)
	resp.Success(c, req)
}

func deleteAPIKey(c *gin.Context) {
	id := c.Param("id")
	idNum, err := strconv.Atoi(id)
	if err != nil {
		resp.InvalidParam(c)
		return
	}
	if err := op.APIKeyDelete(idNum, c.Request.Context()); err != nil {
		log.Errorf("failed to delete api key: %v", err)
		resp.InternalError(c)
		return
	}
	resp.Success(c, nil)
}

func getStatsAPIKeyById(c *gin.Context) {
	id := c.GetInt("api_key_id")
	stats := op.StatsAPIKeyGet(id)
	info, err := op.APIKeyGet(id, c.Request.Context())
	if err != nil {
		log.Errorf("failed to get api key %d: %v", id, err)
		resp.InternalError(c)
		return
	}
	models, err := op.GroupListModel(c.Request.Context())
	if err != nil {
		log.Errorf("failed to group list model: %v", err)
		resp.InternalError(c)
		return
	}
	var modelsString string
	if info.SupportedModels == "" {
		modelsString = strings.Join(models, ", ")
	} else {
		supportedModels := lo.Map(strings.Split(info.SupportedModels, ","), func(s string, _ int) string {
			return strings.TrimSpace(s)
		})
		models = lo.Filter(models, func(m string, _ int) bool {
			return lo.Contains(supportedModels, m)
		})
		modelsString = strings.Join(models, ", ")
	}
	info.SupportedModels = modelsString
	info.APIKey = maskSecret(info.APIKey)
	resp.Success(c, map[string]any{
		"stats": stats,
		"info":  info,
	})
}

func loginAPIKey(c *gin.Context) {
	resp.Success(c, nil)
}
