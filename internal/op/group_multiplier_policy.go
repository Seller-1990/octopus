package op

import (
	"context"
	"fmt"
	"math"
	"strconv"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
)

const (
	MultiplierPolicyStatusAllowed = "allowed"
	MultiplierPolicyStatusBlocked = "blocked"
	MultiplierPolicyStatusUnknown = "unknown"
	MultiplierSourceGroup         = "group"
	MultiplierSourceCandidate     = "candidate"
)

type groupItemMultiplierPolicy struct {
	multiplier          *float64
	groupMultiplier     *float64
	effectiveMultiplier *float64
	source              string
	cap                 *float64
	status              string
	reason              string
	known               bool
}

func configuredMultiplierCap() (float64, bool) {
	value, err := SettingGetString(model.SettingKeyDefaultMultiplierCap)
	if err != nil {
		return 0, false
	}
	cap, err := strconv.ParseFloat(value, 64)
	if err != nil || cap < 0 || math.IsNaN(cap) || math.IsInf(cap, 0) || cap == 0 {
		return 0, false
	}
	return cap, true
}

func evaluateGroupItemMultiplierPolicies(ctx context.Context, items []model.GroupItem) []groupItemMultiplierPolicy {
	if len(items) == 0 {
		return nil
	}
	channelIDs := make([]int, 0, len(items))
	seenChannels := make(map[int]struct{}, len(items))
	for _, item := range items {
		if _, ok := seenChannels[item.ChannelID]; ok {
			continue
		}
		seenChannels[item.ChannelID] = struct{}{}
		channelIDs = append(channelIDs, item.ChannelID)
	}
	capValue, capEnabled := configuredMultiplierCap()
	bindingMap, bindingErr := SiteChannelBindingMapByChannelIDs(channelIDs, ctx)
	if bindingErr != nil {
		// cap 开启时按成本控制点 fail-closed：查不到绑定就不能证明倍率在
		// cap 内，统一按 blocked 处理，与 HeaderPolicy 的 DB 失败 fail-closed
		// 语义一致。cap 关闭时保持两态语义：未知→放行并告警。
		multiplierBindingFailureTotal.Add(1)
		log.Warnf("load site channel bindings failed (cap_enabled=%t): %v", capEnabled, bindingErr)
	}
	groupMultipliers := channelGroupMultiplierMap(ctx, bindingMap)

	policies := make([]groupItemMultiplierPolicy, len(items))
	for index, item := range items {
		policy := groupItemMultiplierPolicy{status: MultiplierPolicyStatusAllowed}
		if capEnabled {
			capCopy := capValue
			policy.cap = &capCopy
		}
		if bindingErr != nil && capEnabled {
			policy.status = MultiplierPolicyStatusBlocked
			policy.reason = "multiplier lookup failed; fail-closed while cap enabled"
			policies[index] = policy
			continue
		}
		// 两态（阶段 2 补充，用户拍板「提前 evaluate 两态化」）：
		// 判定只看分组倍率 + known——candidate 倍率不再参与 effective/blocked 判定（D2' A'）。
		gm, hasGroup := groupMultipliers[item.ChannelID]
		if hasGroup {
			value := gm.Value
			policy.groupMultiplier = &value
			policy.effectiveMultiplier = &value
			policy.source = MultiplierSourceGroup
			policy.known = gm.Known
		} else {
			// 无分组倍率：暂定 1x（放行 + 标注 unknown，effectiveMultiplier=1）
			one := 1.0
			policy.effectiveMultiplier = &one
			policy.status = MultiplierPolicyStatusUnknown
			policy.reason = "group multiplier unknown; treated as tentative"
		}
		if capEnabled && hasGroup && gm.Known && *policy.effectiveMultiplier > capValue {
			policy.status = MultiplierPolicyStatusBlocked
			policy.reason = fmt.Sprintf("multiplier %.4gx exceeds cap %.4gx", *policy.effectiveMultiplier, capValue)
		}
		policies[index] = policy
	}
	return policies
}

func applyGroupItemMultiplierPolicies(ctx context.Context, items []model.GroupItem) []model.GroupItem {
	policies := evaluateGroupItemMultiplierPolicies(ctx, items)
	for index := range items {
		if index >= len(policies) {
			break
		}
		policy := policies[index]
		items[index].Multiplier = policy.multiplier
		items[index].GroupMultiplier = policy.groupMultiplier
		items[index].EffectiveMultiplier = policy.effectiveMultiplier
		items[index].MultiplierSource = policy.source
		items[index].MultiplierCap = policy.cap
		items[index].PolicyStatus = policy.status
		items[index].PolicyReason = policy.reason
		// 阶段 2 补充：known 无条件写（false 也落 API），前端 `known !== true` 可区分「真允许/暂定放行」
		items[index].MultiplierKnown = &policy.known
	}
	return items
}
