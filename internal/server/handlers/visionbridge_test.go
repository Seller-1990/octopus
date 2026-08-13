package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func serveVisionBridgeTest(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/vision-bridge/test", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	testVisionBridge(c)
	return recorder
}

func TestVisionBridgeTestValidation(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"missing model", `{"base_url":"http://127.0.0.1:9"}`, http.StatusBadRequest},
		{"missing base_url", `{"model":"glm-4v"}`, http.StatusBadRequest},
		{"bad scheme", `{"model":"glm-4v","base_url":"ftp://example.com/v1"}`, http.StatusBadRequest},
		{"no host", `{"model":"glm-4v","base_url":"http://"}`, http.StatusBadRequest},
		{
			"too many fallbacks",
			`{"model":"glm-4v","base_url":"http://127.0.0.1:9/v1","fallback_models":"a,b,c,d,e,f,g,h,i"}`,
			http.StatusBadRequest,
		},
		{"malformed json", `{not json`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := serveVisionBridgeTest(t, tc.body); got.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", got.Code, tc.want, got.Body.String())
			}
		})
	}
}

// 探测真发请求：body 内 api_key 生效为 Authorization 头；上游错误体被截断，不整段回显。
func TestVisionBridgeTestProbeAuthAndErrorTruncation(t *testing.T) {
	var gotAuth string
	hugeError := strings.Repeat("E", 8192)
	vlm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		http.Error(w, hugeError, http.StatusBadGateway)
	}))
	defer vlm.Close()

	recorder := serveVisionBridgeTest(t, fmt.Sprintf(
		`{"model":"glm-4v","base_url":%q,"api_key":"probe-secret"}`, vlm.URL,
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("probe endpoint should return 200 with per-model results, got %d body %s", recorder.Code, recorder.Body.String())
	}
	if gotAuth != "Bearer probe-secret" {
		t.Fatalf("body api_key must be used for probe auth, got %q", gotAuth)
	}

	var parsed struct {
		Data []struct {
			Model string `json:"model"`
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if len(parsed.Data) != 1 || parsed.Data[0].OK {
		t.Fatalf("expected single failed result, got %+v", parsed.Data)
	}
	if len(parsed.Data[0].Error) == 0 || len(parsed.Data[0].Error) > 600 {
		t.Fatalf("probe error must be truncated (~500 runes), got %d chars", len(parsed.Data[0].Error))
	}
}
