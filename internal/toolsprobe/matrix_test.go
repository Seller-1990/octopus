package toolsprobe

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

// matrixFixture 构造带启用 key 的 chat 渠道，BaseURL 指向 mock server。
func matrixFixture(t *testing.T, serverURL string) model.Channel {
	t.Helper()
	key := model.ChannelKey{ChannelKey: "test-key", Enabled: true}
	return model.Channel{
		Name:     "matrix-channel",
		Type:     outbound.OutboundTypeOpenAIChat,
		BaseUrls: []model.BaseUrl{{URL: serverURL}},
		Keys:     []model.ChannelKey{key},
	}
}

// chatResp 构造 OpenAI chat completions 2xx 响应。
func chatResp(withToolCall bool) string {
	if withToolCall {
		return `{"id":"1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"location\":\"Beijing\"}"}}]},"finish_reason":"tool_calls"}]}`
	}
	return `{"id":"1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`
}

func errResp(status int, message string) string {
	body, _ := json.Marshal(map[string]any{"error": map[string]string{"message": message}})
	return string(body)
}

// matrixServer 按场景响应的 mock 上游。
// scene 结构：requiredStatus（0=不命中 required 分支，全部走 auto）、requiredBody、autoStatus、autoBody。
type matrixServer struct {
	requiredStatus int
	requiredBody   string
	autoStatus     int
	autoBody       string
	calls          []string
}

func (m *matrixServer) handler(w http.ResponseWriter, r *http.Request) {
	body := make([]byte, r.ContentLength)
	_, _ = r.Body.Read(body)
	isRequired := strings.Contains(string(body), `"tool_choice":"required"`)
	if isRequired {
		m.calls = append(m.calls, "required")
		w.WriteHeader(m.requiredStatus)
		_, _ = w.Write([]byte(m.requiredBody))
		return
	}
	m.calls = append(m.calls, "auto")
	w.WriteHeader(m.autoStatus)
	_, _ = w.Write([]byte(m.autoBody))
}

// probeErrorText 与 Run 内部构造的错误文本保持一致（registry 按 hash 累计，预热需同文）。
func probeErrorText(status int, body string) string {
	return fmt.Sprintf("upstream error: %d: %s", status, strings.TrimSpace(body))
}

