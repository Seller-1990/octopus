package task

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
)

func UsageMaintenanceTask() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := op.UsageFactsFlushPending(ctx); err != nil {
		log.Warnf("usage facts flush failed: %v", err)
		return
	}
	for batch := 0; batch < 20; batch++ {
		count, err := op.UsageFactsBackfill(ctx, 500)
		if err != nil {
			log.Warnf("usage facts backfill failed: %v", err)
			return
		}
		if count == 0 {
			break
		}
	}
	for batch := 0; batch < 100; batch++ {
		count, err := aggregateBatchWithRetry(ctx)
		if err != nil {
			log.Warnf("usage aggregate update failed after retries: %v", err)
			return
		}
		if count == 0 {
			break
		}
	}
	if deleted, err := op.UsageAggregateRetention(ctx, time.Now(), 0); err != nil {
		log.Warnf("usage aggregate retention failed: %v", err)
	} else if deleted > 0 {
		log.Debugf("usage aggregate retention removed %d hourly rows", deleted)
	}
	for batch := 0; batch < 20; batch++ {
		deleted, err := op.UsageFactsRetention(ctx, time.Now(), 0, 1000)
		if err != nil {
			log.Warnf("usage facts retention failed: %v", err)
			return
		}
		if deleted == 0 {
			break
		}
		log.Debugf("usage facts retention removed %d aggregated rows", deleted)
	}
	if updated, err := op.RouteCandidateHealthRefresh(ctx, time.Now(), 5); err != nil {
		log.Warnf("route candidate health refresh failed: %v", err)
	} else if updated > 0 {
		log.Debugf("route candidate health refresh updated %d candidates", updated)
	}
}

// aggregateBatchWithRetry 对单批 usage 聚合做有界重试。
// SQLite 单写者下偶发的写锁竞争（database is locked）会让整批事务回滚；
// 该事务内部已保证失败零副作用（claim 与 persist 同事务），重试即可，
// 无需人工干预。退避 2s/4s，总上限受任务 ctx（5 分钟）约束。
func aggregateBatchWithRetry(ctx context.Context) (int, error) {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if attempt > 1 {
			select {
			case <-time.After(time.Duration(1<<(attempt-1)) * 2 * time.Second):
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}
		count, err := op.UsageAggregatePending(ctx, 1000)
		if err == nil {
			return count, nil
		}
		lastErr = err
		log.Warnf("usage aggregate update failed (attempt %d/3): %v", attempt, err)
	}
	return 0, lastErr
}
