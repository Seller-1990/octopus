package op

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
	"gorm.io/gorm"
)

// toolsUnsupportedPatternGroups 是「tools/function calling 不支持」类错误的白名单，按语义类别分组。
// 类别 key 作为 ≥2 确认 registry 的计数维度（复审修复：从「措辞级」升为「类别级」——
// 同一错误类别的不同措辞可跨路径累计，如 `tools not supported` 与 `does not support tools`）。
// pattern 一律小写、子串匹配；匹配失败归入「不判定」（保持现状标记）。
var toolsUnsupportedPatternGroups = map[string][]string{
	// tools 参数被拒绝（OpenAI/Anthropic/Gemini 及中转网关常见）
	"tools_param_rejected": {
		"tools not supported",
		"tools are not supported",
		"tools is not a supported parameter",
		"unsupported parameter: tools",
		"the tools parameter is not supported",
		"does not support the tools parameter",
		"does not support the 'tools' parameter",
		"unrecognized request argument supplied: tools",
		"does not support tools",
		"does not currently support tools",
		"doesn't support tools",
		"not support tools",
		"not support the tools parameter",
	},
	// tool calls / tool calling 被拒绝（复数、动名词形态）
	"tool_calls_rejected": {
		"tool calls are not supported",
		"tool calls not supported",
		"does not support tool calls",
		"not support tool calls",
		"tool calling is not supported",
		"tool calling not supported",
		"does not support tool calling",
		"does not support the 'tool_calls' parameter",
	},
	// function calling 被拒绝（OpenAI 旧参数名）
	"function_calling_rejected": {
		"function calling not supported",
		"function calling is not supported",
		"function calling is disabled",
		"does not support function calling",
	},
	// 中文网关常见表述
	"chinese_rejected": {
		"不支持工具",
		"不支持函数调用",
		"不支持 tools",
		"不支持tool",
		"工具调用不支持",
		"不支持调用工具",
		"tools 参数不支持",
		"tool 参数不支持",
		"工具调用功能已关闭",
	},
}

// MatchToolsUnsupportedError 判断上游错误文本是否命中 tools 不支持白名单。
// 供探测（判定 false）与 T9 失败反馈（回写 false）共用。
func MatchToolsUnsupportedError(message string) bool {
	_, ok := matchToolsUnsupportedPattern(message)
	return ok
}

// matchToolsUnsupportedPattern 返回命中的第一个错误类别（作为计数 key）。
// 计数按「错误类别」而非全文 hash 或措辞（复审修复：probe 侧带 "upstream error: %d:" 前缀、
// T9 侧传裸 body，文本不同但同类即可累计；动态错误文本含 trace_id/时间戳不影响；同义不同措辞也累计）。
// 中文否定语境排除（数据对抗者 P1）：`不支持 tools 以外的参数` 语义是「tools 之外不支持」= tools 受支持，
// 含「以外的」时视为误伤，不命中。
func matchToolsUnsupportedPattern(message string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(message))
	for category, patterns := range toolsUnsupportedPatternGroups {
		for _, pattern := range patterns {
			if !strings.Contains(lower, pattern) {
				continue
			}
			if category == "chinese_rejected" && strings.Contains(lower, "以外的") {
				return "", false // 否定语境误伤排除
			}
			return category, true
		}
	}
	return "", false
}

// toolsProbeRegistry 是「≥2 次确认」的进程内计数 registry（U4）。
// key=(channelID, modelName, pattern)，计数按命中的白名单 pattern 归类；TTL 过期重置。
// 探测侧与 T9 侧共用：只要两侧错误文本命中同一 pattern 就累计（P0 修复）。
type toolsProbeRegistry struct {
	mu      sync.Mutex
	entries map[string]*toolsProbeEntry
}

type toolsProbeEntry struct {
	count     int
	updatedAt time.Time
}

// toolsProbeRegistryTTL 失败确认窗口（复审修复：10min→24h）。
// 低频渠道 probe 第 1 次命中后，下一次真实失败可能间隔数小时——10min 窗口会过期重置使 ≥2 永达不到；
// 24h 平衡「近期性」与「低频可确认」。进程内 registry，重启清零（文档明示）。
const toolsProbeRegistryTTL = 24 * time.Hour

func newToolsProbeRegistry() *toolsProbeRegistry {
	return &toolsProbeRegistry{entries: make(map[string]*toolsProbeEntry)}
}

