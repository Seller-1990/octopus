package op

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

// multiplierCapMu 串行化 EnforceMultiplierCap 的整个读-判定-写周期：并发触发点
// （同步完成 / pricing 刷新 / 设置变更 / 启动兜底）各自做全表决策，交错执行会基于
// 过期快照互相覆盖 policy_blocked 终态。低频全量操作，串行化无性能代价。
// 注意：勿在 db.Transaction 闭包内调用 EnforceMultiplierCap——SQLite 连接池
// SetMaxOpenConns(1) 下会形成「持连接等锁 / 持锁等连接」死锁。
var multiplierCapMu sync.Mutex

// ApplyGroupDefaultsResult holds the counts of affected rows.
type ApplyGroupDefaultsResult struct {
	GroupsUpdated   int64 `json:"groups_updated"`
	GroupsSuspended int64 `json:"groups_suspended"`
	GroupsRecovered int64 `json:"groups_recovered"`
	ItemsRemoved    int64 `json:"items_removed"`
	ItemsBlocked    int64 `json:"items_blocked"`
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
//  2. Recomputes multiplier policy without deleting group items
//  3. Re-sorts each group's items according to the sort strategy and persists new priorities
//  4. Marks site user groups exceeding the multiplier cap as policy-blocked
func ApplyGroupDefaults(ctx context.Context) (*ApplyGroupDefaultsResult, error) {
	result := &ApplyGroupDefaultsResult{}
	// 本函数分多段写库（mode / sort_strategy / item priority / policy_blocked），
	// 任意路径退出都刷新分组缓存：中途报错时部分写入已落库，跳过刷新会让缓存滞留旧值。
	defer func() { _ = groupRefreshCache(ctx) }()

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

	// 2. Apply default sort strategy to all groups
	sortStr, _ := SettingGetString(model.SettingKeyDefaultGroupSortStrategy)
	if sortStr == "" {
		sortStr = "non_relay_balance"
	}
	if err := db.GetDB().WithContext(ctx).
		Model(&model.Group{}).
		Where("sort_strategy != ?", sortStr).
		Update("sort_strategy", sortStr).Error; err != nil {
		return nil, fmt.Errorf("apply default sort strategy: %w", err)
	}

	// 3 & 4. Enforce multiplier cap on items + apply sort strategy to priorities
	capVal, hasCap := configuredMultiplierCap()

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
	groupMultiplierByChannel := channelGroupMultiplierMap(ctx, bindingMap)

	for _, group := range groups {
		if len(group.Items) == 0 {
			continue
		}

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

			// 阶段 2 补充（D2' A' + 修订 11 + N3）：candidate 倍率不再参与排序/计数；
			// 排序统一「known=true 用真实值，否则按 1x」；ItemsBlocked 只计「known 超限」。
			mul := 1.0
			gm, hasGroup := groupMultiplierByChannel[item.ChannelID]
			if hasGroup && gm.Known {
				mul = gm.Value
			}

			if hasCap && hasGroup && gm.Known && gm.Value > capVal {
				result.ItemsBlocked++
			}

			items = append(items, enrichedItem{
				item:       item,
				isReserve:  isReserve,
				balance:    balance,
				multiplier: mul,
			})
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

	// 4. Update the independent policy-block state. Synchronization state is
	// intentionally left untouched so a successful sync cannot clear it.
	blocked, recovered, err := EnforceMultiplierCap(ctx)
	if err != nil {
		return nil, err
	}
	// Keep the legacy response field for client compatibility; it now counts
	// groups blocked by the multiplier policy rather than sync-suspended rows.
	result.GroupsSuspended = blocked
	result.GroupsRecovered = recovered

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

// EnforceMultiplierCap marks site user groups whose multiplier exceeds the
// configured default_multiplier_cap as policy-blocked and recovers groups when
// the cap is disabled or the multiplier returns within the limit.
func EnforceMultiplierCap(ctx context.Context) (int64, int64, error) {
	multiplierCapMu.Lock()
	defer multiplierCapMu.Unlock()
	cap, capEnabled := configuredMultiplierCap()
	var groups []model.SiteUserGroup
	// 只取判定所需列，不拉 raw_payload 等大字段（全表扫描的读取量随账号数放大）。
	if err := db.GetDB().WithContext(ctx).
		Select("id", "multiplier", "multiplier_known", "policy_blocked", "policy_block_reason").
		Find(&groups).Error; err != nil {
		return 0, 0, fmt.Errorf("load multiplier policy groups: %w", err)
	}
	var blocked, recovered int64
	for _, group := range groups {
		// 两态规则（阶段 2 v2 X3，用户拍板提前落地）：仅 known=true 且超 cap 才拦；
		// known=false/nil（暂定/未知）一律放行（recover 分支对非 isBlocked 的已拦组解阻）。
		isBlocked := capEnabled && group.Multiplier != nil && validGroupMultiplier(*group.Multiplier) &&
			group.MultiplierKnown != nil && *group.MultiplierKnown && *group.Multiplier > cap
		if isBlocked {
			reason := fmt.Sprintf("multiplier exceeds cap (%.4g > %.4g)", *group.Multiplier, cap)
			if group.PolicyBlocked && group.PolicyBlockReason == reason {
				continue
			}
			now := time.Now()
			if err := db.GetDB().WithContext(ctx).Model(&model.SiteUserGroup{}).
				Where("id = ?", group.ID).
				Updates(map[string]any{
					"policy_blocked":      true,
					"policy_block_reason": reason,
					"policy_blocked_at":   &now,
				}).Error; err != nil {
				return blocked, recovered, fmt.Errorf("block multiplier policy for group %d: %w", group.ID, err)
			}
			blocked++
			continue
		}
		if !group.PolicyBlocked {
			continue
		}
		if err := db.GetDB().WithContext(ctx).Model(&model.SiteUserGroup{}).
			Where("id = ?", group.ID).
			Updates(map[string]any{
				"policy_blocked":      false,
				"policy_block_reason": "",
				"policy_blocked_at":   nil,
			}).Error; err != nil {
			return blocked, recovered, fmt.Errorf("recover multiplier policy for group %d: %w", group.ID, err)
		}
		recovered++
	}
	return blocked, recovered, nil
}
