package notify

import (
	"sync"
	"testing"
	"time"
)

// N1 并发单测（验证纪律：照抄 livelog_test 模式，-race 下运行）。

func TestBusPublishFanout(t *testing.T) {
	bus := NewBus()
	s1, ch1 := bus.Subscribe()
	s2, ch2 := bus.Subscribe()
	if len(s1) != 0 || len(s2) != 0 {
		t.Fatal("fresh subscribers should get empty snapshot")
	}

	published := bus.Publish(Event{Type: TypeSiteBatch, Level: LevelSuccess, Title: "done"})
	if published.ID == 0 {
		t.Fatal("publish must assign monotonic id")
	}
	for name, ch := range map[string]chan Event{"s1": ch1, "s2": ch2} {
		select {
		case got := <-ch:
			if got.ID != published.ID || got.Title != published.Title {
				t.Fatalf("%s got wrong event: %+v", name, got)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s did not receive event", name)
		}
	}
	bus.Unsubscribe(ch1)
	bus.Unsubscribe(ch2)
	// 幂等
	bus.Unsubscribe(ch1)
}

func TestBusSlowSubscriberKicked(t *testing.T) {
	bus := NewBus()
	_, slow := bus.Subscribe()
	_, healthy := bus.Subscribe()

	// 灌满慢订阅者的缓冲
	for i := 0; i < subscriberBuffer+1; i++ {
		bus.Publish(Event{Type: TypeSiteBatch, Title: "spam"})
	}
	// 慢订阅者应被踢出：先读出缓冲内残余，随后通道关闭
	for {
		_, ok := <-slow
		if !ok {
			break
		}
	}
	select {
	case got := <-healthy:
		if got.Title != "spam" {
			t.Fatalf("healthy subscriber got wrong event: %+v", got)
		}
	default:
		t.Fatal("healthy subscriber should still receive events")
	}
	bus.Unsubscribe(healthy)
}

func TestBusRingRotation(t *testing.T) {
	bus := NewBus()
	total := ringCapacity + 30
	for i := 0; i < total; i++ {
		bus.Publish(Event{Type: TypeSiteBatch, Title: "event"})
	}
	snapshot, ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)
	if len(snapshot) != ringCapacity {
		t.Fatalf("snapshot size = %d, want %d", len(snapshot), ringCapacity)
	}
	// 最旧被淘汰：快照第一条 ID = total-ringCapacity+1
	if snapshot[0].ID != int64(total-ringCapacity+1) {
		t.Fatalf("oldest id = %d, want %d", snapshot[0].ID, total-ringCapacity+1)
	}
	if snapshot[len(snapshot)-1].ID != int64(total) {
		t.Fatalf("newest id = %d, want %d", snapshot[len(snapshot)-1].ID, total)
	}
}

func TestBusSnapshotThenIncrement(t *testing.T) {
	bus := NewBus()
	bus.Publish(Event{Type: TypeSiteBatch, Title: "before"})
	snapshot, ch := bus.Subscribe()
	if len(snapshot) != 1 {
		t.Fatalf("snapshot = %d events, want 1", len(snapshot))
	}
	bus.Publish(Event{Type: TypeSiteBatch, Title: "after"})
	select {
	case got := <-ch:
		if got.Title != "after" {
			t.Fatalf("increment got %q", got.Title)
		}
	case <-time.After(time.Second):
		t.Fatal("no incremental event after snapshot")
	}
	bus.Unsubscribe(ch)
}

func TestBusConcurrentPublishSubscribe(t *testing.T) {
	bus := NewBus()
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				snapshot, ch := bus.Subscribe()
				_ = snapshot
				select {
				case <-ch:
					bus.Unsubscribe(ch)
				case <-stop:
					bus.Unsubscribe(ch)
					return
				}
			}
		}()
	}
	for i := 0; i < 200; i++ {
		bus.Publish(Event{Type: TypeSiteBatch, Title: "concurrent"})
	}
	close(stop)
	wg.Wait()
}
