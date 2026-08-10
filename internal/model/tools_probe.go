package model

// ToolsProbeState 渠道×模型 tools 探测状态（v3.1 判别矩阵五态 + 细分）。
type ToolsProbeState string

const (
	// ToolsProbeStateAccepted auto/降级 2xx：协议层接受 tools 参数（弱证据，T4 语义）。
	ToolsProbeStateAccepted ToolsProbeState = "accepted"
	// ToolsProbeStateExecuted required 逼出 tool_call：执行确认（强证据）。
	ToolsProbeStateExecuted ToolsProbeState = "executed"
	// ToolsProbeStateRequiredUnsupported required 400 → auto 2xx：支持 tools 但 required 不可用。
	ToolsProbeStateRequiredUnsupported ToolsProbeState = "required_unsupported"
	// ToolsProbeStateRequiredIgnored required 200 无 tool_call：模型不服从 required 或网关静默剥参。
	ToolsProbeStateRequiredIgnored ToolsProbeState = "required_ignored"
	// ToolsProbeStateUnsupported 白名单 ≥2 确认：不支持 tools。
	ToolsProbeStateUnsupported ToolsProbeState = "unsupported"
	// ToolsProbeStatePending 白名单第 1 次命中：待确认（≥2 确认机制）。
	ToolsProbeStatePending ToolsProbeState = "pending"
	// ToolsProbeStateUnknown 非白名单错误 / 5xx / 超时：不判定。
	ToolsProbeStateUnknown ToolsProbeState = "unknown"
)

// ToolsProbeResult 探测结果（v3.1 判别矩阵输出）。
type ToolsProbeResult struct {
	State    ToolsProbeState
	Supports bool   // State 为 accepted/executed/required_unsupported 时 true；unsupported 时 false
	Source   string // probe / manual / manual-required-fallback
}