func (r *toolsProbeRegistry) key(channelID int, modelName, pattern string) string {
	return fmt.Sprintf("%d\x00%s\x00%s", channelID, modelName, pattern)
}

// recordFailure 记录一次 tools 不支持错误；返回 true 表示该 (channel, model, pattern)
// 已累计 ≥2 次（可确认 false）。不同 pattern 不累计（语义上视为不同错误类别）。
func (r *toolsProbeRegistry) recordFailure(channelID int, modelName, pattern string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := r.key(channelID, modelName, pattern)
	now := time.Now()
	entry, ok := r.entries[key]
	if !ok || now.Sub(entry.updatedAt) > toolsProbeRegistryTTL {
		r.entries[key] = &toolsProbeEntry{count: 1, updatedAt: now}
		return false
	}
	entry.count++
	entry.updatedAt = now
	return entry.count >= 2
}

func (r *toolsProbeRegistry) reset(channelID int, modelName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	prefix := fmt.Sprintf("%d\x00%s\x00", channelID, modelName)
	for k := range r.entries {
		if strings.HasPrefix(k, prefix) {
			delete(r.entries, k)
		}
	}
}

func (r *toolsProbeRegistry) recordSuccess(channelID int, modelName string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := r.key(channelID, modelName, "")
	now := time.Now()
	entry, ok := r.entries[key]
	if !ok || now.Sub(entry.updatedAt) > toolsProbeRegistryTTL {
		r.entries[key] = &toolsProbeEntry{count: 1, updatedAt: now}
		return false
	}
	entry.count++
	entry.updatedAt = now
	return entry.count >= 2
}

// ConfirmToolsUnsupportedOnce 供探测侧（toolsprobe 包）记录一次白名单命中并判断是否达 ≥2 次确认。
// 返回 true 表示已确认 false（探测应回填 false）。
func ConfirmToolsUnsupportedOnce(channelID int, modelName, errText string) bool {
	pattern, ok := matchToolsUnsupportedPattern(errText)
	if !ok {
		return false
	}
	return toolsProbeCounts.recordFailure(channelID, modelName, pattern)
}

var toolsProbeCounts = newToolsProbeRegistry()

// ToolsProbeFn 是探测器 hook（由 internal/toolsprobe 包 init 注入，测试可替换）。
// toolChoice: ""=auto（自动探测）；"required"=手动测试（判别矩阵）。
// 返回 (model.ToolsProbeResult, error)；error 表示完全无法探测（embedding/无 key/构造失败），不写列。
var ToolsProbeFn = func(ctx context.Context, channel model.Channel, modelName, toolChoice string) (model.ToolsProbeResult, error) {
	return model.ToolsProbeResult{State: model.ToolsProbeStateUnknown}, fmt.Errorf("tools probe not registered")
}

// toolsProbeCooldown 探测冷却期：已探测且 probed_at 在冷却期内的条目跳过，避免每日全量重探风暴（FIX-E）。
const toolsProbeCooldown = 6 * time.Hour

// probePrompts 是 tools 探测用的随机 prompt 池（防指纹，避免固定 "Reply with the word ok." 易封禁）。
var probePrompts = []string{
	"Hi! Please reply with a single short word.",
	"Reply with only the word 'ok'.",
	"What is 1+1? Answer in one word.",
	"Say hello in exactly one short word.",
}

// ResolveProbePrompt 返回一个随机探测 prompt，优先用户自定义（SettingKeyGroupHealthProbePrompt）。
// 抽为公共函数供 grouphealth 与 toolsprobe 复用（R9：随机 prompt 防封禁）。
func ResolveProbePrompt() string {
	custom, _ := SettingGetString(model.SettingKeyGroupHealthProbePrompt)
	if custom = strings.TrimSpace(custom); custom != "" {
		lines := strings.Split(custom, "\n")
		var prompts []string
		for _, line := range lines {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				prompts = append(prompts, trimmed)
			}
		}
		if len(prompts) > 0 {
			return prompts[rand.Intn(len(prompts))]
		}
	}
	return probePrompts[rand.Intn(len(probePrompts))]
}

