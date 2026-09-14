package op

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

// 回归：SiteModelPriceManualUpsert 曾无条件把 GroupMultiplierKnown 置 true。
// 但 GroupMultiplier 是 float64，HTTP 请求体漏传该字段时它就是 Go 零值 0，
// 于是「未提供倍率」被固化成「已确认免费」——下游按 0 成本选路，
// 该渠道的用量与成本统计也一并归零。
func TestSiteModelPriceManualUpsertDefaultsOmittedMultiplierToOne(t *testing.T) {
	ctx := setupBackupTestDB(t)
	fixture := createPricingFixture(t, ctx, "manual-omitted-multiplier")

	manual, err := SiteModelPriceManualUpsert(ctx, model.SiteModelPriceQuote{
		RouteCandidateID: &fixture.candidate.ID,
		Unit:             model.PriceUnitPerMillionTokens,
		Currency:         "USD",
		Input:            3,
		Output:           6,
	})
	if err != nil {
		t.Fatalf("SiteModelPriceManualUpsert failed: %v", err)
	}
	if manual.GroupMultiplierKnown {
		t.Fatalf("omitted group_multiplier must not be recorded as known: %+v", manual)
	}
	if manual.GroupMultiplier != 1 {
		t.Fatalf("omitted group_multiplier must fall back to 1, got %v", manual.GroupMultiplier)
	}

	price, err := EffectivePriceForCandidate(ctx, fixture.candidate.ID, "")
	if err != nil {
		t.Fatalf("resolve effective price failed: %v", err)
	}
	if price.Input != 3 || price.Output != 6 {
		t.Fatalf("manual price was zeroed by the default multiplier: %+v", price)
	}
	if price.GroupMultiplier != 1 {
		t.Fatalf("effective multiplier should be 1, got %v", price.GroupMultiplier)
	}
}

// F17 本意不得回退：管理员显式给出的非零倍率必须标为真值。
func TestSiteModelPriceManualUpsertMarksExplicitNonZeroMultiplierKnown(t *testing.T) {
	ctx := setupBackupTestDB(t)
	fixture := createPricingFixture(t, ctx, "manual-nonzero-multiplier")

	manual, err := SiteModelPriceManualUpsert(ctx, model.SiteModelPriceQuote{
		RouteCandidateID: &fixture.candidate.ID,
		Unit:             model.PriceUnitPerMillionTokens,
		Currency:         "USD",
		Input:            2,
		Output:           4,
		GroupMultiplier:  5,
	})
	if err != nil {
		t.Fatalf("SiteModelPriceManualUpsert failed: %v", err)
	}
	if !manual.GroupMultiplierKnown {
		t.Fatalf("explicit non-zero multiplier must be marked known: %+v", manual)
	}
	if manual.GroupMultiplier != 5 {
		t.Fatalf("explicit multiplier was rewritten: %v", manual.GroupMultiplier)
	}

	price, err := EffectivePriceForCandidate(ctx, fixture.candidate.ID, "")
	if err != nil {
		t.Fatalf("resolve effective price failed: %v", err)
	}
	if price.Input != 10 || price.Output != 20 {
		t.Fatalf("5x multiplier was not applied: %+v", price)
	}
}

// 「显式免费」这一合法状态仍必须可表达（免费分组需要它）。
func TestSiteModelPriceManualUpsertKeepsDeclaredFreeMultiplier(t *testing.T) {
	ctx := setupBackupTestDB(t)
	fixture := createPricingFixture(t, ctx, "manual-declared-free")

	manual, err := SiteModelPriceManualUpsert(ctx, model.SiteModelPriceQuote{
		RouteCandidateID:     &fixture.candidate.ID,
		Unit:                 model.PriceUnitPerMillionTokens,
		Currency:             "USD",
		Input:                3,
		Output:               6,
		GroupMultiplier:      0,
		GroupMultiplierKnown: true,
	})
	if err != nil {
		t.Fatalf("SiteModelPriceManualUpsert failed: %v", err)
	}
	if !manual.GroupMultiplierKnown || manual.GroupMultiplier != 0 {
		t.Fatalf("declared free multiplier was not preserved: %+v", manual)
	}
}
