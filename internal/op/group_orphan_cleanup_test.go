package op

import (
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

// TestCleanupOrphanGroupItems 验证孤儿组成员兜底清理只删除引用不存在渠道的行，
// 并保留仍然有效的组成员。
func TestCleanupOrphanGroupItems(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)

	group := model.Group{Name: "orphan-cleanup-group", Mode: model.GroupModeFailover}
	if err := GroupCreate(&group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}

	channel := model.Channel{Name: "orphan-cleanup-channel", Model: "orphan-model", Enabled: true}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	valid := model.GroupItem{GroupID: group.ID, ChannelID: channel.ID, ModelName: "orphan-model", Priority: 1, Weight: 1}
	orphan := model.GroupItem{GroupID: group.ID, ChannelID: 999999, ModelName: "ghost-model", Priority: 2, Weight: 1}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&[]model.GroupItem{valid, orphan}).Error; err != nil {
		t.Fatalf("insert group items: %v", err)
	}

	deleted, err := CleanupOrphanGroupItems(ctx)
	if err != nil {
		t.Fatalf("CleanupOrphanGroupItems: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 orphan row deleted, got %d", deleted)
	}

	var remaining []model.GroupItem
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("group_id = ?", group.ID).
		Order("id ASC").
		Find(&remaining).Error; err != nil {
		t.Fatalf("load remaining items: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("expected 1 remaining group item, got %d", len(remaining))
	}
	if remaining[0].ChannelID != channel.ID {
		t.Fatalf("remaining item channel_id = %d, want %d", remaining[0].ChannelID, channel.ID)
	}
}
