package sitesync

import (
	"context"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

// setupRewriteDedupeFixture 构造 rewrite 测试环境：
// site + account + 3 个托管 channel（A=openai_chat 目标、B=anthropic、C=openai-response 源）+ group + bindings。
// 用 split=true 的不同 routeType 复合 binding key（避免 (site_account_id, group_key) 唯一约束冲突）。
func setupRewriteDedupeFixture(t *testing.T, ctx context.Context) (*model.Site, *model.SiteAccount, int, int) {
	t.Helper()
	site := model.Site{Name: "rewrite-dedupe-site", Platform: model.SitePlatformAPI, BaseURL: "https://rewrite.example", Enabled: true}
	if err := op.SiteCreate(&site, ctx); err != nil {
		t.Fatalf("create site: %v", err)
	}
	account := model.SiteAccount{
		SiteID: site.ID, Name: "rewrite-dedupe-account",
		CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "tok",
		Enabled: true, AutoSync: true,
	}
	if err := op.SiteAccountCreate(&account, ctx); err != nil {
		t.Fatalf("create account: %v", err)
	}
	group := model.Group{Name: "rewrite-group", Mode: model.GroupModeRoundRobin}
	if err := op.GroupCreate(&group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	mkChan := func(name string) model.Channel {
		ch := model.Channel{Name: name, Model: "m1", Enabled: true}
		if err := op.ChannelCreate(&ch, ctx); err != nil {
			t.Fatalf("create channel %s: %v", name, err)
		}
		return ch
	}
	chA := mkChan("rewrite-target-openai") // openai_chat（无后缀 key = "default"）
	chB := mkChan("rewrite-source-anthropic")
	chC := mkChan("rewrite-source-response")
	mkBinding := func(chID int, groupKey string) {
		if err := dbpkg.GetDB().WithContext(ctx).Create(&model.SiteChannelBinding{
			SiteID: site.ID, SiteAccountID: account.ID, GroupKey: groupKey, ChannelID: chID,
		}).Error; err != nil {
			t.Fatalf("create binding %s: %v", groupKey, err)
		}
	}
	mkBinding(chA.ID, model.SiteDefaultGroupKey)
	mkBinding(chB.ID, model.SiteDefaultGroupKey+"::anthropic")
	mkBinding(chC.ID, model.SiteDefaultGroupKey+"::openai-response")
	return &site, &account, group.ID, chA.ID
}

// rewriteCtx 构造 rewrite 调用参数。routeType 决定目标（m1 归 openai_chat → 目标 "default"）。
func rewriteCtx(t *testing.T, ctx context.Context, accountID int, chAID int) (map[string]model.SiteUserGroup, map[string][]model.SiteToken, []model.SiteModel, map[string]int) {
	t.Helper()
	g := model.SiteUserGroup{
		SiteAccountID:   accountID,
		GroupKey:        model.SiteDefaultGroupKey,
		Name:            model.SiteDefaultGroupName,
		ModelSyncStatus: model.SiteGroupModelSyncStatusSynced,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&g).Error; err != nil {
		t.Fatalf("create site user group: %v", err)
	}
	groupMap := map[string]model.SiteUserGroup{model.SiteDefaultGroupKey: g}
	tokens := map[string][]model.SiteToken{
		model.SiteDefaultGroupKey: {{Name: "default", Token: "sk-test", GroupKey: model.SiteDefaultGroupKey, GroupName: model.SiteDefaultGroupName, Enabled: true, IsDefault: true}},
	}
	models := []model.SiteModel{{
		GroupKey:  model.SiteDefaultGroupKey,
		ModelName: "m1",
		RouteType: model.SiteModelRouteTypeOpenAIChat,
	}}
	bindingByKey := map[string]int{model.SiteDefaultGroupKey: chAID}
	return groupMap, tokens, models, bindingByKey
}

// TestRewriteDedupeRemovesDuplicateWhenTargetOccupied 场景 1（残留态）：
// 同 (group, m1) 已存在于目标 openai channel，源 anthropic/response channel 的条目
// 搬家到同一目标时应被删除（目标行已代表该组合），而不是撞唯一索引——failed 账号半提交残留态覆盖。
func TestRewriteDedupeRemovesDuplicateWhenTargetOccupied(t *testing.T) {
	ctx := setupProjectTestDB(t)
	site, account, groupID, chA := setupRewriteDedupeFixture(t, ctx)
	groupMap, tokens, models, bindingByKey := rewriteCtx(t, ctx, account.ID, chA)

	// 目标 A 已有 m1；源 anthropic/response 各一条（残留态：上次同步半提交后未搬成）
	items := []model.GroupItem{{GroupID: groupID, ChannelID: chA, ModelName: "m1", Priority: 1, Weight: 1}}
	var bindings []model.SiteChannelBinding
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id = ?", account.ID).Find(&bindings).Error; err != nil {
		t.Fatalf("list bindings: %v", err)
	}
	for _, b := range bindings {
		if b.ChannelID == chA {
			continue
		}
		items = append(items, model.GroupItem{GroupID: groupID, ChannelID: b.ChannelID, ModelName: "m1", Priority: 2, Weight: 1})
	}
	for i := range items {
		if err := op.GroupItemAdd(&items[i], ctx); err != nil {
			t.Fatalf("create group item %d: %v", i, err)
		}
	}

	err := rewriteManagedGroupItemsForAccount(ctx, site, account, true, groupMap, tokens, models, bindingByKey)
	if err != nil {
		t.Fatalf("rewrite must not fail on duplicate target (residual state): %v", err)
	}
	var count int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.GroupItem{}).
		Where("group_id = ? AND model_name = ?", groupID, "m1").
		Count(&count).Error; err != nil {
		t.Fatalf("count m1 items: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 item for (group,m1) after dedupe, got %d", count)
	}
}

// TestRewriteDedupeSameBatchCollision 场景 2（同批互相撞）：
// 两条源条目（anthropic、response channel）都指向同一 openai 目标，目标无该组合——
// 事务内第一条移入后，第二条应检测到目标已存在而删除，而非撞唯一索引。
func TestRewriteDedupeSameBatchCollision(t *testing.T) {
	ctx := setupProjectTestDB(t)
	site, account, groupID, chA := setupRewriteDedupeFixture(t, ctx)
	groupMap, tokens, models, bindingByKey := rewriteCtx(t, ctx, account.ID, chA)

	var bindings []model.SiteChannelBinding
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id = ?", account.ID).Find(&bindings).Error; err != nil {
		t.Fatalf("list bindings: %v", err)
	}
	for _, b := range bindings {
		if b.ChannelID == chA {
			continue
		}
		item := model.GroupItem{GroupID: groupID, ChannelID: b.ChannelID, ModelName: "m1", Priority: 2, Weight: 1}
		if err := op.GroupItemAdd(&item, ctx); err != nil {
			t.Fatalf("create source item: %v", err)
		}
	}

	err := rewriteManagedGroupItemsForAccount(ctx, site, account, true, groupMap, tokens, models, bindingByKey)
	if err != nil {
		t.Fatalf("rewrite must not fail on same-batch collision: %v", err)
	}
	var count int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.GroupItem{}).
		Where("group_id = ? AND model_name = ?", groupID, "m1").
		Count(&count).Error; err != nil {
		t.Fatalf("count m1 items: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 item after same-batch dedupe, got %d", count)
	}
}
