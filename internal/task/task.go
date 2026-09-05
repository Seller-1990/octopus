package task

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/safe"
)

type taskEntry struct {
	name        string
	fn          func()
	runOnStart  bool
	phase       time.Duration
	ticker      *time.Ticker
	stopCh      chan struct{}
	doneCh      chan struct{}
	updateCh    chan struct{}
	stopOnce    sync.Once
	interval    atomic.Int64
	loopStarted atomic.Bool
	running     atomic.Bool
}

var (
	tasks            = make(map[string]*taskEntry)
	tasksMu          sync.RWMutex
	schedulerStarted atomic.Bool
)

// Register 注册一个定时任务
// runOnStart: 是否在启动时立即执行一次
func Register(name string, interval time.Duration, runOnStart bool, fn func()) {
	registerTask(name, interval, 0, runOnStart, fn)
}

// RegisterWithPhase 注册带相位偏移的定时任务：首个 ticker 事件延后 phase
// 再触发。用于错开同周期任务，避免多个周期写者同相启动、每次同时抢
// SQLite 写锁（N2：stats_save 与 usage_maintenance 同为 10 分钟且同相，
// 观测到 usage 聚合每轮 busy_timeout 超时失败）。
func RegisterWithPhase(name string, interval, phase time.Duration, runOnStart bool, fn func()) {
	registerTask(name, interval, phase, runOnStart, fn)
}

func registerTask(name string, interval, phase time.Duration, runOnStart bool, fn func()) {
	tasksMu.Lock()
	if _, exists := tasks[name]; exists {
		tasksMu.Unlock()
		log.Warnf("task %s already registered, skipping", name)
		return
	}

	entry := &taskEntry{
		name:       name,
		fn:         fn,
		runOnStart: runOnStart,
		phase:      phase,
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
		updateCh:   make(chan struct{}, 1),
	}
	entry.interval.Store(int64(max(interval, 0)))
	tasks[name] = entry
	tasksMu.Unlock()

	if schedulerStarted.Load() {
		startTaskLoop(entry)
	}
	log.Debugf("task %s registered with interval %v, phase %v, runOnStart: %v", name, interval, phase, runOnStart)
}

// Update 更新任务的执行间隔
// 当 interval 为 0 时暂停任务；后续设置为正数会复用原函数并重新启动。
func Update(name string, interval time.Duration) {
	tasksMu.RLock()
	entry, exists := tasks[name]
	tasksMu.RUnlock()
	if !exists {
		log.Warnf("task %s not found", name)
		return
	}

	entry.interval.Store(int64(max(interval, 0)))
	select {
	case entry.updateCh <- struct{}{}:
		log.Infof("task %s interval updated to %v", name, interval)
	default:
		// A pending notification will read the latest atomic interval.
	}
}

// RUN 启动所有已注册任务。
// 调用方（cmd/start.go）在独立 goroutine 中执行；各任务循环自带生命周期，
// 启动后 RUN 直接返回——不再 park 一个无意义的常驻 goroutine（F21），
// 主流程由 shutdown.Listen 阻塞。
func RUN() {
	startScheduler()
}

func startScheduler() {
	schedulerStarted.Store(true)
	tasksMu.RLock()
	for _, entry := range tasks {
		startTaskLoop(entry)
	}
	tasksMu.RUnlock()
}

func startTaskLoop(entry *taskEntry) {
	if entry == nil || !entry.loopStarted.CompareAndSwap(false, true) {
		return
	}
	safe.Go("task-loop:"+entry.name, func() {
		runTask(entry)
	})
}

func runTask(entry *taskEntry) {
	defer close(entry.doneCh)

	// 根据配置决定是否在启动时立即执行
	if entry.runOnStart && time.Duration(entry.interval.Load()) > 0 {
		triggerTask(entry, "startup")
	}

	// 相位偏移：先等 phase 再启动 ticker，使同周期任务彼此错开
	// （仅启动时生效；Update 重置 ticker 沿用既有相位语义）。
	if entry.phase > 0 {
		select {
		case <-time.After(entry.phase):
		case <-entry.stopCh:
			return
		}
	}

	var tickerC <-chan time.Time
	resetTicker := func() {
		if entry.ticker != nil {
			entry.ticker.Stop()
			entry.ticker = nil
		}
		interval := time.Duration(entry.interval.Load())
		if interval > 0 {
			entry.ticker = time.NewTicker(interval)
			tickerC = entry.ticker.C
		} else {
			tickerC = nil
		}
	}
	resetTicker()
	defer func() {
		if entry.ticker != nil {
			entry.ticker.Stop()
		}
	}()

	for {
		select {
		case <-tickerC:
			triggerTask(entry, "ticker")
		case <-entry.updateCh:
			resetTicker()
		case <-entry.stopCh:
			return
		}
	}
}

func triggerTask(entry *taskEntry, trigger string) {
	if entry == nil {
		return
	}
	if !entry.running.CompareAndSwap(false, true) {
		log.Warnf("task %s skipped: previous run still in progress (trigger=%s)", entry.name, trigger)
		return
	}
	safe.Go("task-exec:"+entry.name+":"+trigger, func() {
		defer entry.running.Store(false)
		entry.fn()
	})
}
