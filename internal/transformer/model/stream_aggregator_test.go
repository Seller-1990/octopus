package model

import "testing"

func TestMergeToolCallDeltaDoesNotDuplicateFunctionName(t *testing.T) {
	toolCalls := []ToolCall{{
		Index: 0,
		ID:    "call_1",
		Type:  "function",
		Function: FunctionCall{
			Name: "Write",
		},
	}}

	toolCalls = MergeToolCallDelta(toolCalls, ToolCall{
		Index: 0,
		Function: FunctionCall{
			Name:      "Write",
			Arguments: `{"file_path":`,
		},
	})
	toolCalls = MergeToolCallDelta(toolCalls, ToolCall{
		Index: 0,
		Function: FunctionCall{
			Name:      "Write",
			Arguments: `"a.txt"}`,
		},
	})

	if len(toolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].Function.Name != "Write" {
		t.Fatalf("function name duplicated: %q", toolCalls[0].Function.Name)
	}
	if toolCalls[0].Function.Arguments != `{"file_path":"a.txt"}` {
		t.Fatalf("arguments not merged: %q", toolCalls[0].Function.Arguments)
	}
}

func TestMergeToolCallDeltaSetsFunctionNameWhenMissing(t *testing.T) {
	toolCalls := []ToolCall{{Index: 0, Type: "function"}}

	toolCalls = MergeToolCallDelta(toolCalls, ToolCall{
		Index: 0,
		Function: FunctionCall{
			Name: "Search",
		},
	})

	if len(toolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].Function.Name != "Search" {
		t.Fatalf("function name not set: %q", toolCalls[0].Function.Name)
	}
}

func TestMergeToolCallDeltaTreatsIndexReuseWithDifferentIDAsNewCall(t *testing.T) {
	toolCalls := []ToolCall{{
		Index: 0,
		ID:    "call_1",
		Type:  "function",
		Function: FunctionCall{
			Name:      "Write",
			Arguments: `{"file":1}`,
		},
	}}

	// Responses API 的 output_index 可能复用；ID 不同时必须作为新的 tool call，
	// 不能把两个不同调用的 arguments 拼接在一起。
	toolCalls = MergeToolCallDelta(toolCalls, ToolCall{
		Index: 0,
		ID:    "call_2",
		Type:  "function",
		Function: FunctionCall{
			Name:      "Read",
			Arguments: `{"file":2}`,
		},
	})

	if len(toolCalls) != 2 {
		t.Fatalf("expected two tool calls, got %d", len(toolCalls))
	}
	if toolCalls[0].Function.Arguments != `{"file":1}` || toolCalls[1].Function.Arguments != `{"file":2}` {
		t.Fatalf("tool calls corrupted: %#v", toolCalls)
	}
}
