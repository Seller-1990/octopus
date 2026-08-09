package op

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

// TestPersistedSiteGroupMultiplierMapFallsBackToRawPayload 保留于 op 包（依赖
// setupCatalogProvisionTest/mustCreatePricingRow/persistedSiteGroupMultiplierMap
// 等 op 内部符号）；纯解析器测试 TestStoredSiteGroupMultiplier 已随解析器移入
// internal/model（阶段 1 改动 B'）。
func TestPersistedSiteGroupMultiplierMapFallsBackToRawPayload(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	site := model.Site{
		Name: "raw-group-multiplier-site", Platform: model.SitePlatformNewAPI,
		BaseURL: "https://raw-group-multiplier.example.com", Enabled: true,
	}
	mustCreatePricingRow(t, ctx, &site)
	account := model.SiteAccount{
		SiteID: site.ID, Name: "raw-group-multiplier-account",
		CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "token", Enabled: true,
	}
	mustCreatePricingRow(t, ctx, &account)
	group := model.SiteUserGroup{
		SiteAccountID: account.ID,
		GroupKey:      "complimentary",
		Name:          "Complimentary",
		RawPayload:    `{"data":{"claude_code":{"ratio":5},"complimentary":{"ratio":0}}}`,
	}
	mustCreatePricingRow(t, ctx, &group)

	values := persistedSiteGroupMultiplierMap(ctx, []int{account.ID})
	key := siteAccountGroupKey(account.ID, group.GroupKey)
	// 阶段 2 补充：raw_payload 读时兜底关闭——该行 multiplier 列为 nil（fixture 未设），
	// 即使 raw_payload 含 ratio 也不再被读路径解析（未知按暂定放行，不暴露倍率）。
	if _, ok := values[key]; ok {
		t.Fatalf("raw payload fallback still exposed multiplier: %+v", values)
	}
}
