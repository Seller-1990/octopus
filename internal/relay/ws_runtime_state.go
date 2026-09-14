package relay

import (
	"strings"
	"sync"
	"time"
)

const (
	wsResponseConnAffinityTTL = time.Hour
	// wsResponseConnPruneInterval 是惰性清理的节流间隔。
	wsResponseConnPruneInterval = time.Minute
)

type wsResponseConnBinding struct {
	connID    string
	expiresAt time.Time
}

var wsResponseConnState = struct {
	mu        sync.RWMutex
	bindings  map[string]wsResponseConnBinding
	lastPrune time.Time
}{bindings: make(map[string]wsResponseConnBinding)}

func bindWSResponseConn(responseID, connID string, ttl time.Duration) {
	responseID = strings.TrimSpace(responseID)
	connID = strings.TrimSpace(connID)
	if responseID == "" || connID == "" {
		return
	}
	if ttl <= 0 {
		ttl = wsResponseConnAffinityTTL
	}
	now := time.Now()
	wsResponseConnState.mu.Lock()
	// 惰性清理若每次 bind 都做，就是对全表的扫描：条目数随 TTL 内的响应数
	// 增长，bind 次数也随之增长，整体退化为 O(n²) 且全程持写锁。按时间节流
	// 后均摊为常数；过期条目最晚在下一个节流窗口被清掉，不影响 TTL 语义。
	if now.Sub(wsResponseConnState.lastPrune) >= wsResponseConnPruneInterval {
		pruneExpiredWSResponseConnBindingsLocked(now)
		wsResponseConnState.lastPrune = now
	}
	wsResponseConnState.bindings[responseID] = wsResponseConnBinding{connID: connID, expiresAt: now.Add(ttl)}
	wsResponseConnState.mu.Unlock()
}

// pruneExpiredWSResponseConnBindingsLocked deletes any binding whose expiresAt
// has passed. Must be called with wsResponseConnState.mu held for writing.
func pruneExpiredWSResponseConnBindingsLocked(now time.Time) {
	for id, b := range wsResponseConnState.bindings {
		if !b.expiresAt.IsZero() && now.After(b.expiresAt) {
			delete(wsResponseConnState.bindings, id)
		}
	}
}

func getWSResponseConn(responseID string) (string, bool) {
	responseID = strings.TrimSpace(responseID)
	if responseID == "" {
		return "", false
	}
	now := time.Now()
	wsResponseConnState.mu.RLock()
	binding, ok := wsResponseConnState.bindings[responseID]
	wsResponseConnState.mu.RUnlock()
	if !ok || strings.TrimSpace(binding.connID) == "" {
		return "", false
	}
	if !binding.expiresAt.IsZero() && now.After(binding.expiresAt) {
		wsResponseConnState.mu.Lock()
		if current, exists := wsResponseConnState.bindings[responseID]; exists &&
			!current.expiresAt.IsZero() && now.After(current.expiresAt) {
			delete(wsResponseConnState.bindings, responseID)
		}
		wsResponseConnState.mu.Unlock()
		return "", false
	}
	return binding.connID, true
}

func resetWSResponseConnStateForTest() {
	wsResponseConnState.mu.Lock()
	wsResponseConnState.bindings = make(map[string]wsResponseConnBinding)
	wsResponseConnState.lastPrune = time.Time{}
	wsResponseConnState.mu.Unlock()
}
