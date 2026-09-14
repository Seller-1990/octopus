package handlers

import (
	"context"
	"github.com/bestruirui/octopus/internal/utils/xstrings"
	"net/http"
	"strconv"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/bestruirui/octopus/internal/task"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/safe"
	"github.com/gin-gonic/gin"
)

func init() {
	router.NewGroupRouter("/api/v1/channel").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/list", http.MethodGet).
				Handle(listChannel),
		).
		AddRoute(
			router.NewRoute("/create", http.MethodPost).
				Handle(createChannel),
		).
		AddRoute(
			router.NewRoute("/update", http.MethodPost).
				Handle(updateChannel),
		).
		AddRoute(
			router.NewRoute("/enable", http.MethodPost).
				Handle(enableChannel),
		).
		AddRoute(
			router.NewRoute("/delete/:id", http.MethodDelete).
				Handle(deleteChannel),
		).
		AddRoute(
			router.NewRoute("/fetch-model", http.MethodPost).
				Handle(fetchModel),
		)
	router.NewGroupRouter("/api/v1/channel").
		Use(middleware.Auth()).
		AddRoute(
			router.NewRoute("/sync", http.MethodPost).
				Handle(syncChannel),
		).
		AddRoute(
			router.NewRoute("/last-sync-time", http.MethodGet).
				Handle(getLastSyncTime),
		)
}

func listChannel(c *gin.Context) {
	channels, err := op.ChannelList(c.Request.Context())
	if err != nil {
		log.Errorf("failed to channel list: %v", err)
		resp.InternalError(c)
		return
	}
	channelIDs := make([]int, 0, len(channels))
	for _, channel := range channels {
		channelIDs = append(channelIDs, channel.ID)
	}
	bindingMap, err := op.SiteChannelBindingMapByChannelIDs(channelIDs, c.Request.Context())
	if err != nil {
		log.Errorf("failed to site channel binding map by channel ids: %v", err)
		resp.InternalError(c)
		return
	}
	for i, channel := range channels {
		stats := op.StatsChannelGet(channel.ID)
		channels[i].Stats = &stats
		if binding, ok := bindingMap[channel.ID]; ok {
			channels[i].Managed = true
			channels[i].ManagedSource = &model.ManagedChannelSource{
				SiteID:          binding.SiteID,
				SiteAccountID:   binding.SiteAccountID,
				SiteUserGroupID: binding.SiteUserGroupID,
				GroupKey:        binding.GroupKey,
			}
		}
	}
	resp.Success(c, channels)
}

func createChannel(c *gin.Context) {
	var channel model.Channel
	if err := c.ShouldBindJSON(&channel); err != nil {
		resp.InvalidJSON(c)
		return
	}
	// 代理模式校验唯一权威在 op.ChannelCreate（C250913-09）：错误经 apperror
	// WithStatus(400) 透传，避免 handler/op 双份规则「改一处不生效」漂移。
	if err := op.ChannelCreate(&channel, c.Request.Context()); err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, crudError(codeChannelCreateFailed, "channel create failed", err))
		return
	}
	stats := op.StatsChannelGet(channel.ID)
	channel.Stats = &stats
	createdChannel := channel
	safe.Go("channel-create-postprocess", func() {
		runChannelPostProcess(&createdChannel)
	})
	resp.Success(c, channel)
}

// runChannelPostProcess 渠道创建/更新共用的异步后处理：价格入库、延迟探测、
// 自动建组。原两处逐字复制（C250913-08）；模型名拆分统一走 xstrings 的
// trim+去重实现——旧实现不 trim、不去重，靠下游空串兜底才未出 bug。
func runChannelPostProcess(channel *model.Channel) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	modelArray := xstrings.SplitTrimCompactUnique(",", channel.Model, channel.CustomModel)
	helper.LLMPriceAddToDB(modelArray, ctx)
	helper.ChannelBaseUrlDelayUpdate(channel, ctx)
	helper.ChannelAutoGroup(channel, ctx)
}

func updateChannel(c *gin.Context) {
	var req model.ChannelUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	channel, err := op.ChannelUpdate(&req, c.Request.Context())
	if err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, crudError(codeChannelUpdateFailed, "channel update failed", err))
		return
	}
	stats := op.StatsChannelGet(channel.ID)
	channel.Stats = &stats
	updatedChannel := *channel
	safe.Go("channel-update-postprocess", func() {
		runChannelPostProcess(&updatedChannel)
	})
	resp.Success(c, channel)
}

func enableChannel(c *gin.Context) {
	var request struct {
		ID      int  `json:"id"`
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := op.ChannelEnabled(request.ID, request.Enabled, c.Request.Context()); err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, crudError(codeChannelUpdateFailed, "channel update failed", err))
		return
	}
	resp.Success(c, nil)
}

func deleteChannel(c *gin.Context) {
	id := c.Param("id")
	idNum, err := strconv.Atoi(id)
	if err != nil {
		resp.InvalidParam(c)
		return
	}
	if err := op.ChannelDel(idNum, c.Request.Context()); err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, crudError(codeChannelDeleteFailed, "channel delete failed", err))
		return
	}
	resp.Success(c, nil)
}
func fetchModel(c *gin.Context) {
	var request model.Channel
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	models, err := helper.FetchModels(c.Request.Context(), request)
	if err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, crudError(codeChannelFetchModelsFailed, "channel fetch models failed", err))
		return
	}
	resp.Success(c, models)
}

func syncChannel(c *gin.Context) {
	task.SyncModelsTask()
	resp.Success(c, nil)
}

func getLastSyncTime(c *gin.Context) {
	time := task.GetLastSyncModelsTime()
	resp.Success(c, time)
}
