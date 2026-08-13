package relay

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/modelvendor"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/visionbridge"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

// Handler 级集成测试：锁定 vision bridge 的核心不变量（backend-adversary 测试缺口清单）。
// 注意 analysis cache 是包级单例且 cacheKey 不含 VLM base URL——各用例必须用
// 不同的图片字节与焦点文本隔离缓存，否则前一个用例的描述会污染后一个。

// vbIntegrationEnv 一次搭好：VLM 假服务 + 全局设置 + 开了视觉桥的 API key + 能力索引。
type vbIntegrationEnv struct {
	vlmHits atomic.Int32
	apiKey  *model.APIKey
}

func setupVisionBridgeEnv(t *testing.T, vlmHandler http.HandlerFunc, keyOptIn bool) *vbIntegrationEnv {
	t.Helper()
	env := &vbIntegrationEnv{}

	// analysis cache 是包级单例且 TTL 远长于测试：-count>1 时上一轮的描述会命中缓存，
	// 破坏 VLM 调用计数断言——进出各重置一次
	visionbridge.ResetServiceForTest()
	t.Cleanup(visionbridge.ResetServiceForTest)

	vlmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env.vlmHits.Add(1)
		vlmHandler(w, r)
	}))
	t.Cleanup(vlmServer.Close)

	for key, value := range map[model.SettingKey]string{
		model.SettingKeyVisionBridgeEnabled: "true",
		model.SettingKeyVisionBridgeModel:   "vb-int-vlm",
		model.SettingKeyVisionBridgeBaseURL: vlmServer.URL,
	} {
		if err := op.SettingSetString(key, value); err != nil {
			t.Fatalf("SettingSetString(%s) failed: %v", key, err)
		}
	}
	t.Cleanup(func() {
		// 设置缓存是包级全局：清干净，避免污染同包后续不建 DB 的测试
		_ = op.SettingSetString(model.SettingKeyVisionBridgeEnabled, "false")
		_ = op.SettingSetString(model.SettingKeyVisionBridgeModel, "")
		_ = op.SettingSetString(model.SettingKeyVisionBridgeBaseURL, "")
	})

	modelvendor.ReplaceVisionIndex(map[string]modelvendor.VisionEntry{
		"vb-text-model":   {Provider: "openai", Vision: false},
		"vb-vision-model": {Provider: "openai", Vision: true},
	})
	t.Cleanup(func() { modelvendor.ReplaceVisionIndex(nil) })

	env.apiKey = &model.APIKey{Name: "vb-int-key", Enabled: true, VisionBridge: keyOptIn}
	if err := op.APIKeyCreate(env.apiKey, t.Context()); err != nil {
		t.Fatalf("APIKeyCreate failed: %v", err)
	}
	return env
}

