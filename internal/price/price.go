package price

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/client"
	"github.com/bestruirui/octopus/internal/globalprice"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/modelvendor"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
)

const llmPriceUrl = "https://models.dev/api.json"

// registryModel 是 models.dev 中单个模型条目，只取本项目用得到的字段。
type registryModel struct {
	ID         string          `json:"id"`
	Cost       model.LLMPrice  `json:"cost"`
	Modalities json.RawMessage `json:"modalities"`
}

// registryHasVision 判定 models.dev 条目的视觉输入能力：modalities.input 含 image 或 video。
// 宽松容错：modalities 缺失/结构异常 → 未知（false, 不写列）。
// P1 修复：不直接依赖顶层 Unmarshal（单条 modalities 结构异常会炸掉整个价格更新）——
// 这里用 RawMessage 单独防御解析，异常降级为 false 而非整包失败。
func registryHasVision(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var m struct {
		Input []string `json:"input"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return false // 结构异常 → 未知，不判定
	}
	for _, in := range m.Input {
		switch strings.ToLower(strings.TrimSpace(in)) {
		case "image", "video":
			return true
		}
	}
	return false
}

type registryProvider struct {
	Models map[string]registryModel `json:"models"`
}

var Provider = []string{
	"openai",     // GPT 系列
	"anthropic",  // Claude 系列
	"google",     // Gemini 系列
	"deepseek",   // DeepSeek 系列
	"xai",        // Grok 系列
	"alibaba",    // Qwen 系列
	"zhipuai",    // GLM 系列
	"minimax",    // MiniMax 系列
	"moonshotai", // Kimi/Moonshot
	"v0",         // v0 系列
}

var lastUpdateTime time.Time

func UpdateLLMPrice(ctx context.Context) error {
	log.Debugf("update LLM price task started")
	startTime := time.Now()
	defer func() {
		log.Debugf("update LLM price task finished, update time: %s", time.Since(startTime))
	}()
	// 方案 C（稳定性自查）：ctx 无 deadline 时（任务 goroutine 用 context.Background 调用）
	// HTTP 请求无超时——models.dev 挂起会让任务永不返回，task 的 running 标志永久阻塞后续所有价格更新。
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	client, err := client.GetHTTPClientSystemProxy(false)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, llmPriceUrl, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch LLM info: %s", resp.Status)
	}
	var rawPrice map[string]registryProvider
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	if err := json.Unmarshal(body, &rawPrice); err != nil {
		return fmt.Errorf("failed to parse LLM info: %w", err)
	}
	prices := make(map[string]model.LLMPrice)
	for _, provider := range Provider {
		for _, model := range rawPrice[provider].Models {
			model.ID = strings.ToLower(model.ID)
			prices[model.ID] = model.Cost
		}
	}
	globalprice.Replace(prices)
	modelvendor.ReplaceIndex(vendorIndex(rawPrice))
	modelvendor.ReplaceVisionIndex(visionIndex(rawPrice))
	lastUpdateTime = time.Now()
	return nil
}

// visionIndex 把 models.dev 的 provider→models 结构翻成「模型名 → 视觉能力」索引。
// 与 vendorIndex 同源：遍历**全部** provider（不套价格白名单——P1 修复：
// 覆盖 mistral/meta/cohere 等非价格厂商的视觉模型，如 pixtral 旗舰），
// 托管方（openrouter/groq）由 ReplaceVisionIndex 的 prefixAliases 过滤裁掉。
// key 与 item.ID 双写（对齐 vendorIndex 口径，P1 修复 vendor/ 前缀模型名可查）。
func visionIndex(raw map[string]registryProvider) map[string]modelvendor.VisionEntry {
	index := make(map[string]modelvendor.VisionEntry)
	for provider, entry := range raw {
		for key, item := range entry.Models {
			vision := registryHasVision(item.Modalities)
			for _, name := range []string{key, item.ID} {
				name = strings.ToLower(strings.TrimSpace(name))
				if name == "" {
					continue
				}
				index[name] = modelvendor.VisionEntry{Provider: provider, Vision: vision}
			}
		}
	}
	return index
}

// vendorIndex 把 models.dev 的 provider→models 结构翻成「模型名 → provider」索引。
// 这里刻意不套用 Provider 价格白名单：厂商识别需要更宽的覆盖面，
// 由 modelvendor.ReplaceIndex 负责裁掉 openrouter/groq 这类托管方。
func vendorIndex(raw map[string]registryProvider) map[string]string {
	index := make(map[string]string)
	for provider, entry := range raw {
		for key, item := range entry.Models {
			for _, name := range []string{key, item.ID} {
				name = strings.ToLower(strings.TrimSpace(name))
				if name == "" {
					continue
				}
				index[name] = provider
			}
		}
	}
	return index
}

func GetLastUpdateTime() time.Time {
	return lastUpdateTime
}

func GetLLMPrice(modelName string) *model.LLMPrice {
	modelName = strings.ToLower(modelName)
	price, err := op.LLMGet(modelName)
	if err == nil {
		return &price
	}
	price, ok := globalprice.Get(modelName)
	if !ok {
		return nil
	}
	return &price
}
