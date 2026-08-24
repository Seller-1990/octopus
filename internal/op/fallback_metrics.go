package op

import "sync/atomic"

// 这些计数器记录请求路径上的静默降级次数，供后续管理面板/告警使用。
// 当前仅通过日志与这两个导出函数观察。
var (
	headerPolicyFallbackTotal     atomic.Uint64
	multiplierBindingFailureTotal atomic.Uint64
	multiplierLookupFailureTotal  atomic.Uint64
)

func HeaderPolicyFallbackTotal() uint64 {
	return headerPolicyFallbackTotal.Load()
}

func MultiplierBindingFailureTotal() uint64 {
	return multiplierBindingFailureTotal.Load()
}

func MultiplierLookupFailureTotal() uint64 {
	return multiplierLookupFailureTotal.Load()
}
