package balancer

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func partitionGroup(items ...model.GroupItem) model.Group {
	return model.Group{Mode: model.GroupModeFailover, Items: items}
}

func item(channelID int, name string, priority int) model.GroupItem {
	return model.GroupItem{ChannelID: channelID, ModelName: name, Priority: priority}
}

func TestStablePartitionKeepsRelativeOrder(t *testing.T) {
	it := NewIterator(partitionGroup(
		item(1, "text-a", 1),
		item(2, "vision-a", 2),
		item(3, "text-b", 3),
		item(4, "vision-b", 4),
	), 0, "m")
	it.StablePartition(func(g model.GroupItem) bool { return g.ChannelID == 2 || g.ChannelID == 4 })

	var order []int
	for it.Next() {
		order = append(order, it.Item().ChannelID)
	}
	want := []int{2, 4, 1, 3}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestStablePartitionRelocatesSticky(t *testing.T) {
	group := partitionGroup(item(1, "text-a", 1), item(2, "vision-a", 2))
	group.SessionKeepTime = 60
	SetSticky(42, "m", 1, 9) // 粘住 text-a
	t.Cleanup(func() { DeleteSticky(42, "m") })

	it := NewIterator(group, 42, "m")
	if !it.Next() || it.Item().ChannelID != 1 || !it.IsSticky() {
		t.Fatal("precondition: sticky channel 1 should be first")
	}

	it2 := NewIterator(group, 42, "m")
	it2.StablePartition(func(g model.GroupItem) bool { return g.ChannelID == 2 })
	if !it2.Next() || it2.Item().ChannelID != 2 {
		t.Fatalf("vision channel should be first after partition, got %d", it2.Item().ChannelID)
	}
	if it2.IsSticky() {
		t.Fatal("first item is not the sticky channel")
	}
	if !it2.Next() || it2.Item().ChannelID != 1 || !it2.IsSticky() {
		t.Fatal("sticky flag should follow channel 1 to its new position")
	}
	if it2.StickyKeyID() != 9 {
		t.Fatalf("sticky key id lost, got %d", it2.StickyKeyID())
	}
}

func TestStablePartitionAfterNextIsNoop(t *testing.T) {
	it := NewIterator(partitionGroup(item(1, "a", 1), item(2, "b", 2)), 0, "m")
	it.Next()
	first := it.Item().ChannelID
	it.StablePartition(func(g model.GroupItem) bool { return g.ChannelID != first })
	if it.Item().ChannelID != first {
		t.Fatal("partition after iteration started must be a no-op")
	}
}
