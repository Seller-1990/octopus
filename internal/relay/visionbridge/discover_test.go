package visionbridge

import (
	"strings"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func textPart(s string) model.MessageContentPart {
	return model.MessageContentPart{Type: "text", Text: &s}
}

func imagePart(url string) model.MessageContentPart {
	return model.MessageContentPart{Type: "image_url", ImageURL: &model.ImageURL{URL: url}}
}

func multiContentMsg(role string, parts ...model.MessageContentPart) model.Message {
	return model.Message{Role: role, Content: model.MessageContent{MultipleContent: parts}}
}

func testConfig() Config {
	return Config{
		MaxImagesPerRequest:    8,
		MaxRequestBytes:        20 * 1024 * 1024,
		MaxImageReferenceBytes: 15 * 1024 * 1024,
		MaxResultChars:         20000,
		MinResultChars:         30,
		CacheSize:              16,
		CacheTTL:               15 * time.Minute,
		URLCacheTTL:            2 * time.Minute,
	}
}

func TestDiscoverOrderAndToolMessages(t *testing.T) {
	req := &model.InternalLLMRequest{Messages: []model.Message{
		multiContentMsg("user", textPart("看这两张图"), imagePart(validDataURI(64)), imagePart("https://example.com/a.png")),
		multiContentMsg("assistant", textPart("好的")),
		multiContentMsg("tool", textPart("result"), imagePart("https://example.com/b.png")),
	}}
	refs, err := Discover(req, testConfig())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(refs) != 3 {
		t.Fatalf("expected 3 refs, got %d", len(refs))
	}
	if refs[0].MessageIndex != 0 || refs[0].PartIndex != 1 || !refs[0].IsDataURI {
		t.Fatalf("ref[0] unexpected: %+v", refs[0])
	}
	if refs[2].MessageIndex != 2 || refs[2].URL != "https://example.com/b.png" {
		t.Fatalf("tool message image missed: %+v", refs[2])
	}
	if !strings.HasPrefix(refs[0].Identity, "sha256:") {
		t.Fatalf("data URI identity should be hashed, got %q", refs[0].Identity)
	}
	if refs[1].Identity != "https://example.com/a.png" {
		t.Fatalf("URL identity should be the URL itself, got %q", refs[1].Identity)
	}
}

func TestDiscoverNoImages(t *testing.T) {
	req := &model.InternalLLMRequest{Messages: []model.Message{
		multiContentMsg("user", textPart("纯文本")),
	}}
	refs, err := Discover(req, testConfig())
	if err != nil || refs != nil {
		t.Fatalf("expected nil,nil got %v,%v", refs, err)
	}
}

func TestDiscoverTooManyImages(t *testing.T) {
	cfg := testConfig()
	cfg.MaxImagesPerRequest = 2
	req := &model.InternalLLMRequest{Messages: []model.Message{
		multiContentMsg("user",
			imagePart(validDataURI(16)), imagePart(validDataURI(16)), imagePart(validDataURI(16))),
	}}
	if _, err := Discover(req, cfg); err == nil || !strings.Contains(err.Error(), "too many images") {
		t.Fatalf("expected too-many-images error, got %v", err)
	}
}

func TestDiscoverTotalBytesLimit(t *testing.T) {
	cfg := testConfig()
	cfg.MaxRequestBytes = 100
	req := &model.InternalLLMRequest{Messages: []model.Message{
		multiContentMsg("user", imagePart(validDataURI(96)), imagePart(validDataURI(96))),
	}}
	if _, err := Discover(req, cfg); err == nil || !strings.Contains(err.Error(), "total size limit") {
		t.Fatalf("expected total-size error, got %v", err)
	}
}

func TestDiscoverInvalidImageFailsClosed(t *testing.T) {
	req := &model.InternalLLMRequest{Messages: []model.Message{
		multiContentMsg("user", imagePart(validDataURI(16)), imagePart("file:///etc/passwd")),
	}}
	if _, err := Discover(req, testConfig()); err == nil {
		t.Fatal("invalid reference should fail the whole discovery")
	}
}

func TestHasImages(t *testing.T) {
	withImage := &model.InternalLLMRequest{Messages: []model.Message{
		multiContentMsg("user", textPart("hi"), imagePart(validDataURI(16))),
	}}
	if !withImage.HasImages() {
		t.Fatal("HasImages should be true")
	}
	plain := &model.InternalLLMRequest{Messages: []model.Message{
		multiContentMsg("user", textPart("hi")),
	}}
	if plain.HasImages() {
		t.Fatal("HasImages should be false for text-only request")
	}
}
