package op

import (
	"context"
	"reflect"
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

// resetRouteCandidatePerfCacheForTest 清空表现快照缓存。op 的各测试共用
// setupBackupTestDB 换库，进程级快照若不重置会把上一个测试的聚合结果泄漏
// 给下一个测试（setupBackupTestDB 已接入）。
func resetRouteCandidatePerfCacheForTest() {
	routeCandidatePerfCache.mu.Lock()
	routeCandidatePerfCache.snapshot = nil
	routeCandidatePerfCache.loadedAt = time.Time{}
	routeCandidatePerfCache.refreshing = false
	routeCandidatePerfCache.mu.Unlock()
}

func seedAttemptFact(t *testing.T, ctx context.Context, candidateID int, status model.AttemptStatus, attribution model.AttemptAttribution, durationMS int64) {
	t.Helper()
	fact := model.UsageAttemptFact{
		RelayLogID:       time.Now().UnixNano(),
		AttemptNumber:    1,
		Time:             time.Now().Unix(),
		RouteCandidateID: candidateID,
		Status:           status,
		Attribution:      attribution,
		DurationMS:       durationMS,
		Outcome:          model.RequestOutcomeSuccess,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&fact).Error; err != nil {
		t.Fatalf("seed attempt fact failed: %v", err)
	}
}

// TestRouteCandidatePerfSnapshotColdStartAndCacheHit 快照冷启动加载真实聚合，
// 且 TTL 内的后续调用命中同一份快照（不重复查库）。
func TestRouteCandidatePerfSnapshotColdStartAndCacheHit(t *testing.T) {
	ctx := setupBackupTestDB(t)

	seedAttemptFact(t, ctx, 1, model.AttemptSuccess, model.AttemptAttributionUpstream, 100)
	seedAttemptFact(t, ctx, 1, model.AttemptSuccess, model.AttemptAttributionUpstream, 200)
	seedAttemptFact(t, ctx, 1, model.AttemptSuccess, model.AttemptAttributionUpstream, 300)
	seedAttemptFact(t, ctx, 1, model.AttemptFailed, model.AttemptAttributionUpstream, 0)
	seedAttemptFact(t, ctx, 2, model.AttemptSuccess, model.AttemptAttributionUpstream, 50)

	snap1, err := routeCandidatePerformanceForPlan(ctx)
	if err != nil {
		t.Fatalf("first snapshot load failed: %v", err)
	}
	perf1, ok := snap1[1]
	if !ok {
		t.Fatal("expected performance entry for candidate 1")
	}
	if perf1.SuccessCount != 3 || perf1.FailureCount != 1 || perf1.SuccessDurationMS != 600 {
		t.Fatalf("unexpected aggregation for candidate 1: %+v", perf1)
	}
	if _, ok := snap1[2]; !ok {
		t.Fatal("expected performance entry for candidate 2")
	}

	snap2, err := routeCandidatePerformanceForPlan(ctx)
	if err != nil {
		t.Fatalf("second snapshot load failed: %v", err)
	}
	if reflect.ValueOf(snap1).Pointer() != reflect.ValueOf(snap2).Pointer() {
		t.Fatal("expected cached snapshot to be reused within TTL (no re-query)")
	}
}

// TestRouteCandidatePerfMapFilteredMatchesAll 按 ID 过滤版本与全量版本对同一
// 候选给出一致结果（共享查询构建器的口径守卫）。
func TestRouteCandidatePerfMapFilteredMatchesAll(t *testing.T) {
	ctx := setupBackupTestDB(t)

	seedAttemptFact(t, ctx, 7, model.AttemptSuccess, model.AttemptAttributionUpstream, 120)
	seedAttemptFact(t, ctx, 7, model.AttemptFailed, model.AttemptAttributionUpstream, 0)

	filtered, err := routeCandidatePerformanceMap(ctx, []int{7}, time.Now())
	if err != nil {
		t.Fatalf("filtered map failed: %v", err)
	}
	all, err := routeCandidatePerformanceMapAll(ctx, time.Now())
	if err != nil {
		t.Fatalf("all map failed: %v", err)
	}
	if filtered[7] != all[7] {
		t.Fatalf("filtered and all disagree for candidate 7: %+v vs %+v", filtered[7], all[7])
	}

	// 无流量的候选在两个版本中都缺席（零值语义由读取侧提供）
	if _, ok := all[999]; ok {
		t.Fatal("candidate without facts must be absent from the map")
	}
}
