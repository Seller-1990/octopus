package toolsprobe

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/safe"
	"github.com/google/uuid"
)

// 批量 tools 测试（v3.1 R6）：POST /api/v1/group/tools-test 返回 202 + task_id，
// 前端轮询 /status/:task_id 看进度。对齐 group-health 先例（异步任务 + 轮询）。
// 批量上限防误触（每条 = 一次真实扣费请求）。

const (
	// MaxBatchItems 单次批量探测上限（R6 拍板，默认 20）。
	MaxBatchItems = 20
	// batchTaskTTL 任务保留时间，超过后清理（进程内状态，重启清零）。
	batchTaskTTL = 10 * time.Minute
)

// BatchItem 单条待测条目（channel_id + model_name 唯一）。
type BatchItem struct {
	ChannelID int    `json:"channel_id"`
	ModelName string `json:"model_name"`
}

// BatchItemResult 单条结果（五态 + 写回后的 supports/source；error 为探测失败文本）。
type BatchItemResult struct {
	ChannelID int    `json:"channel_id"`
	ModelName string `json:"model_name"`
	State     string `json:"state"`
	Supports  bool   `json:"supports"`
	Source    string `json:"source"`
	Error     string `json:"error,omitempty"`
}

// BatchTask 批量任务状态（前端轮询载体）。
type BatchTask struct {
	ID         string            `json:"task_id"`
	ToolChoice string            `json:"tool_choice"`
	Total      int               `json:"total"`
	Done       int               `json:"done"`
	Running    bool              `json:"running"`
	Results    []BatchItemResult `json:"results"`
	CreatedAt  time.Time         `json:"created_at"`
}

// batchManager 进程内批量任务管理器（map + mutex，TTL 过期清理）。
type batchManager struct {
	mu    sync.Mutex
	tasks map[string]*BatchTask
}

var defaultBatchManager = &batchManager{tasks: make(map[string]*BatchTask)}

func newTaskID() string {
	return uuid.NewString()
}

// StartBatch 启动一个批量 tools 探测任务。超过 MaxBatchItems 直接拒绝；
// 空列表拒绝。异步执行，写回按证据层级（ApplyToolsProbeResult 内部处理）。
func StartBatch(ctx context.Context, items []BatchItem, toolChoice string) (*BatchTask, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("items is empty")
	}
	if len(items) > MaxBatchItems {
		return nil, fmt.Errorf("batch size %d exceeds limit %d", len(items), MaxBatchItems)
	}
	if toolChoice != "required" {
		toolChoice = ""
	}
	task := &BatchTask{
		ID:         newTaskID(),
		ToolChoice: toolChoice,
		Total:      len(items),
		Running:    true,
		Results:    make([]BatchItemResult, 0, len(items)),
		CreatedAt:  time.Now(),
	}
	defaultBatchManager.mu.Lock()
	defaultBatchManager.tasks[task.ID] = task
	defaultBatchManager.mu.Unlock()

	safe.Go("tools-test-batch", func() {
		runCtx := context.Background()
		// defer 兜底：即使 runBatchItem 内部 panic（safe.Go 会 recover），
		// Running 也一定置 false，cleaner 才能回收任务、前端轮询才能结束（P2 修复）。
		defer func() {
			defaultBatchManager.mu.Lock()
			task.Running = false
			defaultBatchManager.mu.Unlock()
		}()
		for _, item := range items {
			runBatchItem(runCtx, task, item)
		}
	})
	// 返回快照副本，避免 handler 序列化 goroutine 仍在 append 的活指针（P0 修复）
	return BatchStatus(task.ID), nil
}

// batchProbeFn 批量任务的探测函数（测试可替换为 stub，避免真实扣费请求）。
var batchProbeFn = Run

// runBatchItem 执行单条探测并写回，推进任务进度。
func runBatchItem(ctx context.Context, task *BatchTask, item BatchItem) {
	defer func() {
		defaultBatchManager.mu.Lock()
		task.Done++
		defaultBatchManager.mu.Unlock()
	}()

	channel, err := op.ChannelGet(item.ChannelID, ctx)
	if err != nil {
		appendBatchResult(task, BatchItemResult{ChannelID: item.ChannelID, ModelName: item.ModelName, Error: "channel not found"})
		return
	}
	result, probeErr := batchProbeFn(ctx, *channel, item.ModelName, task.ToolChoice)
	if probeErr != nil {
		appendBatchResult(task, BatchItemResult{ChannelID: item.ChannelID, ModelName: item.ModelName, Error: probeErr.Error()})
		return
	}
	if err := op.ApplyToolsProbeResult(item.ChannelID, item.ModelName, result, firstEnabledKeyID(*channel)); err != nil {
		appendBatchResult(task, BatchItemResult{ChannelID: item.ChannelID, ModelName: item.ModelName, Error: err.Error()})
		return
	}
	appendBatchResult(task, BatchItemResult{
		ChannelID: item.ChannelID,
		ModelName: item.ModelName,
		State:     string(result.State),
		Supports:  result.Supports,
		Source:    result.Source,
	})
}

func appendBatchResult(task *BatchTask, r BatchItemResult) {
	defaultBatchManager.mu.Lock()
	task.Results = append(task.Results, r)
	defaultBatchManager.mu.Unlock()
}

// BatchStatus 返回任务状态副本（不存在返回 nil）。
func BatchStatus(taskID string) *BatchTask {
	defaultBatchManager.mu.Lock()
	defer defaultBatchManager.mu.Unlock()
	task, ok := defaultBatchManager.tasks[taskID]
	if !ok {
		return nil
	}
	cp := *task
	cp.Results = append([]BatchItemResult(nil), task.Results...)
	return &cp
}

// startBatchCleaner 定期清理过期任务（进程内状态，避免泄漏）。
func startBatchCleaner() {
	safe.Go("tools-test-batch-cleaner", func() {
		ticker := time.NewTicker(batchTaskTTL)
		defer ticker.Stop()
		for range ticker.C {
			now := time.Now()
			defaultBatchManager.mu.Lock()
			for id, t := range defaultBatchManager.tasks {
				if !t.Running && now.Sub(t.CreatedAt) > batchTaskTTL {
					delete(defaultBatchManager.tasks, id)
				}
			}
			defaultBatchManager.mu.Unlock()
		}
	})
}

func firstEnabledKeyID(channel model.Channel) *int {
	for _, k := range channel.Keys {
		if k.Enabled {
			id := k.ID
			return &id
		}
	}
	return nil
}

func init() {
	startBatchCleaner()
	log.Debugf("tools probe batch manager initialized")
}
