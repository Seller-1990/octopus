package balancer

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

// TestFailoverCandidatesStableOrder 锁定 failover 的顺序契约：priority 升序，
// 同 priority 成员保持传入顺序（即 DB 的 priority ASC, id ASC 加载序）。
// 回归背景：sortByPriority 曾用不稳定的 sort.Slice，同优先级成员的实际
// 调用顺序每次可能漂移，表现为 failover 乱序。
func TestFailoverCandidatesStableOrder(t *testing.T) {
	newItems := func() []model.GroupItem {
		return []model.GroupItem{
			{ID: 1, Priority: 10, ChannelID: 101},
			{ID: 2, Priority: 5, ChannelID: 102},
			{ID: 3, Priority: 10, ChannelID: 103},
			{ID: 4, Priority: 5, ChannelID: 104},
			{ID: 5, Priority: 10, ChannelID: 105},
		}
	}

	f := &Failover{}
	wantChannelIDs := []int{102, 104, 101, 103, 105}

	for round := 0; round < 50; round++ {
		got := f.Candidates(newItems())
		if len(got) != len(wantChannelIDs) {
			t.Fatalf("round %d: candidate count = %d, want %d", round, len(got), len(wantChannelIDs))
		}
		for i, item := range got {
			if item.ChannelID != wantChannelIDs[i] {
				t.Fatalf("round %d: candidate[%d].ChannelID = %d, want %d (full order: %v)",
					round, i, item.ChannelID, wantChannelIDs[i], channelIDs(got))
			}
		}
	}
}

func channelIDs(items []model.GroupItem) []int {
	ids := make([]int, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ChannelID)
	}
	return ids
}
