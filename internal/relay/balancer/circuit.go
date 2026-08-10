package balancer

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
)

// CircuitState 熔断器状态
type CircuitState int

type FailureKind int

const (
	StateClosed   CircuitState = iota // 正常通行
	StateOpen                         // 熔断中，拒绝所有请求
	StateHalfOpen                     // 半开，仅允许单个试探请求

	FailureHard FailureKind = iota
	FailureSoftRateLimit
)

// circuitEntry 单个熔断器条目
type circuitEntry struct {
	State               CircuitState
	ConsecutiveFailures int64
	LastFailureTime     time.Time
	TripCount           int // 累计熔断触发次数（用于指数退避）
	HalfOpenSince       time.Time
	mu                  sync.Mutex
}

// 全局熔断器存储
var globalBreaker sync.Map // key: string -> value: *circuitEntry

// circuitKey 生成熔断器键：channelID:channelKeyID:modelName
func circuitKey(channelID, keyID int, modelName string) string {
	return fmt.Sprintf("%d:%d:%s", channelID, keyID, modelName)
}

// CircuitStatus 熔断状态快照条目（管理面板用，结构化字段避免前端解析拼接 key）。
type CircuitStatus struct {
	ChannelID    int          `json:"channel_id"`
	ChannelKeyID int          `json:"channel_key_id"`
	ModelName    string       `json:"model_name"`
	State        CircuitState `json:"state"`
	// StateLabel 人类可读状态（closed/open/half_open），避免前端依赖 int 枚举。
	StateLabel          string    `json:"state_label"`
	ConsecutiveFailures int64     `json:"consecutive_failures"`
	TripCount           int       `json:"trip_count"`
	LastFailureTime     time.Time `json:"last_failure_time"`
	// CooldownUntil 冷却结束绝对时间戳（前端本地倒计时，避免高频轮询）。
	CooldownUntil time.Time `json:"cooldown_until,omitempty"`
}

// Snapshot 导出全部熔断条目（管理面）。
// 逐个 entry 持 mu 锁读（circuitEntry 是 Mutex 非 RWMutex——P1 修复：
// 不持锁直接读字段会读到跨写不一致的撕裂状态；锁序与热路径一致，无死锁）。
// 顺带惰性清理 Closed 且长时间无失败的纯噪音条目（P1 修复：closed 单调膨胀）。
func Snapshot() []CircuitStatus {
	out := make([]CircuitStatus, 0, 8)
	now := time.Now()
	globalBreaker.Range(func(key, value any) bool {
		k, ok := key.(string)
		if !ok {
			return true
		}
		entry := value.(*circuitEntry)

		entry.mu.Lock()
		defer entry.mu.Unlock()

		status := CircuitStatus{
			State:               entry.State,
			ConsecutiveFailures: entry.ConsecutiveFailures,
			TripCount:           entry.TripCount,
			LastFailureTime:     entry.LastFailureTime,
		}
		switch entry.State {
		case StateOpen:
			status.StateLabel = "open"
			cooldown := GetCooldown(entry.TripCount)
			status.CooldownUntil = entry.LastFailureTime.Add(cooldown)
		case StateHalfOpen:
			status.StateLabel = "half_open"
		default:
			status.StateLabel = "closed"
		}
		parseCircuitKey(k, &status)

		// 惰性清理（P1 修复）：仅当「完全健康（失败计数 0）且长时间无活动」才删。
		// 原条件 `Closed && >10min` 会把「低频故障节律」的失败计数抹掉（每次 Snapshot 后
		// ConsecutiveFailures 归零 → 永远到不了阈值，熔断对该渠道免疫）；且不能在管理面
		// 只读操作里改写还在累计失败的状态机。RecordSuccess 后计数为 0 的条目删除安全。
		if entry.State == StateClosed && entry.ConsecutiveFailures == 0 &&
			!entry.LastFailureTime.IsZero() && now.Sub(entry.LastFailureTime) > 10*time.Minute {
			globalBreaker.Delete(k)
			return true
		}
		out = append(out, status)
		return true
	})
	return out
}

// parseCircuitKey 把 "channelID:keyID:modelName" 拆回结构化字段。
// 模型名可能含冒号（罕见），故只从左侧切两段，余下全部归 modelName。
func parseCircuitKey(key string, status *CircuitStatus) {
	parts := strings.SplitN(key, ":", 3)
	if len(parts) < 3 {
		return
	}
	var cid, kid int
	fmt.Sscanf(parts[0], "%d", &cid)
	fmt.Sscanf(parts[1], "%d", &kid)
	status.ChannelID = cid
	status.ChannelKeyID = kid
	status.ModelName = parts[2]
}

// ResetCircuit 手动重置熔断状态（管理面）。
// scope=all 全量重置；否则按 (channelID, keyID, modelName) 精确重置——
// keyID=0/modelName="" 表示按渠道前缀重置（对齐 resetCircuitBreakerByChannel 先例）。
func ResetCircuit(scope string, channelID, keyID int, modelName string) {
	if scope == "all" {
		globalBreaker.Range(func(key, _ any) bool {
			globalBreaker.Delete(key)
			return true
		})
		return
	}
	if channelID > 0 && keyID > 0 && modelName != "" {
		globalBreaker.Delete(circuitKey(channelID, keyID, modelName))
		return
	}
	if channelID > 0 {
		resetCircuitBreakerByChannel(channelID)
	}
}

func resetCircuitBreakerByChannel(channelID int) {
	prefix := fmt.Sprintf("%d:", channelID)
	globalBreaker.Range(func(key, _ any) bool {
		if k, ok := key.(string); ok && strings.HasPrefix(k, prefix) {
			globalBreaker.Delete(k)
		}
		return true
	})
}

