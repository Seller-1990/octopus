package task

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/sitesync"
	"github.com/bestruirui/octopus/internal/utils/log"
)

const verificationRetrySweepLimit = 10

// VerificationRetentionTask 每日物理删除超出保留期的验证会话/任务行。
// 保留期来自设置 verification_session_retention_days（默认 7 天）；
// 启动即执行一次，清掉升级前的存量积压。
func VerificationRetentionTask() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	days, err := op.SettingGetInt(model.SettingKeyVerificationSessionRetentionDays)
	if err != nil || days <= 0 {
		days = 7
	}
	deleted, err := op.VerificationRetentionCleanup(ctx, time.Now(), days)
	if err != nil {
		log.Warnf("verification retention cleanup failed: %v", err)
		return
	}
	if deleted > 0 {
		log.Infof("verification retention cleanup removed %d rows (retention %dd)", deleted, days)
	}
}

func VerificationRetryTask() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	if cleaned, err := op.VerificationSessionCleanup(ctx, time.Now()); err != nil {
		log.Warnf("verification credential cleanup failed: %v", err)
	} else if cleaned > 0 {
		log.Debugf("verification credential cleanup expired %d sessions", cleaned)
	}

	sessionIDs, err := op.VerificationRetryPendingSessionIDs(ctx, verificationRetrySweepLimit)
	if err != nil {
		log.Warnf("list pending verification retries failed: %v", err)
		return
	}
	for _, sessionID := range sessionIDs {
		runCtx, runCancel := context.WithTimeout(ctx, 3*time.Minute)
		err := sitesync.RetryVerificationSession(runCtx, sessionID)
		runCancel()
		if err != nil {
			log.Warnw(
				"pending verification retry failed",
				"session_id", sessionID,
				"error", err,
			)
		}
	}
}
