package op

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/cache"
	"github.com/bestruirui/octopus/internal/utils/log"
	"gorm.io/gorm"
)

var apiKeyCache = cache.New[int, model.APIKey](16)
var apiKeyIDMap = cache.New[string, int](16)

// apiKeyQuotaLocks 按 key 串行化配额读改写。原全局互斥锁让所有成功请求的
// 配额落账完全串行，是热路径上不必要的争用点。
var apiKeyQuotaLocks sync.Map // map[int]*sync.Mutex

func lockAPIKeyQuota(id int) func() {
	v, _ := apiKeyQuotaLocks.LoadOrStore(id, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

const defaultAPIKeyQuotaPeriod = "monthly"

func normalizeAPIKeyQuotaPeriod(period string) (string, error) {
	period = strings.ToLower(strings.TrimSpace(period))
	if period == "" {
		return defaultAPIKeyQuotaPeriod, nil
	}
	switch period {
	case "daily", "weekly", "monthly":
		return period, nil
	default:
		return "", fmt.Errorf("quota_period must be daily, weekly, or monthly")
	}
}

// ValidateAPIKeyQuota validates the public quota settings accepted by the API.
func ValidateAPIKeyQuota(limit float64, period string) error {
	if limit < 0 {
		return fmt.Errorf("quota_limit must be non-negative")
	}
	_, err := normalizeAPIKeyQuotaPeriod(period)
	return err
}

func APIKeyCreate(key *model.APIKey, ctx context.Context) error {
	period, err := normalizeAPIKeyQuotaPeriod(key.QuotaPeriod)
	if err != nil {
		return err
	}
	if key.QuotaLimit < 0 {
		return fmt.Errorf("quota_limit must be non-negative")
	}
	key.QuotaPeriod = period
	key.QuotaUsed = 0
	if key.QuotaLimit > 0 {
		key.QuotaResetAt = computeNextQuotaReset(period, time.Now())
	} else {
		key.QuotaResetAt = 0
	}
	if err := db.GetDB().WithContext(ctx).Create(key).Error; err != nil {
		return fmt.Errorf("failed to create API key: %w", err)
	}
	apiKeyCache.Set(key.ID, *key)
	apiKeyIDMap.Set(key.APIKey, key.ID)
	return nil
}

func APIKeyUpdate(key *model.APIKey, ctx context.Context) error {
	unlock := lockAPIKeyQuota(key.ID)
	defer unlock()
	existing, ok := apiKeyCache.Get(key.ID)
	if !ok {
		return fmt.Errorf("API key not found")
	}
	period, err := normalizeAPIKeyQuotaPeriod(key.QuotaPeriod)
	if err != nil {
		return err
	}
	if key.QuotaLimit < 0 {
		return fmt.Errorf("quota_limit must be non-negative")
	}
	key.APIKey = existing.APIKey
	key.QuotaPeriod = period
	if key.QuotaLimit <= 0 {
		key.QuotaUsed = 0
		key.QuotaResetAt = 0
	} else if existing.QuotaLimit <= 0 || existing.QuotaPeriod != period {
		key.QuotaUsed = 0
		key.QuotaResetAt = computeNextQuotaReset(period, time.Now())
	} else {
		key.QuotaUsed = existing.QuotaUsed
		key.QuotaResetAt = existing.QuotaResetAt
		if key.QuotaResetAt == 0 {
			key.QuotaResetAt = computeNextQuotaReset(period, time.Now())
		}
	}
	if err := db.GetDB().WithContext(ctx).Omit("api_key").Save(key).Error; err != nil {
		return fmt.Errorf("failed to update API key: %w", err)
	}
	apiKeyCache.Set(key.ID, *key)
	return nil
}

func APIKeyList(ctx context.Context) ([]model.APIKey, error) {
	keys := make([]model.APIKey, 0, apiKeyCache.Len())
	for _, apiKey := range apiKeyCache.GetAll() {
		keys = append(keys, apiKey)
	}
	return keys, nil
}

func APIKeyGet(id int, ctx context.Context) (model.APIKey, error) {
	apiKey, ok := apiKeyCache.Get(id)
	if !ok {
		return model.APIKey{}, fmt.Errorf("API key not found")
	}
	return apiKey, nil
}

func APIKeyGetByAPIKey(apiKey string, ctx context.Context) (model.APIKey, error) {
	id, ok := apiKeyIDMap.Get(apiKey)
	if !ok {
		return model.APIKey{}, fmt.Errorf("API key not found")
	}
	return APIKeyGet(id, ctx)
}

func APIKeyDelete(id int, ctx context.Context) error {
	k, ok := apiKeyCache.Get(id)
	if !ok {
		return fmt.Errorf("API key not found")
	}
	result := db.GetDB().WithContext(ctx).Delete(&k)
	// Error 必须先于 RowsAffected 检查：DB 故障时 RowsAffected 恒为 0，
	// 先查行数会把真实错误误报成 "API key not found"（F15）。
	if result.Error != nil {
		return fmt.Errorf("failed to delete API key: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("API key not found")
	}
	// 行删除成功后再清统计：避免 DELETE 失败时 stats 已丢失
	// （旧序 StatsAPIKeyDel 在前，键还在但统计已删）。
	if err := StatsAPIKeyDel(id); err != nil {
		log.Warnf("failed to delete stats for API key %d after row removal: %v", id, err)
	}
	RateLimitDel(id)
	apiKeyCache.Del(k.ID)
	apiKeyIDMap.Del(k.APIKey)
	return nil
}

func apiKeyRefreshCache(ctx context.Context) error {
	apiKeys := []model.APIKey{}
	if err := db.GetDB().WithContext(ctx).Find(&apiKeys).Error; err != nil {
		return err
	}
	apiKeyCache.Clear()
	apiKeyIDMap.Clear()
	for _, apiKey := range apiKeys {
		apiKeyCache.Set(apiKey.ID, apiKey)
		apiKeyIDMap.Set(apiKey.APIKey, apiKey.ID)
	}
	return nil
}

func computeNextQuotaReset(period string, now time.Time) int64 {
	local := now.In(now.Location())
	switch period {
	case "daily":
		next := local.AddDate(0, 0, 1)
		return time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, local.Location()).Unix()
	case "weekly":
		daysUntilMonday := (8 - int(local.Weekday())) % 7
		if daysUntilMonday == 0 {
			daysUntilMonday = 7
		}
		next := local.AddDate(0, 0, daysUntilMonday)
		return time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, local.Location()).Unix()
	case "monthly":
		return time.Date(local.Year(), local.Month()+1, 1, 0, 0, 0, 0, local.Location()).Unix()
	default:
		return time.Date(local.Year(), local.Month()+1, 1, 0, 0, 0, 0, local.Location()).Unix()
	}
}

// APIKeyResetQuota 条件重置 API key 配额:锁内重读 DB 当前状态,仅当
// reset_at 仍等于调用方所见快照(seenResetAt)时才真正重置;不匹配说明
// 周期已被并发请求推进或被管理员修改,此时放弃重置(幂等),也不再让
// 调用方携带的旧周期参数覆盖当前设置——周期一律取锁内 DB 当前值。
// 无论是否重置,都返回重检后的实际状态,调用方不得假设「已重置且 used=0」。
func APIKeyResetQuota(ctx context.Context, id int, seenResetAt int64, now time.Time) (model.APIKey, error) {
	var current model.APIKey
	unlock := lockAPIKeyQuota(id)
	defer unlock()
	if err := db.GetDB().WithContext(ctx).First(&current, id).Error; err != nil {
		return current, fmt.Errorf("failed to load API key for quota reset: %w", err)
	}
	if current.QuotaResetAt == seenResetAt {
		period, err := normalizeAPIKeyQuotaPeriod(current.QuotaPeriod)
		if err != nil {
			return current, err
		}
		nextReset := computeNextQuotaReset(period, now)
		if err := db.GetDB().WithContext(ctx).Model(&model.APIKey{}).Where("id = ?", id).Updates(map[string]interface{}{
			"quota_used":     0,
			"quota_reset_at": nextReset,
			"quota_period":   period,
		}).Error; err != nil {
			return current, fmt.Errorf("failed to reset API key quota: %w", err)
		}
		current.QuotaUsed = 0
		current.QuotaResetAt = nextReset
		current.QuotaPeriod = period
	}
	if _, ok := apiKeyCache.Get(id); ok {
		apiKeyCache.Set(id, current)
	}
	return current, nil
}

func APIKeyIncrementQuotaUsed(ctx context.Context, id int, cost float64) error {
	if id == 0 || cost <= 0 {
		return nil
	}
	unlock := lockAPIKeyQuota(id)
	defer unlock()
	if err := db.GetDB().WithContext(ctx).Model(&model.APIKey{}).Where("id = ?", id).Update("quota_used", gorm.Expr("quota_used + ?", cost)).Error; err != nil {
		return fmt.Errorf("failed to increment API key quota: %w", err)
	}
	// DB 侧已是原子自增，缓存侧在锁内做同样增量即可，无需回读 SELECT——
	// 那是每请求多付的一次 DB 往返，且曾在 SQLite 单连接下放大排队。
	if key, ok := apiKeyCache.Get(id); ok {
		key.QuotaUsed += cost
		apiKeyCache.Set(id, key)
	}
	return nil
}
