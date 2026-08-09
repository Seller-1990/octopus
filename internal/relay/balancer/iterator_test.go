package balancer

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

// TestIteratorFiltersBlockedItems 阶段 7 收口：NewIteratorWithPreference 排除
// PolicyStatus=="blocked" 的条目（两态下唯一拦截状态）；allowed/unknown 保留。
// 用 Failover 确定性排序 + SessionKeepTime=0 避免包级 sticky 干扰；
// 断言成员集合而非顺序（Candidates 会重排，W12）。
func TestIteratorFiltersBlockedItems(t *testing.T) {
	Reset()

	items := []model.GroupItem{
		{ChannelID: 1, ModelName: "blocked-model", Priority: 1, PolicyStatus: "blocked"},
		{ChannelID: 2, ModelName: "allowed-model", Priority: 2, PolicyStatus: "allowed"},
		{ChannelID: 3, ModelName: "unknown-model", Priority: 3, PolicyStatus: "unknown"},
	}
	group := model.Group{Mode: model.GroupModeFailover, Items: items, SessionKeepTime: 0}

	it := NewIteratorWithPreference(group, 0, "request-model", nil)
	if it == nil {
		t.Fatal("NewIteratorWithPreference returned nil")
	}

	seen := make(map[int]bool)
	for it.Next() {
		item := it.Item()
		seen[item.ChannelID] = true
	}
	if len(seen) != 2 {
		t.Fatalf("expected 2 eligible items, got %d (seen=%v)", len(seen), seen)
	}
	if seen[1] {
		t.Fatal("blocked item (channel 1) leaked into iterator")
	}
	if !seen[2] || !seen[3] {
		t.Fatalf("allowed/unknown items missing: %v", seen)
	}
}
