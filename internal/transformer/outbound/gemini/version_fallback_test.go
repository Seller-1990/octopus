package gemini

import "testing"

// G-H5 回退规则的导出形态（供 helper.FetchModels 使用）：裸主机补 /v1beta，
// 已带版本段的路径原样返回，非版本 v 前缀（/viewer）不得误判。
func TestAppendVersionFallback(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// 函数契约：作用于渠道 base（调用方随后再拼 /models 等资源路径）
		{"bare host", "https://generativelanguage.googleapis.com", "https://generativelanguage.googleapis.com/v1beta"},
		{"explicit v1beta", "https://example.com/v1beta", "https://example.com/v1beta"},
		{"explicit v1", "https://example.com/v1", "https://example.com/v1"},
		// 与 chat 适配器 G-H5 同口径：规则只检查路径首段，代理路径带版本段时
		// 两侧行为一致地追加（chat 存量语义如此，fetch 不擅自更聪明）
		{"proxy path then version (parity with chat)", "https://proxy.example.com/gemini/v1beta", "https://proxy.example.com/gemini/v1beta/v1beta"},
		{"non-version v segment", "https://example.com/viewer", "https://example.com/viewer/v1beta"},
		{"trailing slash", "https://example.com/", "https://example.com/v1beta"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := AppendVersionFallback(tc.in)
			if err != nil {
				t.Fatalf("AppendVersionFallback(%q) error = %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("AppendVersionFallback(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