// vbChatUpstream 起一个记录收到请求体的 OpenAI chat 假上游。
func vbChatUpstream(t *testing.T, modelName string) (*httptest.Server, *atomic.Int32, *atomic.Pointer[string]) {
	t.Helper()
	var hits atomic.Int32
	var lastBody atomic.Pointer[string]
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read upstream body: %v", err)
		}
		body := string(data)
		lastBody.Store(&body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"resp_vb","object":"chat.completion","created":1,"model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":"answered"}}]}`, modelName)
	}))
	t.Cleanup(server.Close)
	return server, &hits, &lastBody
}

func vbCreateChannel(t *testing.T, name, baseURL, modelName string) *model.Channel {
	t.Helper()
	channel := &model.Channel{
		Name:     name,
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: baseURL + "/v1"}},
		Model:    modelName,
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: name + "-key"}},
	}
	if err := op.ChannelCreate(channel, t.Context()); err != nil {
		t.Fatalf("ChannelCreate %s failed: %v", name, err)
	}
	return channel
}

func vbCreateGroup(t *testing.T, name string, items ...*model.GroupItem) *model.Group {
	t.Helper()
	group := &model.Group{Name: name, Mode: model.GroupModeFailover, RetryEnabled: false}
	if err := op.GroupCreate(group, t.Context()); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	for _, item := range items {
		item.GroupID = group.ID
		if err := op.GroupItemAdd(item, t.Context()); err != nil {
			t.Fatalf("GroupItemAdd failed: %v", err)
		}
	}
	return group
}

// vbImageDataURI 用可区分的填充字节生成合法 data URI（不同用例不同 identity，隔离缓存）。
// 填充字节须选 base64 编码后不含 +、/ 的值（如 0x11/0x22/0x33/0x44）：断言用
// strings.Contains 直接比对上游 JSON，编码器可能转义的字符会让负向断言静默变永真。
func vbImageDataURI(fill byte, size int) string {
	payload := make([]byte, size)
	for i := range payload {
		payload[i] = fill
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(payload)
}

func vbImageChatBody(groupName, focusText, dataURI string) string {
	return fmt.Sprintf(
		`{"model":%q,"messages":[{"role":"user","content":[{"type":"text","text":%q},{"type":"image_url","image_url":{"url":%q}}]}]}`,
		groupName, focusText, dataURI,
	)
}

func vbServeRequest(t *testing.T, apiKeyID int, body string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("api_key_id", apiKeyID)
	Handler(inbound.InboundTypeOpenAIChat, c)
	return recorder
}

// T1：已证实纯文本通道收到的必须是描述文本，绝不是原图（bridged 换入 + 禁用 rawBody 直通）。
func TestHandlerVisionBridgeReplacesImageForTextOnlyChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupRelayTestDB(t)

	description := strings.Repeat("红色圆形与蓝色三角组成的测试图。", 4)
	env := setupVisionBridgeEnv(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, chatCompletionJSON(description))
	}, true)

	upstream, upstreamHits, upstreamBody := vbChatUpstream(t, "vb-text-model")
	channel := vbCreateChannel(t, "vb-int-text", upstream.URL, "vb-text-model")
	vbCreateGroup(t, "vb-int-group-t1", &model.GroupItem{ChannelID: channel.ID, ModelName: "vb-text-model", Priority: 1, Weight: 1})

	dataURI := vbImageDataURI(0x11, 96)
	recorder := vbServeRequest(t, env.apiKey.ID, vbImageChatBody("vb-int-group-t1", "T1 这张图里有什么", dataURI))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 via bridged request, got %d body %s", recorder.Code, recorder.Body.String())
	}
	if env.vlmHits.Load() != 1 {
		t.Fatalf("expected exactly 1 VLM call, got %d", env.vlmHits.Load())
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("expected exactly 1 upstream call, got %d", upstreamHits.Load())
	}
	got := *upstreamBody.Load()
	rawPayload := strings.TrimPrefix(dataURI, "data:image/png;base64,")
	if strings.Contains(got, rawPayload) {
		t.Fatal("invariant broken: original image reached proven text-only upstream")
	}
	if !strings.Contains(got, "[Image 1") || !strings.Contains(got, "红色圆形") {
		t.Fatalf("upstream should receive marker + VLM description, got %s", got)
	}
}

// T2：视觉可用通道必须被稳定分区排到纯文本通道之前（即使 priority 落后），
// 原图直通视觉通道、VLM 与纯文本通道零调用。
func TestHandlerVisionBridgePrefersVisionChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupRelayTestDB(t)

	env := setupVisionBridgeEnv(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("VLM must not be called when a vision channel is available")
	}, true)

	textUpstream, textHits, _ := vbChatUpstream(t, "vb-text-model")
	visionUpstream, visionHits, visionBody := vbChatUpstream(t, "vb-vision-model")
	textChannel := vbCreateChannel(t, "vb-int-text2", textUpstream.URL, "vb-text-model")
	visionChannel := vbCreateChannel(t, "vb-int-vision", visionUpstream.URL, "vb-vision-model")
	// 纯文本通道 priority 更优：分区必须仍把视觉通道排前
	vbCreateGroup(t, "vb-int-group-t2",
		&model.GroupItem{ChannelID: textChannel.ID, ModelName: "vb-text-model", Priority: 1, Weight: 1},
		&model.GroupItem{ChannelID: visionChannel.ID, ModelName: "vb-vision-model", Priority: 2, Weight: 1},
	)

	dataURI := vbImageDataURI(0x22, 96)
	recorder := vbServeRequest(t, env.apiKey.ID, vbImageChatBody("vb-int-group-t2", "T2 描述这张图", dataURI))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 via vision channel, got %d body %s", recorder.Code, recorder.Body.String())
	}
	if visionHits.Load() != 1 || textHits.Load() != 0 {
		t.Fatalf("vision channel must be tried first (vision=%d text=%d)", visionHits.Load(), textHits.Load())
	}
	rawPayload := strings.TrimPrefix(dataURI, "data:image/png;base64,")
	if !strings.Contains(*visionBody.Load(), rawPayload) {
		t.Fatal("vision channel must receive the original image untouched")
	}
	if env.vlmHits.Load() != 0 {
		t.Fatalf("VLM must not be called, got %d calls", env.vlmHits.Load())
	}
}

// T3：VLM 链失败时 fail-closed——纯文本上游零调用，502 vision_fallback_exhausted，
// 且不回显 VLM 内网地址与上游错误细节。
func TestHandlerVisionBridgeFailClosedOnVLMFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupRelayTestDB(t)

	env := setupVisionBridgeEnv(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"vlm exploded with secret detail"}`, http.StatusInternalServerError)
	}, true)

	upstream, upstreamHits, _ := vbChatUpstream(t, "vb-text-model")
	channel := vbCreateChannel(t, "vb-int-text3", upstream.URL, "vb-text-model")
	vbCreateGroup(t, "vb-int-group-t3", &model.GroupItem{ChannelID: channel.ID, ModelName: "vb-text-model", Priority: 1, Weight: 1})

	recorder := vbServeRequest(t, env.apiKey.ID, vbImageChatBody("vb-int-group-t3", "T3 看图", vbImageDataURI(0x33, 96)))

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d body %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "vision_fallback_exhausted") {
		t.Fatalf("expected distinct error code, got %s", body)
	}
	if strings.Contains(body, "secret detail") || strings.Contains(body, "127.0.0.1") {
		t.Fatalf("VLM internals leaked to client: %s", body)
	}
	if upstreamHits.Load() != 0 {
		t.Fatalf("text-only upstream must never see the request, got %d hits", upstreamHits.Load())
	}
}

