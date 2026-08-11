package modelvendor

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

// TestReplaceCapabilityIndex 能力索引基本行为：过滤托管方 + 大小写不敏感。
func TestReplaceCapabilityIndex(t *testing.T) {
	ReplaceCapabilityIndex(map[string]CapabilityEntry{
		"gpt-4o":    {Provider: "openai", Capabilities: uint8(model.CapMultimodal) | uint8(model.CapImageGen)},
		"whisper-1": {Provider: "openai", Capabilities: uint8(model.CapVoiceInput)},
	})
	caps, ok := LookupCapabilities("gpt-4o")
	if !ok {
		t.Fatal("gpt-4o should be in capability index")
	}
	if caps&uint8(model.CapMultimodal) == 0 || caps&uint8(model.CapImageGen) == 0 {
		t.Fatalf("expected multimodal+imagegen bits, got %d", caps)
	}
	// openrouter 托管方条目不覆盖真厂商能力（只替换时被 prefixAliases 过滤）
	ReplaceCapabilityIndex(map[string]CapabilityEntry{
		"gpt-4o": {Provider: "openrouter", Capabilities: 0},
	})
	if _, ok := LookupCapabilities("gpt-4o"); ok {
		t.Fatal("openrouter-only index should not produce capability (filtered)")
	}
}

// TestLookupCapabilitiesCaseInsensitive 索引大小写不敏感。
func TestLookupCapabilitiesCaseInsensitive(t *testing.T) {
	ReplaceCapabilityIndex(map[string]CapabilityEntry{
		"GPT-4O": {Provider: "openai", Capabilities: uint8(model.CapMultimodal)},
	})
	if caps, ok := LookupCapabilities("gpt-4o"); !ok || caps&uint8(model.CapMultimodal) == 0 {
		t.Fatalf("expected case-insensitive lookup, got caps=%d ok=%v", caps, ok)
	}
}

// TestReplaceVisionIndexBackCompat 旧 ReplaceVisionIndex 仍工作（多模态位派生）。
func TestReplaceVisionIndexBackCompat(t *testing.T) {
	ReplaceVisionIndex(map[string]VisionEntry{
		"claude-3": {Provider: "anthropic", Vision: true},
	})
	if v, ok := LookupVision("claude-3"); !ok || !v {
		t.Fatalf("legacy ReplaceVisionIndex must still populate vision, got %v ok=%v", v, ok)
	}
	if caps, ok := LookupCapabilities("claude-3"); !ok || caps&uint8(model.CapMultimodal) == 0 {
		t.Fatalf("legacy ReplaceVisionIndex must derive multimodal bit, got caps=%d ok=%v", caps, ok)
	}
}
