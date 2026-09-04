package task

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/price"
	"github.com/bestruirui/octopus/internal/site"
	"github.com/bestruirui/octopus/internal/utils/log"
)

const (
	TaskStatsSave              = "stats_save"
	TaskRelayLogSave           = "relay_log_save"
	TaskBaseUrlDelay           = "base_url_delay"
	TaskWSAffinityCleanup      = "ws_affinity_cleanup"
	TaskUsageMaintenance       = "usage_maintenance"
	TaskVerificationRetry      = "verification_retry"
	TaskVerificationRetention  = "verification_retention"
	TaskSub2APIRefresh         = "sub2api_refresh"
	TaskGroupItemOrphanCleanup = "group_item_orphan_cleanup"
)

func Init() {
	priceUpdateIntervalHours, err := op.SettingGetInt(model.SettingKeyModelInfoUpdateInterval)
	if err != nil {
		log.Errorf("failed to get model info update interval: %v", err)
		return
	}
	priceUpdateInterval := time.Duration(priceUpdateIntervalHours) * time.Hour
	// 注册价格更新任务
	Register(string(model.SettingKeyModelInfoUpdateInterval), priceUpdateInterval, true, func() {
		if err := price.UpdateLLMPrice(context.Background()); err != nil {
			log.Warnf("failed to update price info: %v", err)
		}
	})

	// 注册基础URL延迟任务
	Register(TaskBaseUrlDelay, 24*time.Hour, true, ChannelBaseUrlDelayTask)

	// 注册LLM同步任务
	syncLLMIntervalHours, err := op.SettingGetInt(model.SettingKeySyncLLMInterval)
	if err != nil {
		log.Warnf("failed to get sync LLM interval: %v", err)
		return
	}
	syncLLMInterval := time.Duration(syncLLMIntervalHours) * time.Hour
	Register(string(model.SettingKeySyncLLMInterval), syncLLMInterval, true, SyncModelsTask)

	siteSyncIntervalHours, err := op.SettingGetInt(model.SettingKeySiteSyncInterval)
	if err != nil {
		log.Warnf("failed to get site sync interval: %v", err)
		return
	}
	siteSyncInterval := time.Duration(siteSyncIntervalHours) * time.Hour
	Register(string(model.SettingKeySiteSyncInterval), siteSyncInterval, true, SiteSyncTask)

	siteCheckinIntervalHours, err := op.SettingGetInt(model.SettingKeySiteCheckinInterval)
	if err != nil {
		log.Warnf("failed to get site checkin interval: %v", err)
		return
	}
	siteCheckinInterval := time.Duration(siteCheckinIntervalHours) * time.Hour
	Register(string(model.SettingKeySiteCheckinInterval), siteCheckinInterval, true, SiteCheckinTask)

	// 注册统计保存任务
	statsSaveIntervalMinutes, err := op.SettingGetInt(model.SettingKeyStatsSaveInterval)
	if err != nil {
		log.Warnf("failed to get stats save interval: %v", err)
		return
	}
	statsSaveInterval := time.Duration(statsSaveIntervalMinutes) * time.Minute
	Register(TaskStatsSave, statsSaveInterval, false, op.StatsSaveDBTask)
	// 注册中继日志保存任务
	Register(TaskRelayLogSave, time.Hour, false, func() {
		if err := op.RelayLogSaveDBTask(context.Background()); err != nil {
			log.Warnf("relay log save db task failed: %v", err)
		}
	})

	Register(TaskWSAffinityCleanup, 10*time.Minute, false, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		deleted, err := op.WSResponseAffinityCleanup(ctx, time.Now())
		if err != nil {
			log.Warnf("ws response affinity cleanup failed: %v", err)
			return
		}
		if deleted > 0 {
			log.Debugf("ws response affinity cleanup removed %d expired rows", deleted)
		}
	})

	// N2：与 stats_save（同为 10 分钟周期、同相启动）错开 90s，
	// 避免两个周期写者每次同时抢 SQLite 写锁导致 usage 聚合整轮超时失败。
	RegisterWithPhase(TaskUsageMaintenance, 10*time.Minute, 90*time.Second, true, UsageMaintenanceTask)
	Register(TaskVerificationRetry, time.Minute, true, VerificationRetryTask)
	// N3：每日物理清理超出保留期的终态验证会话/任务行
	// （VerificationSessionCleanup 只标记不删除，行以 ~1K/日净增）。
	// runOnStart=true：升级后首次启动即清理存量积压。
	Register(TaskVerificationRetention, 24*time.Hour, true, VerificationRetentionTask)
	Register(TaskSub2APIRefresh, site.Sub2APIRefreshTaskInterval, true, Sub2APIRefreshTask)

	// 孤儿组成员兜底清理：正常删渠道时 channelDel 已同步删除其组成员；
	// 这里处理历史遗留/其他路径产生的「引用不存在渠道」的脏数据。
	Register(TaskGroupItemOrphanCleanup, 30*time.Minute, true, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		deleted, err := op.CleanupOrphanGroupItems(ctx)
		if err != nil {
			log.Warnf("group item orphan cleanup failed: %v", err)
			return
		}
		if deleted > 0 {
			log.Infof("group item orphan cleanup removed %d rows", deleted)
		}
	})

	// 注册被动离群退役(POR)任务（默认间隔 2 分钟，总开关在任务内判断）
	outlierIntervalMinutes, err := op.SettingGetInt(model.SettingKeyOutlierRetireInterval)
	if err != nil || outlierIntervalMinutes <= 0 {
		outlierIntervalMinutes = 2
	}
	Register(string(model.SettingKeyOutlierRetireInterval), time.Duration(outlierIntervalMinutes)*time.Minute, false, SiteOutlierRetireTask)

	// 注册 WebDAV 自动备份任务（间隔为 0 时不运行）
	webdavIntervalHours, err := op.SettingGetInt(model.SettingKeyWebDAVBackupInterval)
	if err != nil {
		log.Warnf("failed to get webdav backup interval: %v", err)
		webdavIntervalHours = 0
	}
	webdavInterval := time.Duration(webdavIntervalHours) * time.Hour
	Register(string(model.SettingKeyWebDAVBackupInterval), webdavInterval, false, WebDAVBackupTask)
}