// T4：key 未开启视觉桥时行为与无此功能完全一致——原图照旧发给纯文本通道，VLM 零调用。
func TestHandlerVisionBridgeKeyOptOutKeepsBaseline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupRelayTestDB(t)

	env := setupVisionBridgeEnv(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("VLM must not be called for opt-out key")
	}, false)

	upstream, upstreamHits, upstreamBody := vbChatUpstream(t, "vb-text-model")
	channel := vbCreateChannel(t, "vb-int-text4", upstream.URL, "vb-text-model")
	vbCreateGroup(t, "vb-int-group-t4", &model.GroupItem{ChannelID: channel.ID, ModelName: "vb-text-model", Priority: 1, Weight: 1})

	dataURI := vbImageDataURI(0x44, 96)
	recorder := vbServeRequest(t, env.apiKey.ID, vbImageChatBody("vb-int-group-t4", "T4 看图", dataURI))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 baseline passthrough, got %d body %s", recorder.Code, recorder.Body.String())
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("expected upstream hit, got %d", upstreamHits.Load())
	}
	rawPayload := strings.TrimPrefix(dataURI, "data:image/png;base64,")
	if !strings.Contains(*upstreamBody.Load(), rawPayload) {
		t.Fatal("opt-out key must keep pre-feature behavior (image passes through)")
	}
	if env.vlmHits.Load() != 0 {
		t.Fatalf("VLM must not be called, got %d", env.vlmHits.Load())
	}
}

func chatCompletionJSON(content string) string {
	return fmt.Sprintf(`{"id":"vlm_resp","object":"chat.completion","created":1,"model":"vb-int-vlm","choices":[{"index":0,"message":{"role":"assistant","content":%q}}]}`, content)
}
