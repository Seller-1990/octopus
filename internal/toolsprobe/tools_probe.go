// Package toolsprobe 探测渠道×模型是否支持 tools 调用。
// 独立于 op 包（op→helper→client→op 存在循环依赖，探测器需调用 helper 的 HTTP 发送，
// 故放到本包，init 时注入 op.ToolsProbeFn hook）。op 不 import 本包，无环。
package toolsprobe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func init() {
	op.ToolsProbeFn = Run
}

// toolsProbeSemaphore 限制并发探测请求数（节流）。
var toolsProbeSemaphore = make(chan struct{}, 1)

const probeTimeout = 12 * time.Second

// Run 探测渠道×模型是否支持 tools 调用（v3.1 判别矩阵）。
// toolChoice: ""=auto（自动探测）；"required"=手动测试（判别矩阵，含降级对照）。
// 返回 model.ToolsProbeResult；error 表示完全无法探测（embedding/无 key/构造失败）。
// 判别矩阵（每格状态见 model.ToolsProbeResult）：
//
//	auto 2xx            → accepted（true）
//	auto 4xx 白名单     → pending（第 1 次）/ unsupported（≥2）
//	auto 4xx 非白名单   → unknown
//	required 2xx+tool_call → executed（true，强证据）
//	required 2xx 无 tool_call → required_ignored（不写）
//	required 4xx → 降级 auto（同 auto 规则，返回 required_unsupported（auto 2xx）/ unsupported/pending/unknown）
//	required 非 4xx     → unknown
//	注：required-400 本身不进 ≥2 registry（R7），仅降级后的 auto-400 白名单进。
func Run(ctx context.Context, channel model.Channel, modelName, toolChoice string) (model.ToolsProbeResult, error) {
	if !outbound.IsChatChannelType(channel.Type) {
		return model.ToolsProbeResult{}, fmt.Errorf("channel type %d is not a chat channel, tools probe skipped", channel.Type)
	}
	var usedKey *model.ChannelKey
	for i := range channel.Keys {
		if channel.Keys[i].Enabled {
			usedKey = &channel.Keys[i]
			break
		}
	}
	if usedKey == nil || strings.TrimSpace(usedKey.ChannelKey) == "" {
		return model.ToolsProbeResult{}, fmt.Errorf("channel %d has no enabled key with non-empty secret, tools probe skipped", channel.ID)
	}

	toolsProbeSemaphore <- struct{}{}
	defer func() { <-toolsProbeSemaphore }()

	// toolChoice="required" → 手动判别矩阵（含降级对照）
	if toolChoice == "required" {
		return runRequiredProbe(ctx, &channel, usedKey, modelName)
	}
	// auto 模式（自动探测）
	return runAutoProbe(ctx, &channel, usedKey, modelName, ""), nil
}

// runRequiredProbe 手动 required 判别矩阵：required 请求 + 4xx 降级 auto 对照。
func runRequiredProbe(ctx context.Context, channel *model.Channel, usedKey *model.ChannelKey, modelName string) (model.ToolsProbeResult, error) {
	resp, statusCode, body, err := doProbeRequest(ctx, channel, usedKey, modelName, "required")
	if err != nil {
		return model.ToolsProbeResult{State: model.ToolsProbeStateUnknown}, err
	}
	_ = resp
	if statusCode >= 200 && statusCode < 300 {
		// required 2xx：读 body 判断是否有工具调用（按协议格式）
		if responseHasToolCall(body, channel.Type) {
			return model.ToolsProbeResult{State: model.ToolsProbeStateExecuted, Supports: true, Source: "manual"}, nil
		}
		// required 2xx 无 tool_call：不服从 required 或网关静默剥参（不写，可强制标不支持）
		return model.ToolsProbeResult{State: model.ToolsProbeStateRequiredIgnored}, nil
	}
	if statusCode >= 400 && statusCode < 500 {
		// 4xx：降级 auto 对照（required-400 本身不进 registry）
		return runAutoProbe(ctx, channel, usedKey, modelName, "manual-required-fallback"), nil
	}
	// 5xx/其他：渠道故障，不判定
	return model.ToolsProbeResult{State: model.ToolsProbeStateUnknown}, nil
}

