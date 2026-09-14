package openai

import (
	"bytes"
	"context"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func countEvents(events []ResponsesStreamEvent, name string) int {
	n := 0
	for _, e := range events {
		if e.Type == name {
			n++
		}
	}
	return n
}

// 回归：终结事件此前只在 UsageDelta 分支发出。上游整条流不带 usage 时
// （部分 Anthropic/Gemini 上游如此），Responses 客户端收不到
// response.completed，只能等超时。DONE 是流的确定终点，必须兜底补发。
func TestTransformStreamEventsEmitsTerminalWithoutUsage(t *testing.T) {
	i := &ResponseInbound{}
	out, err := i.TransformStreamEvents(context.Background(), []model.StreamEvent{
		{Kind: model.StreamEventKindMessageStart, ID: "resp_no_usage", Model: "claude", Role: "assistant"},
		{Kind: model.StreamEventKindTextDelta, ID: "resp_no_usage", Model: "claude", Delta: &model.StreamDelta{Text: "hi"}},
		{Kind: model.StreamEventKindMessageStop, ID: "resp_no_usage", Model: "claude", StopReason: model.FinishReasonStop},
		{Kind: model.StreamEventKindDone},
	})
	if err != nil {
		t.Fatalf("TransformStreamEvents failed: %v", err)
	}

	events := parseSSEEvents(t, out)
	if n := countEvents(events, "response.completed"); n != 1 {
		t.Fatalf("expected exactly one response.completed, got %d: %v", n, eventTypes(events))
	}
	if !bytes.HasSuffix(bytes.TrimSpace(out), []byte("data: [DONE]")) {
		t.Fatalf("terminal event must precede [DONE], got %q", string(out))
	}
}

// usage 事件已经发过终结事件时，DONE 兜底不得重复补发。
func TestTransformStreamEventsDoesNotDuplicateTerminalWithUsage(t *testing.T) {
	i := &ResponseInbound{}
	out, err := i.TransformStreamEvents(context.Background(), []model.StreamEvent{
		{Kind: model.StreamEventKindMessageStart, ID: "resp_with_usage", Model: "claude", Role: "assistant"},
		{Kind: model.StreamEventKindTextDelta, ID: "resp_with_usage", Model: "claude", Delta: &model.StreamDelta{Text: "hi"}},
		{Kind: model.StreamEventKindMessageStop, ID: "resp_with_usage", Model: "claude", StopReason: model.FinishReasonStop},
		{Kind: model.StreamEventKindUsageDelta, ID: "resp_with_usage", Model: "claude", Usage: &model.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2}},
		{Kind: model.StreamEventKindDone},
	})
	if err != nil {
		t.Fatalf("TransformStreamEvents failed: %v", err)
	}

	events := parseSSEEvents(t, out)
	if n := countEvents(events, "response.completed"); n != 1 {
		t.Fatalf("expected exactly one response.completed, got %d: %v", n, eventTypes(events))
	}
}

// 上游未收到 MessageStop（异常截断）时不得凭空制造成功终结事件。
func TestTransformStreamEventsWithoutMessageStopEmitsNoTerminal(t *testing.T) {
	i := &ResponseInbound{}
	out, err := i.TransformStreamEvents(context.Background(), []model.StreamEvent{
		{Kind: model.StreamEventKindMessageStart, ID: "resp_truncated", Model: "claude", Role: "assistant"},
		{Kind: model.StreamEventKindTextDelta, ID: "resp_truncated", Model: "claude", Delta: &model.StreamDelta{Text: "partial"}},
		{Kind: model.StreamEventKindDone},
	})
	if err != nil {
		t.Fatalf("TransformStreamEvents failed: %v", err)
	}

	events := parseSSEEvents(t, out)
	if n := countEvents(events, "response.completed"); n != 0 {
		t.Fatalf("no terminal event expected without MessageStop, got %d: %v", n, eventTypes(events))
	}
}
