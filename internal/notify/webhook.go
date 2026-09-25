package notify

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/safe"
)

const webhookTimeout = 5 * time.Second

// WebhookConfigLoader 由装配方注入（handlers 装配时读设置缓存），notify 包
// 不直接依赖 op/db——保证本包单测无需数据库。返回 (url, 逗号分隔事件过滤,
// enabled)。
var WebhookConfigLoader func() (url string, events string, enabled bool)

// SetWebhookLoader 装配 Webhook 配置读取器（N3）。传 nil 恢复禁用。
func SetWebhookLoader(loader func() (url string, events string, enabled bool)) {
	WebhookConfigLoader = loader
}

// dispatchWebhookAsync 异步投递 Webhook：超时 5s、失败记 Warn、不重试
// （内网自用，通知丢失无害；避免重试风暴）。URL 为空或总开关关闭时不发。
// 事件过滤：events 为空 = 全部；否则逗号分隔 type 精确匹配。
// dispatchWebhookAsync 在发布时刻被同步调用：读配置（设置缓存，无 IO）
// 并过滤，然后才起异步 goroutine 做 HTTP 投递——配置语义为「发布时刻生效」。
func dispatchWebhookAsync(event Event) {
	if WebhookConfigLoader == nil {
		return
	}
	url, events, enabled := WebhookConfigLoader()
	if !enabled || strings.TrimSpace(url) == "" {
		return
	}
	if filter := strings.TrimSpace(events); filter != "" {
		matched := false
		for _, name := range strings.Split(filter, ",") {
			if strings.TrimSpace(name) == event.Type {
				matched = true
				break
			}
		}
		if !matched {
			return
		}
	}
	payload, err := json.Marshal(event)
	if err != nil {
		log.Warnf("notify webhook marshal failed: %v", err)
		return
	}
	safe.Go("notify-webhook", func() {
		client := &http.Client{Timeout: webhookTimeout}
		resp, err := client.Post(url, "application/json", bytes.NewReader(payload))
		if err != nil {
			log.Warnf("notify webhook post failed (type=%s): %v", event.Type, err)
			return
		}
		defer func() {
			_ = resp.Body.Close()
		}()
		if resp.StatusCode >= 300 {
			log.Warnf("notify webhook post got status %d (type=%s)", resp.StatusCode, event.Type)
		}
	})
}
