package op

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

// ApplyGroupDefaultsResult holds the counts of affected rows.
type ApplyGroupDefaultsResult struct {
	GroupsUpdated   int64 `json:"groups_updated"`
	GroupsSuspended int64 `json:"groups_suspended"`
	ItemsRemoved    int64 `json:"items_removed"`
	ItemsSorted     int64 `json:"items_sorted"`
}

var modeFromString = map[string]model.GroupMode{
	"round_robin": model.GroupModeRoundRobin,
	"random":      model.GroupModeRandom,
	"failover":    model.GroupModeFailover,
	"weighted":    model.GroupModeWeighted,
}

// enrichedItem holds a GroupItem plus the metadata needed for sorting/filtering.
type enrichedItem struct {
	item       model.GroupItem
	isReserve  bool
	balance    float64
	multiplier float64
}

// ApplyGroupDefaults reads default_group_load_balance, default_group_sort_strategy,
// and default_multiplier_cap from settings, then:
//  1. Applies the load balance mode to all groups
//  2. Removes group items whose multiplier exceeds the configured cap
//  3. Re-sorts each group's items according to the sort strategy and persists new priorities
//  4. Suspends site user groups exceeding the multiplier cap
func ApplyGroupDefaults(ctx context.Context) (*ApplyGroupDefaultsResult, error) {
	result := &ApplyGroupDefaultsResult{}

	// 1. Apply default load balance mode to all groups
	lbStr, _ := SettingGetString(model.SettingKeyDefaultGroupLoadBalance)
	if lbStr != "" {
		mode, ok := modeFromString[lbStr]
		if ok {
			res := db.GetDB().WithContext(ctx).
				Model(&model.Group{}).
				Where("mode != ?", mode).
				Update("mode", mode)
			if res.Error != nil {
				return nil, fmt.Errorf("apply default load balance: %w", res.Error)
			}
			result.GroupsUpdated = res.RowsAffected
		}
	}

	// 2 & 3. Enforce multiplier cap on items + apply sort strategy
	capStr, _ := SettingGetString(model.SettingKeyDefaultMultiplierCap)
	capVal, capErr := strconv.ParseFloat(capStr, 64)
	hasCap := capErr == nil && capVal > 0

	sortStr, _ := SettingGetString(model.SettingKeyDefaultGroupSortStrategy)
	if sortStr == "" {
		sortStr = "non_relay_balance"
	}

	// Collect all channel IDs across all group items
	groups := groupCache.GetAll()
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

	// Load metadata for sorting and filtering
	bindingMap, _ := SiteChannelBindingMapByChannelIDs(channelIDs, ctx)
	balanceByAccount := accountBalanceMap(ctx, bindingMap)
	multiplierByKey := channelCandidateMultiplierMap(ctx, channelIDs)
	groupMultiplierByChannel := channelGroupMultiplierMap(ctx, bindingMap)

	for _, group := range groups {
		if len(group.Items) == 0 {
			continue
		}

		items := make([]enrichedItem, 0, len(group.Items))
		var itemsToDelete []int

		for _, item := range group.Items {
			isReserve := false
			if channel, ok := channelCache.Get(item.ChannelID); ok {
				isReserve = channel.IsReserve
			}

			var balance float64
			if binding, ok := bindingMap[item.ChannelID]; ok {
				balance = balanceByAccount[binding.SiteAccountID]
			}

			mul := math.Inf(1)
			if gm := groupMultiplierByChannel[item.ChannelID]; gm != nil {
				mul = *gm
			} else if m := multiplierByKey[routeCandidateKey(item.ChannelID, item.ModelName)]; m != nil {
				mul = *m
			}

			if hasCap && !math.IsInf(mul, 1) && mul > capVal {
				itemsToDelete = append(itemsToDelete, item.ID)
				continue
			}

			items = append(items, enrichedItem{
				item:       item,
				isReserve:  isReserve,
				balance:    balance,
				multiplier: mul,
			})
		}

		// Delete items exceeding cap
		if len(itemsToDelete) > 0 {
			if err := db.GetDB().WithContext(ctx).
				Where("id IN ?", itemsToDelete).
				Delete(&model.GroupItem{}).Error; err != nil {
				return nil, fmt.Errorf("delete items exceeding cap for group %d: %w", group.ID, err)
			}
			result.ItemsRemoved += int64(len(itemsToDelete))
		}

		// Sort items by strategy
		sortEnrichedItems(items, sortStr)

		// Update priorities in DB
		for i, ei := range items {
			newPriority := i + 1
			if ei.item.Priority != newPriority {
				if err := db.GetDB().WithContext(ctx).
					Model(&model.GroupItem{}).
					Where("id = ?", ei.item.ID).
					Update("priority", newPriority).Error; err != nil {
					return nil, fmt.Errorf("update priority for item %d: %w", ei.item.ID, err)
				}
				result.ItemsSorted++
			}
		}
	}

	// 4. Enforce multiplier cap on site user groups (original behavior)
	suspended, err := EnforceMultiplierCap(ctx)
	if err != nil {
		return nil, err
	}
	result.GroupsSuspended = suspended

	// Refresh group cache to reflect all changes
	_ = groupRefreshCache(ctx)

	return result, nil
}