// probeToolsForNewItems 对新添加的 GroupItem 异步探测并回填（含缓存刷新闭环）。
// 已探测（SupportsTools 非 nil）且 probed_at 在冷却期内的条目跳过；调用方需在 item 创建提交后调用。
func probeToolsForNewItems(ctx context.Context, items []model.GroupItem) {
	if len(items) == 0 {
		return
	}
	now := time.Now()
	for _, item := range items {
		if item.ChannelID == 0 || item.ModelName == "" {
			continue
		}
		// FIX-E：跳过冷却期内已探测条目，避免付费重探 + 翻转继承值。
		// 冷却只认 probed_at（U7/Reset 写 nil+probed_at=now 也应进入冷却）。
		// 调用方（preset 激活/新增 items）可能传裸结构体（无 ProbedAt），
		// 故合并 DB 已有行的最近探测时间——继承的旧值/预设镜像值才生效。
		probedAt := item.SupportsToolsProbedAt
		if probedAt == nil {
			// 调用方（preset 激活/新增 items）可能传裸结构体（无 ProbedAt），合并 DB 已有行的最近探测时间。
			// 复审修复：`supports_tools_probed_at IS NOT NULL` 排除刚创建的新行（probed_at=nil，
			// Order id DESC 会取到新行自己导致冷却自指失效——数据对抗者 P1）。
			var existing model.GroupItem
			if err := db.GetDB().WithContext(ctx).
				Select("supports_tools_probed_at").
				Where("channel_id = ? AND model_name = ? AND supports_tools_probed_at IS NOT NULL", item.ChannelID, item.ModelName).
				Order("id DESC").Limit(1).First(&existing).Error; err == nil {
				probedAt = existing.SupportsToolsProbedAt
			}
		}
		if probedAt != nil && now.Sub(*probedAt) < toolsProbeCooldown {
			continue
		}
		item := item
		go func() {
			channel, err := ChannelGet(item.ChannelID, context.Background())
			if err != nil {
				return
			}
			probeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			result, err := ToolsProbeFn(probeCtx, *channel, item.ModelName, "")
			if err != nil {
				// 完全无法探测（embedding/无 key/构造失败）→ 不写
				return
			}
			if err := ApplyToolsProbeResult(item.ChannelID, item.ModelName, result, firstEnabledKeyID(*channel)); err != nil {
				log.Warnf("tools probe backfill failed (channel=%d model=%s): %v", item.ChannelID, item.ModelName, err)
			}
		}()
	}
}

func firstEnabledKeyID(channel model.Channel) *int {
	for _, k := range channel.Keys {
		if k.Enabled {
			id := k.ID
			return &id
		}
	}
	return nil
}

// ApplyToolsProbeResult 按证据层级写探测结果（v3.1 R2/R4，R3 修订）。
// 证据层级：manual-force（管理员强制）> executed（required 执行确认）> t9（真实失败≥2）= unsupported（探测≥2）> accepted/required_unsupported（弱 2xx）。
// R3（评审修订）：executed 是「一次观测结果」，不是永久配置——达到 ≥2 真实失败确认的
// unsupported/T9 可覆盖它（系统自愈：渠道 tools 能力被关闭后自动纠正）。仅管理员显式
// 强制的 manual-force 永久保护。
// 写规则：
//   - executed：强 true 证据，覆盖除 manual-force 外的任何行（含 t9 false）。
//   - accepted / required_unsupported：弱 true 证据，只写「当前 nil 或 true」的行（不覆盖任何 false，含 t9 false）。
//   - unsupported：≥2 确认 false，可覆盖 executed/manual（R3），但绝不覆盖 manual-force。
//   - pending / required_ignored / unknown：不判定，不写列。
//
// 空值安全（R1 修订）：守卫用 COALESCE(source,”) 比较——旧库 AutoMigrate 加列后
// supports_tools_source 为 NULL，`NULL NOT IN`/`NOT (NULL=...)` 求值为 UNKNOWN 不命中，
// 导致历史行探测结果静默不落库；COALESCE 让 NULL 行也参与判定。
func ApplyToolsProbeResult(channelID int, modelName string, result model.ToolsProbeResult, keyID *int) error {
	if channelID <= 0 || modelName == "" {
		return nil
	}
	now := time.Now()
	base := db.GetDB().Model(&model.GroupItem{}).Where("channel_id = ? AND model_name = ?", channelID, modelName)
	updates := map[string]any{
		"supports_tools_probe_key_id": keyID,
		"supports_tools_probed_at":    &now,
		"supports_tools_source":       result.Source,
	}
	switch result.State {
	case model.ToolsProbeStateExecuted:
		updates["supports_tools"] = true
		// 强 true（required 执行确认）：不覆盖管理员强制标不支持（manual-force），其余行（含 NULL）全更新
		if err := base.Where("COALESCE(supports_tools_source, '') <> ?", "manual-force").
			Updates(updates).Error; err != nil {
			return err
		}
	case model.ToolsProbeStateAccepted, model.ToolsProbeStateRequiredUnsupported:
		updates["supports_tools"] = true
		// 弱 true：只覆盖 nil 或已 true 的行（不覆盖任何 false）。
		// source 保留 manual-force 保护标记（R3 后 manual 不再永久保护，无需 CASE 保留）；
		// probed_at 照常推进。
		updates["supports_tools_source"] = gorm.Expr(
			"CASE WHEN COALESCE(supports_tools_source, '') = ? THEN supports_tools_source ELSE ? END",
			"manual-force", result.Source,
		)
		if err := base.Where("supports_tools IS NULL OR supports_tools = ?", true).
			Updates(updates).Error; err != nil {
			return err
		}
	case model.ToolsProbeStateUnsupported:
		updates["supports_tools"] = false
		// ≥2 确认 false：可覆盖 executed/manual（R3），但绝不覆盖管理员强制（manual-force）。
		if err := base.Where("COALESCE(supports_tools_source, '') <> ?", "manual-force").
			Updates(updates).Error; err != nil {
			return err
		}
	default:
		// pending / required_ignored / unknown：不判定，不写列
		return nil
	}
	refreshGroupsAfterToolsUpdate(channelID, modelName)
	return nil
}

