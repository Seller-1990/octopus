package relay

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
)

// usage 完全缺失时，应使用 TransportInputTokens 兜底填充 input，output 保持 0。
func TestSetInternalResponseFallbackWhenUsageMissing(t *testing.T) {
	m := &RelayMetrics{TransportInputTokens: intPtr(123)}
	m.SetInternalResponse(&transformerModel.InternalLLMResponse{}, "test-model")

	if m.Stats.InputToken != 123 {
		t.Fatalf("input token: got %d want 123 (fallback)", m.Stats.InputToken)
	}
	if m.BillInputTokens == nil || *m.BillInputTokens != 123 {
		t.Fatalf("bill input tokens: got %v want 123", m.BillInputTokens)
	}
	if m.Stats.OutputToken != 0 {
		t.Fatalf("output token: got %d want 0", m.Stats.OutputToken)
	}
}

// usage 存在但输入侧全为 0（仅上报 output）时，input 兜底、output 保留。
func TestSetInternalResponseFallbackWhenInputZero(t *testing.T) {
	m := &RelayMetrics{TransportInputTokens: intPtr(50)}
	m.SetInternalResponse(&transformerModel.InternalLLMResponse{
		Usage: &transformerModel.Usage{PromptTokens: 0, CompletionTokens: 30},
	}, "test-model")

	if m.Stats.InputToken != 50 {
		t.Fatalf("input token: got %d want 50 (fallback)", m.Stats.InputToken)
	}
	if m.Stats.OutputToken != 30 {
		t.Fatalf("output token: got %d want 30 (preserved)", m.Stats.OutputToken)
	}
}

// 上游正常上报 input 时不触发兜底（保留真实值，而非估算值）。
func TestSetInternalResponseNoFallbackWhenInputReported(t *testing.T) {
	m := &RelayMetrics{TransportInputTokens: intPtr(999)}
	m.SetInternalResponse(&transformerModel.InternalLLMResponse{
		Usage: &transformerModel.Usage{PromptTokens: 12, CompletionTokens: 7},
	}, "test-model")

	if m.Stats.InputToken != 12 {
		t.Fatalf("input token: got %d want 12 (reported, not fallback)", m.Stats.InputToken)
	}
	if m.Stats.OutputToken != 7 {
		t.Fatalf("output token: got %d want 7", m.Stats.OutputToken)
	}
}

// 仅缓存命中（input_tokens=0 但 cache_read>0）属于已上报输入，不应被估算覆盖。
func TestSetInternalResponseNoFallbackWhenCacheOnly(t *testing.T) {
	m := &RelayMetrics{TransportInputTokens: intPtr(999)}
	m.SetInternalResponse(&transformerModel.InternalLLMResponse{
		Usage: &transformerModel.Usage{PromptTokens: 0, CacheReadInputTokens: 40, CompletionTokens: 5},
	}, "test-model")

	if m.Stats.InputToken != 0 {
		t.Fatalf("input token: got %d want 0 (cache-only is reported input)", m.Stats.InputToken)
	}
}

func TestRelayMetricsKeepsUnconvertibleSiteCreditOutOfUSDCost(t *testing.T) {
	m := &RelayMetrics{}
	m.SetEffectivePrice(model.EffectivePrice{
		QuoteID:           12,
		Source:            model.PriceQuoteSourceSiteExact,
		Unit:              model.PriceUnitSiteCredit,
		Currency:          "SITE_CREDIT",
		Input:             4,
		Output:            8,
		GroupMultiplier:   2,
		Convertible:       false,
		ExchangeRateToUSD: 0,
	})
	m.SetInternalResponse(&transformerModel.InternalLLMResponse{
		Usage: &transformerModel.Usage{
			PromptTokens:     1_000_000,
			CompletionTokens: 500_000,
		},
	}, "credit-model")

	if math.Abs(m.PriceOriginalCost-8) > 1e-9 {
		t.Fatalf("original site-credit cost = %f, want 8", m.PriceOriginalCost)
	}
	if m.Stats.InputCost != 0 || m.Stats.OutputCost != 0 {
		t.Fatalf("unconvertible credits leaked into USD cost: %+v", m.Stats)
	}
}

func TestRelayMetricsCalculatesPerRequestPriceOnce(t *testing.T) {
	m := &RelayMetrics{}
	m.SetEffectivePrice(model.EffectivePrice{
		QuoteID:           13,
		Source:            model.PriceQuoteSourceSiteExact,
		Unit:              model.PriceUnitPerRequest,
		Currency:          "CNY",
		PerRequest:        5,
		GroupMultiplier:   1,
		Convertible:       true,
		ExchangeRateToUSD: 0.2,
	})
	m.SetInternalResponse(&transformerModel.InternalLLMResponse{
		Usage: &transformerModel.Usage{
			PromptTokens:     2_000_000,
			CompletionTokens: 3_000_000,
		},
	}, "request-model")

	if m.PriceOriginalCost != 5 {
		t.Fatalf("per-request original cost = %f, want 5", m.PriceOriginalCost)
	}
	if math.Abs(m.Stats.InputCost-1) > 1e-9 || m.Stats.OutputCost != 0 {
		t.Fatalf("per-request converted cost mismatch: %+v", m.Stats)
	}
}