// sortEnrichedItems sorts items in-place according to the given strategy.
func sortEnrichedItems(items []enrichedItem, strategy string) {
	switch strategy {
	case "non_relay_balance":
		sort.SliceStable(items, func(i, j int) bool {
			ti, tj := tierVal(items[i].isReserve), tierVal(items[j].isReserve)
			if ti != tj {
				return ti < tj
			}
			if ti == 0 {
				return items[i].balance > items[j].balance
			}
			// tier 1: multiplier asc, then balance desc
			if items[i].multiplier != items[j].multiplier {
				return items[i].multiplier < items[j].multiplier
			}
			return items[i].balance > items[j].balance
		})
	case "non_relay_multiplier":
		sort.SliceStable(items, func(i, j int) bool {
			ti, tj := tierVal(items[i].isReserve), tierVal(items[j].isReserve)
			if ti != tj {
				return ti < tj
			}
			if ti == 0 {
				if items[i].multiplier != items[j].multiplier {
					return items[i].multiplier < items[j].multiplier
				}
				return items[i].balance > items[j].balance
			}
			return items[i].multiplier < items[j].multiplier
		})
	case "multiplier_balance":
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].multiplier != items[j].multiplier {
				return items[i].multiplier < items[j].multiplier
			}
			return items[i].balance > items[j].balance
		})
	case "balance_only":
		sort.SliceStable(items, func(i, j int) bool {
			return items[i].balance > items[j].balance
		})
	default:
		// fallback to non_relay_balance
		sort.SliceStable(items, func(i, j int) bool {
			ti, tj := tierVal(items[i].isReserve), tierVal(items[j].isReserve)
			if ti != tj {
				return ti < tj
			}
			if ti == 0 {
				return items[i].balance > items[j].balance
			}
			if items[i].multiplier != items[j].multiplier {
				return items[i].multiplier < items[j].multiplier
			}
			return items[i].balance > items[j].balance
		})
	}
}

func tierVal(isReserve bool) int {
	if isReserve {
		return 1
	}
	return 0
}

// EnforceMultiplierCap suspends site user groups whose multiplier exceeds
// the configured default_multiplier_cap. Returns the number of affected rows.
func EnforceMultiplierCap(ctx context.Context) (int64, error) {
	capStr, _ := SettingGetString(model.SettingKeyDefaultMultiplierCap)
	cap, err := strconv.ParseFloat(capStr, 64)
	if err != nil || cap <= 0 {
		return 0, nil // no cap configured
	}

	res := db.GetDB().WithContext(ctx).
		Model(&model.SiteUserGroup{}).
		Where("multiplier > ? AND projection_suspended = ?", cap, false).
		Updates(map[string]interface{}{
			"projection_suspended":      true,
			"projection_suspend_reason": fmt.Sprintf("multiplier exceeds cap (%.2f)", cap),
		})
	if res.Error != nil {
		return 0, fmt.Errorf("enforce multiplier cap: %w", res.Error)
	}
	return res.RowsAffected, nil
}
