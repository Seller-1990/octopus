package migrate

import (
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// TestMigratePollutedMultiplierResetsKnownSignatures C250913-05：污染签名
// (manual_override=true, known=true, multiplier=0) 被重置为 known=false；
// 合法行（显式免费经 UI 不可达、非零倍率、站点来源报价）不受影响。
func TestMigratePollutedMultiplierResetsKnownSignatures(t *testing.T) {
	conn, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := conn.AutoMigrate(&model.SiteModelPriceQuote{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	quotes := []model.SiteModelPriceQuote{
		{ModelName: "polluted", ManualOverride: true, GroupMultiplier: 0, GroupMultiplierKnown: true, Source: model.PriceQuoteSourceManualOverride, Currency: "USD", Unit: model.PriceUnitPerMillionTokens, ObservedAt: testNow()},
		{ModelName: "explicit-free-kept-if-ever", ManualOverride: false, GroupMultiplier: 0, GroupMultiplierKnown: true, Source: model.PriceQuoteSourceSiteExact, Currency: "USD", Unit: model.PriceUnitPerMillionTokens, ObservedAt: testNow()},
		{ModelName: "nonzero", ManualOverride: true, GroupMultiplier: 2.5, GroupMultiplierKnown: true, Source: model.PriceQuoteSourceManualOverride, Currency: "USD", Unit: model.PriceUnitPerMillionTokens, ObservedAt: testNow()},
	}
	for i := range quotes {
		if err := conn.Create(&quotes[i]).Error; err != nil {
			t.Fatalf("seed quote: %v", err)
		}
	}
	// GORM 零值+default:1 陷阱：INSERT 会跳过 multiplier=0，显式回写以构造污染签名
	if err := conn.Model(&model.SiteModelPriceQuote{}).Where("model_name = ?", "polluted").
		Update("group_multiplier", 0).Error; err != nil {
		t.Fatalf("force zero multiplier: %v", err)
	}

	if err := migrateSiteModelPriceQuotePollutedMultiplier(conn); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	var polluted model.SiteModelPriceQuote
	if err := conn.First(&polluted, "model_name = ?", "polluted").Error; err != nil {
		t.Fatalf("load polluted: %v", err)
	}
	if polluted.GroupMultiplierKnown {
		t.Fatal("polluted row must have known reset to false")
	}
	var untouched model.SiteModelPriceQuote
	if err := conn.First(&untouched, "model_name = ?", "explicit-free-kept-if-ever").Error; err != nil {
		t.Fatalf("load untouched: %v", err)
	}
	if !untouched.GroupMultiplierKnown {
		t.Fatal("non-manual row must be untouched")
	}
	var nonzero model.SiteModelPriceQuote
	if err := conn.First(&nonzero, "model_name = ?", "nonzero").Error; err != nil {
		t.Fatalf("load nonzero: %v", err)
	}
	if !nonzero.GroupMultiplierKnown {
		t.Fatal("non-zero manual row must keep known=true")
	}

	// 幂等：重跑无副作用
	if err := migrateSiteModelPriceQuotePollutedMultiplier(conn); err != nil {
		t.Fatalf("second run failed: %v", err)
	}
}

func testNow() time.Time { return time.Unix(1700000000, 0) }
