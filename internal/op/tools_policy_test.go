package op

import (
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func TestMatchToolsUnsupportedError(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"The model does not support function calling", true},
		{"tools is not a supported parameter", true},
		{"unsupported parameter: tools", true},
		{"该模型不支持工具调用", true},
		{"该模型不支持函数调用", true},
		{"upstream error: 400: tools not supported by this model", true},
		{"model not found", false},
		{"rate limit exceeded", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := MatchToolsUnsupportedError(tc.msg); got != tc.want {
			t.Fatalf("MatchToolsUnsupportedError(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}

func TestGroupGetEnabledMapForToolsFiltersFalseItems(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	site := model.Site{Name: "tools-site", Platform: model.SitePlatformNewAPI, BaseURL: "https://tools.example", Enabled: true}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&site).Error; err != nil {
		t.Fatalf("create site: %v", err)
	}
	account := model.SiteAccount{SiteID: site.ID, Name: "tools-account", CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "token", Enabled: true}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}
	mkChannel := func(name string) model.Channel {
		ch := model.Channel{Name: name, Model: "tools-model", Enabled: true}
		if err := ChannelCreate(&ch, ctx); err != nil {
			t.Fatalf("create channel %s: %v", name, err)
		}
		return ch
	}
	chTrue := mkChannel("tools-true-channel")
	chFalse := mkChannel("tools-false-channel")
	chNil := mkChannel("tools-nil-channel")

	group := model.Group{Name: "tools-group", Mode: model.GroupModeFailover}
	if err := GroupCreate(&group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	items := []model.GroupItem{
		{GroupID: group.ID, ChannelID: chTrue.ID, ModelName: "tools-model", Priority: 1, Weight: 1, SupportsTools: toolsBoolPtr(true)},
		{GroupID: group.ID, ChannelID: chFalse.ID, ModelName: "tools-model", Priority: 2, Weight: 1, SupportsTools: toolsBoolPtr(false)},
		{GroupID: group.ID, ChannelID: chNil.ID, ModelName: "tools-model", Priority: 3, Weight: 1}, // nil 未探测
	}
	for i := range items {
		if err := dbpkg.GetDB().WithContext(ctx).Create(&items[i]).Error; err != nil {
			t.Fatalf("create item %d: %v", i, err)
		}
	}
	if err := groupRefreshCacheByID(group.ID, ctx); err != nil {
		t.Fatalf("refresh cache: %v", err)
	}

	// 未勾选：不过滤
	g, err := GroupGetEnabledMapForTools("tools-group", ctx, false)
	if err != nil {
		t.Fatalf("GroupGetEnabledMapForTools(false): %v", err)
	}
	if len(g.Items) != 3 {
		t.Fatalf("toolsOnly=false should keep 3 items, got %d", len(g.Items))
	}

	// 勾选：跳过 supports_tools=false，保留 true + nil
	g, err = GroupGetEnabledMapForTools("tools-group", ctx, true)
	if err != nil {
		t.Fatalf("GroupGetEnabledMapForTools(true): %v", err)
	}
	if len(g.Items) != 2 {
		t.Fatalf("toolsOnly=true should keep 2 items (true+nil), got %d", len(g.Items))
	}
	for _, item := range g.Items {
		if item.SupportsTools != nil && !*item.SupportsTools {
			t.Fatalf("toolsOnly=true leaked supports_tools=false item (channel=%d)", item.ChannelID)
		}
	}
}

func toolsBoolPtr(v bool) *bool { return &v }
