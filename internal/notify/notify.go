// Package notify 提供任务结果的进程内通知总线（PLAN_TASK_NOTIFY N1）。
// 内存 ring buffer（最近 100 条）+ 扇出订阅；通知不是数据，重启丢失可接受，
// 不入库存——避免重蹈 verification_sessions 高水位膨胀的覆辙。
package notify

import (
	"sync"
	"time"
)

// Level 事件级别。
type Level string

const (
	LevelInfo    Level = "info"
	LevelSuccess Level = "success"
	LevelWarn    Level = "warn"
	LevelError   Level = "error"
)

// 事件类型（N4 埋点使用）。
const (
	TypeSiteBatch         = "site.batch"
	TypeSiteCheckinFailed = "site.checkin.failed"
	TypeSiteSyncFailed    = "site.sync.failed"
	TypeVerificationTask  = "verification.task"
)

const (
	ringCapacity     = 100
	subscriberBuffer = 16
)

// Event 单条通知。ID 单调递增，前端用它做去重与未读水位。
type Event struct {
	ID    int64             `json:"id"`
	Type  string            `json:"type"`
	Level Level             `json:"level"`
	Title string            `json:"title"`
	Body  string            `json:"body"`
	Time  time.Time         `json:"time"`
	Data  map[string]string `json:"data,omitempty"`
}

// Bus 通知总线：ring buffer + 扇出订阅。
type Bus struct {
	mu     sync.Mutex
	nextID int64
	ring   []Event
	subs   map[chan Event]struct{}
}

// Default 全局单例（不引入 DI，包级单例即可）。
var Default = NewBus()

// NewBus 创建总线。
func NewBus() *Bus {
	return &Bus{
		ring: make([]Event, 0, ringCapacity),
		subs: make(map[chan Event]struct{}),
	}
}

// Publish 发布事件：写 ring + 扇出全部订阅者。慢消费者（订阅 chan 满）被
// 踢出——与 relay/livelog 同策略：先注销再 close，其他订阅者不受影响，
// 该连接的 SSE 读到关闭即结束，前端退避重连取新快照。
func (b *Bus) Publish(event Event) Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	event.ID = b.nextID
	if event.Time.IsZero() {
		event.Time = time.Now()
	}
	if len(b.ring) >= ringCapacity {
		// 滚动淘汰最旧
		copy(b.ring, b.ring[len(b.ring)-ringCapacity+1:])
		b.ring = b.ring[:ringCapacity-1]
	}
	b.ring = append(b.ring, event)
	for ch := range b.subs {
		select {
		case ch <- event:
		default:
			delete(b.subs, ch)
			close(ch)
		}
	}
	// Webhook 在锁外异步投递（5s 超时），不得阻塞订阅者扇出。
	go dispatchWebhookAsync(event)
	return event
}

// Subscribe 返回当前快照与该订阅者专属的更新通道。建连先补发快照再增量，
// 与 SSE 快照语义一致。
func (b *Bus) Subscribe() ([]Event, chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan Event, subscriberBuffer)
	b.subs[ch] = struct{}{}
	snapshot := make([]Event, len(b.ring))
	copy(snapshot, b.ring)
	return snapshot, ch
}

// Unsubscribe 注销订阅者通道；对未知或已注销通道幂等。
func (b *Bus) Unsubscribe(ch chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.subs[ch]; ok {
		delete(b.subs, ch)
		close(ch)
	}
}
