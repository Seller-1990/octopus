package balancer

import (
	"testing"
	"time"
)

func TestResetCircuitBreakerByChannelRemovesOnlyTargetChannel(t *testing.T) {
	Reset()
	globalBreaker.Store(circuitKey(1, 10, "gpt-4o"), &circuitEntry{
		State:           StateOpen,
		LastFailureTime: time.Now(),
		TripCount:       1,
	})
	globalBreaker.Store(circuitKey(10, 10, "gpt-4o"), &circuitEntry{
		State:           StateOpen,
		LastFailureTime: time.Now(),
		TripCount:       1,
	})
	globalBreaker.Store(circuitKey(2, 20, "gpt-4o"), &circuitEntry{
		State:           StateOpen,
		LastFailureTime: time.Now(),
		TripCount:       1,
	})

	ResetStateByChannel(1)

	if tripped, _ := IsTripped(1, 10, "gpt-4o"); tripped {
		t.Fatal("expected target channel circuit breaker to be reset")
	}
	if tripped, _ := IsTripped(10, 10, "gpt-4o"); !tripped {
		t.Fatal("expected channel with similar prefix to remain tripped")
	}
	if tripped, _ := IsTripped(2, 20, "gpt-4o"); !tripped {
		t.Fatal("expected unrelated channel circuit breaker to remain tripped")
	}
}

func TestResetStickyByChannelRemovesOnlyTargetChannel(t *testing.T) {
	Reset()
	SetSticky(1, "gpt-4o", 10, 100)
	SetSticky(2, "gpt-4o", 20, 200)
	SetSticky(3, "claude", 10, 300)

	ResetStateByChannel(10)

	if entry := GetSticky(1, "gpt-4o", time.Minute); entry != nil {
		t.Fatalf("expected target channel sticky session to be reset, got %#v", entry)
	}
	if entry := GetSticky(3, "claude", time.Minute); entry != nil {
		t.Fatalf("expected second target channel sticky session to be reset, got %#v", entry)
	}
	if entry := GetSticky(2, "gpt-4o", time.Minute); entry == nil || entry.ChannelID != 20 {
		t.Fatalf("expected unrelated sticky session to remain, got %#v", entry)
	}
}

func TestHalfOpenDoesNotRemainTrippedForeverWithoutResult(t *testing.T) {
	Reset()
	key := circuitKey(7, 8, "gpt-4o")
	globalBreaker.Store(key, &circuitEntry{
		State:         StateHalfOpen,
		TripCount:     1,
		HalfOpenSince: time.Now().Add(-61 * time.Second),
	})

	tripped, remaining := IsTripped(7, 8, "gpt-4o")
	if !tripped {
		t.Fatal("expected expired half-open probe to be tripped again")
	}
	if remaining <= 0 {
		t.Fatalf("expected expired half-open probe to return cooldown, got %v", remaining)
	}

	value, ok := globalBreaker.Load(key)
	if !ok {
		t.Fatal("expected circuit entry to remain after half-open timeout")
	}
	entry := value.(*circuitEntry)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.State != StateOpen {
		t.Fatalf("expected expired half-open entry to return to open, got %v", entry.State)
	}
	if !entry.HalfOpenSince.IsZero() {
		t.Fatalf("expected half-open timestamp to be cleared, got %v", entry.HalfOpenSince)
	}
}

// TestSnapshotExportsStructuredStatus 熔断管理面：Snapshot 导出结构化字段 + 冷却时间戳。
func TestSnapshotExportsStructuredStatus(t *testing.T) {
	Reset()
	globalBreaker.Store(circuitKey(3, 30, "claude-3"), &circuitEntry{
		State:               StateOpen,
		ConsecutiveFailures: 7,
		TripCount:           2,
		LastFailureTime:     time.Now().Add(-30 * time.Second),
	})

	items := Snapshot()
	found := false
	for _, it := range items {
		if it.ChannelID == 3 && it.ChannelKeyID == 30 && it.ModelName == "claude-3" {
			found = true
			if it.StateLabel != "open" {
				t.Fatalf("expected state_label=open, got %s", it.StateLabel)
			}
			if it.CooldownUntil.IsZero() {
				t.Fatal("expected open entry to have cooldown_until")
			}
			if it.ConsecutiveFailures != 7 || it.TripCount != 2 {
				t.Fatalf("expected failures=7 trips=2, got %d/%d", it.ConsecutiveFailures, it.TripCount)
			}
		}
	}
	if !found {
		t.Fatal("expected snapshot to include open circuit entry")
	}
}

