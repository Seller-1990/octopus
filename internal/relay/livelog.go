package relay

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

// LiveRequestState 表示请求在实时日志中的状态。
type LiveRequestState string

const (
	LiveRequestRunning       LiveRequestState = "running"
	LiveRequestSuccess       LiveRequestState = "success"
	LiveRequestFailed        LiveRequestState = "failed"
	LiveRequestCanceled      LiveRequestState = "canceled"
	LiveRequestIndeterminate LiveRequestState = "indeterminate"
)

// LiveLogEvent 表示实时日志生命周期事件类型。
type LiveLogEvent string

const (
	LiveEventAttemptStarted  LiveLogEvent = "attempt.started"
	LiveEventAttemptFinished LiveLogEvent = "attempt.finished"
)

// LiveAttempt 表示一次渠道尝试的实时状态。
type LiveAttempt struct {
	Type         LiveLogEvent `json:"-"`
	AttemptIndex int          `json:"attempt_index"`
	ChannelName  string       `json:"channel_name"`
	ModelName    string       `json:"model_name"`
	Error        string       `json:"error"`
}

// LiveLog 表示一条可持续更新的请求概览。
type LiveLog struct {
	ID               int64            `json:"id"`
	State            LiveRequestState `json:"state"`
	StartedAt        time.Time        `json:"started_at"`
	CompletedAt      time.Time        `json:"completed_at,omitempty"`
	DurationMS       int64            `json:"duration_ms"`
	RequestModelName string           `json:"request_model_name"`
	ActualModelName  string           `json:"actual_model_name"`
	ChannelName      string           `json:"channel_name"`
	InputTokens      int64            `json:"input_tokens"`
	OutputTokens     int64            `json:"output_tokens"`
	CacheReadTokens  int64            `json:"cache_read_tokens"`
	CacheWriteTokens int64            `json:"cache_write_tokens"`
	TotalCost        float64          `json:"total_cost"`
	Error            string           `json:"error,omitempty"`

	// 终态补充字段：与 DB 历史日志一致，供前端在实时列表里继续展示
	// key 倍率与完整的 key 切换记录（仅请求完成后写入）。
	PriceGroupMultiplier      *float64               `json:"price_group_multiplier,omitempty"`
	PriceGroupMultiplierKnown *bool                  `json:"price_group_multiplier_known,omitempty"`
	Attempts                  []model.ChannelAttempt `json:"attempts,omitempty"`
}

type liveLogRecord struct {
	LiveLog
	currentAttempt *LiveAttempt
}

type liveAttemptControl struct {
	index  int
	cancel context.CancelFunc
}

const (
	liveOverviewBufferSize = 16
	liveDetailBufferSize   = 16
	liveMaxFinishedRecords = 200
)

var (
	liveLogMu sync.Mutex
	// liveLogRecords 保留运行中与近期完成的请求；订阅者集合支持多个
	// 管理窗口同时观察（此前单 channel 架构下新连接会踢掉旧连接）。
	liveLogRecords = make(map[int64]liveLogRecord)
	liveOverviewSubs = make(map[chan LiveLog]struct{})
	liveDetailSubs   = make(map[int64]map[chan LiveAttempt]struct{})

	liveAttemptMu sync.Mutex
	liveAttempts  = make(map[int64]liveAttemptControl)
)

func isLiveFinished(state LiveRequestState) bool {
	return state == LiveRequestSuccess || state == LiveRequestFailed ||
		state == LiveRequestCanceled || state == LiveRequestIndeterminate
}

// startLiveLog 创建一条 running 状态的实时日志并广播概览。
func startLiveLog(id int64, startedAt time.Time, requestModel string) {
	liveLogMu.Lock()
	liveLogRecords[id] = liveLogRecord{LiveLog: LiveLog{
		ID:               id,
		State:            LiveRequestRunning,
		StartedAt:        startedAt,
		RequestModelName: requestModel,
	}}
	sendLiveOverviewLocked(liveLogRecords[id].LiveLog)
	liveLogMu.Unlock()
}

// failLiveLog 将尚未结束的实时日志标记为 failed。
func failLiveLog(id int64, err error) {
	liveLogMu.Lock()
	record, ok := liveLogRecords[id]
	if !ok || isLiveFinished(record.State) {
		liveLogMu.Unlock()
		return
	}
	liveLogMu.Unlock()

	finishLiveLog(id, LiveRequestFailed, err)
}

// finishLiveLogIfRunning 兜底结束仍处于 running 状态的实时日志。
func finishLiveLogIfRunning(id int64, message string) {
	liveLogMu.Lock()
	record, ok := liveLogRecords[id]
	if !ok || isLiveFinished(record.State) {
		liveLogMu.Unlock()
		return
	}
	liveLogMu.Unlock()

	var err error
	if message != "" {
		err = &liveLogFinishError{message: message}
	}
	finishLiveLog(id, LiveRequestFailed, err)
}

