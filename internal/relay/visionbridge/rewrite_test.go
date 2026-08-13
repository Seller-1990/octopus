package visionbridge

import (
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestRewriteSingleImage(t *testing.T) {
	req := &model.InternalLLMRequest{Model: "orig", Messages: []model.Message{
		multiContentMsg("user", textPart("图里是什么？"), imagePart(validDataURI(64))),
	}}
	refs, err := Discover(req, testConfig())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	out, err := RewriteRequest(req, refs, "一段视觉描述")
	if err != nil {
		t.Fatalf("RewriteRequest: %v", err)
	}
	if out.HasImages() {
		t.Fatal("rewritten request must not contain images")
	}
	parts := out.Messages[0].Content.MultipleContent
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts (text+marker+joint), got %d", len(parts))
	}
	if *parts[1].Text != "[Image 1]" {
		t.Fatalf("marker part unexpected: %q", *parts[1].Text)
	}
	if !strings.HasPrefix(*parts[2].Text, "[Image 1 — Visual analysis]\n一段视觉描述") {
		t.Fatalf("joint block unexpected: %q", *parts[2].Text)
	}
}

func TestRewriteMultiImageAcrossMessages(t *testing.T) {
	req := &model.InternalLLMRequest{Messages: []model.Message{
		multiContentMsg("user", imagePart(validDataURI(16)), textPart("对比一下"), imagePart(validDataURI(32))),
		multiContentMsg("assistant", textPart("好的")),
		multiContentMsg("user", imagePart("https://example.com/c.png"), textPart("以及这张")),
	}}
	refs, err := Discover(req, testConfig())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	out, err := RewriteRequest(req, refs, "联合分析文本")
	if err != nil {
		t.Fatalf("RewriteRequest: %v", err)
	}
	if out.HasImages() {
		t.Fatal("residual images after rewrite")
	}

	first := out.Messages[0].Content.MultipleContent
	if *first[0].Text != "[Image 1]" || *first[2].Text != "[Image 2]" {
		t.Fatalf("message 0 markers wrong: %q / %q", *first[0].Text, *first[2].Text)
	}
	for _, p := range first {
		if strings.Contains(*p.Text, "Joint visual analysis") {
			t.Fatal("joint block must not appear before the last image")
		}
	}

	last := out.Messages[2].Content.MultipleContent
	if *last[0].Text != "[Image 3]" {
		t.Fatalf("message 2 marker wrong: %q", *last[0].Text)
	}
	if !strings.HasPrefix(*last[1].Text, "[Images 1-3 — Joint visual analysis]\n联合分析文本") {
		t.Fatalf("joint block missing after last image: %q", *last[1].Text)
	}
	if *last[2].Text != "以及这张" {
		t.Fatalf("trailing text lost: %q", *last[2].Text)
	}
}

func TestRewriteDoesNotMutateOriginal(t *testing.T) {
	req := &model.InternalLLMRequest{Messages: []model.Message{
		multiContentMsg("user", textPart("q"), imagePart(validDataURI(64))),
	}}
	refs, _ := Discover(req, testConfig())
	if _, err := RewriteRequest(req, refs, "desc"); err != nil {
		t.Fatalf("RewriteRequest: %v", err)
	}
	if !req.HasImages() {
		t.Fatal("original request was mutated: image part lost")
	}
	if req.Messages[0].Content.MultipleContent[1].Type != "image_url" {
		t.Fatal("original content parts reordered")
	}
}

// 副本与原请求不得共享底层数组：出站转换（Normalize / FlattenUnsupportedBlocks）
// 会对副本的 MultipleContent / Tools 做 [:0] 就地压缩，共享数组会把原请求改出重复元素
// （原请求随后被 replay 状态与 relay log 使用）。
func TestRewriteCopyIsolatedFromInPlaceTransforms(t *testing.T) {
	empty := ""
	req := &model.InternalLLMRequest{
		Tools: []model.Tool{{Type: "server_search"}, {Type: "function"}},
		Messages: []model.Message{
			multiContentMsg("user", model.MessageContentPart{Type: "text", Text: &empty}, textPart("A"), textPart("B")),
			multiContentMsg("user", textPart("看图"), imagePart(validDataURI(64))),
		},
	}
	refs, _ := Discover(req, testConfig())
	out, err := RewriteRequest(req, refs, "desc")
	if err != nil {
		t.Fatalf("RewriteRequest: %v", err)
	}
	// 模拟出站转换的就地压缩：对副本做空文本清理与 Tools 过滤
	for i := range out.Messages {
		out.Messages[i].Normalize()
	}
	out.FlattenUnsupportedBlocks(model.AlternationProviderOpenAI)

	got := req.Messages[0].Content.MultipleContent
	if len(got) != 3 || *got[1].Text != "A" || *got[2].Text != "B" {
		t.Fatalf("original no-image message corrupted by transform on copy: %+v", got)
	}
	if len(req.Tools) != 2 || req.Tools[0].Type != "server_search" {
		t.Fatalf("original Tools corrupted by transform on copy: %+v", req.Tools)
	}
}

// Responses 出站以 RawInputItems / 扩展 RawResponseItems 为权威输入源（优先于 Messages）——
// 副本若原样携带会把原图直接送进纯文本上游；原请求的 raw items 必须原封不动（vision 通道仍需）。
func TestRewriteClearsRawInputItemsOnCopyOnly(t *testing.T) {
	rawItems := []byte(`[{"type":"message","role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,AAAA"}]}]`)
	req := &model.InternalLLMRequest{Messages: []model.Message{
		multiContentMsg("user", textPart("看图"), imagePart(validDataURI(64))),
	}}
	req.SetOpenAIRawInputItems(rawItems)
	refs, _ := Discover(req, testConfig())
	out, err := RewriteRequest(req, refs, "desc")
	if err != nil {
		t.Fatalf("RewriteRequest: %v", err)
	}
	if len(out.OpenAIRawInputItems()) != 0 {
		t.Fatal("bridged copy must not carry raw input items (they contain original images)")
	}
	if len(out.GetOpenAIExtensions().RawResponseItems) != 0 {
		t.Fatal("bridged copy must not carry extension raw response items")
	}
	if len(req.OpenAIRawInputItems()) == 0 || len(req.GetOpenAIExtensions().RawResponseItems) == 0 {
		t.Fatal("original request raw items must stay intact for vision channels")
	}
}

func TestRewriteAssistantAndToolMessagesPreserved(t *testing.T) {
	req := &model.InternalLLMRequest{Messages: []model.Message{
		multiContentMsg("user", textPart("看图")),
		multiContentMsg("tool", textPart("tool output"), imagePart(validDataURI(16))),
	}}
	refs, _ := Discover(req, testConfig())
	out, err := RewriteRequest(req, refs, "描述")
	if err != nil {
		t.Fatalf("RewriteRequest: %v", err)
	}
	toolParts := out.Messages[1].Content.MultipleContent
	if *toolParts[0].Text != "tool output" {
		t.Fatal("tool text part lost")
	}
	if out.Messages[1].Role != "tool" {
		t.Fatal("tool role lost")
	}
}
