package balancer

import (
	"testing"
	"time"
)

// TestCircuitKeyScopedByUpstreamModelName 锁定熔断键的模型名语义：
// 键 = channelID:keyID:modelName，其中 modelName 必须是"上游模型名"
// （分组 item 的 ModelName）。此契约是 F02 的根源：compact 路径曾用
// 客户端模型名记账（RecordSuccess/RecordFailure），而 iterator 用上游
// 模型名查询（SkipCircuitBreak → IsTripped），重映射分组下熔断器永远
// 读不到自己写下的键。本测试固化"同名命中、异名不命中"的预期，防止
// 任一调用点再度漂移。
func TestCircuitKeyScopedByUpstreamModelName(t *testing.T) {
	Reset()
	defer Reset()

	const (
		channelID = 71
		keyID     = 77
	)

	// 直接播种 Open 态条目，避免依赖 RecordFailure 的阈值设置读取。
	globalBreaker.Store(circuitKey(channelID, keyID, "glm-5.2-upstream"), &circuitEntry{
		State:               StateOpen,
		ConsecutiveFailures: 6,
		LastFailureTime:     time.Now(),
		TripCount:           6,
	})

	if tripped, _ := IsTripped(channelID, keyID, "glm-5.2-upstream"); !tripped {
		t.Fatal("expected breaker tripped under the upstream model name it was recorded with")
	}
	// 重映射场景：客户端模型名与上游模型名不同（如 "glm-5.2" → "glm-5.2-upstream"）。
	// 若记账误用客户端名，则此处为 true（键不匹配），正是 F02 的故障形态。
	if tripped, _ := IsTripped(channelID, keyID, "glm-5.2"); tripped {
		t.Fatal("breaker state for upstream model name leaked onto the client-facing model name key")
	}
}
