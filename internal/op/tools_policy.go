package op

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
)

// toolsUnsupportedPatterns 是「tools/function calling 不支持」类错误的白名单（探测 + T9 失败反馈共用）。
// 覆盖 OpenAI / Anthropic / Gemini / 中文网关常见错误特征。匹配失败归入「不判定」（保持现状标记）。
var toolsUnsupportedPatterns = []string{
	"tools not supported",
	"function calling not supported",
	"function calling is not supported",
	"function calling is disabled",
	"tools is not a supported parameter",
	"tools are not supported",
	"unsupported parameter: tools",
	"does not support function calling",
	"does not support tools",
	"not support tools",
	"不支持工具",
	"不支持函数调用",
	"不支持 tools",
	"工具调用不支持",
	"不支持调用工具",
}

// MatchToolsUnsupportedError 判断上游错误文本是否命中 tools 不支持白名单。
// 供探测（判定 false）与 T9 失败反馈（回写 false）共用。
func MatchToolsUnsupportedError(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	for _, pattern := range toolsUnsupportedPatterns {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

// toolsProbeRegistry 是「≥2 次确认」的进程内计数 registry（U4）。
// key=(channelID, modelName)，记录错误文本 hash + 计数 + 时间戳；TTL 过期重置。
// 探测侧与 T9 侧共用；重启即清（文档明示）。
type toolsProbeRegistry struct {
	mu      sync.Mutex
	entries map[string]*toolsProbeEntry
}

type toolsProbeEntry struct {
	count     int
	errHash   string
	updatedAt time.Time
}

const toolsProbeRegistryTTL = 10 * time.Minute

func newToolsProbeRegistry() *toolsProbeRegistry {
	return &toolsProbeRegistry{entries: make(map[string]*toolsProbeEntry)}
}

func (r *toolsProbeRegistry) key(channelID int, modelName string) string {
	return fmt.Sprintf("%d\x00%s", channelID, modelName)
}

// recordFailure 记录一次 tools 不支持错误；返回 true 表示已达 ≥2 次（可确认 false）。
// 不同错误文本不累计。
func (r *toolsProbeRegistry) recordFailure(channelID int, modelName, errText string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := r.key(channelID, modelName)
	hash := toolsErrorHash(errText)
	now := time.Now()
	entry, ok := r.entries[key]
	if !ok || now.Sub(entry.updatedAt) > toolsProbeRegistryTTL {
		r.entries[key] = &toolsProbeEntry{count: 1, errHash: hash, updatedAt: now}
		return false
	}
	if entry.errHash != hash {
		entry.count = 1
		entry.errHash = hash
		entry.updatedAt = now
		return false
	}
	entry.count++
	entry.updatedAt = now
	return entry.count >= 2
}

func (r *toolsProbeRegistry) reset(channelID int, modelName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, r.key(channelID, modelName))
}

func toolsErrorHash(message string) string {
	s := strings.ToLower(strings.TrimSpace(message))
	if len(s) > 200 {
		s = s[:200]
	}
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return fmt.Sprintf("%x", h)
}

// ConfirmToolsUnsupportedOnce 供探测侧（toolsprobe 包）记录一次白名单命中并判断是否达 ≥2 次确认。
// 返回 true 表示已确认 false（探测应回填 false）。
func ConfirmToolsUnsupportedOnce(channelID int, modelName, errText string) bool {
	if !MatchToolsUnsupportedError(errText) {
		return false
	}
	return toolsProbeCounts.recordFailure(channelID, modelName, errText)
}

var toolsProbeCounts = newToolsProbeRegistry()

// ToolsProbeFn 是探测器 hook（由 internal/toolsprobe 包 init 注入，测试可替换）。
// 返回 (supportsTools, error)；error 表示探测失败（保持 nil 未探测态）或未确认。
var ToolsProbeFn = func(ctx context.Context, channel model.Channel, modelName string) (bool, error) {
	return false, fmt.Errorf("tools probe not registered")
}

// toolsProbeCooldown 探测冷却期：已探测且 probed_at 在冷却期内的条目跳过，避免每日全量重探风暴（FIX-E）。
const toolsProbeCooldown = 6 * time.Hour

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
		// FIX-E：跳过冷却期内已探测条目（含 preset 继承的旧值），避免付费重探 + 翻转继承值
		if item.SupportsTools != nil && item.SupportsToolsProbedAt != nil &&
			now.Sub(*item.SupportsToolsProbedAt) < toolsProbeCooldown {
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
			supports, err := ToolsProbeFn(probeCtx, *channel, item.ModelName)
			if err != nil {
				// 探测失败/未确认保持 nil（未探测态）
				return
			}
			now := time.Now()
			updates := map[string]any{
				"supports_tools":              supports,
				"supports_tools_probe_key_id": firstEnabledKeyID(*channel),
				"supports_tools_probed_at":    &now,
				"supports_tools_source":       "probe",
			}
			if err := db.GetDB().WithContext(context.Background()).
				Model(&model.GroupItem{}).
				Where("channel_id = ? AND model_name = ?", item.ChannelID, item.ModelName).
				Updates(updates).Error; err != nil {
				log.Warnf("tools probe backfill failed (channel=%d model=%s): %v", item.ChannelID, item.ModelName, err)
				return
			}
			refreshGroupsAfterToolsUpdate(item.ChannelID, item.ModelName)
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

// ReportToolsUnsupported T9 失败反馈：真实请求带 tools 遇 tools 不支持错误 → 回写 false。
// 导出供 relay 包调用；按 (channel_id, model_name) 全量更新 + 刷新所有受影响分组（U6）。
func ReportToolsUnsupported(channelID int, modelName, errText string) {
	if channelID <= 0 || modelName == "" || !MatchToolsUnsupportedError(errText) {
		return
	}
	if !toolsProbeCounts.recordFailure(channelID, modelName, errText) {
		return // 未达 ≥2 次
	}
	now := time.Now()
	if err := db.GetDB().
		Model(&model.GroupItem{}).
		Where("channel_id = ? AND model_name = ?", channelID, modelName).
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
	if err := db.GetDB().
		Model(&model.GroupItem{}).
		Where("channel_id = ? AND model_name = ? AND supports_tools = ?", channelID, modelName, false).
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

// recordSuccess 记录一次带 tools 的真实请求成功；返回 true 表示已达 ≥2 次独立成功（可回写 nil）。
// 与 recordFailure 同 registry 结构但独立实例（成功/失败计数隔离）。
func (r *toolsProbeRegistry) recordSuccess(channelID int, modelName string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := r.key(channelID, modelName)
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

// UpdateGroupItemToolsSupport 手动重探后的回填（reprobe handler 调用）：按 (channel, model) 全量更新 + 刷新缓存。
func UpdateGroupItemToolsSupport(channelID int, modelName string, updates map[string]any) error {
	if channelID <= 0 || modelName == "" {
		return fmt.Errorf("channel_id and model_name are required")
	}
	if err := db.GetDB().
		Model(&model.GroupItem{}).
		Where("channel_id = ? AND model_name = ?", channelID, modelName).
		Updates(updates).Error; err != nil {
		return err
	}
	refreshGroupsAfterToolsUpdate(channelID, modelName)
	return nil
}
