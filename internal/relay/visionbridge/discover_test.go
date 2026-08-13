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
	cfg := defaultConfig()
	cfg.CacheTTL = 15 * time.Minute
	cfg.URLCacheTTL = 2 * time.Minute
	return cfg
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
	// 缓存身份惰性计算（不再存 ImageRef 字段）：data URI 取 payload sha256，URL 取 URL 本身
	if id := imageIdentity(refs[0].URL); !strings.HasPrefix(id, "sha256:") {
		t.Fatalf("data URI identity should be hashed, got %q", id)
	}
	if id := imageIdentity(refs[1].URL); id != "https://example.com/a.png" {
		t.Fatalf("URL identity should be the URL itself, got %q", id)
	}
}

// Message.Images 是客户端可写旁路字段：藏在里面的图必须 fail-closed 整体报错，
// 不给绕过 bridge 的旁路（纯文本通道被跳过，vision 通道不受影响）。
func TestDiscoverRejectsImagesField(t *testing.T) {
	url := validDataURI(16)
	req := &model.InternalLLMRequest{Messages: []model.Message{
		{
			Role:    "user",
			Content: model.MessageContent{MultipleContent: []model.MessageContentPart{textPart("看图")}},
			Images:  []model.MessageContentPart{imagePart(url)},
		},
	}}
	if !req.HasImages() {
		t.Fatal("HasImages must scan Message.Images")
	}
	if _, err := Discover(req, testConfig()); err == nil || !strings.Contains(err.Error(), "images field") {
		t.Fatalf("expected fail-closed error for images field, got %v", err)
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
