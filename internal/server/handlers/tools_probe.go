package handlers

import (
	"net/http"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/bestruirui/octopus/internal/toolsprobe"
	"github.com/gin-gonic/gin"
)

func init() {
	router.NewGroupRouter("/api/v1/tools-probe").
		Use(middleware.Auth()).
		AddRoute(
			router.NewRoute("/reprobe", http.MethodPost).
				Handle(reprobeToolsSupport),
		).
		AddRoute(
			router.NewRoute("/force-unsupported", http.MethodPost).
				Handle(forceToolsUnsupported),
		).
		AddRoute(
			router.NewRoute("/reset", http.MethodPost).
				Handle(resetToolsState),
		).
		AddRoute(
			router.NewRoute("/batch", http.MethodPost).
				Handle(batchToolsTest),
		).
		AddRoute(
			router.NewRoute("/batch/status/:task_id", http.MethodGet).
				Handle(batchToolsTestStatus),
		)
}

// batchToolsTestRequest 批量测试请求（v3.1 R6）：items 列表 → 异步任务 + 轮询。
type batchToolsTestRequest struct {
	Items      []toolsprobe.BatchItem `json:"items" binding:"required"`
	ToolChoice string                 `json:"tool_choice"` // "required"=手动判别矩阵；空=auto
}

// batchToolsTest 启动批量 tools 探测（202 + task_id，前端轮询 /batch/status/:task_id）。
func batchToolsTest(c *gin.Context) {
	var req batchToolsTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, "items are required")
		return
	}
	task, err := toolsprobe.StartBatch(c.Request.Context(), req.Items, req.ToolChoice)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, task)
}

// batchToolsTestStatus 查询批量任务进度。
func batchToolsTestStatus(c *gin.Context) {
	task := toolsprobe.BatchStatus(c.Param("task_id"))
	if task == nil {
		resp.Error(c, http.StatusNotFound, "task not found")
		return
	}
	resp.Success(c, task)
}

type reprobeToolsSupportRequest struct {
	ChannelID  int    `json:"channel_id" binding:"required"`
	ModelName  string `json:"model_name" binding:"required"`
	ToolChoice string `json:"tool_choice"` // "required"=手动判别矩阵（v3.1）；空=auto
}

// reprobeToolsSupport 手动重探单个渠道×模型的 tools 支持（v3.1 五态契约）。
// tool_choice=required 时走判别矩阵（含降级对照）；结果按证据层级写入。
func reprobeToolsSupport(c *gin.Context) {
	var req reprobeToolsSupportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, "channel_id and model_name are required")
		return
	}
	channel, err := op.ChannelGet(req.ChannelID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusNotFound, "channel not found")
		return
	}
	result, probeErr := toolsprobe.Run(c.Request.Context(), *channel, req.ModelName, req.ToolChoice)
	if probeErr != nil {
		// 完全无法探测（embedding/无 key/构造失败）：不写列
		resp.Error(c, http.StatusBadGateway, "probe failed: "+probeErr.Error())
		return
	}
	// 按证据层级写入（ApplyToolsProbeResult 内部处理各态：executed/accepted/unsupported 写列；pending/ignored/unknown 不写）
	if err := op.ApplyToolsProbeResult(req.ChannelID, req.ModelName, result, firstEnabledKeyID(channel)); err != nil {
		resp.Error(c, http.StatusInternalServerError, "failed to update supports_tools: "+err.Error())
		return
	}
	resp.Success(c, gin.H{
		"channel_id": req.ChannelID,
		"model_name": req.ModelName,
		"state":      result.State,
		"supports":   result.Supports,
		"source":     result.Source,
	})
}

type forceToolsUnsupportedRequest struct {
	ChannelID int    `json:"channel_id" binding:"required"`
	ModelName string `json:"model_name" binding:"required"`
}

// forceToolsUnsupported 管理员强制标不支持（最高级证据，任何探测不覆盖）。
func forceToolsUnsupported(c *gin.Context) {
	var req forceToolsUnsupportedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, "channel_id and model_name are required")
		return
	}
	if err := op.ForceToolsUnsupported(req.ChannelID, req.ModelName); err != nil {
		resp.Error(c, http.StatusInternalServerError, "failed to force unsupported: "+err.Error())
		return
	}
	resp.Success(c, gin.H{"channel_id": req.ChannelID, "model_name": req.ModelName, "supports_tools": false, "source": "manual-force"})
}

// resetToolsState 管理员恢复自动（解除强制，回到未探测待重探）。
func resetToolsState(c *gin.Context) {
	var req forceToolsUnsupportedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, "channel_id and model_name are required")
		return
	}
	if err := op.ResetToolsState(req.ChannelID, req.ModelName); err != nil {
		resp.Error(c, http.StatusInternalServerError, "failed to reset tools state: "+err.Error())
		return
	}
	resp.Success(c, gin.H{"channel_id": req.ChannelID, "model_name": req.ModelName, "supports_tools": nil})
}

func firstEnabledKeyID(channel *model.Channel) *int {
	for i := range channel.Keys {
		if channel.Keys[i].Enabled {
			id := channel.Keys[i].ID
			return &id
		}
	}
	return nil
}
