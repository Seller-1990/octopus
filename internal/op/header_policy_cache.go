package op

import (
	"context"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

// headerPolicyCacheTTL 控制启用态 HeaderPolicy 列表的缓存时间。该数据只在
// HeaderPolicyUpsert / HeaderPolicyDelete 时变化，写路径会显式清空缓存；
// TTL 仅用于兜底（例如备份导入等旁路写入场景）。
const headerPolicyCacheTTL = 3 * time.Second

type headerPolicyCacheEntry struct {
	items     []model.HeaderPolicy
	expiresAt time.Time
}

var headerPolicyCache = struct {
	sync.Mutex
	entry *headerPolicyCacheEntry
}{}

func enabledHeaderPolicies(ctx context.Context) ([]model.HeaderPolicy, error) {
	headerPolicyCache.Lock()
	if entry := headerPolicyCache.entry; entry != nil && time.Now().Before(entry.expiresAt) {
		items := entry.items
		headerPolicyCache.Unlock()
		return items, nil
	}
	headerPolicyCache.Unlock()

	var items []model.HeaderPolicy
	if err := db.GetDB().WithContext(ctx).
		Where("enabled = ?", true).
		Order("scope ASC, scope_id ASC").
		Find(&items).Error; err != nil {
		return nil, err
	}

	headerPolicyCache.Lock()
	headerPolicyCache.entry = &headerPolicyCacheEntry{
		items:     items,
		expiresAt: time.Now().Add(headerPolicyCacheTTL),
	}
	headerPolicyCache.Unlock()
	return items, nil
}

func clearHeaderPolicyCache() {
	headerPolicyCache.Lock()
	headerPolicyCache.entry = nil
	headerPolicyCache.Unlock()
}
