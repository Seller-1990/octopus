package grouphealth

import (
	"context"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestBuildProbeRequestForResponses(t *testing.T) {
	channel := &model.Channel{
		Type:     outbound.OutboundTypeOpenAIResponse,
		BaseUrls: []model.BaseUrl{{URL: "https://example.com/v1"}},
	}
	usedKey := &model.ChannelKey{ID: 1, ChannelKey: "sk-test"}

	req, err := buildProbeRequest(context.Background(), channel, usedKey, "gpt-5.4")
	if err != nil {
		t.Fatalf("buildProbeRequest returned error: %v", err)
	}
	if req.URL.Path != "/v1/responses" {
		t.Fatalf("expected /v1/responses, got %s", req.URL.Path)
	}
}

func TestBuildProbeRequestForEmbeddings(t *testing.T) {
	channel := &model.Channel{
		Type:     outbound.OutboundTypeOpenAIEmbedding,
		BaseUrls: []model.BaseUrl{{URL: "https://example.com/v1"}},
	}
	usedKey := &model.ChannelKey{ID: 1, ChannelKey: "sk-test"}

	req, err := buildProbeRequest(context.Background(), channel, usedKey, "text-embedding-3-large")
	if err != nil {
		t.Fatalf("buildProbeRequest returned error: %v", err)
	}
	if req.URL.Path != "/v1/embeddings" {
		t.Fatalf("expected /v1/embeddings, got %s", req.URL.Path)
	}
}

func TestResolveProbePromptUsesConfiguredPrompt(t *testing.T) {
	ctx := setupGroupHealthTestDB(t)
	if err := op.SettingRefreshCache(ctx); err != nil {
		t.Fatalf("SettingRefreshCache failed: %v", err)
	}
	if err := op.SettingSetString(model.SettingKeyGroupHealthProbePrompt, "  use this prompt  "); err != nil {
		t.Fatalf("SettingSetString failed: %v", err)
	}
	if got := resolveProbePrompt(); got != "use this prompt" {
		t.Fatalf("expected configured prompt, got %q", got)
	}
}

func TestResolveProbePromptFiltersBlankLines(t *testing.T) {
	ctx := setupGroupHealthTestDB(t)
	if err := op.SettingRefreshCache(ctx); err != nil {
		t.Fatalf("SettingRefreshCache failed: %v", err)
	}
	if err := op.SettingSetString(model.SettingKeyGroupHealthProbePrompt, "\nfirst\n  \nsecond\n"); err != nil {
		t.Fatalf("SettingSetString failed: %v", err)
	}
	allowed := map[string]bool{"first": true, "second": true}
	for i := 0; i < 20; i++ {
		if got := resolveProbePrompt(); !allowed[got] {
			t.Fatalf("unexpected configured prompt %q", got)
		}
	}
}

func TestResolveProbePromptFallsBackToBuiltIns(t *testing.T) {
	ctx := setupGroupHealthTestDB(t)
	if err := op.SettingRefreshCache(ctx); err != nil {
		t.Fatalf("SettingRefreshCache failed: %v", err)
	}
	if err := op.SettingSetString(model.SettingKeyGroupHealthProbePrompt, " \n\t"); err != nil {
		t.Fatalf("SettingSetString failed: %v", err)
	}
	allowed := make(map[string]bool, len(probePrompts))
	for _, prompt := range probePrompts {
		allowed[prompt] = true
	}
	for i := 0; i < 20; i++ {
		if got := resolveProbePrompt(); !allowed[got] {
			t.Fatalf("unexpected fallback prompt %q", got)
		}
	}
}
