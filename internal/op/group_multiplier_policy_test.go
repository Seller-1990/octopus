package op

import (
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func TestMultiplierCapBlocksRoutingWithoutDeletingGroupItem(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	if err := settingRefreshCache(ctx); err != nil {
		t.Fatalf("refresh settings: %v", err)
	}
	if err := SettingSetString(model.SettingKeyDefaultMultiplierCap, "4"); err != nil {
		t.Fatalf("set multiplier cap: %v", err)
	}
	t.Cleanup(func() { _ = SettingSetString(model.SettingKeyDefaultMultiplierCap, "0") })

	site := model.Site{Name: "policy-site", Platform: model.SitePlatformNewAPI, BaseURL: "https://policy.example", Enabled: true}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&site).Error; err != nil {
		t.Fatalf("create site: %v", err)
	}
	account := model.SiteAccount{SiteID: site.ID, Name: "policy-account", CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "token", Enabled: true}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}
	multiplier := 6.0
	known := true // 阶段 2 v2 X3：两态判定下 fixture 须标 known=true 才会被拦（T1）
	groupOwner := model.SiteUserGroup{SiteAccountID: account.ID, GroupKey: model.SiteDefaultGroupKey, Name: model.SiteDefaultGroupName, Multiplier: &multiplier, MultiplierKnown: &known}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&groupOwner).Error; err != nil {
		t.Fatalf("create site group: %v", err)
	}
	channel := model.Channel{Name: "policy-channel", Model: "policy-model", Enabled: true}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&model.SiteChannelBinding{
		SiteID: site.ID, SiteAccountID: account.ID, SiteUserGroupID: &groupOwner.ID,
		GroupKey: model.SiteDefaultGroupKey, ChannelID: channel.ID,
	}).Error; err != nil {
		t.Fatalf("create binding: %v", err)
	}
	group := model.Group{Name: "policy-model", Mode: model.GroupModeFailover}
	if err := GroupCreate(&group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	item := model.GroupItem{GroupID: group.ID, ChannelID: channel.ID, ModelName: "policy-model", Priority: 1, Weight: 999}
	if err := GroupItemAdd(&item, ctx); err != nil {
		t.Fatalf("create group item: %v", err)
	}
	loadedGroup, err := GroupGet(group.ID, ctx)
	if err != nil {
		t.Fatalf("reload group: %v", err)
	}
	group = *loadedGroup

	result, err := ApplyGroupDefaults(ctx)
	if err != nil {
		t.Fatalf("apply defaults: %v", err)
	}
	if result.ItemsRemoved != 0 || result.ItemsBlocked == 0 {
		t.Fatalf("unexpected apply result: %+v", result)
	}
	var count int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.GroupItem{}).Where("id = ?", item.ID).Count(&count).Error; err != nil {
		t.Fatalf("count group item: %v", err)
	}
	if count != 1 {
		t.Fatal("multiplier policy deleted the group item")
	}

	requirements := model.ProtocolRouteRequirements{InboundProtocol: model.ProtocolOpenAIChat}
	planned, preview, _, err := CatalogPlanGroup(ctx, group.Name, requirements, group)
	if err != nil {
		t.Fatalf("plan group: %v", err)
	}
	if len(planned.Items) != 0 {
		t.Fatalf("over-cap item entered route plan: %+v", planned.Items)
	}
	if len(preview.Decisions) != 1 || preview.Decisions[0].PolicyStatus != MultiplierPolicyStatusBlocked {
		t.Fatalf("route preview did not explain policy block: %+v", preview.Decisions)
	}

	var stored model.SiteUserGroup
	if err := dbpkg.GetDB().WithContext(ctx).First(&stored, groupOwner.ID).Error; err != nil {
		t.Fatalf("reload site group: %v", err)
	}
	if !stored.PolicyBlocked || stored.ProjectionSuspended {
		t.Fatalf("policy state was not isolated from sync suspension: %+v", stored)
	}

	if err := SettingSetString(model.SettingKeyDefaultMultiplierCap, "6"); err != nil {
		t.Fatalf("raise multiplier cap: %v", err)
	}
	if _, _, err := EnforceMultiplierCap(ctx); err != nil {
		t.Fatalf("recover multiplier policy: %v", err)
	}
	recoveredGroup, err := GroupGetEnabledMap(group.Name, ctx)
	if err != nil {
		t.Fatalf("load recovered group: %v", err)
	}
	planned, _, _, err = CatalogPlanGroup(ctx, group.Name, requirements, recoveredGroup)
	if err != nil {
		t.Fatalf("plan recovered group: %v", err)
	}
	if len(planned.Items) != 1 {
		t.Fatalf("item did not recover after cap increase: %+v", planned.Items)
	}
}

func TestConfiguredMultiplierCapTreatsZeroAsUnlimited(t *testing.T) {
	settingCache.Set(model.SettingKeyDefaultMultiplierCap, "0")
	t.Cleanup(func() { settingCache.Del(model.SettingKeyDefaultMultiplierCap) })
	if _, enabled := configuredMultiplierCap(); enabled {
		t.Fatal("zero cap must disable the limit")
	}
}