// runAutoProbe auto 模式探测（自动探测或 required 降级对照）。
// fallbackSource 非空表示这是 required 4xx 降级后的 auto 请求。
func runAutoProbe(ctx context.Context, channel *model.Channel, usedKey *model.ChannelKey, modelName, fallbackSource string) model.ToolsProbeResult {
	resp, statusCode, body, err := doProbeRequest(ctx, channel, usedKey, modelName, "")
	if err != nil {
		return model.ToolsProbeResult{State: model.ToolsProbeStateUnknown}
	}
	_ = resp
	if statusCode >= 200 && statusCode < 300 {
		// auto 2xx：接受 tools 参数（弱证据）。降级来源用 manual-required-fallback。
		source := "probe"
		if fallbackSource != "" {
			source = fallbackSource
		}
		if fallbackSource != "" {
			// required 4xx → auto 2xx：支持但 required 不可用
			return model.ToolsProbeResult{State: model.ToolsProbeStateRequiredUnsupported, Supports: true, Source: source}
		}
		return model.ToolsProbeResult{State: model.ToolsProbeStateAccepted, Supports: true, Source: source}
	}
	// R4 修复：只有 4xx 才进入白名单累计——5xx/其他是网关故障，即使 body 含白名单词
	// （如 502 包裹原始错误文本）也不应把故障当模型能力结论写成 false。
	if statusCode < 400 || statusCode >= 500 {
		return model.ToolsProbeResult{State: model.ToolsProbeStateUnknown}
	}
	message := fmt.Sprintf("upstream error: %d: %s", statusCode, strings.TrimSpace(string(body)))
	if op.MatchToolsUnsupportedError(message) {
		// 白名单：≥2 确认才 unsupported；第 1 次 pending
		if op.ConfirmToolsUnsupportedOnce(channel.ID, modelName, message) {
			return model.ToolsProbeResult{State: model.ToolsProbeStateUnsupported, Source: "probe"}
		}
		return model.ToolsProbeResult{State: model.ToolsProbeStatePending}
	}
	return model.ToolsProbeResult{State: model.ToolsProbeStateUnknown}
}

// responseHasToolCall 判断 2xx 响应体是否含非 null 的工具调用（required 执行确认启发式）。
// R7 修复：从字符串扫描改为 encoding/json 结构化解析——原实现用 strings.Contains + 自制 token
// 判定，合法空白变体（`"type" : "tool_use"`）会漏判、正文含关键词（Gemini functionCall）会误判。
// 按协议分格式检测：OpenAI chat "tool_calls" 数组；Anthropic content 块 "type":"tool_use"；
// Gemini functionCall；OpenAI Responses "type":"function_call"。解析失败（结构异常/非 JSON）→ false。
func responseHasToolCall(body []byte, channelType outbound.OutboundType) bool {
	if len(body) == 0 {
		return false
	}
	switch channelType {
	case outbound.OutboundTypeAnthropic:
		var resp anthropicResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return false
		}
		for _, block := range resp.Content {
			if block.Type == "tool_use" {
				return true
			}
		}
		return false
	case outbound.OutboundTypeGemini:
		var resp geminiResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return false
		}
		for _, cand := range resp.Candidates {
			for _, part := range cand.Content.Parts {
				if part.FunctionCall != nil {
					return true
				}
			}
		}
		return false
	case outbound.OutboundTypeOpenAIResponse:
		var resp responsesResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return false
		}
		for _, item := range resp.Output {
			if item.Type == "function_call" {
				return true
			}
		}
		return false
	default:
		// OpenAI chat completions：choices[].message.tool_calls 非空数组
		var resp openAIChatResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return false
		}
		for _, choice := range resp.Choices {
			if choice.Message != nil && len(choice.Message.ToolCalls) > 0 {
				return true
			}
		}
		return false
	}
}