// liveLogFinishError 包装兜底结束时的错误信息。
type liveLogFinishError struct{ message string }

func (e *liveLogFinishError) Error() string { return e.message }

// liveLogOutcome 在请求完成时更新实时日志的终态与统计字段。
func liveLogOutcome(
	id int64,
	outcome model.RequestOutcome,
	err error,
	channelName string,
	actualModel string,
	inputTokens int64,
	outputTokens int64,
	cacheReadTokens int64,
	cacheWriteTokens int64,
	totalCost float64,
	attempts []model.ChannelAttempt,
	priceGroupMultiplier *float64,
	priceGroupMultiplierKnown *bool,
) {
	if id == 0 {
		return
	}
	state := LiveRequestFailed
	switch outcome {
	case model.RequestOutcomeSuccess:
		state = LiveRequestSuccess
	case model.RequestOutcomeClientCanceled:
		state = LiveRequestCanceled
	case model.RequestOutcomeIndeterminate:
		state = LiveRequestIndeterminate
	}
	liveLogMu.Lock()
	record, ok := liveLogRecords[id]
	if !ok || isLiveFinished(record.State) {
		liveLogMu.Unlock()
		return
	}
	record.State = state
	record.CompletedAt = time.Now()
	record.DurationMS = record.CompletedAt.Sub(record.StartedAt).Milliseconds()
	record.ChannelName = channelName
	record.ActualModelName = actualModel
	record.InputTokens = inputTokens
	record.OutputTokens = outputTokens
	record.CacheReadTokens = cacheReadTokens
	record.CacheWriteTokens = cacheWriteTokens
	record.TotalCost = totalCost
	record.Attempts = attempts
	record.PriceGroupMultiplier = priceGroupMultiplier
	record.PriceGroupMultiplierKnown = priceGroupMultiplierKnown
	if state == LiveRequestSuccess {
		record.Error = ""
	} else if err != nil {
		record.Error = err.Error()
	}
	record.currentAttempt = nil
	liveLogRecords[id] = record
	sendLiveOverviewLocked(record.LiveLog)
	trimLiveRecordsLocked()
	closeLiveDetailLocked(id)
	liveLogMu.Unlock()
}

// liveLogAttemptStarted 广播一次尝试开始，并登记可中止句柄。
func liveLogAttemptStarted(id int64, index int, channelName, modelName string, cancel context.CancelFunc) {
	attempt := &LiveAttempt{Type: LiveEventAttemptStarted, AttemptIndex: index, ChannelName: channelName, ModelName: modelName}

	liveAttemptMu.Lock()
	liveAttempts[id] = liveAttemptControl{index: index, cancel: cancel}
	liveAttemptMu.Unlock()

	liveLogMu.Lock()
	record, ok := liveLogRecords[id]
	if !ok {
		liveLogMu.Unlock()
		return
	}
	record.currentAttempt = attempt
	record.ActualModelName = modelName
	record.ChannelName = channelName
	record.Error = ""
	liveLogRecords[id] = record
	sendLiveOverviewLocked(record.LiveLog)
	sendLiveDetailLocked(id, *attempt)
	liveLogMu.Unlock()
}

// liveLogAttemptFinished 广播一次尝试结束，并清理可中止句柄。
func liveLogAttemptFinished(id int64, index int, channelName, modelName string, err error) {
	attempt := &LiveAttempt{Type: LiveEventAttemptFinished, AttemptIndex: index, ChannelName: channelName, ModelName: modelName}
	if err != nil {
		attempt.Error = err.Error()
	}

	liveLogMu.Lock()
	record, ok := liveLogRecords[id]
	if !ok {
		liveLogMu.Unlock()
		return
	}
	if record.currentAttempt != nil && record.currentAttempt.AttemptIndex == index {
		record.currentAttempt = nil
	}
	if err != nil {
		record.Error = err.Error()
	}
	liveLogRecords[id] = record
	sendLiveOverviewLocked(record.LiveLog)
	sendLiveDetailLocked(id, *attempt)
	liveLogMu.Unlock()
}

// finishLiveLog 以指定终态结束实时日志。
func finishLiveLog(id int64, state LiveRequestState, err error) {
	liveLogMu.Lock()
	record, ok := liveLogRecords[id]
	if !ok || isLiveFinished(record.State) {
		liveLogMu.Unlock()
		return
	}
	record.State = state
	record.CompletedAt = time.Now()
	record.DurationMS = record.CompletedAt.Sub(record.StartedAt).Milliseconds()
	if state == LiveRequestSuccess {
		record.Error = ""
	} else if err != nil {
		record.Error = err.Error()
	}
	record.currentAttempt = nil
	liveLogRecords[id] = record
	sendLiveOverviewLocked(record.LiveLog)
	trimLiveRecordsLocked()
	closeLiveDetailLocked(id)
	liveLogMu.Unlock()
}

