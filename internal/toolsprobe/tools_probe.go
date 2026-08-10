// Package toolsprobe 探测渠道×模型是否支持 tools 调用。
// 独立于 op 包（op→helper→client→op 存在循环依赖，探测器需调用 helper 的 HTTP 发送，
// 故放到本包，init 时注入 op.ToolsProbeFn hook）。op 不 import 本包，无环。
package toolsprobe

import (
	"context"
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
var toolsProbeSemaphore = make(chan struct{}, 4)

// Run 探测渠道×模型是否支持 tools 调用。
// 判定：2xx → true（接受 tools 参数，T4 语义收窄）；命中白名单且 ≥2 次 → false；
// 其他错误 / embedding 渠道 / 无 enabled key → error（保持 nil 未探测态）。
func Run(ctx context.Context, channel model.Channel, modelName string) (bool, error) {
	if !outbound.IsChatChannelType(channel.Type) {
		return false, fmt.Errorf("channel type %d is not a chat channel, tools probe skipped", channel.Type)
	}
	var usedKey *model.ChannelKey
	for i := range channel.Keys {
		if channel.Keys[i].Enabled {
			usedKey = &channel.Keys[i]
			break
		}
	}
	if usedKey == nil {
		return false, fmt.Errorf("channel %d has no enabled key, tools probe skipped", channel.ID)
	}

	toolsProbeSemaphore <- struct{}{}
	defer func() { <-toolsProbeSemaphore }()

	probeCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	request, err := buildProbeRequest(probeCtx, &channel, usedKey, modelName)
	if err != nil {
		return false, err
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
		return false, err
	}
	httpClient, err := helper.ChannelHTTPClientWithContext(probeCtx, &channel)
	if err != nil {
		return false, err
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		// 2xx：接受 tools 参数（无法区分静默剥参，文档明示）
		return true, nil
	}
	body, _ := io.ReadAll(io.LimitReader(response.Body, 8*1024))
	message := fmt.Sprintf("upstream error: %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	if op.MatchToolsUnsupportedError(message) {
		// FIX-F：探测命中白名单经 ≥2 次确认 → 返回 (false, nil) 让回填写 false（此前只返回 error 不回填，是死代码）
		if op.ConfirmToolsUnsupportedOnce(channel.ID, modelName, message) {
			return false, nil
		}
		return false, fmt.Errorf("tools unsupported detected, awaiting confirmation")
	}
	return false, fmt.Errorf("%s", message)
}

func buildProbeRequest(ctx context.Context, channel *model.Channel, usedKey *model.ChannelKey, modelName string) (*http.Request, error) {
	if channel == nil {
		return nil, fmt.Errorf("channel is nil")
	}
	if usedKey == nil || strings.TrimSpace(usedKey.ChannelKey) == "" {
		return nil, fmt.Errorf("channel key is empty")
	}
	if strings.TrimSpace(modelName) == "" {
		return nil, fmt.Errorf("model name is empty")
	}
	request := buildProbeInternalRequest(channel.Type, modelName)
	adapter := outbound.Get(channel.Type)
	if adapter == nil {
		return nil, fmt.Errorf("unsupported outbound type: %d", channel.Type)
	}
	return adapter.TransformRequest(ctx, request, channel.GetBaseUrl(), usedKey.ChannelKey)
}

// buildProbeInternalRequest 构造带一个最小 function 定义的探测请求。
// MaxTokens=128（工具参数 JSON 可能超过 16 token，避免截断误判）。
func buildProbeInternalRequest(channelType outbound.OutboundType, modelName string) *transformerModel.InternalLLMRequest {
	stream := false
	ping := "Reply with the word ok."
	tool := transformerModel.Tool{
		Type: "function",
		Function: transformerModel.Function{
			Name:        "get_weather",
			Description: "Get the current weather for a location.",
			Parameters:  []byte(`{"type":"object","properties":{"location":{"type":"string"}},"required":["location"]}`),
		},
	}
	tokens := int64(128)

	switch channelType {
	case outbound.OutboundTypeOpenAIResponse:
		return &transformerModel.InternalLLMRequest{
			Model:               modelName,
			RawAPIFormat:        transformerModel.APIFormatOpenAIResponse,
			Messages:            []transformerModel.Message{{Role: "user", Content: transformerModel.MessageContent{Content: &ping}}},
			Stream:              &stream,
			MaxCompletionTokens: &tokens,
			Tools:               []transformerModel.Tool{tool},
		}
	case outbound.OutboundTypeAnthropic:
		return &transformerModel.InternalLLMRequest{
			Model:        modelName,
			RawAPIFormat: transformerModel.APIFormatAnthropicMessage,
			Messages:     []transformerModel.Message{{Role: "user", Content: transformerModel.MessageContent{Content: &ping}}},
			Stream:       &stream,
			MaxTokens:    &tokens,
			Tools:        []transformerModel.Tool{tool},
		}
	case outbound.OutboundTypeGemini:
		return &transformerModel.InternalLLMRequest{
			Model:        modelName,
			RawAPIFormat: transformerModel.APIFormatGeminiContents,
			Messages:     []transformerModel.Message{{Role: "user", Content: transformerModel.MessageContent{Content: &ping}}},
			Stream:       &stream,
			MaxTokens:    &tokens,
			Tools:        []transformerModel.Tool{tool},
		}
	default:
		return &transformerModel.InternalLLMRequest{
			Model:        modelName,
			RawAPIFormat: transformerModel.APIFormatOpenAIChatCompletion,
			Messages:     []transformerModel.Message{{Role: "user", Content: transformerModel.MessageContent{Content: &ping}}},
			Stream:       &stream,
			MaxTokens:    &tokens,
			Tools:        []transformerModel.Tool{tool},
		}
	}
}
