package modelvendor

import (
	"strings"
	"sync"
)

var (
	indexMu     sync.RWMutex
	modelIndex  = map[string]string{}
	visionIndex = map[string]bool{}
)

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

// VisionEntry 模型视觉能力的注册表条目（Provider 用于 prefixAliases 过滤）。
type VisionEntry struct {
	Provider string
	Vision   bool
}

// ReplaceVisionIndex 用外部注册表（models.dev）整体替换「模型名 → 视觉能力」索引。
// 与厂商索引同源同过滤（只接受 prefixAliases 可映射的厂商，防 openrouter/groq 托管方同名条目
// 按 map 遍历顺序覆盖真厂商能力值）。视觉判定 = modalities.input 含 image/video。
func ReplaceVisionIndex(entries map[string]VisionEntry) {
	next := make(map[string]bool, len(entries))
	for modelName, entry := range entries {
		if _, ok := prefixAliases[strings.ToLower(strings.TrimSpace(entry.Provider))]; !ok {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(modelName))
		if key == "" {
			continue
		}
		next[key] = entry.Vision
	}

	indexMu.Lock()
	visionIndex = next
	indexMu.Unlock()
}

// LookupVision 查询模型是否多模态（视觉输入）。ok=false 表示索引无此模型（未知）。
func LookupVision(name string) (bool, bool) {
	indexMu.RLock()
	defer indexMu.RUnlock()
	vision, ok := visionIndex[strings.ToLower(strings.TrimSpace(name))]
	return vision, ok
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
