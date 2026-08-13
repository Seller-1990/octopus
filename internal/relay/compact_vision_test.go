package relay

import (
	"encoding/json"
	"testing"
)

// compact 是 raw 直通入口，responsesInputHasImages 是「原图绝不透传给纯文本模型」
// 不变量在该入口的唯一探测手段：input_image 必须被识别，识别不出的形态按无图放行。
func TestResponsesInputHasImages(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"empty", ``, false},
		{"bare string input", `"hello"`, false},
		{"text only", `[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]`, false},
		{"input image", `[{"type":"message","role":"user","content":[{"type":"input_text","text":"看图"},{"type":"input_image","image_url":"data:image/png;base64,AAAA"}]}]`, true},
		{"image in later item", `[{"type":"function_call","call_id":"c1","arguments":"{}"},{"type":"message","role":"user","content":[{"type":"input_image","image_url":"https://example.com/a.png"}]}]`, true},
		{"string content message", `[{"role":"user","content":"纯文本"},{"type":"message","role":"user","content":[{"type":"input_image","image_url":"x"}]}]`, true},
		{"malformed json", `{not json`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := responsesInputHasImages(json.RawMessage(tc.input)); got != tc.want {
				t.Fatalf("responsesInputHasImages(%s) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}
