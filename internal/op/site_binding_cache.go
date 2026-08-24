package op

import (
	"context"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

// siteChannelBindingCacheTTL 是 binding 缓存的兜底过期时间。正常写入路径都会
// 调用 InvalidateSiteChannelBindingCache 精准失效；TTL 仅用于旁路写入（如未
// 覆盖到的新增写入点）时的最终一致兜底。
const siteChannelBindingCacheTTL = 5 * time.Minute

type siteChannelBindingCacheEntry struct {
	items     []model.SiteChannelBinding
	expiresAt time.Time
}

var siteChannelBindingCache = struct {
	sync.Mutex
	entry *siteChannelBindingCacheEntry
}{}

func allSiteChannelBindings(ctx context.Context) ([]model.SiteChannelBinding, error) {
	siteChannelBindingCache.Lock()
	if entry := siteChannelBindingCache.entry; entry != nil && time.Now().Before(entry.expiresAt) {
		items := entry.items
		siteChannelBindingCache.Unlock()
		return items, nil
	}
	siteChannelBindingCache.Unlock()

	var items []model.SiteChannelBinding
	if err := db.GetDB().WithContext(ctx).Find(&items).Error; err != nil {
		return nil, err
	}

	siteChannelBindingCache.Lock()
	siteChannelBindingCache.entry = &siteChannelBindingCacheEntry{
		items:     items,
		expiresAt: time.Now().Add(siteChannelBindingCacheTTL),
	}
	siteChannelBindingCache.Unlock()
	return items, nil
}

func clearSiteChannelBindingCache() {
	siteChannelBindingCache.Lock()
	siteChannelBindingCache.entry = nil
	siteChannelBindingCache.Unlock()
}

// InvalidateSiteChannelBindingCache 清空绑定缓存。sitesync 与 op 的所有绑定
// 写入路径在事务提交后调用此函数，保证缓存与数据库一致。
func InvalidateSiteChannelBindingCache() {
	clearSiteChannelBindingCache()
}
