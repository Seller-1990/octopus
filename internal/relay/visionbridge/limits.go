package visionbridge

import (
	"context"
	"fmt"
	"time"

	"github.com/bestruirui/octopus/internal/conf"
)

// Config 是 conf.VisionBridge 的规范化快照（默认值已回填）。
type Config struct {
	Enabled                bool
	VisionModel            string
	VisionBaseURL          string
	VisionAPIKeyEnv        string
	VisionFallbackModels   []string
	Language               string
	RequestTimeout         time.Duration
	MaxInflight            int
	MaxImagesPerRequest    int
	MaxRequestBytes        int
	MaxImageReferenceBytes int
	MaxResultChars         int
	MinResultChars         int
	CacheSize              int
	CacheTTL               time.Duration
	URLCacheTTL            time.Duration
	TargetChannelIDs       map[int]struct{}
}

// loadConfig 读取全局配置并回填默认值（与 conf.setDefaults 保持一致，
// 防御直接构造 AppConfig 的测试/嵌入场景）。
func loadConfig() Config {
	raw := conf.AppConfig.VisionBridge
	cfg := Config{
		Enabled:                raw.Enabled,
		VisionModel:            raw.VisionModel,
		VisionBaseURL:          raw.VisionBaseURL,
		VisionAPIKeyEnv:        raw.VisionAPIKeyEnv,
		VisionFallbackModels:   raw.VisionFallbackModels,
		Language:               raw.Language,
		RequestTimeout:         durationOr(raw.RequestTimeoutSeconds, 120*time.Second),
		MaxInflight:            intOr(raw.MaxInflightVisionRequests, 4),
		MaxImagesPerRequest:    intOr(raw.MaxImagesPerRequest, 8),
		MaxRequestBytes:        intOr(raw.MaxRequestBytes, 20*1024*1024),
		MaxImageReferenceBytes: intOr(raw.MaxImageReferenceBytes, 15*1024*1024),
		MaxResultChars:         intOr(raw.MaxResultChars, 20000),
		MinResultChars:         intOr(raw.MinResultChars, 30),
		CacheSize:              intOr(raw.AnalysisCacheSize, 128),
		CacheTTL:               durationOr(raw.AnalysisCacheTTLSeconds, 900*time.Second),
		URLCacheTTL:            durationOr(raw.AnalysisURLCacheTTLSeconds, 120*time.Second),
	}
	if len(raw.TargetChannelIDs) > 0 {
		cfg.TargetChannelIDs = make(map[int]struct{}, len(raw.TargetChannelIDs))
		for _, id := range raw.TargetChannelIDs {
			cfg.TargetChannelIDs[id] = struct{}{}
		}
	}
	return cfg
}

func intOr(v, fallback int) int {
	if v <= 0 {
		return fallback
	}
	return v
}

func durationOr(seconds int, fallback time.Duration) time.Duration {
	if seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

// inflightLimiter 全局 VLM 并发信号量：bridge 是兜底路径，
// 不应放大到把 VLM 后端打挂。
type inflightLimiter struct {
	slots chan struct{}
}

func newInflightLimiter(size int) *inflightLimiter {
	if size <= 0 {
		size = 4
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
