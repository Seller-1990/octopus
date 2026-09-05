package relay

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

// clearLiveLogState 清空包级实时日志状态。独立于 resetLiveLogState：
// cleanup 执行期间不得再注册新 cleanup，否则 runCleanup 无限循环。
func clearLiveLogState() {
	liveLogMu.Lock()
	liveLogRecords = make(map[int64]liveLogRecord)
	for ch := range liveOverviewSubs {
		close(ch)
	}
	liveOverviewSubs = make(map[chan LiveLog]struct{})
	for _, group := range liveDetailSubs {
		for ch := range group {
			close(ch)
		}
	}
	liveDetailSubs = make(map[int64]map[chan LiveAttempt]struct{})
	liveLogMu.Unlock()

	// 尝试中止句柄由 liveAttemptMu 单独管理，遗漏会跨测试泄漏 cancel 句柄。
	liveAttemptMu.Lock()
	liveAttempts = make(map[int64]liveAttemptControl)
	liveAttemptMu.Unlock()
}

// resetLiveLogState 清空实时日志状态并注册测试结束后的清理。
func resetLiveLogState(t *testing.T) {
	t.Helper()
	clearLiveLogState()
	t.Cleanup(clearLiveLogState)
}

func recvOverview(t *testing.T, ch chan LiveLog) LiveLog {
	t.Helper()
	select {
	case msg, ok := <-ch:
		if !ok {
			t.Fatal("overview channel closed unexpectedly")
		}
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for overview event")
		return LiveLog{}
	}
}

func expectClosed(t *testing.T, ch chan LiveLog, label string) {
	t.Helper()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatalf("%s: expected closed channel, got event", label)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("%s: timed out waiting for channel close", label)
	}
}

func TestLiveOverviewMultipleSubscribers(t *testing.T) {
	resetLiveLogState(t)

	_, ch1 := OpenLiveOverview()
	_, ch2 := OpenLiveOverview()

	startLiveLog(1, time.Now(), "gpt-4")
	if msg := recvOverview(t, ch1); msg.ID != 1 || msg.State != LiveRequestRunning {
		t.Fatalf("subscriber 1 got unexpected event: %+v", msg)
	}
	if msg := recvOverview(t, ch2); msg.ID != 1 || msg.State != LiveRequestRunning {
		t.Fatalf("subscriber 2 got unexpected event: %+v", msg)
	}

	CloseLiveOverview(ch1)
	expectClosed(t, ch1, "closed subscriber")

	// 已注销的订阅者不再收到事件，其余订阅者不受影响。
	startLiveLog(2, time.Now(), "gpt-4")
	if msg := recvOverview(t, ch2); msg.ID != 2 {
		t.Fatalf("surviving subscriber got unexpected event: %+v", msg)
	}
	expectClosed(t, ch1, "unsubscribed channel")
	CloseLiveOverview(ch2)
	// 对已注销/未知通道重复注销必须幂等。
	CloseLiveOverview(ch1)
	CloseLiveOverview(ch2)
}

func TestLiveOverviewSlowSubscriberKicked(t *testing.T) {
	resetLiveLogState(t)

	_, slow := OpenLiveOverview()
	_, active := OpenLiveOverview()

	// 先把两个订阅者的 buffer 都填满（不触发踢出）。
	for i := 0; i < liveOverviewBufferSize; i++ {
		liveLogMu.Lock()
		sendLiveOverviewLocked(LiveLog{ID: int64(i + 1), State: LiveRequestRunning})
		liveLogMu.Unlock()
	}

	// 启动活跃订阅者消费，并等待第一条被消费（腾出 buffer 空间），
	// 避免并发调度下两个订阅者同时满而被一起踢出。
	consumed := make(chan struct{})
	var received atomic.Int32
	go func() {
		for range active {
			if received.Add(1) == 1 {
				close(consumed)
			}
		}
	}()
	select {
	case <-consumed:
	case <-time.After(2 * time.Second):
		t.Fatal("active subscriber did not consume any event")
	}

	// 再发 1 条：慢订阅者 buffer 满被踢，活跃订阅者有空间继续接收。
	liveLogMu.Lock()
	sendLiveOverviewLocked(LiveLog{ID: int64(liveOverviewBufferSize + 1), State: LiveRequestRunning})
	liveLogMu.Unlock()

	liveLogMu.Lock()
	_, stillSubscribed := liveOverviewSubs[slow]
	liveLogMu.Unlock()
	if stillSubscribed {
		t.Fatal("slow subscriber should have been kicked after buffer overflow")
	}

	CloseLiveOverview(active)
	// 等待消费 goroutine 处理完 close 前缓冲的全部事件。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := received.Load(); got == int32(liveOverviewBufferSize+1) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("active subscriber received %d events, want %d", received.Load(), liveOverviewBufferSize+1)
}

func TestLiveDetailSubscriberLifecycle(t *testing.T) {
	resetLiveLogState(t)

	startLiveLog(100, time.Now(), "gpt-4")

	ch1, ok := OpenLiveDetail(100)
	if !ok {
		t.Fatal("OpenLiveDetail returned false for running request")
	}
	ch2, ok := OpenLiveDetail(100)
	if !ok {
		t.Fatal("second OpenLiveDetail returned false for running request")
	}

	// 两个订阅者都应收到尝试事件（旧单观众架构下 ch2 会踢掉 ch1）。
	liveLogAttemptStarted(100, 1, "channel-a", "gpt-4", nil)
	for label, ch := range map[string]chan LiveAttempt{"sub1": ch1, "sub2": ch2} {
		select {
		case update, ok := <-ch:
			if !ok || update.Type != LiveEventAttemptStarted {
				t.Fatalf("%s got unexpected update: %+v", label, update)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s timed out waiting for attempt event", label)
		}
	}

	// 请求终结：组内全部订阅者被关闭。
	liveLogOutcome(100, model.RequestOutcomeSuccess, nil, "channel-a", "gpt-4", 1, 1, 0, 0, 0, nil, nil, nil)
	for label, ch := range map[string]chan LiveAttempt{"sub1": ch1, "sub2": ch2} {
		select {
		case _, ok := <-ch:
			if ok {
				t.Fatalf("%s: expected closed detail channel", label)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: timed out waiting for detail close", label)
		}
	}

	// 终结后再注销：成员守卫必须保证幂等，不得 double close panic。
	CloseLiveDetail(100, ch1)
	CloseLiveDetail(100, ch2)
	CloseLiveDetail(100, ch1)
	CloseLiveDetail(999, ch1)
}

func TestLiveDetailRejectsFinishedRequest(t *testing.T) {
	resetLiveLogState(t)

	startLiveLog(200, time.Now(), "gpt-4")
	liveLogOutcome(200, model.RequestOutcomeIndeterminate, nil, "", "", 1, 1, 0, 0, 0, nil, nil, nil)

	if _, ok := OpenLiveDetail(200); ok {
		t.Fatal("OpenLiveDetail should reject finished request")
	}

	record, ok := liveLogRecords[200]
	if !ok {
		t.Fatal("record missing after outcome")
	}
	if record.State != LiveRequestIndeterminate {
		t.Fatalf("state = %q, want indeterminate", record.State)
	}
	if !isLiveFinished(record.State) {
		t.Fatal("indeterminate should count as finished")
	}
}
