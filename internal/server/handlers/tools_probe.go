package handlers

import (
	"net/http"
	"time"

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
		)
}

type reprobeToolsSupportRequest struct {
	ChannelID int    `json:"channel_id" binding:"required"`
	ModelName string `json:"model_name" binding:"required"`
}

// reprobeToolsSupport 手动重探单个渠道×模型的 tools 支持，并回填（v3）。
// 触发 toolsprobe.Run（经 op.ToolsProbeFn hook），结果写回所有含该 (channel, model) 的组行。
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
	supports, probeErr := toolsprobe.Run(c.Request.Context(), *channel, req.ModelName)
	now := time.Now()
	source := "manual"
	if probeErr != nil {
		// 探测失败：不改变标记，返回错误信息
		resp.Error(c, http.StatusBadGateway, "probe failed: "+probeErr.Error())
		return
	}
	updates := map[string]any{
		"supports_tools":              supports,
		"supports_tools_probe_key_id": firstEnabledKeyID(channel),
		"supports_tools_probed_at":    &now,
		"supports_tools_source":       source,
	}
	if err := op.UpdateGroupItemToolsSupport(req.ChannelID, req.ModelName, updates); err != nil {
		resp.Error(c, http.StatusInternalServerError, "failed to update supports_tools: "+err.Error())
		return
	}
	resp.Success(c, gin.H{"channel_id": req.ChannelID, "model_name": req.ModelName, "supports_tools": supports})
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
