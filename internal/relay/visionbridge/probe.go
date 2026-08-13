package visionbridge

import (
	"context"
	"time"
)

// probeImage 内置测试图（96×96 PNG，450B）：白底 + 红色圆 + 蓝色三角。
// 内容可辨识——正常 VLM 的描述必然提到颜色/形状，可复用 extractText 的最短长度校验。
const probeImage = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAGAAAABgCAIAAABt+uBvAAABiUlEQVR42u3aQZKCMBBGYenyJq44jqdnxVlwa5UCSbpJfpKX7Ywp8/GcwqGnbdserP1lEAAEEEAAAQQQQACx/q1n7HbrPO/96LUsdwSaQr5qHLjcXcoLlEVzR6ZyoGKaezFZQ53AfbSAYk8lbmQK51E2MpGTyBqZzhk0jUzq3Qsa8VUjAqjmhVWLiILcQPUvqVREFATQpUCtatf5lFEQQAABBBBAAAEE0EPpqYPO0w4KAuhqoPq1Sz1NpKAIoJqXVO1hNAUFAdW5sIKzDKbz7jUnPUzkDLJzMKZwEuUpIWt+HvEZKmt7Kv0JM2YUmXJVAGJOmjvpTtf8XgGiIF8+zogoaFSg73A8EVHQkEC/yRRHREHjAe3FUhYRBQ0GdJxJQUQUNBJQSiC5EVHQMEDpaWRFREFjAOX+ZUn/fQoaAKjsFjnxVRTUO5Dnfz0pr6WgroH8Dy1Od6Cgfgvy55OyDwV1WlBUPqe7UVCPBcXmc7wn0x3cKAIEEEAAAQQQQCyAAAIIIIAA6mx9AEMWjXq6FblFAAAAAElFTkSuQmCC"

// probeTimeout 单模型探测超时：管理页交互场景，宁可快速失败。
const probeTimeout = 30 * time.Second

// probeErrorLimit 探测错误文案上限：vlm 错误可能内嵌最多 8KB 上游响应体，截断防 UI 撑爆。
const probeErrorLimit = 500

// ProbeResult 是单个 VLM 模型的连通性测试结果。
type ProbeResult struct {
	Model     string `json:"model"`
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
	Preview   string `json:"preview,omitempty"`
}

// Probe 用内置测试图逐个真调配置中的模型链（主模型 + 备选），
// 供设置页「测试」按钮使用。校验规则与生产路径一致（extractText）。
func Probe(ctx context.Context, cfg Config) []ProbeResult {
	refs := []ImageRef{{URL: probeImage, IsDataURI: true}}
	prompt := BuildPrompt("auto", "描述图中的形状和颜色 / describe the shapes and colors", 1)
	models := modelChain(cfg)
	results := make([]ProbeResult, 0, len(models))
	for _, modelName := range models {
		attemptCtx, cancel := context.WithTimeout(ctx, probeTimeout)
		start := time.Now()
		text, err := analyzeWithModel(attemptCtx, cfg, modelName, refs, prompt)
		cancel()
		result := ProbeResult{Model: modelName, LatencyMS: time.Since(start).Milliseconds()}
		if err != nil {
			result.Error = truncateRunes(err.Error(), probeErrorLimit)
		} else {
			result.OK = true
			result.Preview = truncateRunes(text, 160)
		}
		results = append(results, result)
	}
	return results
}

// SnapshotSettings 暴露当前运行时配置快照（管理端测试端点用：
// 表单未填的字段回落已保存值）。
func SnapshotSettings() Config {
	return snapshotSettings()
}
