package visionbridge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

// 运行参数常量：这些不是用户需要调的旋钮（v0.2 评审 + Step 2 简化决策），
// 默认值来自方案评审与 Step 0a 实测；确有需求时再择项开放。
const (
	// requestTimeout VLM 阶段总超时，主备模型链公平切分（Step 0a：VLM 中位延迟 38s）。
	requestTimeout = 120 * time.Second
	// maxInflight 全局 VLM 并发上限：bridge 是兜底路径，不应放大流量打挂 VLM 后端。
	maxInflight            = 4
	maxImagesPerRequest    = 8
	maxRequestBytes        = 20 * 1024 * 1024
	maxImageReferenceBytes = 15 * 1024 * 1024
	maxResultChars         = 20000
	// minResultChars 描述最短长度；短于此视为 VLM 失败（Step 0a：空描述诱发下游自信幻觉）。
	minResultChars = 30
	cacheSize      = 128
	// cacheTTL data URI 内容寻址可长缓存；urlCacheTTL URL 内容可变只短缓存。
	cacheTTL    = 900 * time.Second
	urlCacheTTL = 120 * time.Second
)

// Config 是一次请求生命周期内的 bridge 配置快照：
// 5 个 DB 设置（管理页可改，即时生效）+ 上方常量。
type Config struct {
	Enabled                bool
	VisionModel            string
	VisionBaseURL          string
	VisionAPIKey           string
	VisionFallbackModels   []string
	RequestTimeout         time.Duration
	MaxImagesPerRequest    int
	MaxRequestBytes        int
	MaxImageReferenceBytes int
	MaxResultChars         int
	MinResultChars         int
	CacheTTL               time.Duration
	URLCacheTTL            time.Duration
}

// snapshotSettings 从运行时设置缓存读取 bridge 配置（内存读，每请求一次）。
func snapshotSettings() Config {
	cfg := defaultConfig()
	cfg.Enabled, _ = op.SettingGetBool(model.SettingKeyVisionBridgeEnabled)
	cfg.VisionModel = trimmedSetting(model.SettingKeyVisionBridgeModel)
	cfg.VisionBaseURL = trimmedSetting(model.SettingKeyVisionBridgeBaseURL)
	cfg.VisionAPIKey = trimmedSetting(model.SettingKeyVisionBridgeAPIKey)
	cfg.VisionFallbackModels = splitModels(trimmedSetting(model.SettingKeyVisionBridgeFallbackModels))
	return cfg
}

func defaultConfig() Config {
	return Config{
		RequestTimeout:         requestTimeout,
		MaxImagesPerRequest:    maxImagesPerRequest,
		MaxRequestBytes:        maxRequestBytes,
		MaxImageReferenceBytes: maxImageReferenceBytes,
		MaxResultChars:         maxResultChars,
		MinResultChars:         minResultChars,
		CacheTTL:               cacheTTL,
		URLCacheTTL:            urlCacheTTL,
	}
}

func trimmedSetting(key model.SettingKey) string {
	value, _ := op.SettingGetString(key)
	return strings.TrimSpace(value)
}

// splitModels 解析逗号分隔的模型列表（空项忽略）。
func splitModels(raw string) []string {
	if raw == "" {
		return nil
	}
	var models []string
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part != "" {
			models = append(models, part)
		}
	}
	return models
}

// inflightLimiter 全局 VLM 并发信号量。
type inflightLimiter struct {
	slots chan struct{}
}

func newInflightLimiter(size int) *inflightLimiter {
	if size <= 0 {
		size = maxInflight
	}
	return &inflightLimiter{slots: make(chan struct{}, size)}
}

func (l *inflightLimiter) Acquire(ctx context.Context) error {
	select {
	case l.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("vision bridge inflight wait canceled: %w", ctx.Err())
	}
}

func (l *inflightLimiter) Release() {
	<-l.slots
}