// getOrCreateEntry 获取或创建熔断器条目
func getOrCreateEntry(key string) *circuitEntry {
	if v, ok := globalBreaker.Load(key); ok {
		return v.(*circuitEntry)
	}
	entry := &circuitEntry{State: StateClosed}
	actual, _ := globalBreaker.LoadOrStore(key, entry)
	return actual.(*circuitEntry)
}

// getThreshold 获取熔断阈值配置
func getThreshold() int64 {
	v, err := op.SettingGetInt(model.SettingKeyCircuitBreakerThreshold)
	if err != nil || v <= 0 {
		return 5
	}
	return int64(v)
}

// GetCooldown 获取当前冷却时间（带指数退避）
func GetCooldown(tripCount int) time.Duration {
	base, err := op.SettingGetInt(model.SettingKeyCircuitBreakerCooldown)
	if err != nil || base <= 0 {
		base = 60
	}
	maxCooldown, err := op.SettingGetInt(model.SettingKeyCircuitBreakerMaxCooldown)
	if err != nil || maxCooldown <= 0 {
		maxCooldown = 600
	}

	// 指数退避：baseCooldown * 2^(tripCount-1)
	cooldown := base
	if tripCount > 1 {
		shift := tripCount - 1
		if shift > 20 { // 防止溢出
			shift = 20
		}
		cooldown = base << shift
	}
	if cooldown > maxCooldown {
		cooldown = maxCooldown
	}

	return time.Duration(cooldown) * time.Second
}

// IsTripped 检查通道是否处于熔断状态
// 返回 tripped=true 表示该通道应被跳过，remaining 为剩余冷却时间
func IsTripped(channelID, keyID int, modelName string) (tripped bool, remaining time.Duration) {
	key := circuitKey(channelID, keyID, modelName)
	v, ok := globalBreaker.Load(key)
	if !ok {
		return false, 0 // 无记录，视为 Closed
	}
	entry := v.(*circuitEntry)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	switch entry.State {
	case StateClosed:
		return false, 0

	case StateOpen:
		cooldown := GetCooldown(entry.TripCount)
		elapsed := time.Since(entry.LastFailureTime)
		if elapsed >= cooldown {
			now := time.Now()
			entry.State = StateHalfOpen
			entry.HalfOpenSince = now
			log.Infof("circuit breaker [%s] Open -> HalfOpen (cooldown %v elapsed)", key, cooldown)
			return false, 0
		}
		// 仍在冷却中
		return true, cooldown - elapsed

	case StateHalfOpen:
		cooldown := GetCooldown(entry.TripCount)
		if entry.HalfOpenSince.IsZero() {
			entry.HalfOpenSince = time.Now()
		}
		if time.Since(entry.HalfOpenSince) >= cooldown {
			entry.State = StateOpen
			entry.LastFailureTime = time.Now()
			entry.HalfOpenSince = time.Time{}
			log.Warnf("circuit breaker [%s] HalfOpen -> Open (probe timed out, cooldown=%v)", key, cooldown)
			return true, cooldown
		}
		// 已有试探请求在进行中，拒绝其他请求
		return true, 0

	default:
		return false, 0
	}
}

// RecordSuccess 记录成功，重置熔断器状态
func RecordSuccess(channelID, keyID int, modelName string) {
	key := circuitKey(channelID, keyID, modelName)
	v, ok := globalBreaker.Load(key)
	if !ok {
		return
	}
	entry := v.(*circuitEntry)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.State == StateHalfOpen {
		log.Infof("circuit breaker [%s] HalfOpen -> Closed (probe succeeded)", key)
	}

	// 重置全部状态
	entry.State = StateClosed
	entry.ConsecutiveFailures = 0
	entry.TripCount = 0
	entry.HalfOpenSince = time.Time{}
}

// RecordFailure 记录失败，可能触发熔断。
// FailureSoftRateLimit 用于 429/503 这类软失败：Closed 状态下不累计阈值，
// HalfOpen 状态下重新进入 Open，但不放大 TripCount。
func RecordFailure(channelID, keyID int, modelName string, kind FailureKind) {
	key := circuitKey(channelID, keyID, modelName)
	entry := getOrCreateEntry(key)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	entry.LastFailureTime = time.Now()
	entry.HalfOpenSince = time.Time{}

	switch entry.State {
	case StateClosed:
		if kind == FailureSoftRateLimit {
			return
		}
		entry.ConsecutiveFailures++
		threshold := getThreshold()
		if entry.ConsecutiveFailures >= threshold {
			entry.State = StateOpen
			entry.TripCount++
			log.Warnf("circuit breaker [%s] Closed -> Open (failures=%d >= threshold=%d, tripCount=%d, cooldown=%v)",
				key, entry.ConsecutiveFailures, threshold, entry.TripCount, GetCooldown(entry.TripCount))
		}

	case StateHalfOpen:
		if kind == FailureSoftRateLimit {
			entry.State = StateOpen
			log.Warnf("circuit breaker [%s] HalfOpen -> Open (soft rate limit, tripCount=%d, cooldown=%v)",
				key, entry.TripCount, GetCooldown(entry.TripCount))
			return
		}
		// 试探失败，重新进入 Open 状态，TripCount 递增（冷却时间翻倍）
		entry.State = StateOpen
		entry.TripCount++
		entry.ConsecutiveFailures = 0 // 重新开始计数
		log.Warnf("circuit breaker [%s] HalfOpen -> Open (probe failed, tripCount=%d, cooldown=%v)",
			key, entry.TripCount, GetCooldown(entry.TripCount))

	case StateOpen:
		// 理论上不应该在 Open 状态下接收到失败记录（请求应被拒绝），
		// 但为安全起见仍更新失败时间
	}
}