// TestSnapshotLazilyPrunesIdleClosedEntries closed 噪音条目惰性清理（防单调膨胀）。
func TestSnapshotLazilyPrunesIdleClosedEntries(t *testing.T) {
	Reset()
	idleKey := circuitKey(4, 40, "stale-model")
	globalBreaker.Store(idleKey, &circuitEntry{
		State:           StateClosed,
		LastFailureTime: time.Now().Add(-30 * time.Minute),
	})
	freshKey := circuitKey(5, 50, "fresh-model")
	globalBreaker.Store(freshKey, &circuitEntry{
		State:           StateClosed,
		LastFailureTime: time.Now(),
	})

	Snapshot()

	if _, ok := globalBreaker.Load(idleKey); ok {
		t.Fatal("expected idle closed entry to be pruned by snapshot")
	}
	if _, ok := globalBreaker.Load(freshKey); !ok {
		t.Fatal("expected fresh closed entry to remain")
	}
}

// TestResetCircuitScopes ResetCircuit 粒度：item / channel / all。
func TestResetCircuitScopes(t *testing.T) {
	Reset()
	globalBreaker.Store(circuitKey(6, 60, "gpt-4o"), &circuitEntry{State: StateOpen, LastFailureTime: time.Now()})
	globalBreaker.Store(circuitKey(6, 61, "gpt-4o"), &circuitEntry{State: StateOpen, LastFailureTime: time.Now()})
	globalBreaker.Store(circuitKey(7, 70, "gpt-4o"), &circuitEntry{State: StateOpen, LastFailureTime: time.Now()})

	// item：精确重置一条
	ResetCircuit("", 6, 60, "gpt-4o")
	if _, ok := globalBreaker.Load(circuitKey(6, 60, "gpt-4o")); ok {
		t.Fatal("expected item reset to remove exact entry")
	}
	if _, ok := globalBreaker.Load(circuitKey(6, 61, "gpt-4o")); !ok {
		t.Fatal("expected sibling entry to remain after item reset")
	}

	// channel：按渠道前缀重置
	ResetCircuit("", 6, 0, "")
	if _, ok := globalBreaker.Load(circuitKey(6, 61, "gpt-4o")); ok {
		t.Fatal("expected channel reset to clear channel 6")
	}
	if _, ok := globalBreaker.Load(circuitKey(7, 70, "gpt-4o")); !ok {
		t.Fatal("expected channel 7 to remain after channel reset")
	}

	// all：全量
	ResetCircuit("all", 0, 0, "")
	if _, ok := globalBreaker.Load(circuitKey(7, 70, "gpt-4o")); ok {
		t.Fatal("expected all reset to clear everything")
	}
}

// TestSnapshotKeepsNearThresholdFailures P1 修复回归：低频故障节律（计数接近阈值但 >10min
// 无活动）不得被惰性清理抹掉——否则熔断对该渠道免疫（每次 Snapshot 后计数归零永不到阈值）。
func TestSnapshotKeepsNearThresholdFailures(t *testing.T) {
	Reset()
	key := circuitKey(9, 90, "gpt-4o")
	globalBreaker.Store(key, &circuitEntry{
		State:               StateClosed,
		ConsecutiveFailures: 4, // 接近阈值 5，仍有观察价值
		LastFailureTime:     time.Now().Add(-30 * time.Minute),
	})

	Snapshot()

	if _, ok := globalBreaker.Load(key); !ok {
		t.Fatal("expected near-threshold failure entry to survive snapshot cleanup")
	}
}

// TestSnapshotPrunesOnlyZeroFailures 仅失败计数为 0 的 closed 条目才被清理。
func TestSnapshotPrunesOnlyZeroFailures(t *testing.T) {
	Reset()
	idleKey := circuitKey(11, 110, "stale-model")
	globalBreaker.Store(idleKey, &circuitEntry{
		State:               StateClosed,
		ConsecutiveFailures: 0, // 完全健康
		LastFailureTime:     time.Now().Add(-30 * time.Minute),
	})
	accKey := circuitKey(12, 120, "accumulating")
	globalBreaker.Store(accKey, &circuitEntry{
		State:               StateClosed,
		ConsecutiveFailures: 2, // 有失败累计
		LastFailureTime:     time.Now().Add(-30 * time.Minute),
	})

	Snapshot()

	if _, ok := globalBreaker.Load(idleKey); ok {
		t.Fatal("expected zero-failure idle entry to be pruned")
	}
	if _, ok := globalBreaker.Load(accKey); !ok {
		t.Fatal("expected accumulating-failure entry to survive cleanup")
	}
}
