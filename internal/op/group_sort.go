package op

import (
	"context"
	"fmt"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
)

// SortGroupsByStrategy sorts items in the given groups according to each group's
// sort_strategy field (falling back to the global default_group_sort_strategy).
// It loads balance/multiplier/is_reserve metadata, sorts in-memory, and persists
// new priority values. Returns the number of items whose priority was updated.
func SortGroupsByStrategy(groupIDs []int, ctx context.Context) (int64, error) {
	if len(groupIDs) == 0 {
		return 0, nil
	}

	// Deduplicate
	seen := make(map[int]struct{}, len(groupIDs))
	unique := make([]int, 0, len(groupIDs))
	for _, id := range groupIDs {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			unique = append(unique, id)
		}
	}

	// Load global default sort strategy
	globalSortStr, _ := SettingGetString(model.SettingKeyDefaultGroupSortStrategy)
	if globalSortStr == "" {
		globalSortStr = "non_relay_balance"
	}

	// Collect all channel IDs across all group items
	var groups []model.Group
	for _, id := range unique {
		if group, ok := groupCache.Get(id); ok {
			groups = append(groups, group)
		}
	}
	if len(groups) == 0 {
		return 0, nil
	}

	channelIDSet := make(map[int]struct{})
	for _, group := range groups {
		for _, item := range group.Items {
			channelIDSet[item.ChannelID] = struct{}{}
		}
	}
	channelIDs := make([]int, 0, len(channelIDSet))
	for id := range channelIDSet {
		channelIDs = append(channelIDs, id)
	}

	// Load metadata
	bindingMap, _ := SiteChannelBindingMapByChannelIDs(channelIDs, ctx)
	balanceByAccount := accountBalanceMap(ctx, bindingMap)
	groupMultiplierByChannel := channelGroupMultiplierMap(ctx, bindingMap)

	var totalSorted int64

	for _, group := range groups {
		if len(group.Items) == 0 {
			continue
		}

		// Determine effective sort strategy
		sortStr := group.SortStrategy
		if sortStr == "" {
			sortStr = globalSortStr
		}

		// Enrich items
		items := make([]enrichedItem, 0, len(group.Items))
		for _, item := range group.Items {
			isReserve := false
			if channel, ok := channelCache.Get(item.ChannelID); ok {
				isReserve = channel.IsReserve
			}

			var balance float64
			if binding, ok := bindingMap[item.ChannelID]; ok {
				balance = balanceByAccount[binding.SiteAccountID]
			}

			// 阶段 2 补充（D2' A' + 修订 11）：candidate 不再参与排序；排序统一「known=true 用真实值，否则按 1x」
			mul := 1.0
			if gm, ok := groupMultiplierByChannel[item.ChannelID]; ok && gm.Known {
				mul = gm.Value
			}

			items = append(items, enrichedItem{
				item:       item,
				isReserve:  isReserve,
				balance:    balance,
				multiplier: mul,
			})
		}

		// Sort
		sortEnrichedItems(items, sortStr)

		// Update priorities in DB
		for i, ei := range items {
			newPriority := i + 1
			if ei.item.Priority != newPriority {
				if err := db.GetDB().WithContext(ctx).
					Model(&model.GroupItem{}).
					Where("id = ?", ei.item.ID).
					Update("priority", newPriority).Error; err != nil {
					return totalSorted, fmt.Errorf("update priority for item %d: %w", ei.item.ID, err)
				}
				totalSorted++
			}
		}
	}

	// Refresh cache for affected groups
	if err := groupRefreshCacheByIDs(unique, ctx); err != nil {
		log.Warnf("SortGroupsByStrategy: cache refresh failed: %v", err)
	}

	return totalSorted, nil
}

// GroupItemDelByChannel removes all group items belonging to the given channel.
// Used when a managed channel is disabled to clean up stale items.
// Returns the number of items deleted.
func GroupItemDelByChannel(channelID int, ctx context.Context) (int64, error) {
	var items []model.GroupItem
	if err := db.GetDB().WithContext(ctx).
		Where("channel_id = ?", channelID).
		Find(&items).Error; err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return 0, nil
	}

	itemIDs := make([]int, len(items))
	groupIDs := make(map[int]struct{})
	for i, item := range items {
		itemIDs[i] = item.ID
		groupIDs[item.GroupID] = struct{}{}
	}

	res := db.GetDB().WithContext(ctx).
		Where("id IN ?", itemIDs).
		Delete(&model.GroupItem{})
	if res.Error != nil {
		return 0, fmt.Errorf("delete group items for channel %d: %w", channelID, res.Error)
	}

	// Refresh affected group caches
	ids := make([]int, 0, len(groupIDs))
	for id := range groupIDs {
		ids = append(ids, id)
	}
	if err := groupRefreshCacheByIDs(ids, ctx); err != nil {
		log.Warnf("GroupItemDelByChannel: cache refresh failed: %v", err)
	}

	return res.RowsAffected, nil
}
