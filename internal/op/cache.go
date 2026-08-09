package op

import (
	"context"
	"fmt"
	"time"

	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/utils/log"
)

func InitCache() error {
	ctx, cancel := context.WithTimeout(context.Background(), conf.CacheInitTimeout())
	defer cancel()
	if err := settingRefreshCache(ctx); err != nil {
		return fmt.Errorf("setting refresh cache error: %v", err)
	}
	if err := channelRefreshCache(ctx); err != nil {
		return fmt.Errorf("channel refresh cache error: %v", err)
	}
	if err := proxyConfigurationRefreshCache(ctx); err != nil {
		return fmt.Errorf("proxy configuration refresh cache error: %v", err)
	}
	// 阶段 4 第 16 条（v2 Z6）：启动后重算倍率策略状态——settings 就绪后、group 缓存构建前执行，
	// 使重启/外部改库后 policy_blocked 一致性恢复，且 group 缓存按重算后的库构建。
	// 实施后审查 B3：失败降级 Warn 继续启动（内网自用容错优先，避免大实例超时导致整体启动失败）。
	if _, _, err := EnforceMultiplierCap(ctx); err != nil {
		log.Warnf("enforce multiplier cap at startup failed, continuing: %v", err)
	}
	if err := groupRefreshCache(ctx); err != nil {
		return fmt.Errorf("group refresh cache error: %v", err)
	}
	if err := apiKeyRefreshCache(ctx); err != nil {
		return fmt.Errorf("api key refresh cache error: %v", err)
	}
	if err := llmRefreshCache(ctx); err != nil {
		return fmt.Errorf("llm refresh cache error: %v", err)
	}
	if err := statsRefreshCache(ctx); err != nil {
		return fmt.Errorf("stats refresh cache error: %v", err)
	}
	if _, err := CatalogSync(ctx); err != nil {
		return fmt.Errorf("model catalog sync error: %v", err)
	}
	return nil
}

func SaveCache() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := StatsSaveDB(ctx); err != nil {
		return err
	}
	if err := ChannelKeySaveDB(ctx); err != nil {
		return err
	}
	if err := RelayLogSaveDBTask(ctx); err != nil {
		return err
	}
	return nil
}
