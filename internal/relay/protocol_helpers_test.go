package relay

import (
	"testing"

	"github.com/bestruirui/octopus/internal/relay/stream"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
)

// F18：协议定义了终态帧、流以 EOF 结束且终态未出现 = 截断流，必须记失败；
// 终态帧表为空的协议（embedding 等）EOF 即正常结束，不适用。
func TestTruncatedUpstreamStream(t *testing.T) {
	cases := []struct {
		name   string
		result stream.Result
		format transformerModel.APIFormat
		want   bool
	}{
		{
			name:   "eof without terminal on chat completions",
			result: stream.Result{Termination: stream.TerminationUpstreamEOF, PayloadWritten: true},
			format: transformerModel.APIFormatOpenAIChatCompletion,
			want:   true,
		},
		{
			name:   "eof without terminal on anthropic messages",
			result: stream.Result{Termination: stream.TerminationUpstreamEOF, PayloadWritten: true},
			format: transformerModel.APIFormatAnthropicMessage,
			want:   true,
		},
		{
			name:   "protocol terminal reached",
			result: stream.Result{Termination: stream.TerminationProtocolTerminal, PayloadWritten: true, TerminalEvent: "[DONE]"},
			format: transformerModel.APIFormatOpenAIChatCompletion,
			want:   false,
		},
		{
			name:   "eof without any payload",
			result: stream.Result{Termination: stream.TerminationUpstreamEOF},
			format: transformerModel.APIFormatOpenAIChatCompletion,
			want:   false,
		},
		{
			name:   "read error is not the truncated case",
			result: stream.Result{Termination: stream.TerminationReadError, PayloadWritten: true},
			format: transformerModel.APIFormatOpenAIChatCompletion,
			want:   false,
		},
		{
			name:   "format without terminal event table",
			result: stream.Result{Termination: stream.TerminationUpstreamEOF, PayloadWritten: true},
			format: transformerModel.APIFormatOpenAIEmbedding,
			want:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := truncatedUpstreamStream(tc.result, tc.format); got != tc.want {
				t.Fatalf("truncatedUpstreamStream(%+v, %s) = %v, want %v", tc.result, tc.format, got, tc.want)
			}
		})
	}
}
