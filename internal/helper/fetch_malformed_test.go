package helper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func fetchModelsFrom(t *testing.T, body string, contentType string) ([]string, error) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	return FetchModels(context.Background(), model.Channel{
		Type:     outbound.OutboundTypeOpenAIChat,
		BaseUrls: []model.BaseUrl{{URL: server.URL, Delay: 0}},
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "managed-key"}},
	})
}

// 复现 F09:200 成功状态下的畸形响应(空 body/空对象/null/错误对象)必须报错,
// 而不是静默解码成空模型列表,让同步任务撤掉渠道全部模型与路由。
func TestFetchModelsRejectsMalformedSuccessResponses(t *testing.T) {
	cases := []struct{ name, body string }{
		{"empty body", ""},
		{"empty object", "{}"},
		{"json null", "null"},
		{"error object", `{"error": {"message": "upstream exploded"}}`},
		{"json array", `[{"id": "m1"}]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			models, err := fetchModelsFrom(t, tc.body, "application/json")
			if err == nil {
				t.Fatalf("malformed %s response must fail, got models %v", tc.name, models)
			}
		})
	}
}

// 明确的空数组是合法响应:解码成功并返回空列表,是否撤模由调用方策略决定。
func TestFetchModelsAcceptsExplicitEmptyList(t *testing.T) {
	models, err := fetchModelsFrom(t, `{"data": []}`, "application/json")
	if err != nil {
		t.Fatalf("explicit empty list must decode: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("expected empty model list, got %v", models)
	}
}

// 有效列表正常解析。
func TestFetchModelsParsesValidList(t *testing.T) {
	models, err := fetchModelsFrom(t, `{"data": [{"id": "m1"}, {"id": "m2"}]}`, "application/json")
	if err != nil {
		t.Fatalf("valid list must decode: %v", err)
	}
	if len(models) != 2 || models[0] != "m1" || models[1] != "m2" {
		t.Fatalf("unexpected models: %v", models)
	}
}
