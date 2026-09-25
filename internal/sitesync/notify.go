package sitesync

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/notify"
)

// 任务结果通知埋点（PLAN_TASK_NOTIFY N4）。通知不是数据、失败不影响主流程：
// 所有发布都是 fire-and-forget，任何错误都不向上传播。

func publishBatchSummaryNotify(summary *SiteBatchSummary) {
	if summary == nil || summary.Total == 0 {
		return
	}
	level := notify.LevelSuccess
	if summary.Canceled {
		level = notify.LevelWarn
	} else if summary.Failed > 0 {
		level = notify.LevelWarn
	}
	phase := "同步"
	if summary.Phase == SiteBatchPhaseCheckin {
		phase = "签到"
	}
	title := fmt.Sprintf("批量%s完成", phase)
	if summary.Canceled {
		title = fmt.Sprintf("批量%s已取消", phase)
	}
	body := fmt.Sprintf("成功 %d / 失败 %d / 跳过 %d / 共 %d（触发：%s）",
		summary.Success, summary.Failed, summary.Skipped, summary.Total, summary.Trigger)
	notify.Default.Publish(notify.Event{
		Type:  notify.TypeSiteBatch,
		Level: level,
		Title: title,
		Body:  body,
		Data: map[string]string{
			"phase":   string(summary.Phase),
			"trigger": string(summary.Trigger),
		},
	})
}

func publishCheckinFailedNotify(siteRecord *model.Site, account *model.SiteAccount, message string) {
	if siteRecord == nil || account == nil {
		return
	}
	notify.Default.Publish(notify.Event{
		Type:  notify.TypeSiteCheckinFailed,
		Level: notify.LevelError,
		Title: fmt.Sprintf("签到失败：%s / %s", siteRecord.Name, account.Name),
		Body:  message,
		Data: map[string]string{
			"site_id":    fmt.Sprintf("%d", siteRecord.ID),
			"account_id": fmt.Sprintf("%d", account.ID),
		},
	})
}

func publishSyncFailedNotify(siteRecord *model.Site, account *model.SiteAccount, message string) {
	if siteRecord == nil || account == nil {
		return
	}
	notify.Default.Publish(notify.Event{
		Type:  notify.TypeSiteSyncFailed,
		Level: notify.LevelError,
		Title: fmt.Sprintf("同步失败：%s / %s", siteRecord.Name, account.Name),
		Body:  message,
		Data: map[string]string{
			"site_id":    fmt.Sprintf("%d", siteRecord.ID),
			"account_id": fmt.Sprintf("%d", account.ID),
		},
	})
}

func publishVerificationRetryNotify(success bool, accountID int, message string) {
	level := notify.LevelSuccess
	title := "验证重试成功"
	if !success {
		level = notify.LevelWarn
		title = "验证重试失败"
	}
	notify.Default.Publish(notify.Event{
		Type:  notify.TypeVerificationTask,
		Level: level,
		Title: title,
		Body:  message,
		Data: map[string]string{
			"account_id": fmt.Sprintf("%d", accountID),
		},
	})
}