// TestDiscriminationMatrix 判别矩阵 8 分支（v3.1，每格：返回态 / 写入值 / source）。
// 注：required 分支（executed/ignored）只走一次请求；4xx 降级走 required+auto 两次。
func TestDiscriminationMatrix(t *testing.T) {
	cases := []struct {
		name          string
		scene         *matrixServer
		toolChoice    string
		modelName     string
		preseedCount  int // ≥2 用例预热的白名单计数
		wantState     model.ToolsProbeState
		wantSupports  bool
		wantSource    string
		wantErr       bool
		wantCallCount int
	}{
		{
			name:       "required 200 + tool_call → executed(强 true)",
			scene:      &matrixServer{requiredStatus: 200, requiredBody: chatResp(true)},
			toolChoice: "required",
			modelName:  "matrix-exec",
			wantState:  model.ToolsProbeStateExecuted, wantSupports: true, wantSource: "manual",
			wantCallCount: 1,
		},
		{
			name:          "required 200 无 tool_call → required_ignored(不写)",
			scene:         &matrixServer{requiredStatus: 200, requiredBody: chatResp(false)},
			toolChoice:    "required",
			modelName:     "matrix-ignored",
			wantState:     model.ToolsProbeStateRequiredIgnored,
			wantCallCount: 1,
		},
		{
			name:       "required 400 → auto 2xx → required_unsupported(弱 true)",
			scene:      &matrixServer{requiredStatus: 400, requiredBody: errResp(400, "bad request"), autoStatus: 200, autoBody: chatResp(false)},
			toolChoice: "required",
			modelName:  "matrix-fallback",
			wantState:  model.ToolsProbeStateRequiredUnsupported, wantSupports: true, wantSource: "manual-required-fallback",
			wantCallCount: 2,
		},
		{
			name:       "auto 2xx → accepted(弱 true)",
			scene:      &matrixServer{autoStatus: 200, autoBody: chatResp(false)},
			toolChoice: "",
			modelName:  "matrix-accepted",
			wantState:  model.ToolsProbeStateAccepted, wantSupports: true, wantSource: "probe",
			wantCallCount: 1,
		},
		{
			name:          "required 400 → auto 400 白名单第 1 次 → pending(不写)",
			scene:         &matrixServer{requiredStatus: 400, requiredBody: errResp(400, "x"), autoStatus: 400, autoBody: errResp(400, "tools not supported")},
			toolChoice:    "required",
			modelName:     "matrix-pending",
			wantState:     model.ToolsProbeStatePending,
			wantCallCount: 2,
		},
		{
			name:         "required 400 → auto 400 白名单 ≥2 → unsupported(false)",
			scene:        &matrixServer{requiredStatus: 400, requiredBody: errResp(400, "x"), autoStatus: 400, autoBody: errResp(400, "tools not supported")},
			toolChoice:   "required",
			modelName:    "matrix-unsupported",
			preseedCount: 1,
			wantState:    model.ToolsProbeStateUnsupported, wantSupports: false, wantSource: "probe",
			wantCallCount: 2,
		},
		{
			name:          "required 400 → auto 400 非白名单 → unknown(不判定)",
			scene:         &matrixServer{requiredStatus: 400, requiredBody: errResp(400, "x"), autoStatus: 400, autoBody: errResp(400, "model not found")},
			toolChoice:    "required",
			modelName:     "matrix-unknown",
			wantState:     model.ToolsProbeStateUnknown,
			wantCallCount: 2,
		},
		{
			name:          "required 非 4xx（5xx）→ unknown(不判定)",
			scene:         &matrixServer{requiredStatus: 500, requiredBody: errResp(500, "internal error")},
			toolChoice:    "required",
			modelName:     "matrix-5xx",
			wantState:     model.ToolsProbeStateUnknown,
			wantCallCount: 1,
		},
	}

	ctx := setupBatchTestDB(t)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.scene.calls = nil
			srv := httptest.NewServer(http.HandlerFunc(tc.scene.handler))
			defer srv.Close()

			channel := matrixFixture(t, srv.URL)
			// ≥2 用例预热白名单计数（同文同 key，达到第 2 次确认）
			for i := 0; i < tc.preseedCount; i++ {
				op.ConfirmToolsUnsupportedOnce(0, tc.modelName, probeErrorText(400, errResp(400, "tools not supported")))
			}
			result, err := Run(ctx, channel, tc.modelName, tc.toolChoice)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got none (result=%+v)", result)
				}
				return
			}
			if err != nil {
				t.Fatalf("Run returned unexpected error: %v", err)
			}
			if result.State != tc.wantState {
				t.Fatalf("state = %s, want %s (result=%+v)", result.State, tc.wantState, result)
			}
			if result.Supports != tc.wantSupports {
				t.Fatalf("supports = %v, want %v", result.Supports, tc.wantSupports)
			}
			if result.Source != tc.wantSource {
				t.Fatalf("source = %q, want %q", result.Source, tc.wantSource)
			}
			if len(tc.scene.calls) != tc.wantCallCount {
				t.Fatalf("call sequence = %v, want %d calls", tc.scene.calls, tc.wantCallCount)
			}
		})
	}
}

// TestRunSkipsNonChatChannel 非 chat 渠道直接跳过。
func TestRunSkipsNonChatChannel(t *testing.T) {
	ctx := setupBatchTestDB(t)
	channel := model.Channel{Name: "embed", Type: outbound.OutboundTypeOpenAIEmbedding, BaseUrls: []model.BaseUrl{{URL: "http://127.0.0.1:1"}}, Keys: []model.ChannelKey{{ChannelKey: "k", Enabled: true}}}
	_, err := Run(ctx, channel, "m", "")
	if err == nil {
		t.Fatalf("non-chat channel must be rejected")
	}
}

