package op

import (
	"math"
	"strings"
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

// 价格对比查询：仅返回 valid+时间窗内报价、USD 折算含分组倍率与汇率、
// model_multiplier=0 输出 null（前端展示「—」）、汇总含 min/median/max/spread。
func TestSiteModelPriceCompare(t *testing.T) {
	ctx := setupBackupTestDB(t)
	fixture := createPricingFixture(t, ctx, "compare")
	now := time.Now()
	modelName := fixture.candidate.UpstreamModelName

	site2 := model.Site{
		Name: "pricing-site-compare-2", Platform: model.SitePlatformNewAPI,
		BaseURL: "https://pricing-compare-2.example.com", Enabled: true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&site2).Error; err != nil {
		t.Fatalf("create second site: %v", err)
	}

	base := model.SiteModelPriceQuote{
		Unit: model.PriceUnitPerMillionTokens, ModelName: modelName,
	}
	quotes := []model.SiteModelPriceQuote{
		// 站点1（账号绑定行）：USD 2/4，分组倍率 2 → 到手输出 8 USD
		func() model.SiteModelPriceQuote {
			q := base
			q.SiteID = fixture.site.ID
			q.SiteAccountID = &fixture.account.ID
			q.GroupKey = "premium"
			q.Source = model.PriceQuoteSourceSiteExact
			q.Currency = "USD"
			q.Input = 2
			q.Output = 4
			q.ModelMultiplier = 1.5
			q.GroupMultiplier = 2
			q.GroupMultiplierKnown = true
			q.ExchangeRateToUSD = 1
			q.ObservedAt = now
			return q
		}(),
		// 站点2：CNY 8/16，倍率 1，汇率 0.14 → 到手输出 2.24 USD；模型倍率未知(0)
		func() model.SiteModelPriceQuote {
			q := base
			q.SiteID = site2.ID
			q.GroupKey = model.SiteDefaultGroupKey
			q.Source = model.PriceQuoteSourceSiteWide
			q.Currency = "CNY"
			q.Input = 8
			q.Output = 16
			q.GroupMultiplier = 1
			q.GroupMultiplierKnown = true
			q.ExchangeRateToUSD = 0.14
			q.ObservedAt = now
			return q
		}(),
		// rejected：必须排除
		func() model.SiteModelPriceQuote {
			q := base
			q.SiteID = site2.ID
			q.GroupKey = "premium"
			q.Source = model.PriceQuoteSourceSiteWide
			q.Currency = "USD"
			q.Input = 100
			q.Output = 100
			q.GroupMultiplier = 1
			q.ExchangeRateToUSD = 1
			q.ObservedAt = now
			q.Status = model.PriceQuoteStatusRejected
			return q
		}(),
		// 超出 7 天窗口：必须排除
		func() model.SiteModelPriceQuote {
			q := base
			q.SiteID = site2.ID
			q.GroupKey = "legacy"
			q.Source = model.PriceQuoteSourceSiteWide
			q.Currency = "USD"
			q.Input = 50
			q.Output = 50
			q.GroupMultiplier = 1
			q.ExchangeRateToUSD = 1
			q.ObservedAt = now.AddDate(0, 0, -8)
			return q
		}(),
	}
	for i := range quotes {
		quotes[i].RefreshIdentityKey()
		if err := dbpkg.GetDB().WithContext(ctx).Create(&quotes[i]).Error; err != nil {
			t.Fatalf("create quote %d: %v", i, err)
		}
	}

	rows, summary, err := SiteModelPriceCompare(ctx, strings.ToUpper(modelName), 7, 200)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if summary.RowCount != 2 {
		t.Fatalf("expected 2 rows (valid + within window), got %d", summary.RowCount)
	}

	bySite := make(map[int]SiteModelPriceCompareRow, len(rows))
	for _, row := range rows {
		bySite[row.SiteID] = row
	}
	premium, ok := bySite[fixture.site.ID]
	if !ok {
		t.Fatalf("site %d row missing: %+v", fixture.site.ID, rows)
	}
	if premium.OutputUSD == nil || math.Abs(*premium.OutputUSD-8) > 1e-9 {
		t.Fatalf("premium output USD = %v, want 8 (4 × multiplier 2 × rate 1)", premium.OutputUSD)
	}
	if premium.ModelMultiplier == nil || *premium.ModelMultiplier != 1.5 {
		t.Fatalf("premium model multiplier should pass through, got %v", premium.ModelMultiplier)
	}

	discount, ok := bySite[site2.ID]
	if !ok {
		t.Fatalf("site %d row missing: %+v", site2.ID, rows)
	}
	if discount.OutputUSD == nil || math.Abs(*discount.OutputUSD-2.24) > 1e-9 {
		t.Fatalf("discount output USD = %v, want 2.24 (16 × 1 × 0.14)", discount.OutputUSD)
	}
	if discount.ModelMultiplier != nil {
		t.Fatalf("model multiplier 0 must serialize as null (unknown), got %v", *discount.ModelMultiplier)
	}
	if discount.Stale {
		t.Fatal("fresh quote must not be stale")
	}
	if discount.SiteName != site2.Name || premium.SiteAccountName != fixture.account.Name {
		t.Fatalf("display names missing: %+v / %+v", premium, discount)
	}

	if summary.MinOutputUSD == nil || summary.MaxOutputUSD == nil || summary.SpreadRatio == nil {
		t.Fatalf("summary incomplete: %+v", summary)
	}
	if math.Abs(*summary.MinOutputUSD-2.24) > 1e-9 || math.Abs(*summary.MaxOutputUSD-8) > 1e-9 {
		t.Fatalf("summary min/max wrong: %+v", summary)
	}
	if math.Abs(*summary.SpreadRatio-8/2.24) > 1e-6 {
		t.Fatalf("spread ratio = %v, want ~%.6f", *summary.SpreadRatio, 8/2.24)
	}
}
