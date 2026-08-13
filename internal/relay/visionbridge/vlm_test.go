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
	var gotPath, gotModel, gotAuth string
	_, cfg := vlmTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
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
	cfg.VisionAPIKey = "sk-test-vlm"
	refs := []ImageRef{{URL: validDataURI(32)}}
	text, err := analyzeChain(t.Context(), cfg, refs, "prompt")
	if err != nil {
		t.Fatalf("analyzeChain: %v", err)
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
	if gotAuth != "Bearer sk-test-vlm" {
		t.Fatalf("api key from settings not applied, got %q", gotAuth)
	}
}

func TestVLMAnalyzeNoAuthHeaderWhenKeyEmpty(t *testing.T) {
	var gotAuth atomic.Value
	_, cfg := vlmTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
		fmt.Fprint(w, chatResponse(strings.Repeat("local ollama description ", 3)))
	})
	if _, err := analyzeChain(t.Context(), cfg, []ImageRef{{URL: validDataURI(32)}}, "p"); err != nil {
		t.Fatalf("analyzeChain: %v", err)
	}
	if gotAuth.Load() != "" {
		t.Fatalf("empty key must not send Authorization, got %q", gotAuth.Load())
	}
}

// Step 0a 实测的静默降质形态：HTTP 200 + choices 为空，必须判失败。
func TestVLMAnalyzeEmptyChoicesRejected(t *testing.T) {
	_, cfg := vlmTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[],"id":"","model":""}`)
	})
	_, err := analyzeChain(t.Context(), cfg, []ImageRef{{URL: validDataURI(32)}}, "p")
	if err == nil || !strings.Contains(err.Error(), "no choices") {
		t.Fatalf("expected no-choices error, got %v", err)
	}
}

// Step 0a 修正 1：空/超短描述会诱发下游自信幻觉，必须判失败。
func TestVLMAnalyzeShortOutputRejected(t *testing.T) {
	_, cfg := vlmTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, chatResponse("太短"))
	})
	_, err := analyzeChain(t.Context(), cfg, []ImageRef{{URL: validDataURI(32)}}, "p")
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
	text, err := analyzeChain(t.Context(), cfg, []ImageRef{{URL: validDataURI(32)}}, "p")
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
	_, err := analyzeChain(t.Context(), cfg, []ImageRef{{URL: validDataURI(32)}}, "p")
	if err == nil || !strings.Contains(err.Error(), "all vlm models failed") {
		t.Fatalf("expected aggregate failure, got %v", err)
	}
}

func TestVLMAnalyzeTruncatesLongOutput(t *testing.T) {
	_, cfg := vlmTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, chatResponse(strings.Repeat("长", 500)))
	})
	cfg.MaxResultChars = 100
	text, err := analyzeChain(t.Context(), cfg, []ImageRef{{URL: validDataURI(32)}}, "p")
	if err != nil {
		t.Fatalf("analyzeChain: %v", err)
	}
	if got := len([]rune(text)); got != 100 {
		t.Fatalf("expected truncation to 100 runes, got %d", got)
	}
}

func TestProbeReportsPerModelResults(t *testing.T) {
	desc := strings.Repeat("红色圆形与蓝色三角形。", 4)
	_, cfg := vlmTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload.Model == "vlm-bad" {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"channel failed"}`)
			return
		}
		fmt.Fprint(w, chatResponse(desc))
	})
	cfg.VisionFallbackModels = []string{"vlm-bad"}
	results := Probe(t.Context(), cfg)
	if len(results) != 2 {
		t.Fatalf("expected 2 probe results, got %d", len(results))
	}
	if !results[0].OK || results[0].Model != "vlm-a" || results[0].Preview == "" {
		t.Fatalf("primary model probe unexpected: %+v", results[0])
	}
	if results[1].OK || !strings.Contains(results[1].Error, "404") {
		t.Fatalf("bad model probe should fail with 404: %+v", results[1])
	}
}

// 部分 vLLM/网关把 content 返回为内容块数组而非 string——解析必须两者兼容。
func TestVLMAnalyzeContentArrayCompat(t *testing.T) {
	desc := strings.Repeat("数组形态的视觉描述。", 6)
	_, cfg := vlmTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"choices":[{"message":{"content":[{"type":"text","text":%q},{"type":"text","text":"补充"}]}}]}`, desc)
	})
	text, err := analyzeChain(t.Context(), cfg, []ImageRef{{URL: validDataURI(32)}}, "prompt")
	if err != nil {
		t.Fatalf("content array response rejected: %v", err)
	}
	if !strings.Contains(text, "数组形态") || !strings.Contains(text, "补充") {
		t.Fatalf("array content parts lost: %q", text)
	}
}

// 主模型被重复写进备选列表是常见配置：链路必须去重（避免重复真实调用与重复 UI key）。
func TestModelChainDeduplicates(t *testing.T) {
	cfg := Config{VisionModel: "glm-4v", VisionFallbackModels: []string{" glm-4v ", "kimi-vl", "kimi-vl", ""}}
	got := modelChain(cfg)
	want := []string{"glm-4v", "kimi-vl"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("modelChain = %v, want %v", got, want)
	}
}
