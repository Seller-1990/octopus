package handlers

import (
	"net/http"

	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/gin-gonic/gin"
)

// circuitStatusResponse 熔断状态列表（含统计）。
type circuitStatusResponse struct {
	Items    []balancer.CircuitStatus `json:"items"`
	Open     int                      `json:"open"`
	HalfOpen int                      `json:"half_open"`
}

func init() {
	router.NewGroupRouter("/api/v1/circuit").
		Use(middleware.Auth()). // 管理面：绝不能用 APIKeyAuth（P2：租户 key 不得清空熔断）
		AddRoute(
			router.NewRoute("/status", http.MethodGet).
				Handle(circuitStatus),
		).
		AddRoute(
			router.NewRoute("/reset", http.MethodPost).
				Handle(circuitReset),
		)
}

// circuitStatus 熔断状态列表。默认前端只关心熔断中的条目（closed 噪音已由后端惰性清理 + 前端过滤双控）。
func circuitStatus(c *gin.Context) {
	items := balancer.Snapshot()
	out := circuitStatusResponse{Items: items}
	for _, it := range items {
		if it.State == balancer.StateOpen {
			out.Open++
		} else if it.State == balancer.StateHalfOpen {
			out.HalfOpen++
		}
	}
	resp.Success(c, out)
}

// circuitResetRequest 手动重置请求。
// scope 显式指定："all"=全量重置（必须显式，空 body 不做全量——P1 防误点清空熔断保护）。
type circuitResetRequest struct {
	Scope        string `json:"scope"` // "all" | "channel" | "item"
	ChannelID    int    `json:"channel_id"`
	ChannelKeyID int    `json:"channel_key_id"`
	ModelName    string `json:"model_name"`
}

// circuitReset 手动重置熔断。scope 缺省视为精确重置（需 channel_id 等）；全量必须显式 scope=all。
func circuitReset(c *gin.Context) {
	var req circuitResetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidParam(c)
		return
	}
	switch req.Scope {
	case "all":
		balancer.ResetCircuit("all", 0, 0, "")
	case "channel":
		if req.ChannelID <= 0 {
			resp.Error(c, http.StatusBadRequest, "channel_id is required for scope=channel")
			return
		}
		balancer.ResetCircuit("", req.ChannelID, 0, "")
	default:
		// 精确重置：按 (channel, key, model)
		if req.ChannelID <= 0 || req.ChannelKeyID <= 0 || req.ModelName == "" {
			resp.Error(c, http.StatusBadRequest, "channel_id, channel_key_id and model_name are required for item reset")
			return
		}
		balancer.ResetCircuit("", req.ChannelID, req.ChannelKeyID, req.ModelName)
	}
	resp.Success(c, gin.H{"reset": req.Scope})
}
