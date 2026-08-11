package modelvendor

import (
	"strings"
	"sync"

	"github.com/bestruirui/octopus/internal/model"
)

var (
	indexMu     sync.RWMutex
	modelIndex  = map[string]string{}
	capIndex    = map[string]uint8{} // 模型名 → 能力位图（并集合并）
	visionIndex = map[string]bool{}  // 兼容旧字段：仅多模态位
)

// IsKnownProvider 判断 provider 是否在 prefixAliases 白名单内（真厂商，非 openrouter/groq 托管方）。
// 供 price 包在构建能力索引前过滤 provider（P0-3 修复：先过滤再做并集，避免合并后
// 单值 Provider 恰好是托管方导致整条被丢弃）。
func IsKnownProvider(provider string) bool {
	_, ok := prefixAliases[strings.ToLower(strings.TrimSpace(provider))]
	return ok
}

// ReplaceIndex 用外部注册表（models.dev）整体替换「模型名 → 厂商」索引。
//
// 入参是 registry 侧的 provider ID，只接受能通过 prefixAliases 映射到已知厂商的项：
// models.dev 同时收录 openrouter / groq 这类托管方，它们会把同一个模型登记在自己名下，
// 直接采信会让厂商归属随 map 遍历顺序漂移。
func ReplaceIndex(providerByModel map[string]string) {
	next := make(map[string]string, len(providerByModel))
	for modelName, provider := range providerByModel {
		vendor, ok := prefixAliases[strings.ToLower(strings.TrimSpace(provider))]
		if !ok {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(modelName))
		if key == "" {
			continue
		}
		next[key] = vendor
	}

	indexMu.Lock()
	modelIndex = next
	indexMu.Unlock()
}

// CapabilityEntry 模型能力注册表条目（Provider 用于 prefixAliases 过滤）。
type CapabilityEntry struct {
	Provider     string
	Capabilities uint8
}

// ReplaceCapabilityIndex 用外部注册表（models.dev）整体替换「模型名 → 能力位图」索引。
// 与厂商索引同源同过滤（只接受 prefixAliases 可映射的厂商）。
// 合并策略 = 并集（P0-3 修正）：同模型多 provider 条目任一位=1 则置位，
// 避免 map 遍历顺序导致 deepseek-reasoner 等模型能力随机漂移。
// 同时维护 visionIndex（多模态位）兼容旧 LookupVision。
func ReplaceCapabilityIndex(entries map[string]CapabilityEntry) {
	next := make(map[string]uint8, len(entries))
	for modelName, entry := range entries {
		if _, ok := prefixAliases[strings.ToLower(strings.TrimSpace(entry.Provider))]; !ok {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(modelName))
		if key == "" {
			continue
		}
		next[key] |= entry.Capabilities // 并集合并
	}
	nextVision := make(map[string]bool, len(next))
	for k, caps := range next {
		nextVision[k] = caps&uint8(model.CapMultimodal) != 0
	}

	indexMu.Lock()
	capIndex = next
	visionIndex = nextVision
	indexMu.Unlock()
}

// ReplaceVisionIndex 兼容旧调用（仅多模态位）。保留以便旧调用方不受影响。
func ReplaceVisionIndex(entries map[string]VisionEntry) {
	conv := make(map[string]CapabilityEntry, len(entries))
	for modelName, entry := range entries {
		caps := uint8(0)
		if entry.Vision {
			caps |= uint8(model.CapMultimodal)
		}
		conv[modelName] = CapabilityEntry{Provider: entry.Provider, Capabilities: caps}
	}
	ReplaceCapabilityIndex(conv)
}

// LookupCapabilities 查询模型能力位图。ok=false 表示索引无此模型（未知）。
func LookupCapabilities(name string) (uint8, bool) {
	indexMu.RLock()
	defer indexMu.RUnlock()
	caps, ok := capIndex[strings.ToLower(strings.TrimSpace(name))]
	return caps, ok
}

// LookupVision 兼容旧调用：多模态（视觉输入）。ok=false 表示索引无此模型（未知）。
func LookupVision(name string) (bool, bool) {
	indexMu.RLock()
	defer indexMu.RUnlock()
	vision, ok := visionIndex[strings.ToLower(strings.TrimSpace(name))]
	return vision, ok
}

// VisionEntry 兼容旧类型（仅多模态）。
type VisionEntry struct {
	Provider string
	Vision   bool
}

// IndexSize 返回当前注册表索引条目数，用于确认外部注册表是否已加载。
func IndexSize() int {
	indexMu.RLock()
	defer indexMu.RUnlock()
	return len(modelIndex)
}

func lookupIndex(name string) (string, bool) {
	indexMu.RLock()
	defer indexMu.RUnlock()
	vendor, ok := modelIndex[name]
	return vendor, ok
}
