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

func newTestService() *service {
	return &service{cache: newAnalysisCache(16), limiter: newInflightLimiter(4)}
}

func newTestState(t *testing.T, cfg Config, req *model.InternalLLMRequest, canonicalName string, canonicalVision *bool) *RequestState {
	t.Helper()
	refs, err := Discover(req, cfg)
	return &RequestState{
		svc:             newTestService(),
		cfg:             cfg,
		origReq:         req,
		refs:            refs,
		discoverErr:     err,
		focusHint:       lastUserText(req),
		canonicalName:   canonicalName,
		canonicalVision: canonicalVision,
		capMemo:         make(map[string]visionCap),
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
	// 模型名不在 modelvendor 索引中且与 canonical 同名 → 回落 canonical 证据
	if got := newTestState(t, cfg, imageRequest(), "unknown-upstream-x", boolPtr(false)).ActionFor("unknown-upstream-x"); got != ActionBridge {
		t.Fatalf("canonical vision=false should bridge, got %v", got)
	}
	if got := newTestState(t, cfg, imageRequest(), "unknown-upstream-x", boolPtr(true)).ActionFor("unknown-upstream-x"); got != ActionPassthrough {
		t.Fatalf("canonical vision=true should pass through, got %v", got)
	}
	// 上游模型与 canonical 不同名 → canonical 能力不可传染，保守直通
	if got := newTestState(t, cfg, imageRequest(), "deepseek-r1", boolPtr(false)).ActionFor("my-vlm-alias"); got != ActionPassthrough {
		t.Fatalf("canonical evidence must not apply to differently-named upstream model, got %v", got)
	}
	// 双未知 → 保守直通（绝不误替换可能支持视觉的模型）
	if got := newTestState(t, cfg, imageRequest(), "", nil).ActionFor("unknown-upstream-x"); got != ActionPassthrough {
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
	st := newTestState(t, cfg, imageRequest(), "vbtest-vision-model", boolPtr(false))
	if got := st.ActionFor("vbtest-vision-model"); got != ActionPassthrough {
		t.Fatalf("indexed vision model must beat canonical hint, got %v", got)
	}
	if got := st.ActionFor("vbtest-text-model"); got != ActionBridge {
		t.Fatalf("indexed text model should bridge, got %v", got)
	}
}

// 能力判定结果在请求生命周期内快照：索引热替换不影响已判定的模型
// （分区与换入判定必须看到同一结论，否则原图可能直通纯文本模型）。
func TestVisionCapabilityMemoizedAgainstIndexSwap(t *testing.T) {
	modelvendor.ReplaceVisionIndex(map[string]modelvendor.VisionEntry{
		"vbtest-swap-model": {Provider: "openai", Vision: false},
	})
	t.Cleanup(func() { modelvendor.ReplaceVisionIndex(nil) })

	st := newTestState(t, testConfig(), imageRequest(), "", nil)
	if got := st.ActionFor("vbtest-swap-model"); got != ActionBridge {
		t.Fatalf("indexed text model should bridge, got %v", got)
	}
	// 模拟同步任务热替换索引：该模型从索引中消失
	modelvendor.ReplaceVisionIndex(nil)
	if got := st.ActionFor("vbtest-swap-model"); got != ActionBridge {
		t.Fatalf("capability must be memoized across index swap, got %v", got)
	}
}

func TestBridgedRequestMemoizedAcrossChannels(t *testing.T) {
	longDesc := strings.Repeat("详细的联合视觉分析。", 8)
	var upstreamCalls atomic.Int32
	_, cfg := vlmTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		fmt.Fprint(w, chatResponse(longDesc))
	})
	st := newTestState(t, cfg, imageRequest(), "", boolPtr(false))

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
	st := newTestState(t, cfg, imageRequest(), "", boolPtr(false))
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
	shared := newTestService()
	makeState := func() *RequestState {
		req := imageRequest()
		refs, _ := Discover(req, cfg)
		return &RequestState{svc: shared, cfg: cfg, origReq: req, refs: refs, focusHint: lastUserText(req), canonicalVision: boolPtr(false), capMemo: make(map[string]visionCap)}
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
	st := newTestState(t, cfg, req, "", boolPtr(false))
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

// 默认设置（未配置/enabled=false）下 NewRequestState 必须返回 nil —— 功能关闭零开销。
func TestNewRequestStateInactiveByDefault(t *testing.T) {
	if st := NewRequestState(imageRequest(), "", nil, true); st != nil {
		t.Fatal("bridge must be inactive with default settings")
	}
}

// key 级开关关闭时，无论全局设置如何都不做任何工作。
func TestNewRequestStateKeyOptOut(t *testing.T) {
	if st := NewRequestState(imageRequest(), "", nil, false); st != nil {
		t.Fatal("key opt-out must return nil state")
	}
}
