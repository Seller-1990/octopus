package visionbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/client"
	"github.com/bestruirui/octopus/internal/utils/log"
)

const vlmErrorBodyLimit = 8 * 1024

// analyzeChain 依序尝试主模型与 fallback 模型，返回首个可用描述。
// 总超时预算在模型链上公平切分（plan：超时预算公平切分）。
func analyzeChain(ctx context.Context, cfg Config, refs []ImageRef, prompt string) (string, error) {
	models := modelChain(cfg)
	if len(models) == 0 {
		return "", errors.New("vision_model not configured")
	}
	perModel := cfg.RequestTimeout / time.Duration(len(models))
	var errs []string
	for _, modelName := range models {
		attemptCtx, cancel := context.WithTimeout(ctx, perModel)
		text, err := analyzeWithModel(attemptCtx, cfg, modelName, refs, prompt)
		cancel()
		if err == nil {
			return text, nil
		}
		log.Warnf("vision bridge: vlm %s failed: %v", modelName, err)
		errs = append(errs, fmt.Sprintf("%s: %v", modelName, err))
		if ctx.Err() != nil {
			break
		}
	}
	return "", fmt.Errorf("all vlm models failed: %s", strings.Join(errs, "; "))
}

// modelChain 返回主模型 + 备选模型的去重有序列表（主模型被重复写进备选是常见配置，
// 去重避免同一模型白跑一次真实调用，也避免探测结果按模型名渲染时撞 key）。
func modelChain(cfg Config) []string {
	var models []string
	seen := make(map[string]bool)
	appendModel := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		models = append(models, name)
	}
	appendModel(cfg.VisionModel)
	for _, m := range cfg.VisionFallbackModels {
		appendModel(m)
	}
	return models
}

func analyzeWithModel(ctx context.Context, cfg Config, modelName string, refs []ImageRef, prompt string) (string, error) {
	payload, err := json.Marshal(buildVLMPayload(modelName, refs, prompt))
	if err != nil {
		return "", fmt.Errorf("marshal vlm request: %w", err)
	}
	endpoint := strings.TrimSuffix(cfg.VisionBaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("build vlm request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.VisionAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.VisionAPIKey)
	}

	httpClient, err := vlmHTTPClient(cfg.VisionBaseURL)
	if err != nil {
		return "", fmt.Errorf("build vlm http client: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("vlm request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, vlmErrorBodyLimit))
		return "", fmt.Errorf("vlm upstream error: %d: %s", resp.StatusCode, string(body))
	}
	return extractText(resp.Body, cfg)
}

// vlmHTTPClient 沿用全局代理策略（price.go P0-1：有代理走代理、无代理直连）；
// 本地/内网 VLM（如局域网 Ollama）强制直连，避免 loopback 流量被推给代理。
func vlmHTTPClient(baseURL string) (*http.Client, error) {
	useProxy := client.ResolveSystemProxyURL() != ""
	if useProxy {
		if u, err := url.Parse(baseURL); err == nil && isForbiddenHost(strings.ToLower(u.Hostname())) {
			useProxy = false
		}
	}
	return client.GetHTTPClientSystemProxy(useProxy)
}

func buildVLMPayload(modelName string, refs []ImageRef, prompt string) map[string]any {
	parts := make([]map[string]any, 0, len(refs)+1)
	parts = append(parts, map[string]any{"type": "text", "text": prompt})
	for _, ref := range refs {
		parts = append(parts, map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": ref.URL},
		})
	}
	return map[string]any{
		"model":    modelName,
		"stream":   false,
		"messages": []map[string]any{{"role": "user", "content": parts}},
	}
}

// extractText 解析响应并做输出校验。Step 0a 实测两类必须拦截的退化响应：
// ① HTTP 200 + choices 为空；② 描述为空/超短（下游会基于空描述自信幻觉）。
func extractText(body io.Reader, cfg Config) (string, error) {
	var parsed struct {
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("decode vlm response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("vlm returned no choices (silent-degradation response)")
	}
	text := strings.TrimSpace(decodeVLMContent(parsed.Choices[0].Message.Content))
	if runeCount := len([]rune(text)); runeCount < cfg.MinResultChars {
		return "", fmt.Errorf("vlm description too short (%d < %d chars)", runeCount, cfg.MinResultChars)
	}
	return truncateRunes(text, cfg.MaxResultChars), nil
}

// decodeVLMContent 兼容两类 OpenAI 兼容实现：content 为 string，
// 或为 [{type:"text",text:...}] 内容块数组（部分 vLLM/网关）。
func decodeVLMContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}
	var texts []string
	for _, p := range parts {
		if p.Text != "" {
			texts = append(texts, p.Text)
		}
	}
	return strings.Join(texts, "\n")
}