func TestFinalChannelRetainsClientCanceledAttempt(t *testing.T) {
	channelID, channelName := finalChannel([]model.ChannelAttempt{
		{
			ChannelID:   17,
			ChannelName: "images-channel",
			Status:      model.AttemptCanceled,
		},
	})
	if channelID != 17 || channelName != "images-channel" {
		t.Fatalf("finalChannel canceled attempt = (%d, %q)", channelID, channelName)
	}
}

func TestRedactBase64PayloadsForLog(t *testing.T) {
	longPayload := strings.Repeat("A", 4096)
	shortPayload := strings.Repeat("B", 128)
	content := `{"messages":[{"role":"user","content":[` +
		`{"type":"text","text":"看图 data:image/png;base64 提到但没有逗号"},` +
		`{"type":"image_url","image_url":{"url":"data:image/png;base64,` + longPayload + `"}},` +
		`{"type":"image_url","image_url":{"url":"data:image/jpeg;base64,` + shortPayload + `"}}]}]}`

	got := redactBase64PayloadsForLog(content)
	if strings.Contains(got, longPayload) {
		t.Fatal("long base64 payload must be redacted from log content")
	}
	if !strings.Contains(got, `data:image/png;base64,[4096 chars omitted]`) {
		t.Fatalf("redaction placeholder missing or wrong: %s", got)
	}
	// 短 payload（≤阈值）保留，正常小内容不受影响
	if !strings.Contains(got, shortPayload) {
		t.Fatal("short base64 payload should be kept")
	}
	// 占位符不得破坏 JSON 结构（日志查看器要能解析）
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("redacted content is no longer valid JSON: %v", err)
	}

	// 无 base64 的内容原样返回（零成本快路径）
	plain := `{"messages":[{"role":"user","content":"hello"}]}`
	if redactBase64PayloadsForLog(plain) != plain {
		t.Fatal("plain content must pass through unchanged")
	}
}

// Anthropic Messages 的图片没有 data URI 前缀（source.data 纯 base64 串）——
// 这是 Claude Code / Anthropic SDK 的主力形态，无锚点模式必须命中。
func TestRedactBase64PayloadsForLogAnthropicSource(t *testing.T) {
	payload := strings.Repeat("i", 4096)
	content := `{"model":"claude","messages":[{"role":"user","content":[` +
		`{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + payload + `"}},` +
		`{"type":"text","text":"看图"}]}]}`

	got := redactBase64PayloadsForLog(content)
	if strings.Contains(got, payload) {
		t.Fatal("anthropic source.data payload must be redacted")
	}
	if !strings.Contains(got, `"data":"[4096 chars omitted]"`) {
		t.Fatalf("placeholder must sit inside the JSON string: %s", got)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("redacted content is no longer valid JSON: %v", err)
	}
}

// 模式覆盖不了的形态（PHP \/ 转义把 base64 切碎、RFC 2045 折行）由硬上限兜底：
// 无论如何 15MB 级内容绝不原样入库。
func TestRedactBase64PayloadsForLogHardCapBackstop(t *testing.T) {
	// PHP json_encode 形态：所有 / 转义为 \/，base64 连续串被切碎到阈值以下
	seg := strings.Repeat("a", 60) + `\/`
	phpBody := `{"url":"data:image\/png;base64,` + strings.Repeat(seg, 3*logRequestContentMaxBytes/len(seg)) + `"}`
	got := redactBase64PayloadsForLog(phpBody)
	// truncateString 截断后追加 "...(truncated)" 后缀，允许该固定余量
	if len(got) > logRequestContentMaxBytes+len("...(truncated)") {
		t.Fatalf("escaped-slash payload escaped the hard cap: %d bytes", len(got))
	}
	if !strings.HasSuffix(got, "...(truncated)") {
		t.Fatal("hard cap must mark truncation explicitly")
	}

	// 正常长 prompt（无超长 base64 串、总量低于上限）不受截断影响
	longText := `{"messages":[{"role":"user","content":"` + strings.Repeat("正常文本 ", 4000) + `"}]}`
	if redactBase64PayloadsForLog(longText) != longText {
		t.Fatal("long plain prompt below cap must pass through unchanged")
	}
}
