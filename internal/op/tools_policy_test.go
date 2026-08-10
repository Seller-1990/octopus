package op

import (
	"context"
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

// createToolsFixture 创建渠道+分组+条目，返回 (channelID, itemID)。
func createToolsFixture(t *testing.T, ctx context.Context) (int, int) {
	t.Helper()
	return createToolsFixtureWithModel(t, ctx, "write-model")
}

// createToolsFixtureWithModel 创建渠道+分组+条目，返回 (channelID, itemID)。
// modelName 可自定义——用于隔离 registry key 的测试（全局 toolsProbeCounts 跨测试共享，
// 固定 "write-model" 会让计数在测试间串扰，架空「≥2 需两次」断言）。
func createToolsFixtureWithModel(t *testing.T, ctx context.Context, modelName string) (int, int) {
	t.Helper()
	ch := model.Channel{Name: "write-policy-channel", Model: modelName, Enabled: true}
	if err := ChannelCreate(&ch, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	group := model.Group{Name: "write-policy-group", Mode: model.GroupModeFailover}
	if err := GroupCreate(&group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	item := model.GroupItem{GroupID: group.ID, ChannelID: ch.ID, ModelName: modelName, Priority: 1, Weight: 1}
	if err := GroupItemAdd(&item, ctx); err != nil {
		t.Fatalf("create group item: %v", err)
	}
	return ch.ID, item.ID
}

func loadToolsRow(t *testing.T, ctx context.Context, itemID int) model.GroupItem {
	t.Helper()
	var row model.GroupItem
	if err := dbpkg.GetDB().WithContext(ctx).First(&row, itemID).Error; err != nil {
		t.Fatalf("reload group item: %v", err)
	}
	return row
}

// TestApplyToolsProbeResultEvidenceHierarchy 写覆盖按证据层级（v3.1 R2 拍板）：
// manual-force > executed > t9 = unsupported > accepted/required_unsupported。
func TestApplyToolsProbeResultEvidenceHierarchy(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	chID, itemID := createToolsFixture(t, ctx)

	// 1) accepted（弱 true）不覆盖 t9 false
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.GroupItem{}).Where("id = ?", itemID).
		Updates(map[string]any{"supports_tools": false, "supports_tools_source": "t9"}).Error; err != nil {
		t.Fatalf("seed t9 false: %v", err)
	}
	if err := ApplyToolsProbeResult(chID, "write-model", model.ToolsProbeResult{State: model.ToolsProbeStateAccepted, Supports: true, Source: "probe"}, nil); err != nil {
		t.Fatalf("apply accepted: %v", err)
	}
	if row := loadToolsRow(t, ctx, itemID); row.SupportsTools == nil || *row.SupportsTools || row.SupportsToolsSource != "t9" {
		t.Fatalf("accepted must NOT override t9 false, got supports=%v source=%s", row.SupportsTools, row.SupportsToolsSource)
	}

	// 2) required_unsupported（弱 true）同样不覆盖 false
	if err := ApplyToolsProbeResult(chID, "write-model", model.ToolsProbeResult{State: model.ToolsProbeStateRequiredUnsupported, Supports: true, Source: "manual-required-fallback"}, nil); err != nil {
		t.Fatalf("apply required_unsupported: %v", err)
	}
	if row := loadToolsRow(t, ctx, itemID); row.SupportsTools == nil || *row.SupportsTools {
		t.Fatalf("required_unsupported must NOT override t9 false, got supports=%v", row.SupportsTools)
	}

	// 3) executed（强 true）覆盖 t9 false
	if err := ApplyToolsProbeResult(chID, "write-model", model.ToolsProbeResult{State: model.ToolsProbeStateExecuted, Supports: true, Source: "manual"}, nil); err != nil {
		t.Fatalf("apply executed: %v", err)
	}
	if row := loadToolsRow(t, ctx, itemID); row.SupportsTools == nil || !*row.SupportsTools {
		t.Fatalf("executed must override t9 false, got supports=%v", row.SupportsTools)
	}

	// 4) ForceToolsUnsupported 写 false/manual-force
	if err := ForceToolsUnsupported(chID, "write-model"); err != nil {
		t.Fatalf("force unsupported: %v", err)
	}
	row := loadToolsRow(t, ctx, itemID)
	if row.SupportsTools == nil || *row.SupportsTools || row.SupportsToolsSource != "manual-force" {
		t.Fatalf("force must write false/manual-force, got supports=%v source=%s", row.SupportsTools, row.SupportsToolsSource)
	}

	// 5) executed 不覆盖 manual-force（最高级）
	if err := ApplyToolsProbeResult(chID, "write-model", model.ToolsProbeResult{State: model.ToolsProbeStateExecuted, Supports: true, Source: "manual"}, nil); err != nil {
		t.Fatalf("apply executed over force: %v", err)
	}
	row = loadToolsRow(t, ctx, itemID)
	if row.SupportsTools == nil || *row.SupportsTools {
		t.Fatalf("executed must NOT override manual-force, got supports=%v", row.SupportsTools)
	}
	if row.SupportsToolsSource != "manual-force" {
		t.Fatalf("manual-force source must persist, got %s", row.SupportsToolsSource)
	}

	// 6) ResetToolsState 写 nil/u7（解除强制，回到待重探）
	if err := ResetToolsState(chID, "write-model"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	row = loadToolsRow(t, ctx, itemID)
	if row.SupportsTools != nil || row.SupportsToolsSource != "u7" {
		t.Fatalf("reset must write nil/u7, got supports=%v source=%s", row.SupportsTools, row.SupportsToolsSource)
	}

	// 7) 重置后 unsupported（≥2 确认 false）可写 false
	if err := ApplyToolsProbeResult(chID, "write-model", model.ToolsProbeResult{State: model.ToolsProbeStateUnsupported, Source: "probe"}, nil); err != nil {
		t.Fatalf("apply unsupported: %v", err)
	}
	row = loadToolsRow(t, ctx, itemID)
	if row.SupportsTools == nil || *row.SupportsTools {
		t.Fatalf("unsupported must write false after reset, got supports=%v", row.SupportsTools)
	}
}

// TestApplyToolsProbeResultNonDecidingStatesDontWrite pending/required_ignored/unknown 不写列（R14）。
func TestApplyToolsProbeResultNonDecidingStatesDontWrite(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	chID, itemID := createToolsFixture(t, ctx)

	for _, state := range []model.ToolsProbeState{
		model.ToolsProbeStatePending,
		model.ToolsProbeStateRequiredIgnored,
		model.ToolsProbeStateUnknown,
	} {
		if err := ApplyToolsProbeResult(chID, "write-model", model.ToolsProbeResult{State: state}, nil); err != nil {
			t.Fatalf("apply %s: %v", state, err)
		}
	}
	row := loadToolsRow(t, ctx, itemID)
	if row.SupportsTools != nil {
		t.Fatalf("non-deciding states must not write supports_tools, got %v", row.SupportsTools)
	}
	if row.SupportsToolsProbedAt != nil {
		t.Fatalf("non-deciding states must not write probed_at")
	}
}

// TestUnsupportedOverridesExecuted R3 契约（评审修订）：≥2 确认 false（unsupported/T9）可覆盖
// executed 强 true——executed 是一次观测结果，渠道 tools 能力被关闭后系统应自愈；
// 永久保护仅限管理员显式强制的 manual-force。
func TestUnsupportedOverridesExecuted(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	chID, itemID := createToolsFixture(t, ctx)

	// 先 executed（强 true，source=manual）
	if err := ApplyToolsProbeResult(chID, "write-model", model.ToolsProbeResult{State: model.ToolsProbeStateExecuted, Supports: true, Source: "manual"}, nil); err != nil {
		t.Fatalf("apply executed: %v", err)
	}
	// unsupported（≥2 探测确认 false）可覆盖 executed（R3）
	if err := ApplyToolsProbeResult(chID, "write-model", model.ToolsProbeResult{State: model.ToolsProbeStateUnsupported, Source: "probe"}, nil); err != nil {
		t.Fatalf("apply unsupported: %v", err)
	}
	row := loadToolsRow(t, ctx, itemID)
	if row.SupportsTools == nil || *row.SupportsTools {
		t.Fatalf("unsupported must override executed (R3), got supports=%v source=%s", row.SupportsTools, row.SupportsToolsSource)
	}

	// 重置后重新 executed，验证 T9（≥2 真实失败）同样可覆盖
	if err := ApplyToolsProbeResult(chID, "write-model", model.ToolsProbeResult{State: model.ToolsProbeStateExecuted, Supports: true, Source: "manual"}, nil); err != nil {
		t.Fatalf("re-apply executed: %v", err)
	}
	ReportToolsUnsupported(chID, "write-model", "upstream error: 400: tools not supported", 400)
	ReportToolsUnsupported(chID, "write-model", "upstream error: 400: tools not supported", 400)
	row = loadToolsRow(t, ctx, itemID)
	if row.SupportsTools == nil || *row.SupportsTools {
		t.Fatalf("T9 must override executed (R3), got supports=%v source=%s", row.SupportsTools, row.SupportsToolsSource)
	}
}

// TestUnsupportedDoesNotOverrideManualForce P0 回归（守卫极性）：unsupported/T9 不得覆盖管理员强制标不支持。
func TestUnsupportedDoesNotOverrideManualForce(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	chID, itemID := createToolsFixture(t, ctx)

	if err := ForceToolsUnsupported(chID, "write-model"); err != nil {
		t.Fatalf("force unsupported: %v", err)
	}
	// unsupported 分支：不得把 manual-force 降级
	if err := ApplyToolsProbeResult(chID, "write-model", model.ToolsProbeResult{State: model.ToolsProbeStateUnsupported, Source: "probe"}, nil); err != nil {
		t.Fatalf("apply unsupported: %v", err)
	}
	row := loadToolsRow(t, ctx, itemID)
	if row.SupportsToolsSource != "manual-force" {
		t.Fatalf("unsupported must NOT degrade manual-force, got source=%s", row.SupportsToolsSource)
	}
	if row.SupportsTools == nil || *row.SupportsTools {
		t.Fatalf("manual-force false must persist, got supports=%v", row.SupportsTools)
	}

	// T9 分支同样不得降级
	ReportToolsUnsupported(chID, "write-model", "upstream error: 400: tools not supported", 400)
	ReportToolsUnsupported(chID, "write-model", "upstream error: 400: tools not supported", 400)
	row = loadToolsRow(t, ctx, itemID)
	if row.SupportsToolsSource != "manual-force" {
		t.Fatalf("T9 must NOT degrade manual-force, got source=%s", row.SupportsToolsSource)
	}
}

// TestAcceptedOverridesExecutedSource R3 契约：accepted（弱 true）可覆盖 executed 的 source=manual
// （R3 后 manual 不再受永久保护，血缘降级无实际后果——unsupported 本就可覆盖 manual 行）。
// 唯一永久保护是 manual-force。
func TestAcceptedOverridesExecutedSource(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	chID, itemID := createToolsFixture(t, ctx)

	// 1) executed 写 true+manual（强证据）
	if err := ApplyToolsProbeResult(chID, "write-model", model.ToolsProbeResult{State: model.ToolsProbeStateExecuted, Supports: true, Source: "manual"}, nil); err != nil {
		t.Fatalf("apply executed: %v", err)
	}
	// 2) accepted（auto 2xx 弱 true）命中 true 行——值保持 true，source 变 probe（R3 允许）
	if err := ApplyToolsProbeResult(chID, "write-model", model.ToolsProbeResult{State: model.ToolsProbeStateAccepted, Supports: true, Source: "probe"}, nil); err != nil {
		t.Fatalf("apply accepted: %v", err)
	}
	row := loadToolsRow(t, ctx, itemID)
	if row.SupportsTools == nil || !*row.SupportsTools {
		t.Fatalf("accepted must keep true value, got supports=%v", row.SupportsTools)
	}
	// 3) 再遇 unsupported（≥2 false）——可覆盖（R3 自愈）
	if err := ApplyToolsProbeResult(chID, "write-model", model.ToolsProbeResult{State: model.ToolsProbeStateUnsupported, Source: "probe"}, nil); err != nil {
		t.Fatalf("apply unsupported: %v", err)
	}
	row = loadToolsRow(t, ctx, itemID)
	if row.SupportsTools == nil || *row.SupportsTools {
		t.Fatalf("unsupported must override after accepted chain (R3), got supports=%v source=%s", row.SupportsTools, row.SupportsToolsSource)
	}
}

// TestAcceptedPreservesManualForceSource 复审 P0 附带：accepted 不抹 manual-force 标记。
func TestAcceptedPreservesManualForceSource(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	chID, itemID := createToolsFixture(t, ctx)
	if err := ForceToolsUnsupported(chID, "write-model"); err != nil {
		t.Fatalf("force: %v", err)
	}
	// accepted 对 false 行本就不命中（WHERE nil OR true），此处验证 source 保持
	if err := ApplyToolsProbeResult(chID, "write-model", model.ToolsProbeResult{State: model.ToolsProbeStateAccepted, Supports: true, Source: "probe"}, nil); err != nil {
		t.Fatalf("apply accepted: %v", err)
	}
	row := loadToolsRow(t, ctx, itemID)
	if row.SupportsToolsSource != "manual-force" || row.SupportsTools == nil || *row.SupportsTools {
		t.Fatalf("manual-force must persist after accepted, got supports=%v source=%s", row.SupportsTools, row.SupportsToolsSource)
	}
}

// TestToolsProbeRegistryCrossPathAccumulates P0 回归：probe 侧（带 upstream 前缀）与 T9 侧（裸 body）
// 命中同一错误类别可跨路径累计到 ≥2（原全文 hash 永不累计）。
func TestToolsProbeRegistryCrossPathAccumulates(t *testing.T) {
	// 第 1 次：probe 侧文本（带前缀）→ count=1
	if ConfirmToolsUnsupportedOnce(4242, "cross-model", "upstream error: 400: tools not supported by this model") {
		t.Fatalf("first hit must not confirm")
	}
	// 第 2 次：T9 侧裸 body（含动态尾巴，仍命中 "tools not supported"）→ 累计到 2
	if !ConfirmToolsUnsupportedOnce(4242, "cross-model", `{"error":{"message":"tools not supported by this model, trace_id=abc123"}}`) {
		t.Fatalf("cross-path hit must accumulate to 2 (category-keyed)")
	}
	// 不同类别不累计
	if ConfirmToolsUnsupportedOnce(4243, "cross-model", "function calling not supported") {
		t.Fatalf("different category must not accumulate across channels")
	}
}

// TestToolsProbeRegistryCategoryAccumulates 复审 P1：同类别不同措辞可跨路径累计到 ≥2
// （`tools not supported` 与 `does not support tools` 同属 tools_param_rejected 类别）。
func TestToolsProbeRegistryCategoryAccumulates(t *testing.T) {
	if ConfirmToolsUnsupportedOnce(5252, "cat-model", "upstream error: 400: tools not supported") {
		t.Fatalf("first hit must not confirm")
	}
	if !ConfirmToolsUnsupportedOnce(5252, "cat-model", `{"error":{"message":"this model does not support tools, trace_id=xyz"}}`) {
		t.Fatalf("same-category different wording must accumulate to 2")
	}
	// 不同类别不累计
	if ConfirmToolsUnsupportedOnce(5253, "cat-model", "function calling is not supported") {
		t.Fatalf("different category must not accumulate across channels")
	}
}

// TestMatchToolsUnsupportedErrorNegationContext 复审 P1：中文否定语境 `不支持 tools 以外的参数` 不得误命中。
func TestMatchToolsUnsupportedErrorNegationContext(t *testing.T) {
	if MatchToolsUnsupportedError("该模型不支持 tools 以外的参数组合") {
		t.Fatalf("negation context must not match: 不支持 tools 以外的参数 means tools IS supported")
	}
	if !MatchToolsUnsupportedError("该模型不支持 tools 参数") {
		t.Fatalf("plain rejection must still match")
	}
	// 引号参数变体（数据对抗者 P1 漏报）
	if !MatchToolsUnsupportedError("The model 'gpt-3.5-turbo' does not support the 'tools' parameter") {
		t.Fatalf("quoted parameter variant must match")
	}
	// tool calling 形态
	if !MatchToolsUnsupportedError("tool calling is not supported") {
		t.Fatalf("tool calling variant must match")
	}
	// 缩约 doesn't
	if !MatchToolsUnsupportedError("this model doesn't support tools") {
		t.Fatalf("contraction variant must match")
	}
}

// TestResetToolsStateClearsFailureRegistry P1 回归：管理员「恢复自动」后旧失败计数必须清空，
// 否则 24h TTL 内残留的 ≥2 会让单次新失败立即重新确认 false（绕过 ≥2 质量门槛）。
func TestResetToolsStateClearsFailureRegistry(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	chID, itemID := createToolsFixture(t, ctx)

	// 预置失败计数到 ≥2（同类别同 key）
	if !ConfirmToolsUnsupportedOnce(chID, "write-model", "upstream error: 400: tools not supported") {
		if !ConfirmToolsUnsupportedOnce(chID, "write-model", "upstream error: 400: tools not supported") {
			t.Fatalf("preseed registry to 2")
		}
	}
	// 管理员重置（写 nil/u7）
	if err := ResetToolsState(chID, "write-model"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	// 重置后单次新失败不得立即确认（registry 已清空）
	if ConfirmToolsUnsupportedOnce(chID, "write-model", "upstream error: 400: tools not supported") {
		t.Fatalf("reset must clear failure registry: single new failure must NOT confirm")
	}
	// 第二次新失败才达到 ≥2（从零重新累计）
	if !ConfirmToolsUnsupportedOnce(chID, "write-model", "upstream error: 400: tools not supported") {
		t.Fatalf("two fresh failures after reset must confirm")
	}
	row := loadToolsRow(t, ctx, itemID)
	if row.SupportsToolsSource != "u7" {
		t.Fatalf("reset row must be nil/u7, got source=%s", row.SupportsToolsSource)
	}
}

// TestNullSourceRowIsUpdatable R1 回归：旧库 AutoMigrate 加列后 supports_tools_source 为 NULL，
// 守卫用 COALESCE 后 NULL 行也能被探测结果更新（原 `NOT IN`/`NOT (...)` 对 NULL 求值 UNKNOWN 不命中）。
func TestNullSourceRowIsUpdatable(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	chID, itemID := createToolsFixture(t, ctx)
	// 模拟旧库历史行：supports_tools_source 为 NULL
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.GroupItem{}).Where("id = ?", itemID).
		Updates(map[string]any{"supports_tools": nil, "supports_tools_source": nil, "supports_tools_probed_at": nil}).Error; err != nil {
		t.Fatalf("seed null source: %v", err)
	}
	// executed 应能写 true（COALESCE 空值安全）
	if err := ApplyToolsProbeResult(chID, "write-model", model.ToolsProbeResult{State: model.ToolsProbeStateExecuted, Supports: true, Source: "manual"}, nil); err != nil {
		t.Fatalf("apply executed: %v", err)
	}
	row := loadToolsRow(t, ctx, itemID)
	if row.SupportsTools == nil || !*row.SupportsTools {
		t.Fatalf("executed must write true on NULL-source row (R1), got supports=%v", row.SupportsTools)
	}
	// 再置 NULL，T9 失败应能写 false
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.GroupItem{}).Where("id = ?", itemID).
		Updates(map[string]any{"supports_tools": nil, "supports_tools_source": nil, "supports_tools_probed_at": nil}).Error; err != nil {
		t.Fatalf("seed null again: %v", err)
	}
	ReportToolsUnsupported(chID, "write-model", "upstream error: 400: tools not supported", 400)
	ReportToolsUnsupported(chID, "write-model", "upstream error: 400: tools not supported", 400)
	row = loadToolsRow(t, ctx, itemID)
	if row.SupportsTools == nil || *row.SupportsTools {
		t.Fatalf("T9 must write false on NULL-source row (R1), got supports=%v", row.SupportsTools)
	}
}

// TestT9Ignores5xx 审计报告 P0-1 回归：T9 真实失败反馈仅 4xx 才判定——
// 5xx 是网关故障，即使 body 回显白名单文本（502 包裹原始错误）也不得累计写 false。
// 实施后审查修正：
//   - 用唯一 modelName 隔离 registry key（"write-model" 与其它测试共享全局 toolsProbeCounts，
//     跨测试计数会架空「4xx 需两次」断言）；
//   - 补「两次 5xx + 一次 400」判别实验（锁「5xx 不累计」而非仅「不写库」）；
//   - 补 429 限流排除（429 body 含白名单子串不得误判）。
func TestT9Ignores5xx(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	chID, itemID := createToolsFixtureWithModel(t, ctx, "t9-5xx-model") // 唯一 key，避免跨测试 registry 污染
	const modelName = "t9-5xx-model"

	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.GroupItem{}).Where("id = ?", itemID).
		Updates(map[string]any{"supports_tools": true, "supports_tools_source": "probe"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	// 1) 两次 5xx + 一次 400：若 5xx 累计过计数，第一次 400 就会翻转——判别「不累计」
	ReportToolsUnsupported(chID, modelName, `{"error":{"message":"does not support tools"}}`, 502)
	ReportToolsUnsupported(chID, modelName, `{"error":{"message":"does not support tools"}}`, 502)
	ReportToolsUnsupported(chID, modelName, `{"error":{"message":"does not support tools"}}`, 400)
	row := loadToolsRow(t, ctx, itemID)
	if row.SupportsTools == nil || !*row.SupportsTools {
		t.Fatalf("5xx must NOT accumulate: single 400 after two 5xx must not flip, got supports=%v", row.SupportsTools)
	}

	// 2) 同文本第二次 400 → 判定 false（两次独立 4xx 才确认）
	ReportToolsUnsupported(chID, modelName, `{"error":{"message":"does not support tools"}}`, 400)
	row = loadToolsRow(t, ctx, itemID)
	if row.SupportsTools == nil || *row.SupportsTools {
		t.Fatalf("T9 4xx twice must flip to false, got supports=%v source=%s", row.SupportsTools, row.SupportsToolsSource)
	}
}

// TestT9Ignores429 实施后审查 P1：429 限流 body 含白名单子串不得误判 false。
func TestT9Ignores429(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	chID, itemID := createToolsFixtureWithModel(t, ctx, "t9-429-model")
	const modelName = "t9-429-model"

	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.GroupItem{}).Where("id = ?", itemID).
		Updates(map[string]any{"supports_tools": true, "supports_tools_source": "probe"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	// 429 两次，body 含白名单子串（限流回显场景）——不得判定
	ReportToolsUnsupported(chID, modelName, `{"error":{"message":"too many requests: tool calls not supported right now"}}`, 429)
	ReportToolsUnsupported(chID, modelName, `{"error":{"message":"too many requests: tool calls not supported right now"}}`, 429)
	row := loadToolsRow(t, ctx, itemID)
	if row.SupportsTools == nil || !*row.SupportsTools {
		t.Fatalf("T9 429 must NOT flip to false, got supports=%v source=%s", row.SupportsTools, row.SupportsToolsSource)
	}
}
