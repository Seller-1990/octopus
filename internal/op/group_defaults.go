package op

import (
	"context"
	"fmt"
	"strconv"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

// ApplyGroupDefaultsResult holds the counts of affected rows.
type ApplyGroupDefaultsResult struct {
	GroupsUpdated    int64 `json:"groups_updated"`
	GroupsSuspended  int64 `json:"groups_suspended"`
}

var modeFromString = map[string]model.GroupMode{
	"round_robin": model.GroupModeRoundRobin,
	"random":      model.GroupModeRandom,
	"failover":    model.GroupModeFailover,
	"weighted":    model.GroupModeWeighted,
}

// ApplyGroupDefaults reads default_group_load_balance and default_multiplier_cap
// from settings, applies the load balance mode to all groups, and suspends
// site user groups whose multiplier exceeds the configured cap.
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

	// 2. Enforce multiplier cap: suspend site user groups exceeding cap
	suspended, err := EnforceMultiplierCap(ctx)
	if err != nil {
		return nil, err
	}
	result.GroupsSuspended = suspended

	// Refresh group cache to reflect mode changes
	_ = groupRefreshCache(ctx)

	return result, nil
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
