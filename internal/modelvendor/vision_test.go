package modelvendor

import "testing"

// TestReplaceVisionIndexFiltersAggregators 视觉能力索引复用 prefixAliases 过滤——
// openrouter/groq 等托管方同名条目不覆盖真厂商能力值。
func TestReplaceVisionIndexFiltersAggregators(t *testing.T) {
	ReplaceVisionIndex(map[string]VisionEntry{
		// 真厂商条目：gpt-4o 有视觉
		"gpt-4o": {Provider: "openai", Vision: true},
		// 未知厂商：应被过滤
		"mystery-model": {Provider: "unknown-vendor", Vision: true},
	})
	if vision, ok := LookupVision("gpt-4o"); !ok || !vision {
		t.Fatalf("expected gpt-4o vision=true from openai entry, got ok=%v vision=%v", ok, vision)
	}
	if _, ok := LookupVision("mystery-model"); ok {
		t.Fatal("expected unknown-vendor model to be filtered out")
	}
}

// TestReplaceVisionIndexOpenRouterOverwriteIsFiltered openrouter 托管方条目被过滤，
// 不覆盖真厂商能力值（模拟同名条目按 provider 维度各自记录后合并）。
func TestReplaceVisionIndexOpenRouterOverwriteIsFiltered(t *testing.T) {
	// 先写入 openrouter 的 gpt-4o（应被过滤，不产生条目）
	ReplaceVisionIndex(map[string]VisionEntry{
		"gpt-4o": {Provider: "openrouter", Vision: false},
	})
	if _, ok := LookupVision("gpt-4o"); ok {
		t.Fatal("expected openrouter entry to be filtered out entirely")
	}
	// 再写入真厂商条目
	ReplaceVisionIndex(map[string]VisionEntry{
		"gpt-4o": {Provider: "openai", Vision: true},
	})
	if vision, ok := LookupVision("gpt-4o"); !ok || !vision {
		t.Fatalf("expected openai gpt-4o vision=true, got ok=%v vision=%v", ok, vision)
	}
}

// TestReplaceVisionIndexCaseInsensitive 索引 key 大小写不敏感。
func TestReplaceVisionIndexCaseInsensitive(t *testing.T) {
	ReplaceVisionIndex(map[string]VisionEntry{
		"GPT-4O": {Provider: "openai", Vision: true},
	})
	if vision, ok := LookupVision("gpt-4o"); !ok || !vision {
		t.Fatalf("expected case-insensitive lookup, got ok=%v vision=%v", ok, vision)
	}
	if _, ok := LookupVision("no-such-model"); ok {
		t.Fatal("expected missing model to return ok=false")
	}
}
