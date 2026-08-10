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
	ch := model.Channel{Name: "write-policy-channel", Model: "write-model", Enabled: true}
	if err := ChannelCreate(&ch, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	group := model.Group{Name: "write-policy-group", Mode: model.GroupModeFailover}
	if err := GroupCreate(&group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}
	item := model.GroupItem{GroupID: group.ID, ChannelID: ch.ID, ModelName: "write-model", Priority: 1, Weight: 1}
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

// TestUnsupportedDoesNotOverrideExecuted P0 回归：≥2 确认 false（unsupported/T9）不得覆盖 executed 强 true。
func TestUnsupportedDoesNotOverrideExecuted(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	chID, itemID := createToolsFixture(t, ctx)

	// 先 executed（强 true，source=manual）
	if err := ApplyToolsProbeResult(chID, "write-model", model.ToolsProbeResult{State: model.ToolsProbeStateExecuted, Supports: true, Source: "manual"}, nil); err != nil {
		t.Fatalf("apply executed: %v", err)
	}
	// unsupported（≥2 探测确认 false）不得覆盖 executed
	if err := ApplyToolsProbeResult(chID, "write-model", model.ToolsProbeResult{State: model.ToolsProbeStateUnsupported, Source: "probe"}, nil); err != nil {
		t.Fatalf("apply unsupported: %v", err)
	}
	row := loadToolsRow(t, ctx, itemID)
	if row.SupportsTools == nil || !*row.SupportsTools || row.SupportsToolsSource != "manual" {
		t.Fatalf("unsupported must NOT override executed, got supports=%v source=%s", row.SupportsTools, row.SupportsToolsSource)
	}

	// T9 反馈（≥2 失败）同样不得覆盖 executed
	ReportToolsUnsupported(chID, "write-model", "upstream error: 400: tools not supported")
	ReportToolsUnsupported(chID, "write-model", "upstream error: 400: tools not supported")
	row = loadToolsRow(t, ctx, itemID)
	if row.SupportsTools == nil || !*row.SupportsTools || row.SupportsToolsSource != "manual" {
		t.Fatalf("T9 must NOT override executed, got supports=%v source=%s", row.SupportsTools, row.SupportsToolsSource)
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
	ReportToolsUnsupported(chID, "write-model", "upstream error: 400: tools not supported")
	ReportToolsUnsupported(chID, "write-model", "upstream error: 400: tools not supported")
	row = loadToolsRow(t, ctx, itemID)
	if row.SupportsToolsSource != "manual-force" {
		t.Fatalf("T9 must NOT degrade manual-force, got source=%s", row.SupportsToolsSource)
	}
}

// TestAcceptedDoesNotEraseExecutedSource 复审 P0 回归：accepted（弱 true）不得抹掉 executed 的 source=manual
// 保护标记——否则后续 ≥2 false 可经 source 降级绕过 executed 保护。
func TestAcceptedDoesNotEraseExecutedSource(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	chID, itemID := createToolsFixture(t, ctx)

	// 1) executed 写 true+manual（强证据）
	if err := ApplyToolsProbeResult(chID, "write-model", model.ToolsProbeResult{State: model.ToolsProbeStateExecuted, Supports: true, Source: "manual"}, nil); err != nil {
		t.Fatalf("apply executed: %v", err)
	}
	// 2) accepted（auto 2xx 弱 true）命中 true 行——source 必须保留 manual（CASE WHEN 保护）
	if err := ApplyToolsProbeResult(chID, "write-model", model.ToolsProbeResult{State: model.ToolsProbeStateAccepted, Supports: true, Source: "probe"}, nil); err != nil {
		t.Fatalf("apply accepted: %v", err)
	}
	row := loadToolsRow(t, ctx, itemID)
	if row.SupportsToolsSource != "manual" {
		t.Fatalf("accepted must NOT erase executed source=manual, got %s", row.SupportsToolsSource)
	}
	// 3) 再遇 unsupported（≥2 false）——source 仍是 manual → 保护生效
	if err := ApplyToolsProbeResult(chID, "write-model", model.ToolsProbeResult{State: model.ToolsProbeStateUnsupported, Source: "probe"}, nil); err != nil {
		t.Fatalf("apply unsupported: %v", err)
	}
	row = loadToolsRow(t, ctx, itemID)
	if row.SupportsTools == nil || !*row.SupportsTools || row.SupportsToolsSource != "manual" {
		t.Fatalf("executed must survive accepted→unsupported chain, got supports=%v source=%s", row.SupportsTools, row.SupportsToolsSource)
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