// ForceToolsUnsupported 管理员强制标不支持（v3.1 R2 拍板）：最高级证据，任何探测不覆盖。
// 强制解除用 ResetToolsState（写 nil）。同时清空失败计数（P1 修复：管理员显式决策后，
// 残留的 ≥2 计数不应让单次新失败立即重新确认 false——「重新评估」需从零证据开始）。
func ForceToolsUnsupported(channelID int, modelName string) error {
	if channelID <= 0 || modelName == "" {
		return fmt.Errorf("channel_id and model_name are required")
	}
	toolsProbeCounts.reset(channelID, modelName)
	now := time.Now()
	if err := db.GetDB().
		Model(&model.GroupItem{}).
		Where("channel_id = ? AND model_name = ?", channelID, modelName).
		Updates(map[string]any{
			"supports_tools":           false,
			"supports_tools_probed_at": &now,
			"supports_tools_source":    "manual-force",
		}).Error; err != nil {
		return err
	}
	refreshGroupsAfterToolsUpdate(channelID, modelName)
	return nil
}

// ResetToolsState 管理员恢复自动（解除强制标不支持，回到未探测待重探）。
// 同时清空失败/成功计数（P1 修复：与 Force 同理，「回到待重探」需从零证据开始）。
func ResetToolsState(channelID int, modelName string) error {
	if channelID <= 0 || modelName == "" {
		return fmt.Errorf("channel_id and model_name are required")
	}
	toolsProbeCounts.reset(channelID, modelName)
	toolsSuccessRegistry.reset(channelID, modelName)
	now := time.Now()
	if err := db.GetDB().
		Model(&model.GroupItem{}).
		Where("channel_id = ? AND model_name = ?", channelID, modelName).
		Updates(map[string]any{
			"supports_tools":           nil,
			"supports_tools_probed_at": &now,
			"supports_tools_source":    "u7",
		}).Error; err != nil {
		return err
	}
	refreshGroupsAfterToolsUpdate(channelID, modelName)
	return nil
}

// ReportToolsUnsupported T9 失败反馈：真实请求带 tools 遇 tools 不支持错误 → 回写 false。
// 导出供 relay 包调用；按 (channel_id, model_name) 全量更新 + 刷新所有受影响分组（U6）。
func ReportToolsUnsupported(channelID int, modelName, errText string) {
	if channelID <= 0 || modelName == "" {
		return
	}
	pattern, ok := matchToolsUnsupportedPattern(errText)
	if !ok {
		return
	}
	if !toolsProbeCounts.recordFailure(channelID, modelName, pattern) {
		return // 未达 ≥2 次（同 pattern 累计，probe 与 T9 跨路径共用）
	}
	now := time.Now()
	// T9 是强 false 证据（≥2 真实失败），可覆盖 executed/manual（R3），但绝不覆盖管理员强制（manual-force）。
	// COALESCE 空值安全（R1）：NULL source 行也参与判定，历史行失败反馈可落库。
	if err := db.GetDB().
		Model(&model.GroupItem{}).
		Where("channel_id = ? AND model_name = ? AND COALESCE(supports_tools_source, '') <> ?", channelID, modelName, "manual-force").
		Updates(map[string]any{
			"supports_tools":           false,
			"supports_tools_probed_at": &now,
			"supports_tools_source":    "t9",
		}).Error; err != nil {
		log.Warnf("tools unsupported feedback failed (channel=%d model=%s): %v", channelID, modelName, err)
		return
	}
	refreshGroupsAfterToolsUpdate(channelID, modelName)
}