// InterruptLiveAttempt 中止指定请求当前序号匹配的上游尝试。
func InterruptLiveAttempt(id int64, index int) {
	liveAttemptMu.Lock()
	control, ok := liveAttempts[id]
	if !ok || control.index != index {
		liveAttemptMu.Unlock()
		return
	}
	delete(liveAttempts, id)
	liveAttemptMu.Unlock()
	control.cancel()
}

// clearLiveAttempt 移除序号匹配的尝试句柄，返回 false 表示中止操作已经先发生。
func clearLiveAttempt(id int64, index int) bool {
	liveAttemptMu.Lock()
	defer liveAttemptMu.Unlock()
	control, ok := liveAttempts[id]
	if !ok || control.index != index {
		return false
	}
	delete(liveAttempts, id)
	return true
}

// OpenLiveOverview 返回当前快照和该订阅者专属的更新通道。
// 多订阅者并存：每个连接持有独立 channel，互不干扰。
func OpenLiveOverview() ([]LiveLog, chan LiveLog) {
	liveLogMu.Lock()
	defer liveLogMu.Unlock()

	ch := make(chan LiveLog, liveOverviewBufferSize)
	liveOverviewSubs[ch] = struct{}{}
	return liveOverviewSnapshotLocked(), ch
}

// CloseLiveOverview 注销并关闭指定订阅者通道；对未知或已注销通道幂等。
func CloseLiveOverview(ch chan LiveLog) {
	liveLogMu.Lock()
	defer liveLogMu.Unlock()

	if _, ok := liveOverviewSubs[ch]; ok {
		delete(liveOverviewSubs, ch)
		close(ch)
	}
}

// OpenLiveDetail 为运行中的请求打开详情流，并补发当前运行尝试。
func OpenLiveDetail(id int64) (chan LiveAttempt, bool) {
	liveLogMu.Lock()
	defer liveLogMu.Unlock()

	record, ok := liveLogRecords[id]
	if !ok || isLiveFinished(record.State) {
		return nil, false
	}
	ch := make(chan LiveAttempt, liveDetailBufferSize)
	group, ok := liveDetailSubs[id]
	if !ok {
		group = make(map[chan LiveAttempt]struct{})
		liveDetailSubs[id] = group
	}
	group[ch] = struct{}{}
	if record.currentAttempt != nil {
		ch <- *record.currentAttempt
	}
	return ch, true
}

// CloseLiveDetail 注销指定请求详情流中的订阅者通道。
// 必须带成员资格守卫：请求终结时 closeLiveDetailLocked 已关闭整个组，
// handler 的 defer 再调用本函数时不得重复 close。
func CloseLiveDetail(id int64, ch chan LiveAttempt) {
	liveLogMu.Lock()
	defer liveLogMu.Unlock()

	group, ok := liveDetailSubs[id]
	if !ok {
		return
	}
	if _, member := group[ch]; !member {
		return
	}
	delete(group, ch)
	close(ch)
	if len(group) == 0 {
		delete(liveDetailSubs, id)
	}
}

func liveOverviewSnapshotLocked() []LiveLog {
	overviews := make([]LiveLog, 0, len(liveLogRecords))
	for _, record := range liveLogRecords {
		overviews = append(overviews, record.LiveLog)
	}
	sort.Slice(overviews, func(i, j int) bool {
		return overviews[i].ID > overviews[j].ID
	})
	return overviews
}

func trimLiveRecordsLocked() {
	finished := 0
	oldestID := int64(0)
	for id, record := range liveLogRecords {
		if !isLiveFinished(record.State) {
			continue
		}
		finished++
		if oldestID == 0 || id < oldestID {
			oldestID = id
		}
	}
	if finished > liveMaxFinishedRecords {
		delete(liveLogRecords, oldestID)
	}
}

func sendLiveOverviewLocked(message LiveLog) {
	for ch := range liveOverviewSubs {
		select {
		case ch <- message:
		default:
			// 慢消费者踢出：先注销再 close（对其他订阅者无影响）；
			// 其 SSE handler 读到 closed 即结束连接，前端退避重连取新快照。
			delete(liveOverviewSubs, ch)
			close(ch)
		}
	}
}

func sendLiveDetailLocked(id int64, update LiveAttempt) {
	group, ok := liveDetailSubs[id]
	if !ok {
		return
	}
	for ch := range group {
		select {
		case ch <- update:
		default:
			// 拥塞时丢弃该订阅者积压的最旧更新，保留最新状态。
			for {
				select {
				case <-ch:
					continue
				default:
				}
				break
			}
			ch <- update
		}
	}
}

func closeLiveDetailLocked(id int64) {
	group, ok := liveDetailSubs[id]
	if !ok {
		return
	}
	delete(liveDetailSubs, id)
	for ch := range group {
		close(ch)
	}
}
