package price

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

// TestCapabilityIndexUnionMerge P0-3 回归：同一模型名跨 provider（deepseek vs deepseek-ai）
// 能力按并集合并，避免 reasoning 随 provider 顺序随机漂移。
func TestCapabilityIndexUnionMerge(t *testing.T) {
	raw := map[string]registryProvider{
		"deepseek": {
			Models: map[string]registryModel{
				"deepseek-reasoner": {ID: "deepseek-reasoner", Reasoning: true},
			},
		},
		"deepseek-ai": {
			Models: map[string]registryModel{
				"deepseek-reasoner": {ID: "deepseek-reasoner", Reasoning: false},
			},
		},
	}
	index := capabilityIndex(raw)
	entry, ok := index["deepseek-reasoner"]
	if !ok {
		t.Fatal("deepseek-reasoner should be indexed")
	}
	if entry.Capabilities&uint8(model.CapReasoning) == 0 {
		t.Fatalf("union merge must keep reasoning bit across providers, got %d", entry.Capabilities)
	}
}

// TestRegistryCapabilitiesParsing 能力位图解析：modalities input/output + reasoning。
func TestRegistryCapabilitiesParsing(t *testing.T) {
	cases := []struct {
		name       string
		modalities string
		reasoning  bool
		want       uint8
	}{
		{"gpt-4o multimodal", `{"input":["text","image"],"output":["text"]}`, false, uint8(model.CapMultimodal)},
		{"deepseek-reasoning", `{"input":["text"],"output":["text"]}`, true, uint8(model.CapReasoning)},
		{"gpt-image-2 imagegen", `{"input":["text","image"],"output":["image"]}`, false, uint8(model.CapMultimodal) | uint8(model.CapImageGen)},
		{"whisper voice input", `{"input":["audio"],"output":["text"]}`, false, uint8(model.CapVoiceInput)},
		{"tts voice output", `{"input":["text"],"output":["audio"]}`, false, uint8(model.CapVoiceOutput)},
		{"deepseek-chat pdf-only no multimodal", `{"input":["text","pdf"],"output":["text"]}`, false, 0},
		{"nil modalities", "null", false, 0},
	}
	for _, tc := range cases {
		if got := registryCapabilities([]byte(tc.modalities), tc.reasoning); got != tc.want {
			t.Fatalf("%s: got %d want %d", tc.name, got, tc.want)
		}
	}
}
