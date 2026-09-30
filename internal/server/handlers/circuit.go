package handlers

import (
	"context"
	"net/http"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/gin-gonic/gin"
)

// circuitItemResponse 熔断条目 + 展示用名称（渠道名/站点/账号/key 备注）。
// 用户反馈：只有 channel_id 数字无法判断熔断的是哪个站点。
type circuitItemResponse struct {
	balancer.CircuitStatus
	ChannelName     string `json:"channel_name,omitempty"`
	SiteName        string `json:"site_name,omitempty"`
	SiteAccountName string `json:"site_account_name,omitempty"`
	KeyRemark       string `json:"key_remark,omitempty"`
}

// circuitStatusResponse 熔断状态列表（含统计）。
type circuitStatusResponse struct {
	Items    []circuitItemResponse `json:"items"`
	Open     int                   `json:"open"`
	HalfOpen int                   `json:"half_open"`
}

// enrichCircuitItems 批量解析展示名：渠道名/绑定站点与账号/key 备注。
// 查询失败的字段降级为空串（保持熔断列表可用），不影响状态本身。
func enrichCircuitItems(ctx context.Context, items []balancer.CircuitStatus) []circuitItemResponse {
	result := make([]circuitItemResponse, 0, len(items))
	if len(items) == 0 {
		return result
	}
	channelIDs := make([]int, 0, len(items))
	seenChannel := make(map[int]struct{}, len(items))
	for _, it := range items {
		if _, ok := seenChannel[it.ChannelID]; !ok {
			seenChannel[it.ChannelID] = struct{}{}
			channelIDs = append(channelIDs, it.ChannelID)
		}
	}
	channelsByID := make(map[int]model.Channel)
	if channels, err := op.ChannelList(ctx); err == nil {
		for _, ch := range channels {
			channelsByID[ch.ID] = ch
		}
	}
	bindings, _ := op.SiteChannelBindingMapByChannelIDs(channelIDs, ctx)
	siteNames := make(map[int]string)
	accountNames := make(map[int]string)
	for _, binding := range bindings {
		if _, ok := siteNames[binding.SiteID]; !ok {
			if site, err := op.SiteGet(binding.SiteID, ctx); err == nil && site != nil {
				siteNames[binding.SiteID] = site.Name
			}
		}
		if _, ok := accountNames[binding.SiteAccountID]; !ok {
			if account, err := op.SiteAccountGet(binding.SiteAccountID, ctx); err == nil && account != nil {
				accountNames[binding.SiteAccountID] = account.Name
			}
		}
	}
	for _, it := range items {
		item := circuitItemResponse{CircuitStatus: it}
		if ch, ok := channelsByID[it.ChannelID]; ok {
			item.ChannelName = ch.Name
			for _, key := range ch.Keys {
				if key.ID == it.ChannelKeyID {
					item.KeyRemark = key.Remark
					break
				}
			}
		}
		if binding, ok := bindings[it.ChannelID]; ok {
			item.SiteName = siteNames[binding.SiteID]
			item.SiteAccountName = accountNames[binding.SiteAccountID]
		}
		result = append(result, item)
	}
	return result
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
	out := circuitStatusResponse{Items: enrichCircuitItems(c.Request.Context(), items)}
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
