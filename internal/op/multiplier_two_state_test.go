package op

import (
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

// TestTwoStateMultiplierCapMatrix 两态规则回归矩阵（阶段 7 收口，W1/W2/W3/W10/W11 修正）：
// 判定仅「cap 开启 && known=true && value>cap → blocked」；其余一律放行。
// 子测试用独立 group_key 避免 (site_account_id, group_key) 唯一索引冲突（W2）；
// settingRefreshCache 前置（W3）；cap 清理用 t.Cleanup。
func TestTwoStateMultiplierCapMatrix(t *testing.T) {
	cases := []struct {
		name       string
		groupKey   string
		multiplier *float64
		known      bool
		cap        string
		wantStatus string
	}{
		{name: "known true over cap blocked", groupKey: "mtx-a", multiplier: f64Ptr(6), known: true, cap: "4", wantStatus: MultiplierPolicyStatusBlocked},
		{name: "known false over cap allowed (keep-block)", groupKey: "mtx-b", multiplier: f64Ptr(6), known: false, cap: "4", wantStatus: MultiplierPolicyStatusAllowed},
		{name: "no multiplier unknown allowed", groupKey: "mtx-c", multiplier: nil, known: false, cap: "4", wantStatus: MultiplierPolicyStatusUnknown},
		{name: "known true under cap allowed", groupKey: "mtx-d", multiplier: f64Ptr(2), known: true, cap: "4", wantStatus: MultiplierPolicyStatusAllowed},
		{name: "cap disabled over cap allowed", groupKey: "mtx-e", multiplier: f64Ptr(6), known: true, cap: "0", wantStatus: MultiplierPolicyStatusAllowed},
		{name: "cap below one blocks known 1x", groupKey: "mtx-f", multiplier: f64Ptr(1), known: true, cap: "0.5", wantStatus: MultiplierPolicyStatusBlocked},
		// 免费分组守护：已知 0x（免费）必须放行——免费模型不得被跳过调用；
		// 0 永不大于任何 cap，即便 cap 低至 0.5 也不得误伤免费分组。
		{name: "free zero multiplier known allowed", groupKey: "mtx-g", multiplier: f64Ptr(0), known: true, cap: "4", wantStatus: MultiplierPolicyStatusAllowed},
		{name: "free zero multiplier survives low cap", groupKey: "mtx-h", multiplier: f64Ptr(0), known: true, cap: "0.5", wantStatus: MultiplierPolicyStatusAllowed},
		{name: "zero unknown treated as missing not free", groupKey: "mtx-i", multiplier: f64Ptr(0), known: false, cap: "4", wantStatus: MultiplierPolicyStatusAllowed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := setupCatalogProvisionTest(t)
			if err := settingRefreshCache(ctx); err != nil {
				t.Fatalf("refresh settings: %v", err)
			}
			if err := SettingSetString(model.SettingKeyDefaultMultiplierCap, tc.cap); err != nil {
				t.Fatalf("set multiplier cap: %v", err)
			}
			t.Cleanup(func() { _ = SettingSetString(model.SettingKeyDefaultMultiplierCap, "0") })

			site := model.Site{Name: "matrix-site-" + tc.groupKey, Platform: model.SitePlatformNewAPI, BaseURL: "https://matrix.example", Enabled: true}
			if err := dbpkg.GetDB().WithContext(ctx).Create(&site).Error; err != nil {
				t.Fatalf("create site: %v", err)
			}
			account := model.SiteAccount{SiteID: site.ID, Name: "matrix-account-" + tc.groupKey, CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "token", Enabled: true}
			if err := dbpkg.GetDB().WithContext(ctx).Create(&account).Error; err != nil {
				t.Fatalf("create account: %v", err)
			}
			known := tc.known
			group := model.SiteUserGroup{SiteAccountID: account.ID, GroupKey: tc.groupKey, Name: tc.groupKey, Multiplier: tc.multiplier, MultiplierKnown: &known}
			if err := dbpkg.GetDB().WithContext(ctx).Create(&group).Error; err != nil {
				t.Fatalf("create site group: %v", err)
			}
			channel := model.Channel{Name: "matrix-channel-" + tc.groupKey, Model: "matrix-model", Enabled: true}
			if err := ChannelCreate(&channel, ctx); err != nil {
				t.Fatalf("create channel: %v", err)
			}
			if err := dbpkg.GetDB().WithContext(ctx).Create(&model.SiteChannelBinding{
				SiteID: site.ID, SiteAccountID: account.ID, SiteUserGroupID: &group.ID,
				GroupKey: tc.groupKey, ChannelID: channel.ID,
			}).Error; err != nil {
				t.Fatalf("create binding: %v", err)
			}

			items := []model.GroupItem{{ChannelID: channel.ID, ModelName: "matrix-model"}}
			policies := evaluateGroupItemMultiplierPolicies(ctx, items)
			if len(policies) != 1 {
				t.Fatalf("expected 1 policy, got %d", len(policies))
			}
			if policies[0].status != tc.wantStatus {
				t.Fatalf("case %q: status = %q, want %q (multiplier=%v known=%v cap=%s)", tc.name, policies[0].status, tc.wantStatus, tc.multiplier, tc.known, tc.cap)
			}
		})
	}
}

// TestEnforceMultiplierCapKnownAwareRecovers 组级回归锁（W10，X3 用户拍板项）：
// known=false 5x + cap=4 → 不拦；已 policy_blocked 组被 recover 解阻。
func TestEnforceMultiplierCapKnownAwareRecovers(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	if err := settingRefreshCache(ctx); err != nil {
		t.Fatalf("refresh settings: %v", err)
	}
	if err := SettingSetString(model.SettingKeyDefaultMultiplierCap, "4"); err != nil {
		t.Fatalf("set multiplier cap: %v", err)
	}
	t.Cleanup(func() { _ = SettingSetString(model.SettingKeyDefaultMultiplierCap, "0") })

	site := model.Site{Name: "recover-site", Platform: model.SitePlatformNewAPI, BaseURL: "https://recover.example", Enabled: true}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&site).Error; err != nil {
		t.Fatalf("create site: %v", err)
	}
	account := model.SiteAccount{SiteID: site.ID, Name: "recover-account", CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "token", Enabled: true}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}
	// known=false 5x（keep-block 形态）：超限但不拦（两态）
	five := 5.0
	knownFalse := false
	group := model.SiteUserGroup{SiteAccountID: account.ID, GroupKey: "recover-g", Name: "Recover", Multiplier: &five, MultiplierKnown: &knownFalse, PolicyBlocked: true, PolicyBlockReason: "legacy"}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&group).Error; err != nil {
		t.Fatalf("create site group: %v", err)
	}

	if _, _, err := EnforceMultiplierCap(ctx); err != nil {
		t.Fatalf("enforce multiplier cap: %v", err)
	}
	var reloaded model.SiteUserGroup
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloaded, group.ID).Error; err != nil {
		t.Fatalf("reload group: %v", err)
	}
	if reloaded.PolicyBlocked {
		t.Fatalf("known=false 5x group should be recovered (two-state), got policy_blocked=true: %+v", reloaded)
	}
}

func f64Ptr(v float64) *float64 { return &v }
