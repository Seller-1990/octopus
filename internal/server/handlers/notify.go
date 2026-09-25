package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/notify"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/gin-gonic/gin"
)

func init() {
	router.NewGroupRouter("/api/v1/notify").
		AddRoute(
			router.NewRoute("/stream", http.MethodGet).
				Handle(streamNotify),
		)
	// N3 装配：Webhook 配置读设置缓存（发布路径不查库）；notify 包不直接依赖 op。
	notify.SetWebhookLoader(func() (string, string, bool) {
		url, _ := op.SettingGetString(model.SettingKeyNotifyWebhookURL)
		events, _ := op.SettingGetString(model.SettingKeyNotifyWebhookEvents)
		enabled, _ := op.SettingGetBool(model.SettingKeyNotifyWebhookEnabled)
		return url, events, enabled
	})
}

// streamNotify GET /api/v1/notify/stream?token=
// 任务通知 SSE 流：先补发 ring 快照再增量（N2）。鉴权复用日志流同款
// 一次性 stream-token（管理员 JWT 换 token，见 /api/v1/log/stream-token）。
func streamNotify(c *gin.Context) {
	if !requireLogStreamToken(c) {
		return
	}
	prepareLiveSSE(c)

	snapshot, updates := notify.Default.Subscribe()
	defer notify.Default.Unsubscribe(updates)

	for _, event := range snapshot {
		if err := writeNotifySSE(c, event); err != nil {
			return
		}
		c.Writer.Flush()
	}

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-heartbeat.C:
			if err := writeLiveSSEHeartbeat(c); err != nil {
				return
			}
		case event, ok := <-updates:
			if !ok {
				return
			}
			if err := writeNotifySSE(c, event); err != nil {
				return
			}
			c.Writer.Flush()
		}
	}
}

func writeNotifySSE(c *gin.Context, event notify.Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(c.Writer, "event: notify\ndata: %s\n\n", payload); err != nil {
		return err
	}
	return nil
}
