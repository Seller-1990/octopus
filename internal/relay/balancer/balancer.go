package balancer

import (
	"math"
	"math/rand"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/bestruirui/octopus/internal/model"
)

var roundRobinCounter uint64

// Balancer 根据负载均衡模式选择通道
type Balancer interface {
	// Candidates 返回按策略排序的候选列表
	// 调用方在遍历候选列表时自行检查熔断状态
	Candidates(items []model.GroupItem) []model.GroupItem
}

// GetBalancer 根据模式返回对应的负载均衡器
func GetBalancer(mode model.GroupMode) Balancer {
	switch mode {
	case model.GroupModeRoundRobin:
		return &RoundRobin{}
	case model.GroupModeRandom:
		return &Random{}
	case model.GroupModeFailover:
		return &Failover{}
	case model.GroupModeWeighted:
		return &Weighted{}
	default:
		return &RoundRobin{}
	}
}

// RoundRobin 轮询：从上次位置开始轮转排列
type RoundRobin struct{}

func (b *RoundRobin) Candidates(items []model.GroupItem) []model.GroupItem {
	n := len(items)
	if n == 0 {
		return nil
	}
	idx := int(atomic.AddUint64(&roundRobinCounter, 1) % uint64(n))
	result := make([]model.GroupItem, n)
	for i := 0; i < n; i++ {
		result[i] = items[(idx+i)%n]
	}
	return result
}

// Random 随机：随机打乱所有 items
type Random struct{}

func (b *Random) Candidates(items []model.GroupItem) []model.GroupItem {
	n := len(items)
	if n == 0 {
		return nil
	}
	result := make([]model.GroupItem, n)
	copy(result, items)
	rand.Shuffle(n, func(i, j int) {
		result[i], result[j] = result[j], result[i]
	})
	return result
}

// Failover 故障转移：按优先级排序
type Failover struct{}

func (b *Failover) Candidates(items []model.GroupItem) []model.GroupItem {
	if len(items) == 0 {
		return nil
	}
	return sortByPriority(items)
}

// Weighted 加权分配：按权重概率排序
type Weighted struct{}

func (b *Weighted) Candidates(items []model.GroupItem) []model.GroupItem {
	n := len(items)
	if n == 0 {
		return nil
	}

	// 加权随机排序（Efraimidis–Spirakis）：
	// 给每个 item 一个 key = U^(1/w)，按 key 降序排列时，任一位次上
	// 选中 item 的概率恰为 w/剩余总权重。线性打分 rand*w/totalWeight
	// 会系统性高选重项（如 2:1 权重实际约 75/25，见 F05），故弃用。
	type weightedItem struct {
		item  model.GroupItem
		score float64
	}

	scored := make([]weightedItem, n)
	for i, item := range items {
		w := item.Weight
		if w <= 0 {
			w = 1
		}
		scored[i] = weightedItem{
			item:  item,
			score: weightedSampleKey(float64(w), rand.Float64()),
		}
	}

	// 按分数降序排列（分数越高优先级越高）
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	result := make([]model.GroupItem, n)
	for i := range scored {
		result[i] = scored[i].item
	}
	return result
}

// weightedSampleKey 计算加权无放回排序的 Efraimidis–Spirakis 键：U^(1/w)。
// 对任意位次，item 以 w/剩余权重 的概率胜出；纯函数，便于单测锁定。
func weightedSampleKey(w, u float64) float64 {
	if w <= 0 {
		w = 1
	}
	return math.Pow(u, 1/w)
}

func sortByPriority(items []model.GroupItem) []model.GroupItem {
	sorted := make([]model.GroupItem, len(items))
	copy(sorted, items)
	// 必须稳定排序：同 priority 成员的相对顺序即用户在分组里的配置顺序
	//（DB 按 priority ASC, id ASC 加载）。此前用 sort.Slice（pdqsort 不稳定），
	// 同优先级成员的调用顺序每次可能不同——表现为 failover 乱序。
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})
	return sorted
}

// Reset clears in-memory balancer state for tests.
func Reset() {
	roundRobinCounter = 0
	globalBreaker = sync.Map{}
	globalSession = sync.Map{}
}
