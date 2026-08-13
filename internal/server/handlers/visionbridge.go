package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/visionbridge"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/gin-gonic/gin"
)

func init() {
	router.NewGroupRouter("/api/v1/vision-bridge").
		Use(middleware.Auth()).
		AddRoute(
			router.NewRoute("/test", http.MethodPost).
				Use(middleware.RequireJSON()).
				Handle(testVisionBridge),
		)
}

// visionBridgeTestRequest 承载设置页表单当前值：model/base_url/fallback_models
// 按表单字面值整体生效；api_key 因列表接口打码，空值回落已保存值。
type visionBridgeTestRequest struct {
	Model          string `json:"model"`
	BaseURL        string `json:"base_url"`
	APIKey         string `json:"api_key"`
	FallbackModels string `json:"fallback_models"`
}

// maxTestFallbackModels 单次测试的备选模型上限：每个模型是一次最长 30s 的真实外呼，
// 防止一次请求发起任意多次外呼。
const maxTestFallbackModels = 8

// testVisionBridge 用内置测试图逐个真调 VLM 模型链，返回每个模型的
// 可用性/延迟/描述预览（设置页「测试」按钮；Step 0a 结论：VLM 选型必须按部署环境实测）。
func testVisionBridge(c *gin.Context) {
	var reqBody visionBridgeTestRequest
	if err := c.ShouldBindJSON(&reqBody); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	cfg := visionbridge.SnapshotSettings()
	cfg.VisionModel = strings.TrimSpace(reqBody.Model)
	cfg.VisionBaseURL = strings.TrimSpace(reqBody.BaseURL)
	cfg.VisionFallbackModels = nil
	for _, m := range strings.Split(reqBody.FallbackModels, ",") {
		if m = strings.TrimSpace(m); m != "" {
			cfg.VisionFallbackModels = append(cfg.VisionFallbackModels, m)
		}
	}
	if key := strings.TrimSpace(reqBody.APIKey); key != "" {
		cfg.VisionAPIKey = key
	}
	if cfg.VisionModel == "" || cfg.VisionBaseURL == "" {
		resp.Error(c, http.StatusBadRequest, "model and base_url are required")
		return
	}
	if len(cfg.VisionFallbackModels) > maxTestFallbackModels {
		resp.Error(c, http.StatusBadRequest, fmt.Sprintf("too many fallback models (max %d)", maxTestFallbackModels))
		return
	}
	// base_url 与保存路径同一校验规则（scheme/host），测试请求不放宽
	if err := (&model.Setting{Key: model.SettingKeyVisionBridgeBaseURL, Value: cfg.VisionBaseURL}).Validate(); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	resp.Success(c, visionbridge.Probe(c.Request.Context(), cfg))
}