// toolsSuccessRegistry U7 成功计数：≥2 次独立成功才回写 nil（与 T9 的 ≥2 次失败对称，防单次假 2xx 推翻 false）。
var toolsSuccessRegistry = newToolsProbeRegistry()

// ReportToolsSupported T9 反向反馈（U7）：真实 tools 请求成功且当前标记 false → 累计成功，≥2 次才回写 nil 待重探，
// 打破 false→true 死锁。元数据同步更新（Source=u7, probed_at=now）避免血缘矛盾（FIX-C）。
func ReportToolsSupported(channelID int, modelName string) {
	if channelID <= 0 || modelName == "" {
		return
	}
	var count int64
	if err := db.GetDB().
		Model(&model.GroupItem{}).
		Where("channel_id = ? AND model_name = ? AND supports_tools = ?", channelID, modelName, false).
		Count(&count).Error; err != nil || count == 0 {
		toolsSuccessRegistry.reset(channelID, modelName)
		return // 无 false 行，不动作
	}
	if !toolsSuccessRegistry.recordSuccess(channelID, modelName) {
		return // 未达 ≥2 次独立成功
	}
	now := time.Now()
	// U7 不覆盖管理员强制（source=manual-force）
	if err := db.GetDB().
		Model(&model.GroupItem{}).
		Where("channel_id = ? AND model_name = ? AND supports_tools = ? AND NOT (supports_tools_source = ?)", channelID, modelName, false, "manual-force").
		Updates(map[string]any{
			"supports_tools":           nil,
			"supports_tools_probed_at": &now,
			"supports_tools_source":    "u7",
		}).Error; err != nil {
		log.Warnf("tools supported feedback failed (channel=%d model=%s): %v", channelID, modelName, err)
		return
	}
	toolsSuccessRegistry.reset(channelID, modelName)
	refreshGroupsAfterToolsUpdate(channelID, modelName)
}

// refreshGroupsAfterToolsUpdate 刷新所有含该 (channel, model) 条目的分组缓存 + 重置 balancer。
func refreshGroupsAfterToolsUpdate(channelID int, modelName string) {
	ctx := context.Background()
	var items []model.GroupItem
	if err := db.GetDB().WithContext(ctx).
		Select("group_id").Distinct().
		Where("channel_id = ? AND model_name = ?", channelID, modelName).
		Find(&items).Error; err != nil || len(items) == 0 {
		return
	}
	seen := make(map[int]struct{}, len(items))
	for _, item := range items {
		if _, ok := seen[item.GroupID]; ok {
			continue
		}
		seen[item.GroupID] = struct{}{}
		if err := groupRefreshCacheByID(item.GroupID, ctx); err != nil {
			log.Warnf("refresh group cache after tools update failed (group=%d): %v", item.GroupID, err)
		}
	}
	resetBalancerStateForChannel(channelID)
}

// GroupGetEnabledMapForTools 是「仅 tools」key 的路由入口：内部调 GroupGetEnabledMap 后，
// 过滤掉 supports_tools=false 的条目（nil 未探测放行）。新增包装，不改原函数签名（T3/U）。
func GroupGetEnabledMapForTools(name string, ctx context.Context, toolsOnly bool) (model.Group, error) {
	group, err := GroupGetEnabledMap(name, ctx)
	if err != nil {
		return group, err
	}
	if !toolsOnly || len(group.Items) == 0 {
		return group, nil
	}
	kept := make([]model.GroupItem, 0, len(group.Items))
	for _, item := range group.Items {
		if item.SupportsTools != nil && !*item.SupportsTools {
			continue // 确认不支持 tools → 勾选「仅 tools」的 key 跳过
		}
		kept = append(kept, item)
	}
	group.Items = kept
	return group, nil
}
