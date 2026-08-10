package toolsprobe

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

// setupBatchTestDB 为 toolsprobe 包测试初始化独立 sqlite DB（对齐 op 包测试先例）。
func setupBatchTestDB(t *testing.T) context.Context {
	t.Helper()
	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}
	dbPath := filepath.Join(t.TempDir(), "toolsprobe-test.db")
	if err := dbpkg.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = dbpkg.Close() })
	return context.Background()
}

// createBatchFixture 创建渠道+分组+条目，返回渠道 ID。
func createBatchFixture(t *testing.T, ctx context.Context) (int, int) {
	t.Helper()
	ch := model.Channel{Name: "batch-channel", Model: "batch-model", Enabled: true}
	if err := op.ChannelCreate(&ch, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	group := model.Group{Name: "batch-group", Mode: model.GroupModeFailover}
	if err := op.GroupCreate(&group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	item := model.GroupItem{GroupID: group.ID, ChannelID: ch.ID, ModelName: "batch-model", Priority: 1, Weight: 1}
	if err := op.GroupItemAdd(&item, ctx); err != nil {
		t.Fatalf("create group item: %v", err)
	}
	return ch.ID, item.ID
}

func waitForBatchTask(t *testing.T, taskID string, timeout time.Duration) *BatchTask {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		task := BatchStatus(taskID)
		if task != nil && !task.Running {
			return task
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("batch task %s did not finish within %v", taskID, timeout)
	return nil
}

// TestStartBatchRejectsOversize 批量上限（v3.1 R6：默认 20）。
func TestStartBatchRejectsOversize(t *testing.T) {
	ctx := setupBatchTestDB(t)
	items := make([]BatchItem, MaxBatchItems+1)
	for i := range items {
		items[i] = BatchItem{ChannelID: i + 1, ModelName: "m"}
	}
	if _, err := StartBatch(ctx, items, ""); err == nil {
		t.Fatalf("oversize batch must be rejected")
	}
	if _, err := StartBatch(ctx, nil, ""); err == nil {
		t.Fatalf("empty batch must be rejected")
	}
}

// TestStartBatchRunsItemsAndWritesResult 批量任务执行 + 写回 + 进度推进。
// batchProbeFn 用 stub（executed 态，强 true），验证写库与 task 结果。
func TestStartBatchRunsItemsAndWritesResult(t *testing.T) {
	ctx := setupBatchTestDB(t)
	chID, itemID := createBatchFixture(t, ctx)

	orig := batchProbeFn
	batchProbeFn = func(_ context.Context, channel model.Channel, modelName, toolChoice string) (model.ToolsProbeResult, error) {
		return model.ToolsProbeResult{State: model.ToolsProbeStateExecuted, Supports: true, Source: "manual"}, nil
	}
	t.Cleanup(func() { batchProbeFn = orig })

	task, err := StartBatch(ctx, []BatchItem{{ChannelID: chID, ModelName: "batch-model"}}, "required")
	if err != nil {
		t.Fatalf("StartBatch: %v", err)
	}
	done := waitForBatchTask(t, task.ID, 5*time.Second)
	if done.Total != 1 || done.Done != 1 {
		t.Fatalf("task progress mismatch: total=%d done=%d", done.Total, done.Done)
	}
	if len(done.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(done.Results))
	}
	r := done.Results[0]
	if r.State != string(model.ToolsProbeStateExecuted) || !r.Supports || r.Source != "manual" {
		t.Fatalf("unexpected result: %+v", r)
	}

	var row model.GroupItem
	if err := dbpkg.GetDB().WithContext(ctx).First(&row, itemID).Error; err != nil {
		t.Fatalf("reload group item: %v", err)
	}
	if row.SupportsTools == nil || !*row.SupportsTools {
		t.Fatalf("batch must write supports_tools=true, got %v", row.SupportsTools)
	}
	if row.SupportsToolsSource != "manual" {
		t.Fatalf("batch must write source=manual, got %s", row.SupportsToolsSource)
	}
}

// TestStartBatchPropagatesProbeError 探测失败（channel not found）→ 结果带 Error，不 panic。
func TestStartBatchPropagatesProbeError(t *testing.T) {
	ctx := setupBatchTestDB(t)
	task, err := StartBatch(ctx, []BatchItem{{ChannelID: 999999, ModelName: "m"}}, "")
	if err != nil {
		t.Fatalf("StartBatch: %v", err)
	}
	done := waitForBatchTask(t, task.ID, 5*time.Second)
	if len(done.Results) != 1 || done.Results[0].Error == "" {
		t.Fatalf("expected error result, got %+v", done.Results)
	}
}