// TestRunSkipsChannelWithoutKey 无启用 key 直接跳过。
func TestRunSkipsChannelWithoutKey(t *testing.T) {
	ctx := setupBatchTestDB(t)
	channel := model.Channel{Name: "nokey", Type: outbound.OutboundTypeOpenAIChat, BaseUrls: []model.BaseUrl{{URL: "http://127.0.0.1:1"}}, Keys: []model.ChannelKey{{ChannelKey: "", Enabled: true}}}
	_, err := Run(ctx, channel, "m", "")
	if err == nil {
		t.Fatalf("channel without key must be rejected")
	}
}

// TestResponseHasToolCallByProtocol P1 回归：required 执行确认按协议格式检测
// （OpenAI chat / Anthropic tool_use / Gemini functionCall / Responses function_call）。
func TestResponseHasToolCallByProtocol(t *testing.T) {
	openAIResp := chatResp(true)
	anthropicResp := `{"id":"1","type":"message","content":[{"type":"tool_use","id":"tu_1","name":"get_weather","input":{"location":"Beijing"}}],"stop_reason":"tool_use"}`
	geminiResp := `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"get_weather","args":{"location":"Beijing"}}}]}}]}`
	responsesResp := `{"id":"1","output":[{"type":"function_call","name":"get_weather","arguments":"{\"location\":\"Beijing\"}"}],"status":"completed"}`

	cases := []struct {
		name        string
		body        string
		channelType outbound.OutboundType
		want        bool
	}{
		{"openai chat tool_calls", openAIResp, outbound.OutboundTypeOpenAIChat, true},
		{"openai chat no tool_calls", chatResp(false), outbound.OutboundTypeOpenAIChat, false},
		{"openai chat tool_calls null", `{"choices":[{"message":{"role":"assistant","content":"ok","tool_calls":null}}]}`, outbound.OutboundTypeOpenAIChat, false},
		{"openai chat tool_calls null space", `{"choices":[{"message":{"role":"assistant","content":"ok","tool_calls" : null}}]}`, outbound.OutboundTypeOpenAIChat, false},
		{"openai chat tool_calls empty array", `{"choices":[{"message":{"role":"assistant","content":"ok","tool_calls":[]}}]}`, outbound.OutboundTypeOpenAIChat, false},
		{"anthropic tool_use", anthropicResp, outbound.OutboundTypeAnthropic, true},
		{"gemini functionCall", geminiResp, outbound.OutboundTypeGemini, true},
		{"responses function_call", responsesResp, outbound.OutboundTypeOpenAIResponse, true},
		// R7 修复回归：pretty JSON（空白变体）与正文关键词误判
		{"openai chat pretty json", "{\n  \"choices\": [\n    {\n      \"message\": {\n        \"role\": \"assistant\",\n        \"tool_calls\": [\n          { \"id\": \"call_1\", \"type\": \"function\", \"function\": { \"name\": \"get_weather\", \"arguments\": \"{}\" } }\n        ]\n      }\n    }\n  ]\n}", outbound.OutboundTypeOpenAIChat, true},
		{"anthropic tool_use space", `{"content":[{"type" : "tool_use"}]}`, outbound.OutboundTypeAnthropic, true},
		{"gemini keyword in text (no call)", `{"candidates":[{"content":{"parts":[{"text":"the word functionCall appears in prose"}]}}]}`, outbound.OutboundTypeGemini, false},
		{"gemini pretty functionCall", "{\n  \"candidates\": [\n    {\n      \"content\": {\n        \"parts\": [\n          { \"functionCall\": { \"name\": \"get_weather\" } }\n        ]\n      }\n    }\n  ]\n}", outbound.OutboundTypeGemini, true},
		{"responses pretty function_call", "{\n  \"output\": [\n    { \"type\": \"function_call\", \"name\": \"get_weather\" }\n  ]\n}", outbound.OutboundTypeOpenAIResponse, true},
		{"openai chat no choices", `{"id":"1"}`, outbound.OutboundTypeOpenAIChat, false},
		{"invalid json", `not-json{{{`, outbound.OutboundTypeOpenAIChat, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := responseHasToolCall([]byte(tc.body), tc.channelType); got != tc.want {
				t.Fatalf("responseHasToolCall(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
