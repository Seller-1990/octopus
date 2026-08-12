package visionbridge

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func vlmTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, Config) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg := testConfig()
	cfg.VisionModel = "vlm-a"
	cfg.VisionBaseURL = server.URL
	cfg.RequestTimeout = 10 * time.Second
	return server, cfg
}

func chatResponse(content string) string {
	body, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{{"message": map[string]any{"content": content}}},
	})
	return string(body)
}

func TestVLMAnalyzeSuccess(t *testing.T) {
	longDesc := strings.Repeat("图中是一段测试描述。", 10)
	var gotPath, gotModel string
	_, cfg := vlmTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var payload struct {
			Model    string `json:"model"`
			Stream   bool   `json:"stream"`
			Messages []struct {
				Content []map[string]any `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		gotModel = payload.Model
		if payload.Stream {
			t.Error("vlm request must be non-stream")
		}
		if len(payload.Messages) != 1 || len(payload.Messages[0].Content) != 2 {
			t.Errorf("expected 1 message with prompt+1 image, got %+v", payload.Messages)
		}
		fmt.Fprint(w, chatResponse(longDesc))
	})
	client := newVLMClient(cfg)
	refs := []ImageRef{{URL: validDataURI(32)}}
	text, err := client.Analyze(t.Context(), refs, "prompt")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if text != longDesc {
		t.Fatalf("unexpected text: %q", text)
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("unexpected path %q", gotPath)
	}
	if gotModel != "vlm-a" {
		t.Fatalf("unexpected model %q", gotModel)
	}
}

// Step 0a 实测的静默降质形态：HTTP 200 + choices 为空，必须判失败。
func TestVLMAnalyzeEmptyChoicesRejected(t *testing.T) {
	_, cfg := vlmTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[],"id":"","model":""}`)
	})
	client := newVLMClient(cfg)
	_, err := client.Analyze(t.Context(), []ImageRef{{URL: validDataURI(32)}}, "p")
	if err == nil || !strings.Contains(err.Error(), "no choices") {
		t.Fatalf("expected no-choices error, got %v", err)
	}
}

// Step 0a 修正 1：空/超短描述会诱发下游自信幻觉，必须判失败。
func TestVLMAnalyzeShortOutputRejected(t *testing.T) {
	_, cfg := vlmTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, chatResponse("太短"))
	})
	client := newVLMClient(cfg)
	_, err := client.Analyze(t.Context(), []ImageRef{{URL: validDataURI(32)}}, "p")
	if err == nil || !strings.Contains(err.Error(), "too short") {
		t.Fatalf("expected too-short error, got %v", err)
	}
}

func TestVLMAnalyzeFallbackChain(t *testing.T) {
	longDesc := strings.TrimSpace(strings.Repeat("fallback description. ", 5))
	var calls atomic.Int32
	_, cfg := vlmTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		calls.Add(1)
		if payload.Model == "vlm-a" {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":"rate limited"}`)
			return
		}
		fmt.Fprint(w, chatResponse(longDesc))
	})
	cfg.VisionFallbackModels = []string{"vlm-b"}
	client := newVLMClient(cfg)
	text, err := client.Analyze(t.Context(), []ImageRef{{URL: validDataURI(32)}}, "p")
	if err != nil {
		t.Fatalf("fallback should succeed: %v", err)
	}
	if text != longDesc {
		t.Fatalf("unexpected text %q", text)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected 2 upstream calls, got %d", calls.Load())
	}
}

func TestVLMAnalyzeAllModelsFailed(t *testing.T) {
	_, cfg := vlmTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	cfg.VisionFallbackModels = []string{"vlm-b"}
	client := newVLMClient(cfg)
	_, err := client.Analyze(t.Context(), []ImageRef{{URL: validDataURI(32)}}, "p")
	if err == nil || !strings.Contains(err.Error(), "all vlm models failed") {
		t.Fatalf("expected aggregate failure, got %v", err)
	}
}

func TestVLMAnalyzeTruncatesLongOutput(t *testing.T) {
	_, cfg := vlmTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, chatResponse(strings.Repeat("长", 500)))
	})
	cfg.MaxResultChars = 100
	client := newVLMClient(cfg)
	text, err := client.Analyze(t.Context(), []ImageRef{{URL: validDataURI(32)}}, "p")
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got := len([]rune(text)); got != 100 {
		t.Fatalf("expected truncation to 100 runes, got %d", got)
	}
}
