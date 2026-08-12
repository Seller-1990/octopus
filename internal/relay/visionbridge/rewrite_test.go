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