// 各协议最小响应结构（R7：只读取协议规定的数组/对象路径，不做字符串扫描）。
type openAIChatResponse struct {
	Choices []struct {
		Message *struct {
			ToolCalls []json.RawMessage `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
	} `json:"content"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				FunctionCall *json.RawMessage `json:"functionCall"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

type responsesResponse struct {
	Output []struct {
		Type string `json:"type"`
	} `json:"output"`
}

// doProbeRequest 发一次探测请求（独立 12s 预算），返回响应、状态码、body。
func doProbeRequest(ctx context.Context, channel *model.Channel, usedKey *model.ChannelKey, modelName, toolChoice string) (*http.Response, int, []byte, error) {
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	request, err := buildProbeRequest(probeCtx, channel, usedKey, modelName, toolChoice)
	if err != nil {
		return nil, 0, nil, err
	}
	policy, policyErr := op.ResolveHeaderPolicy(probeCtx, channel.ID, 0, 0)
	if policyErr != nil {
		policy = op.HeaderPolicyFailureFallback()
	}
	op.ApplyHeaderPolicy(request.Header, nil, channel.CustomHeader, policy)
	if request.Header.Get("User-Agent") == "" {
		request.Header.Set("User-Agent", "")
	}
	if err := helper.ApplyParamOverride(request, channel.ParamOverride); err != nil {
		return nil, 0, nil, err
	}
	httpClient, err := helper.ChannelHTTPClientWithContext(probeCtx, channel)
	if err != nil {
		return nil, 0, nil, err
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return nil, 0, nil, err
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 8*1024))
	return response, response.StatusCode, body, nil
}

func buildProbeRequest(ctx context.Context, channel *model.Channel, usedKey *model.ChannelKey, modelName, toolChoice string) (*http.Request, error) {
	if channel == nil {
		return nil, fmt.Errorf("channel is nil")
	}
	if usedKey == nil || strings.TrimSpace(usedKey.ChannelKey) == "" {
		return nil, fmt.Errorf("channel key is empty")
	}
	if strings.TrimSpace(modelName) == "" {
		return nil, fmt.Errorf("model name is empty")
	}
	request := buildProbeInternalRequest(channel.Type, modelName, toolChoice)
	adapter := outbound.Get(channel.Type)
	if adapter == nil {
		return nil, fmt.Errorf("unsupported outbound type: %d", channel.Type)
	}
	return adapter.TransformRequest(ctx, request, channel.GetBaseUrl(), usedKey.ChannelKey)
}

// buildProbeInternalRequest 构造带一个最小 function 定义的探测请求。
// toolChoice: "required" 时设置 tool_choice=required（手动判别）；"" 时用 auto。
// MaxTokens=128（工具调用输出 JSON 可能超过 16 token，避免截断）。
// 使用 op.ResolveProbePrompt 随机 prompt 防封禁（R9）。
func buildProbeInternalRequest(channelType outbound.OutboundType, modelName, toolChoice string) *transformerModel.InternalLLMRequest {
	stream := false
	ping := op.ResolveProbePrompt()
	tool := transformerModel.Tool{
		Type: "function",
		Function: transformerModel.Function{
			Name:        "get_weather",
			Description: "Get the current weather for a location.",
			Parameters:  []byte(`{"type":"object","properties":{"location":{"type":"string"}},"required":["location"]}`),
		},
	}
	tokens := int64(128)

	// tool_choice: required（"required" 是跨协议可透传的字符串形式，Anthropic/Gemini 转换器映射为 any/ANY）
	var toolChoiceRef *transformerModel.ToolChoice
	if toolChoice == "required" {
		required := "required"
		toolChoiceRef = &transformerModel.ToolChoice{ToolChoice: &required}
	}

	switch channelType {
	case outbound.OutboundTypeOpenAIResponse:
		return &transformerModel.InternalLLMRequest{
			Model:               modelName,
			RawAPIFormat:        transformerModel.APIFormatOpenAIResponse,
			Messages:            []transformerModel.Message{{Role: "user", Content: transformerModel.MessageContent{Content: &ping}}},
			Stream:              &stream,
			MaxCompletionTokens: &tokens,
			Tools:               []transformerModel.Tool{tool},
			ToolChoice:          toolChoiceRef,
		}
	case outbound.OutboundTypeAnthropic:
		return &transformerModel.InternalLLMRequest{
			Model:        modelName,
			RawAPIFormat: transformerModel.APIFormatAnthropicMessage,
			Messages:     []transformerModel.Message{{Role: "user", Content: transformerModel.MessageContent{Content: &ping}}},
			Stream:       &stream,
			MaxTokens:    &tokens,
			Tools:        []transformerModel.Tool{tool},
			ToolChoice:   toolChoiceRef,
		}
	case outbound.OutboundTypeGemini:
		return &transformerModel.InternalLLMRequest{
			Model:        modelName,
			RawAPIFormat: transformerModel.APIFormatGeminiContents,
			Messages:     []transformerModel.Message{{Role: "user", Content: transformerModel.MessageContent{Content: &ping}}},
			Stream:       &stream,
			MaxTokens:    &tokens,
			Tools:        []transformerModel.Tool{tool},
			ToolChoice:   toolChoiceRef,
		}
	default:
		return &transformerModel.InternalLLMRequest{
			Model:        modelName,
			RawAPIFormat: transformerModel.APIFormatOpenAIChatCompletion,
			Messages:     []transformerModel.Message{{Role: "user", Content: transformerModel.MessageContent{Content: &ping}}},
			Stream:       &stream,
			MaxTokens:    &tokens,
			Tools:        []transformerModel.Tool{tool},
			ToolChoice:   toolChoiceRef,
		}
	}
}
