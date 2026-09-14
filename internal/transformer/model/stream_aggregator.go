package model

import (
	"sort"
	"strings"
)

type StreamAggregator struct {
	chunks []*InternalLLMResponse
}

func (a *StreamAggregator) Add(chunk *InternalLLMResponse) {
	if chunk == nil || chunk.Object == "[DONE]" {
		return
	}
	a.chunks = append(a.chunks, chunk)
}

func (a *StreamAggregator) Reset() {
	a.chunks = nil
}

// choiceAccumulator 在单次 Response() 合并过程中用 strings.Builder 累加增量
// 字符串字段，最后一次性物化。直接对 Message 字段做 `+=` 是 O(N²) 拷贝——
// 200KB 文本 / 1000 分片 ≈ 100MB memcpy，音频转写可达秒级停顿。
type choiceAccumulator struct {
	choice     *Choice
	content    strings.Builder
	reasoning  strings.Builder
	audioData  strings.Builder
	audioTrans strings.Builder
}

func (a *StreamAggregator) Response() *InternalLLMResponse {
	if a == nil || len(a.chunks) == 0 {
		return nil
	}

	firstChunk := a.chunks[0]
	result := &InternalLLMResponse{
		ID:                firstChunk.ID,
		Object:            "chat.completion",
		Created:           firstChunk.Created,
		Model:             firstChunk.Model,
		SystemFingerprint: firstChunk.SystemFingerprint,
		ServiceTier:       firstChunk.ServiceTier,
	}
	accs := make(map[int]*choiceAccumulator)

	for _, chunk := range a.chunks {
		if chunk == nil {
			continue
		}
		if chunk.ID != "" {
			result.ID = chunk.ID
		}
		if chunk.Model != "" {
			result.Model = chunk.Model
		}
		if chunk.Usage != nil {
			result.Usage = chunk.Usage
		}
		for _, choice := range chunk.Choices {
			acc := accs[choice.Index]
			if acc == nil {
				acc = &choiceAccumulator{choice: &Choice{Index: choice.Index, Message: &Message{}}}
				accs[choice.Index] = acc
			}
			mergeChoiceDelta(acc, choice)
		}
	}

	result.Choices = make([]Choice, 0, len(accs))
	indices := make([]int, 0, len(accs))
	for idx := range accs {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	for _, idx := range indices {
		acc := accs[idx]
		if acc.content.Len() > 0 {
			content := acc.content.String()
			acc.choice.Message.Content.Content = &content
		}
		if acc.reasoning.Len() > 0 {
			reasoning := acc.reasoning.String()
			acc.choice.Message.ReasoningContent = &reasoning
		}
		if acc.choice.Message.Audio != nil {
			acc.choice.Message.Audio.Data = acc.audioData.String()
			acc.choice.Message.Audio.Transcript = acc.audioTrans.String()
		}
		result.Choices = append(result.Choices, *acc.choice)
	}
	return result
}

func (a *StreamAggregator) BuildAndReset() *InternalLLMResponse {
	response := a.Response()
	a.Reset()
	return response
}

func mergeChoiceDelta(acc *choiceAccumulator, choice Choice) {
	existingChoice := acc.choice
	if choice.Delta != nil {
		delta := choice.Delta
		if delta.Role != "" {
			existingChoice.Message.Role = delta.Role
		}
		if delta.Content.Content != nil {
			acc.content.WriteString(*delta.Content.Content)
		}
		if len(delta.Content.MultipleContent) > 0 {
			existingChoice.Message.Content.MultipleContent = append(existingChoice.Message.Content.MultipleContent, delta.Content.MultipleContent...)
		}
		if len(delta.Images) > 0 {
			existingChoice.Message.Content.MultipleContent = append(existingChoice.Message.Content.MultipleContent, delta.Images...)
		}
		if delta.Audio != nil {
			if existingChoice.Message.Audio == nil {
				existingChoice.Message.Audio = &struct {
					Data       string `json:"data,omitempty"`
					ExpiresAt  int64  `json:"expires_at,omitempty"`
					ID         string `json:"id,omitempty"`
					Transcript string `json:"transcript,omitempty"`
				}{}
			}
			if delta.Audio.ID != "" {
				existingChoice.Message.Audio.ID = delta.Audio.ID
			}
			if delta.Audio.ExpiresAt > 0 {
				existingChoice.Message.Audio.ExpiresAt = delta.Audio.ExpiresAt
			}
			acc.audioData.WriteString(delta.Audio.Data)
			acc.audioTrans.WriteString(delta.Audio.Transcript)
		}
		if reasoning := delta.GetReasoningContent(); reasoning != "" {
			acc.reasoning.WriteString(reasoning)
		}
		for _, toolCall := range delta.ToolCalls {
			existingChoice.Message.ToolCalls = MergeToolCallDelta(existingChoice.Message.ToolCalls, toolCall)
		}
		if delta.Refusal != "" {
			existingChoice.Message.Refusal = delta.Refusal
		}
	}
	if choice.FinishReason != nil {
		existingChoice.FinishReason = choice.FinishReason
	}
	if choice.Logprobs != nil {
		if existingChoice.Logprobs == nil {
			existingChoice.Logprobs = &LogprobsContent{}
		}
		existingChoice.Logprobs.Content = append(existingChoice.Logprobs.Content, choice.Logprobs.Content...)
	}
}

func MergeToolCallDelta(toolCalls []ToolCall, delta ToolCall) []ToolCall {
	for i, tc := range toolCalls {
		if tc.Index != delta.Index {
			continue
		}
		// Index 不是唯一标识：Responses API 的 output_index 可能在
		// message/reasoning item 之后重新从 0 计数。两侧 ID 均已存在且不同时，
		// 不能把两个不同 tool call 的 name/arguments 拼接在一起；继续查找
		// 是否有 ID 匹配的条目，找不到则按新 tool call 追加。
		if delta.ID != "" && tc.ID != "" && delta.ID != tc.ID {
			continue
		}
		if delta.ID != "" {
			toolCalls[i].ID = delta.ID
		}
		if delta.Type != "" {
			toolCalls[i].Type = delta.Type
		}
		if delta.Function.Name != "" {
			if toolCalls[i].Function.Name == "" {
				toolCalls[i].Function.Name = delta.Function.Name
			} else if toolCalls[i].Function.Name != delta.Function.Name {
				toolCalls[i].Function.Name += delta.Function.Name
			}
		}
		if delta.Function.Arguments != "" {
			toolCalls[i].Function.Arguments += delta.Function.Arguments
		}
		return toolCalls
	}
	return append(toolCalls, delta)
}
