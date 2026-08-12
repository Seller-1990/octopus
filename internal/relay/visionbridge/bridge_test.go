package visionbridge

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bestruirui/octopus/internal/modelvendor"
	"github.com/bestruirui/octopus/internal/transformer/model"
)

func newTestState(t *testing.T, cfg Config, req *model.InternalLLMRequest, canonicalVision *bool) *RequestState {
	t.Helper()
	refs, err := Discover(req, cfg)
	return &RequestState{
		svc:             newService(cfg),
		origReq:         req,
		refs:            refs,
		discoverErr:     err,
		focusHint:       lastUserText(req),
		canonicalVision: canonicalVision,
	}
}

func imageRequest() *model.InternalLLMRequest {
	return &model.InternalLLMRequest{Model: "some-model", Messages: []model.Message{
		multiContentMsg("user", textPart("图里是什么？"), imagePart(validDataURI(64))),
	}}
}

func boolPtr(b bool) *bool { return &b }

func TestActionForCanonicalFallback(t *testing.T) {
	cfg := testConfig()
	// 模型名不在 modelvendor 索引中 → 回落 canonical 证据
	if got := newTestState(t, cfg, imageRequest(), boolPtr(false)).ActionFor(1, "unknown-upstream-x"); got != ActionBridge {
		t.Fatalf("canonical vision=false should bridge, got %v", got)
	}
	if got := newTestState(t, cfg, imageRequest(), boolPtr(true)).ActionFor(1, "unknown-upstream-x"); got != ActionPassthrough {
		t.Fatalf("canonical vision=true should pass through, got %v", got)
	}
	// 双未知 → 保守直通（绝不误替换可能支持视觉的模型）
	if got := newTestState(t, cfg, imageRequest(), nil).ActionFor(1, "unknown-upstream-x"); got != ActionPassthrough {
		t.Fatalf("unknown capability should pass through, got %v", got)
	}
}

func TestActionForVendorIndexPrecedence(t *testing.T) {
	modelvendor.ReplaceVisionIndex(map[string]modelvendor.VisionEntry{
		"vbtest-vision-model": {Provider: "openai", Vision: true},
		"vbtest-text-model":   {Provider: "openai", Vision: false},
	})
	t.Cleanup(func() { modelvendor.ReplaceVisionIndex(nil) })

	cfg := testConfig()
	st := newTestState(t, cfg, imageRequest(), boolPtr(false))
	if got := st.ActionFor(1, "vbtest-vision-model"); got != ActionPassthrough {
		t.Fatalf("indexed vision model must beat canonical hint, got %v", got)
	}
	if got := st.ActionFor(1, "vbtest-text-model"); got != ActionBridge {
		t.Fatalf("indexed text model should bridge, got %v", got)
	}
}

func TestActionForTargetChannelScope(t *testing.T) {
	cfg := testConfig()
	cfg.TargetChannelIDs = map[int]struct{}{7: {}}
	st := newTestState(t, cfg, imageRequest(), boolPtr(false))
	if got := st.ActionFor(7, "unknown-upstream-x"); got != ActionBridge {
		t.Fatalf("in-scope channel should bridge, got %v", got)
	}
	if got := st.ActionFor(8, "unknown-upstream-x"); got != ActionPassthrough {
		t.Fatalf("out-of-scope channel must pass through, got %v", got)
	}
}

func TestBridgedRequestMemoizedAcrossChannels(t *testing.T) {
	longDesc := strings.Repeat("详细的联合视觉分析。", 8)
	var upstreamCalls atomic.Int32
	_, cfg := vlmTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		fmt.Fprint(w, chatResponse(longDesc))
	})
	st := newTestState(t, cfg, imageRequest(), boolPtr(false))

	first, err := st.BridgedRequest(t.Context())
	if err != nil {
		t.Fatalf("BridgedRequest: %v", err)
	}
	if first.HasImages() {
		t.Fatal("bridged request still has images")
	}
	second, err := st.BridgedRequest(t.Context())
	if err != nil {
		t.Fatalf("second BridgedRequest: %v", err)
	}
	if first != second {
		t.Fatal("bridged request should be memoized (same pointer)")
	}
	if upstreamCalls.Load() != 1 {
		t.Fatalf("expected exactly 1 VLM call, got %d", upstreamCalls.Load())
	}
}

func TestBridgedRequestMemoizesFailure(t *testing.T) {
	var upstreamCalls atomic.Int32
	_, cfg := vlmTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	})
	st := newTestState(t, cfg, imageRequest(), boolPtr(false))
	if _, err := st.BridgedRequest(t.Context()); err == nil {
		t.Fatal("expected VLM failure")
	}
	if _, err := st.BridgedRequest(t.Context()); err == nil {
		t.Fatal("expected memoized failure")
	}
	if upstreamCalls.Load() != 1 {
		t.Fatalf("failure should be memoized, got %d upstream calls", upstreamCalls.Load())
	}
}

func TestAnalysisCacheSharedAcrossRequests(t *testing.T) {
	longDesc := strings.Repeat("cache shared description. ", 4)
	var upstreamCalls atomic.Int32
	_, cfg := vlmTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		fmt.Fprint(w, chatResponse(longDesc))
	})
	shared := newService(cfg)
	makeState := func() *RequestState {
		req := imageRequest()
		refs, _ := Discover(req, cfg)
		return &RequestState{svc: shared, origReq: req, refs: refs, focusHint: lastUserText(req), canonicalVision: boolPtr(false)}
	}
	if _, err := makeState().BridgedRequest(t.Context()); err != nil {
		t.Fatalf("first request: %v", err)
	}
	if _, err := makeState().BridgedRequest(t.Context()); err != nil {
		t.Fatalf("second request: %v", err)
	}
	if upstreamCalls.Load() != 1 {
		t.Fatalf("second request should hit analysis cache, got %d upstream calls", upstreamCalls.Load())
	}
}

func TestBridgedRequestDiscoverErrorFailsClosed(t *testing.T) {
	cfg := testConfig()
	cfg.MaxImagesPerRequest = 1
	req := &model.InternalLLMRequest{Messages: []model.Message{
		multiContentMsg("user", imagePart(validDataURI(16)), imagePart(validDataURI(16))),
	}}
	st := newTestState(t, cfg, req, boolPtr(false))
	if _, err := st.BridgedRequest(t.Context()); err == nil || !strings.Contains(err.Error(), "too many images") {
		t.Fatalf("expected discover error to surface, got %v", err)
	}
}

func TestLastUserText(t *testing.T) {
	content := "字符串内容"
	req := &model.InternalLLMRequest{Messages: []model.Message{
		multiContentMsg("user", textPart("第一条")),
		{Role: "assistant", Content: model.MessageContent{Content: &content}},
		multiContentMsg("user", textPart("最后问题"), imagePart(validDataURI(16)), textPart("补充")),
	}}
	if got := lastUserText(req); got != "最后问题\n补充" {
		t.Fatalf("lastUserText = %q", got)
	}
}

// 默认配置（enabled=false）下 NewRequestState 必须返回 nil —— 功能关闭零开销。
func TestNewRequestStateDisabledByDefault(t *testing.T) {
	if st := NewRequestState(imageRequest(), nil); st != nil {
		t.Fatal("bridge must be inactive with default config")
	}
}
