package model

import "testing"

// 回归：StreamEventKindThinkingDelta 分支此前是直接赋值（ReasoningContent = 本片），
// 与同函数内的 TextDelta 累加语义不一致。多片 thinking 只剩最后一片。
func TestInternalResponseFromStreamEventsAccumulatesThinkingDeltas(t *testing.T) {
	events := []StreamEvent{
		{Kind: StreamEventKindMessageStart, ID: "resp_think", Model: "claude", Role: "assistant"},
		{Kind: StreamEventKindThinkingDelta, ID: "resp_think", Model: "claude", Delta: &StreamDelta{Thinking: "part-1|"}},
		{Kind: StreamEventKindThinkingDelta, ID: "resp_think", Model: "claude", Delta: &StreamDelta{Thinking: "part-2|"}},
		{Kind: StreamEventKindThinkingDelta, ID: "resp_think", Model: "claude", Delta: &StreamDelta{Thinking: "part-3"}},
		{Kind: StreamEventKindTextDelta, ID: "resp_think", Model: "claude", Delta: &StreamDelta{Text: "answer"}},
	}

	rebuilt := InternalResponseFromStreamEvents(events)
	if rebuilt == nil || len(rebuilt.Choices) == 0 || rebuilt.Choices[0].Delta == nil {
		t.Fatalf("unexpected rebuilt response: %#v", rebuilt)
	}
	got := rebuilt.Choices[0].Delta.ReasoningContent
	if got == nil || *got != "part-1|part-2|part-3" {
		t.Fatalf("thinking deltas were not accumulated: %#v", got)
	}
	if text := rebuilt.Choices[0].Delta.Content.Content; text == nil || *text != "answer" {
		t.Fatalf("text accumulation regressed: %#v", text)
	}
}

// 只带 signature 的 thinking 分片不得把已累积的 thinking 清空。
func TestInternalResponseFromStreamEventsKeepsThinkingWhenOnlySignatureArrives(t *testing.T) {
	events := []StreamEvent{
		{Kind: StreamEventKindMessageStart, ID: "resp_sig", Model: "claude", Role: "assistant"},
		{Kind: StreamEventKindThinkingDelta, ID: "resp_sig", Model: "claude", Delta: &StreamDelta{Thinking: "kept"}},
		{Kind: StreamEventKindThinkingDelta, ID: "resp_sig", Model: "claude", Delta: &StreamDelta{Signature: "sig-only"}},
	}

	rebuilt := InternalResponseFromStreamEvents(events)
	if rebuilt == nil || len(rebuilt.Choices) == 0 || rebuilt.Choices[0].Delta == nil {
		t.Fatalf("unexpected rebuilt response: %#v", rebuilt)
	}
	got := rebuilt.Choices[0].Delta.ReasoningContent
	if got == nil || *got != "kept" {
		t.Fatalf("thinking was dropped by a signature-only delta: %#v", got)
	}
}
