package visionbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/utils/log"
)

const vlmErrorBodyLimit = 8 * 1024

// vlmClient 面向 OpenAI 兼容端点的最小聊天客户端（非流式）。
type vlmClient struct {
	httpClient *http.Client
	cfg        Config
}

func newVLMClient(cfg Config) *vlmClient {
	return &vlmClient{
		// 单请求超时由调用方 ctx 控制（预算公平切分）；Client 不设全局 Timeout。
		httpClient: &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}},
		cfg:        cfg,
	}
}

// Analyze 依序尝试主模型与 fallback 模型，返回首个可用描述。
// 总超时预算在模型链上公平切分（plan：超时预算公平切分）。
func (c *vlmClient) Analyze(ctx context.Context, refs []ImageRef, prompt string) (string, error) {
	models := c.modelChain()
	if len(models) == 0 {
		return "", errors.New("vision_model not configured")
	}
	perModel := c.cfg.RequestTimeout / time.Duration(len(models))
	var errs []string
	for _, modelName := range models {
		attemptCtx, cancel := context.WithTimeout(ctx, perModel)
		text, err := c.analyzeWithModel(attemptCtx, modelName, refs, prompt)
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

func (c *vlmClient) modelChain() []string {
	var models []string
	if m := strings.TrimSpace(c.cfg.VisionModel); m != "" {
		models = append(models, m)
	}
	for _, m := range c.cfg.VisionFallbackModels {
		if m = strings.TrimSpace(m); m != "" {
			models = append(models, m)
		}
	}
	return models
}

func (c *vlmClient) analyzeWithModel(ctx context.Context, modelName string, refs []ImageRef, prompt string) (string, error) {
	payload, err := json.Marshal(buildVLMPayload(modelName, refs, prompt))
	if err != nil {
		return "", fmt.Errorf("marshal vlm request: %w", err)
	}
	endpoint := strings.TrimSuffix(c.cfg.VisionBaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("build vlm request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if key := os.Getenv(c.cfg.VisionAPIKeyEnv); c.cfg.VisionAPIKeyEnv != "" && key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("vlm request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, vlmErrorBodyLimit))
		return "", fmt.Errorf("vlm upstream error: %d: %s", resp.StatusCode, string(body))
	}
	return c.extractText(resp.Body)
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
func (c *vlmClient) extractText(body io.Reader) (string, error) {
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("decode vlm response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("vlm returned no choices (silent-degradation response)")
	}
	text := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if runeCount := len([]rune(text)); runeCount < c.cfg.MinResultChars {
		return "", fmt.Errorf("vlm description too short (%d < %d chars)", runeCount, c.cfg.MinResultChars)
	}
	return truncateRunes(text, c.cfg.MaxResultChars), nil
}
