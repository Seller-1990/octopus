package visionbridge

import (
	"container/list"
	"sync"
	"time"
)

// analysisCache 进程内 LRU + TTL 缓存。只缓存派生文本与不可逆的图片身份哈希，
// 不缓存图片字节；URL 引用内容可变故用短 TTL，data URI 内容寻址故用长 TTL。
type analysisCache struct {
	mu       sync.Mutex
	capacity int
	entries  map[string]*list.Element
	order    *list.List // 头部 = 最近使用
	now      func() time.Time
}

type cacheEntry struct {
	key       string
	text      string
	expiresAt time.Time
}

func newAnalysisCache(capacity int) *analysisCache {
	if capacity <= 0 {
		capacity = 128
	}
	return &analysisCache{
		capacity: capacity,
		entries:  make(map[string]*list.Element, capacity),
		order:    list.New(),
		now:      time.Now,
	}
}

func (c *analysisCache) Get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[key]
	if !ok {
		return "", false
	}
	entry := el.Value.(*cacheEntry)
	if c.now().After(entry.expiresAt) {
		c.order.Remove(el)
		delete(c.entries, key)
		return "", false
	}
	c.order.MoveToFront(el)
	return entry.text, true
}

func (c *analysisCache) Set(key, text string, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key]; ok {
		entry := el.Value.(*cacheEntry)
		entry.text = text
		entry.expiresAt = c.now().Add(ttl)
		c.order.MoveToFront(el)
		return
	}
	for len(c.entries) >= c.capacity {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		c.order.Remove(oldest)
		delete(c.entries, oldest.Value.(*cacheEntry).key)
	}
	c.entries[key] = c.order.PushFront(&cacheEntry{
		key:       key,
		text:      text,
		expiresAt: c.now().Add(ttl),
	})
}
