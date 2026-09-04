package balancer

import (
	"math/rand"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

// TestWeightedCandidatesSelectionProbability 统计验证加权抽样概率：
// 按 Efraimidis–Spirakis（key = U^(1/w)）排序后，首位命中权重 2 的
// item 的概率应为 2/3；旧的线性打分 rand*w/totalWeight 会给出约 0.75
// （F05 回归）。取 3 万次采样，±10σ 容差（σ≈0.27%），无 flake 空间。
func TestWeightedCandidatesSelectionProbability(t *testing.T) {
	items := []model.GroupItem{
		{ID: 1, ChannelID: 101, ModelName: "heavy", Weight: 2},
		{ID: 2, ChannelID: 102, ModelName: "light", Weight: 1},
	}

	const samples = 30_000
	heavyFirst := 0
	b := &Weighted{}
	for i := 0; i < samples; i++ {
		got := b.Candidates(items)
		if len(got) != 2 {
			t.Fatalf("sample %d: candidate count = %d, want 2", i, len(got))
		}
		if got[0].ChannelID == 101 {
			heavyFirst++
		}
	}
	p := float64(heavyFirst) / samples
	const want = 2.0 / 3.0
	const tol = 0.03 // ≈ 11σ，远宽于 3σ，仅锁"系统性偏差"级别回归
	if p < want-tol || p > want+tol {
		t.Fatalf("P(weight-2 first) = %.4f, want %.4f ± %.4f (old linear scoring yields ~0.75)",
			p, want, tol)
	}
}

// TestWeightedKeyMonotonicInWeight 校验 key=U^(1/w) 的单调性不变量：
// 固定 u ∈ (0,1) 时，权重越大 key 越大（重项占优的机制来源）。
func TestWeightedKeyMonotonicInWeight(t *testing.T) {
	rng := rand.New(rand.NewSource(20260904))
	for i := 0; i < 1000; i++ {
		u := rng.Float64()
		if u == 0 {
			continue
		}
		keyLight := weightedSampleKey(1, u) // u^1
		keyHeavy := weightedSampleKey(2, u) // u^(1/2)
		if keyHeavy <= keyLight {
			t.Fatalf("u=%f: key(2)=%f should exceed key(1)=%f", u, keyHeavy, keyLight)
		}
		if keyLight != u {
			t.Fatalf("key(1) must equal u, got %f", keyLight)
		}
	}
}
