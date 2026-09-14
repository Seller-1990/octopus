package op

import (
	"context"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
)

const routeCandidateHealthWindow = 24 * time.Hour

type routeCandidatePerformance struct {
	RouteCandidateID  int   `gorm:"column:route_candidate_id"`
	SuccessCount      int64 `gorm:"column:success_count"`
	FailureCount      int64 `gorm:"column:failure_count"`
	SuccessDurationMS int64 `gorm:"column:success_duration_ms"`
}

func routeCandidatePerformanceMap(
	ctx context.Context,
	candidateIDs []int,
	now time.Time,
) (map[int]routeCandidatePerformance, error) {
	result := make(map[int]routeCandidatePerformance, len(candidateIDs))
	if len(candidateIDs) == 0 {
		return result, nil
	}
	rows, err := routeCandidatePerformanceRows(ctx, candidateIDs, now)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.RouteCandidateID] = row
	}
	return result, nil
}

// routeCandidatePerformanceMapAll 不按候选过滤，返回 24h 窗口内全部有流量候选
// 的聚合。无流量候选天然缺席，读取侧零值语义与按 ID 过滤版本一致。
func routeCandidatePerformanceMapAll(ctx context.Context, now time.Time) (map[int]routeCandidatePerformance, error) {
	rows, err := routeCandidatePerformanceRows(ctx, nil, now)
	if err != nil {
		return nil, err
	}
	result := make(map[int]routeCandidatePerformance, len(rows))
	for _, row := range rows {
		result[row.RouteCandidateID] = row
	}
	return result, nil
}

func routeCandidatePerformanceRows(ctx context.Context, candidateIDs []int, now time.Time) ([]routeCandidatePerformance, error) {
	query := db.GetDB().WithContext(ctx).
		Model(&model.UsageAttemptFact{}).
		Select(
			"route_candidate_id, "+
				"SUM(CASE WHEN status = ? THEN 1 ELSE 0 END) AS success_count, "+
				"SUM(CASE WHEN status = ? AND attribution = ? THEN 1 ELSE 0 END) AS failure_count, "+
				"SUM(CASE WHEN status = ? THEN duration_ms ELSE 0 END) AS success_duration_ms",
			model.AttemptSuccess,
			model.AttemptFailed,
			model.AttemptAttributionUpstream,
			model.AttemptSuccess,
		)
	if len(candidateIDs) > 0 {
		query = query.Where("route_candidate_id IN ?", candidateIDs)
	}
	var rows []routeCandidatePerformance
	err := query.Where("time >= ?", now.Add(-routeCandidateHealthWindow).Unix()).
		Group("route_candidate_id").
		Scan(&rows).Error
	return rows, err
}

// 选路评分消费的 24h 表现聚合是分钟级慢变量，但聚合查询随历史流量线性变重：
// 此前每个代理请求（CatalogPlanGroup）同步执行一次，请求延迟成为系统历史的
// 函数。热路径改为读带 TTL 的全量快照：过期后由后台单飞刷新、当次请求沿用
// 旧值，每个 TTL 窗口至多一次聚合；冷启动（无快照）同步加载一次，与旧路径
// 的每请求同步查询等价，仅发生一次。
const (
	routeCandidatePerfTTL           = 30 * time.Second
	routeCandidatePerfRefreshBudget = 30 * time.Second
)

var routeCandidatePerfCache struct {
	mu         sync.Mutex
	snapshot   map[int]routeCandidatePerformance
	loadedAt   time.Time
	refreshing bool
}

// routeCandidatePerformanceForPlan 供 CatalogPlanGroup 热路径使用。返回的
// 快照为共享只读数据，调用方不得修改。
func routeCandidatePerformanceForPlan(ctx context.Context) (map[int]routeCandidatePerformance, error) {
	c := &routeCandidatePerfCache
	c.mu.Lock()
	if c.snapshot == nil {
		// 冷启动：同步加载，并发请求在锁上排队等待同一份结果。
		defer c.mu.Unlock()
		fresh, err := routeCandidatePerformanceMapAll(ctx, time.Now())
		if err != nil {
			return nil, err
		}
		c.snapshot, c.loadedAt = fresh, time.Now()
		return fresh, nil
	}
	snap := c.snapshot
	expired := time.Since(c.loadedAt) > routeCandidatePerfTTL
	if expired && !c.refreshing {
		c.refreshing = true
		c.mu.Unlock()
		go func() {
			defer func() {
				c.mu.Lock()
				c.refreshing = false
				c.mu.Unlock()
			}()
			refreshCtx, cancel := context.WithTimeout(context.Background(), routeCandidatePerfRefreshBudget)
			defer cancel()
			fresh, err := routeCandidatePerformanceMapAll(refreshCtx, time.Now())
			if err != nil {
				log.Warnf("route candidate performance snapshot refresh failed: %v", err)
				return
			}
			c.mu.Lock()
			c.snapshot, c.loadedAt = fresh, time.Now()
			c.mu.Unlock()
		}()
		return snap, nil
	}
	c.mu.Unlock()
	return snap, nil
}

func RouteCandidateHealthRefresh(
	ctx context.Context,
	now time.Time,
	minSamples int64,
) (int64, error) {
	if minSamples <= 0 {
		minSamples = 5
	}
	var candidates []model.RouteCandidate
	if err := db.GetDB().WithContext(ctx).
		Where("manual = ? AND status IN ?", false, []model.RouteCandidateStatus{
			model.RouteCandidateActive,
			model.RouteCandidateDegraded,
		}).
		Find(&candidates).Error; err != nil {
		return 0, err
	}
	ids := make([]int, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID)
	}
	performance, err := routeCandidatePerformanceMap(ctx, ids, now)
	if err != nil {
		return 0, err
	}

	var updated int64
	for _, candidate := range candidates {
		stats := performance[candidate.ID]
		total := stats.SuccessCount + stats.FailureCount
		if total < minSamples {
			continue
		}
		status := model.RouteCandidateActive
		if stats.FailureCount*2 >= total {
			status = model.RouteCandidateDegraded
		}
		if status == candidate.Status {
			continue
		}
		result := db.GetDB().WithContext(ctx).
			Model(&model.RouteCandidate{}).
			Where("id = ? AND manual = ? AND status IN ?", candidate.ID, false, []model.RouteCandidateStatus{
				model.RouteCandidateActive,
				model.RouteCandidateDegraded,
			}).
			Update("status", status)
		if result.Error != nil {
			return updated, result.Error
		}
		updated += result.RowsAffected
	}
	// 热路径只读 catalog 缓存，健康度翻转（Active↔Degraded）必须回写缓存，
	// 否则 routeCandidateScore 的降级罚分在下次 CatalogSync 前不生效
	if updated > 0 {
		if err := catalogRefreshCache(ctx); err != nil {
			return updated, err
		}
	}
	return updated, nil
}

func CatalogRouteCandidatesMarkStaleByAccount(ctx context.Context, accountID int) error {
	if accountID <= 0 {
		return nil
	}
	result := db.GetDB().WithContext(ctx).
		Model(&model.RouteCandidate{}).
		Where(
			"site_account_id = ? AND manual = ? AND status IN ?",
			accountID,
			false,
			[]model.RouteCandidateStatus{
				model.RouteCandidateActive,
				model.RouteCandidateDegraded,
			},
		).
		Update("status", model.RouteCandidateStale)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return catalogRefreshCache(ctx)
	}
	return nil
}
