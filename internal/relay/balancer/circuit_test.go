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

// TestRecordFailureInOpenDoesNotExtendCooldown F06 回归：熔断 Open 态收到
// 在途慢失败时不得顺延冷却起点，否则 Open -> HalfOpen 的恢复探测可被
// 无限推迟（高并发 + 慢失败下这是常态而非例外）。
func TestRecordFailureInOpenDoesNotExtendCooldown(t *testing.T) {
	Reset()
	const (
		channelID = 21
		keyID     = 22
		modelName = "gpt-4o"
	)

	// 连续失败达到默认阈值（设置缺失时 getThreshold 回落 5）触发熔断
	for i := 0; i < 5; i++ {
		RecordFailure(channelID, keyID, modelName, FailureHard)
	}
	entryV, ok := globalBreaker.Load(circuitKey(channelID, keyID, modelName))
	if !ok {
		t.Fatal("expected circuit entry to exist after threshold failures")
	}
	opened := entryV.(*circuitEntry)
	opened.mu.Lock()
	if opened.State != StateOpen {
		t.Fatalf("expected StateOpen, got %v", opened.State)
	}
	coolStart := opened.LastFailureTime
	tripCount := opened.TripCount
	opened.mu.Unlock()

	if tripped, _ := IsTripped(channelID, keyID, modelName); !tripped {
		t.Fatal("expected circuit to be tripped while cooling down")
	}

	// 模拟熔断前已发出、此刻才返回的在途慢失败
	time.Sleep(5 * time.Millisecond)
	RecordFailure(channelID, keyID, modelName, FailureHard)
	RecordFailure(channelID, keyID, modelName, FailureSoftRateLimit)

	// 注意：不可在持有 entry.mu 时调用 Snapshot()（其内部会再锁同一把锁），
	// 先在锁内取值，释放后再做全表断言。
	opened.mu.Lock()
	afterTime := opened.LastFailureTime
	afterTrips := opened.TripCount
	cooldownUntil := opened.LastFailureTime.Add(GetCooldown(afterTrips))
	opened.mu.Unlock()

	if !afterTime.Equal(coolStart) {
		t.Fatalf("cooldown start must not move on in-flight failure in Open state: before=%v after=%v",
			coolStart, afterTime)
	}
	if afterTrips != tripCount {
		t.Fatalf("trip count must not change on in-flight failure: before=%d after=%d",
			tripCount, afterTrips)
	}

	// Snapshot 的 CooldownUntil 必须仍以原冷却起点推导
	for _, status := range Snapshot() {
		if status.ChannelID == channelID && status.ChannelKeyID == keyID && status.ModelName == modelName {
			if !status.CooldownUntil.Equal(cooldownUntil) {
				t.Fatalf("CooldownUntil drifted: want %v got %v", cooldownUntil, status.CooldownUntil)
			}
			return
		}
	}
	t.Fatal("expected snapshot to contain the open circuit entry")
}

// TestSoftRateLimitTripsCircuitAfterStreak P1-5 回归：429/503 软失败连续达到
// 阈值必须熔断——软失败此前永不计数，限流渠道在 Failover 定序下每个请求都
// 要白撞一次，熔断器形同虚设。
func TestSoftRateLimitTripsCircuitAfterStreak(t *testing.T) {
	Reset()
	const (
		channelID = 41
		keyID     = 42
		modelName = "gpt-4o-mini"
	)
	// 阈值以下保持 Closed
	for i := 0; i < 4; i++ {
		RecordFailure(channelID, keyID, modelName, FailureSoftRateLimit)
	}
	if tripped, _ := IsTripped(channelID, keyID, modelName); tripped {
		t.Fatal("expected circuit to stay closed below soft threshold")
	}
	// 达到默认阈值（5）后熔断
	RecordFailure(channelID, keyID, modelName, FailureSoftRateLimit)
	if tripped, _ := IsTripped(channelID, keyID, modelName); !tripped {
		t.Fatal("expected circuit to trip after soft-failure streak")
	}
	// 成功后软失败计数随全量重置清零
	RecordSuccess(channelID, keyID, modelName)
	entryV, ok := globalBreaker.Load(circuitKey(channelID, keyID, modelName))
	if !ok {
		t.Fatal("expected entry to survive RecordSuccess")
	}
	entry := entryV.(*circuitEntry)
	entry.mu.Lock()
	soft := entry.ConsecutiveSoftFailures
	state := entry.State
	entry.mu.Unlock()
	if soft != 0 || state != StateClosed {
		t.Fatalf("expected clean reset on success, got soft=%d state=%v", soft, state)
	}
}

// TestRecordFailureStillOpensFromClosed 达到阈值仍正常转 Open、试探失败仍
// 刷新冷却起点（防修复矫枉过正）。
func TestRecordFailureStillOpensFromClosed(t *testing.T) {
	Reset()
	const (
		channelID = 31
		keyID     = 32
		modelName = "claude-3"
	)
	for i := 0; i < 5; i++ {
		RecordFailure(channelID, keyID, modelName, FailureHard)
	}
	if tripped, _ := IsTripped(channelID, keyID, modelName); !tripped {
		t.Fatal("expected circuit to trip after threshold failures from Closed")
	}
	entryV, _ := globalBreaker.Load(circuitKey(channelID, keyID, modelName))
	entry := entryV.(*circuitEntry)
	entry.mu.Lock()
	openedAt := entry.LastFailureTime
	entry.mu.Unlock()

	// 冷却未过，此时再失败仍停在 Open（不走 HalfOpen）；为验证 HalfOpen 探测
	// 失败路径，直接把冷却起点拨到过去使 IsTripped 放行探测窗口
	entry.mu.Lock()
	entry.LastFailureTime = time.Now().Add(-2 * time.Hour)
	entry.mu.Unlock()
	if tripped, _ := IsTripped(channelID, keyID, modelName); tripped {
		t.Fatal("expected Open -> HalfOpen after cooldown elapsed")
	}
	time.Sleep(5 * time.Millisecond)
	RecordFailure(channelID, keyID, modelName, FailureHard) // HalfOpen 探测失败
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.State != StateOpen {
		t.Fatalf("expected probe failure to return to Open, got %v", entry.State)
	}
	if !entry.LastFailureTime.After(openedAt) {
		t.Fatalf("HalfOpen probe failure must refresh cooldown start: before=%v after=%v",
			openedAt, entry.LastFailureTime)
	}
}
