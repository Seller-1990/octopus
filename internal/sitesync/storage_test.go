package sitesync

import (
	"context"
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

func TestSiteMaskedTokenMatchesIgnoresOptionalSKPrefix(t *testing.T) {
	tests := []struct {
		name      string
		fullToken string
		masked    string
	}{
		{name: "full has sk prefix", fullToken: "sk-yzFyREALREALOTkb", masked: "yzFy**********OTkb"},
		{name: "masked has sk prefix", fullToken: "yzFyREALREALOTkb", masked: "sk-yzFy**********OTkb"},
		{name: "both have sk prefix", fullToken: "sk-yzFyREALREALOTkb", masked: "sk-yzFy**********OTkb"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !siteMaskedTokenMatches(tt.fullToken, tt.masked) {
				t.Fatalf("expected %q to match %q", tt.fullToken, tt.masked)
			}
		})
	}
}

func TestApplyPersistedRouteStateGuessesLegacyUnknownRoute(t *testing.T) {
	legacyPayload := model.SiteModelRouteMetadata{
		Source:                 "/api/pricing",
		RouteSupported:         false,
		SupportedEndpointTypes: []string{"/vendor/embeddings"},
		UnsupportedReason:      "site reports endpoint types outside current supported route buckets",
	}.Marshal()
	existing := &model.SiteModel{
		ModelName:       "vendor-embedding-x",
		RouteType:       model.SiteModelRouteTypeUnknown,
		RouteSource:     model.SiteModelRouteSourceSyncInferred,
		RouteRawPayload: legacyPayload,
	}
	item := &model.SiteModel{ModelName: "vendor-embedding-x"}

	applyPersistedRouteState(item, existing, time.Unix(1711929600, 0))

	if item.RouteType != model.SiteModelRouteTypeOpenAIEmbedding {
		t.Fatalf("expected legacy unknown route to be guessed as %q, got %q", model.SiteModelRouteTypeOpenAIEmbedding, item.RouteType)
	}
	metadata, ok := model.ParseSiteModelRouteMetadata(item.RouteRawPayload)
	if !ok {
		t.Fatalf("expected guessed route metadata to parse")
	}
	if !metadata.RouteSupported || !metadata.RouteGuessed {
		t.Fatalf("expected guessed route metadata to mark supported name guess, got %+v", metadata)
	}
	if metadata.RouteType != model.SiteModelRouteTypeOpenAIEmbedding {
		t.Fatalf("expected guessed metadata route type %q, got %q", model.SiteModelRouteTypeOpenAIEmbedding, metadata.RouteType)
	}
}

func TestApplyPersistedRouteStateKeepsManualOverrideUntouched(t *testing.T) {
	existing := &model.SiteModel{
		ModelName:      "vendor-embedding-x",
		RouteType:      model.SiteModelRouteTypeOpenAIChat,
		RouteSource:    model.SiteModelRouteSourceManualOverride,
		ManualOverride: true,
	}
	item := &model.SiteModel{ModelName: "vendor-embedding-x"}

	applyPersistedRouteState(item, existing, time.Unix(1711929600, 0))

	if item.RouteType != model.SiteModelRouteTypeOpenAIChat {
		t.Fatalf("expected manual override route to be preserved, got %q", item.RouteType)
	}
	if !item.ManualOverride {
		t.Fatalf("expected manual override flag to be preserved")
	}
}

func TestMergePersistedSiteTokensPreservesManualFullTokenWhenIncomingIsMasked(t *testing.T) {
	now := time.Unix(1711929600, 0)
	existing := []model.SiteToken{{
		ID:            41,
		SiteAccountID: 9,
		Name:          "primary",
		Token:         "sk-yzFyREALREALOTkb",
		GroupKey:      model.SiteDefaultGroupKey,
		GroupName:     model.SiteDefaultGroupName,
		Enabled:       true,
		ValueStatus:   model.SiteTokenValueStatusReady,
		Source:        "manual",
	}}
	incoming := []model.SiteToken{{
		Name:        "primary",
		Token:       "yzFy**********OTkb",
		GroupKey:    model.SiteDefaultGroupKey,
		GroupName:   model.SiteDefaultGroupName,
		Enabled:     true,
		ValueStatus: model.SiteTokenValueStatusMaskedPending,
		Source:      "sync",
	}}

	merged := mergePersistedSiteTokens(9, existing, incoming, now)
	if len(merged) != 1 {
		t.Fatalf("expected exactly one merged token, got %+v", merged)
	}
	if merged[0].Token != "sk-yzFyREALREALOTkb" {
		t.Fatalf("expected merged token to keep full manual value, got %q", merged[0].Token)
	}
	if merged[0].ValueStatus != model.SiteTokenValueStatusReady {
		t.Fatalf("expected merged token to remain ready, got %q", merged[0].ValueStatus)
	}
	if !merged[0].Enabled {
		t.Fatalf("expected merged token to remain enabled")
	}
}

func TestMergePersistedSiteTokensTreatsOptionalSKPrefixAsSameReadyToken(t *testing.T) {
	now := time.Unix(1711929600, 0)
	existing := []model.SiteToken{{
		ID:            7,
		SiteAccountID: 9,
		Name:          "primary",
		Token:         "sk-abc123",
		GroupKey:      model.SiteDefaultGroupKey,
		GroupName:     model.SiteDefaultGroupName,
		Enabled:       true,
		ValueStatus:   model.SiteTokenValueStatusReady,
		Source:        "manual",
	}}
	incoming := []model.SiteToken{{
		Name:      "primary",
		Token:     "abc123",
		GroupKey:  model.SiteDefaultGroupKey,
		GroupName: model.SiteDefaultGroupName,
		Enabled:   true,
		Source:    "sync",
	}}

	merged := mergePersistedSiteTokens(9, existing, incoming, now)
	if len(merged) != 1 {
		t.Fatalf("expected exactly one merged token, got %+v", merged)
	}
	if merged[0].Token != "sk-abc123" {
		t.Fatalf("expected merged token to preserve stored full token format, got %q", merged[0].Token)
	}
	if merged[0].ValueStatus != model.SiteTokenValueStatusReady {
		t.Fatalf("expected merged token to remain ready, got %q", merged[0].ValueStatus)
	}
}

func TestMergePersistedSiteTokensPreservesLocalDisabledStateForReadyToken(t *testing.T) {
	now := time.Unix(1711929600, 0)
	existing := []model.SiteToken{{
		ID:            8,
		SiteAccountID: 9,
		Name:          "primary",
		Token:         "sk-local-disabled",
		GroupKey:      model.SiteDefaultGroupKey,
		GroupName:     model.SiteDefaultGroupName,
		Enabled:       false,
		ValueStatus:   model.SiteTokenValueStatusReady,
		Source:        "manual",
	}}
	incoming := []model.SiteToken{{
		Name:      "primary",
		Token:     "sk-local-disabled",
		GroupKey:  model.SiteDefaultGroupKey,
		GroupName: model.SiteDefaultGroupName,
		Enabled:   true,
		Source:    "sync",
	}}

	merged := mergePersistedSiteTokens(9, existing, incoming, now)
	if len(merged) != 1 {
		t.Fatalf("expected exactly one merged token, got %+v", merged)
	}
	if merged[0].Enabled {
		t.Fatalf("expected local disabled state to be preserved, got enabled token: %+v", merged[0])
	}
}

func TestMergePersistedSiteTokensPreservesLocalEnabledStateWhenIncomingDisabled(t *testing.T) {
	now := time.Unix(1711929600, 0)
	existing := []model.SiteToken{{
		ID:            9,
		SiteAccountID: 9,
		Name:          "primary",
		Token:         "sk-local-enabled",
		GroupKey:      model.SiteDefaultGroupKey,
		GroupName:     model.SiteDefaultGroupName,
		Enabled:       true,
		ValueStatus:   model.SiteTokenValueStatusReady,
		Source:        "sync",
	}}
	incoming := []model.SiteToken{{
		Name:      "primary",
		Token:     "sk-local-enabled",
		GroupKey:  model.SiteDefaultGroupKey,
		GroupName: model.SiteDefaultGroupName,
		Enabled:   false,
		Source:    "sync",
	}}

	merged := mergePersistedSiteTokens(9, existing, incoming, now)
	if len(merged) != 1 {
		t.Fatalf("expected exactly one merged token, got %+v", merged)
	}
	if !merged[0].Enabled {
		t.Fatalf("expected local enabled state to be preserved, got disabled token: %+v", merged[0])
	}
}

func TestMergePersistedSiteTokensKeepsMaskedPendingWhenMatchIsAmbiguous(t *testing.T) {
	now := time.Unix(1711929600, 0)
	existing := []model.SiteToken{
		{
			ID:            1,
			SiteAccountID: 9,
			Name:          "alpha",
			Token:         "sk-yzFyONEOTkb",
			GroupKey:      model.SiteDefaultGroupKey,
			GroupName:     model.SiteDefaultGroupName,
			Enabled:       true,
			ValueStatus:   model.SiteTokenValueStatusReady,
			Source:        "manual",
		},
		{
			ID:            2,
			SiteAccountID: 9,
			Name:          "beta",
			Token:         "sk-yzFyTWOOTkb",
			GroupKey:      model.SiteDefaultGroupKey,
			GroupName:     model.SiteDefaultGroupName,
			Enabled:       true,
			ValueStatus:   model.SiteTokenValueStatusReady,
			Source:        "manual",
		},
	}
	incoming := []model.SiteToken{{
		Name:        "",
		Token:       "yzFy**********OTkb",
		GroupKey:    model.SiteDefaultGroupKey,
		GroupName:   model.SiteDefaultGroupName,
		Enabled:     true,
		ValueStatus: model.SiteTokenValueStatusMaskedPending,
		Source:      "sync",
	}}

	merged := mergePersistedSiteTokens(9, existing, incoming, now)
	if len(merged) != 3 {
		t.Fatalf("expected masked pending token plus two preserved manual tokens, got %+v", merged)
	}
	maskedCount := 0
	for _, item := range merged {
		if item.Token == "yzFy**********OTkb" {
			maskedCount++
			if item.ValueStatus != model.SiteTokenValueStatusMaskedPending {
				t.Fatalf("expected ambiguous incoming token to remain masked_pending, got %+v", item)
			}
			if item.Enabled {
				t.Fatalf("expected ambiguous masked_pending token to stay disabled")
			}
		}
	}
	if maskedCount != 1 {
		t.Fatalf("expected exactly one preserved masked_pending token, got %+v", merged)
	}
}

func TestMergePersistedSiteTokensDemotesReadyTokenWhenMaskedPatternMismatches(t *testing.T) {
	now := time.Unix(1711929600, 0)
	existing := []model.SiteToken{{
		ID:            5,
		SiteAccountID: 9,
		Name:          "primary",
		Token:         "sk-different-full-token",
		GroupKey:      model.SiteDefaultGroupKey,
		GroupName:     model.SiteDefaultGroupName,
		Enabled:       true,
		ValueStatus:   model.SiteTokenValueStatusReady,
		Source:        "manual",
	}}
	incoming := []model.SiteToken{{
		Name:        "primary",
		Token:       "yzFy**********OTkb",
		GroupKey:    model.SiteDefaultGroupKey,
		GroupName:   model.SiteDefaultGroupName,
		Enabled:     true,
		ValueStatus: model.SiteTokenValueStatusMaskedPending,
		Source:      "sync",
	}}

	merged := mergePersistedSiteTokens(9, existing, incoming, now)
	if len(merged) != 1 {
		t.Fatalf("expected exactly one merged token, got %+v", merged)
	}
	if merged[0].Token != "yzFy**********OTkb" {
		t.Fatalf("expected stale ready token to be replaced by incoming masked value, got %q", merged[0].Token)
	}
	if merged[0].ValueStatus != model.SiteTokenValueStatusMaskedPending {
		t.Fatalf("expected merged token to be demoted to masked_pending, got %q", merged[0].ValueStatus)
	}
	if merged[0].Enabled {
		t.Fatalf("expected demoted token to be disabled until the user re-fills it")
	}
}

func TestPersistSyncSnapshotPreservesGroupProjectionDisabled(t *testing.T) {
	ctx := setupProjectTestDB(t)
	_, account := createProjectionFixture(t, ctx)

	vipGroup := model.SiteUserGroup{SiteAccountID: account.ID, GroupKey: "vip", Name: "VIP", ProjectionDisabled: true}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&vipGroup).Error; err != nil {
		t.Fatalf("create vip group failed: %v", err)
	}

	snapshot := &syncSnapshot{
		accessToken:        account.AccessToken,
		credentialRevision: account.CredentialRevision,
		groups: []model.SiteUserGroup{
			{GroupKey: "vip", Name: "VIP Renamed"},
		},
		tokens: []model.SiteToken{
			{Name: "vip", Token: "key-vip", GroupKey: "vip", GroupName: "VIP", Enabled: true, Source: "sync"},
		},
		status:  model.SiteExecutionStatusSuccess,
		message: "ok",
	}

	if err := persistSyncSnapshot(ctx, account.ID, snapshot); err != nil {
		t.Fatalf("persistSyncSnapshot returned error: %v", err)
	}

	var reloaded model.SiteUserGroup
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id = ? AND group_key = ?", account.ID, "vip").First(&reloaded).Error; err != nil {
		t.Fatalf("query reloaded group failed: %v", err)
	}
	if !reloaded.ProjectionDisabled {
		t.Fatalf("expected projection_disabled to be preserved")
	}
	if reloaded.Name != "VIP Renamed" {
		t.Fatalf("expected synced group metadata to be updated, got %q", reloaded.Name)
	}
}

func TestPersistSyncSnapshotEncryptsCookieSession(t *testing.T) {
	ctx := setupProjectTestDB(t)
	_, account := createProjectionFixture(t, ctx)
	if err := op.InitCache(); err != nil {
		t.Fatalf("initialize settings cache: %v", err)
	}
	if err := op.SettingSetString(model.SettingKeyJWTSecret, "cookie-session-test-secret"); err != nil {
		t.Fatalf("set jwt secret: %v", err)
	}
	const cookie = "session=cookie-only-session; auth_token=opaque"
	snapshot := &syncSnapshot{
		accessToken:        cookie,
		credentialRevision: account.CredentialRevision,
		persistCredential:  true,
		credentialIsCookie: true,
		status:             model.SiteExecutionStatusSuccess,
		message:            "ok",
	}
	if err := persistSyncSnapshot(ctx, account.ID, snapshot); err != nil {
		t.Fatalf("persistSyncSnapshot: %v", err)
	}

	var reloaded model.SiteAccount
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloaded, account.ID).Error; err != nil {
		t.Fatalf("reload account: %v", err)
	}
	if reloaded.AccessToken == cookie || reloaded.SessionCookieEncrypted == "" {
		t.Fatalf("cookie session was not isolated from access_token: %+v", reloaded)
	}
	plain, err := op.DecryptSecret(reloaded.SessionCookieEncrypted)
	if err != nil {
		t.Fatalf("decrypt persisted cookie: %v", err)
	}
	if plain != cookie || reloaded.CredentialRevision != account.CredentialRevision+1 {
		t.Fatalf("unexpected persisted cookie state: plain=%q revision=%d", plain, reloaded.CredentialRevision)
	}
}

func TestPersistSyncSnapshotDoesNotOverwriteNewerCredentials(t *testing.T) {
	ctx := setupProjectTestDB(t)
	_, account := createProjectionFixture(t, ctx)
	if err := op.InitCache(); err != nil {
		t.Fatalf("initialize settings cache: %v", err)
	}
	if err := op.SettingSetString(model.SettingKeyJWTSecret, "snapshot-cas-test-secret"); err != nil {
		t.Fatalf("set jwt secret: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteAccount{}).Where("id = ?", account.ID).Updates(map[string]any{
		"access_token":        "newer-manual-token",
		"credential_revision": 1,
		"last_sync_status":    model.SiteExecutionStatusIdle,
		"last_sync_message":   "newer credentials pending sync",
	}).Error; err != nil {
		t.Fatalf("install newer credentials: %v", err)
	}
	keepGroup := model.SiteUserGroup{SiteAccountID: account.ID, GroupKey: "keep", Name: "Keep"}
	keepToken := model.SiteToken{SiteAccountID: account.ID, Name: "keep", Token: "keep-token", GroupKey: "keep", GroupName: "Keep", Enabled: true}
	keepModel := model.SiteModel{SiteAccountID: account.ID, GroupKey: "keep", ModelName: "keep-model", Source: "sync"}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&keepGroup).Error; err != nil {
		t.Fatalf("create retained group: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&keepToken).Error; err != nil {
		t.Fatalf("create retained token: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&keepModel).Error; err != nil {
		t.Fatalf("create retained model: %v", err)
	}

	snapshot := &syncSnapshot{
		accessToken:        "session=stale-cookie",
		credentialRevision: 0,
		persistCredential:  true,
		groups:             []model.SiteUserGroup{{GroupKey: "stale", Name: "Stale"}},
		tokens:             []model.SiteToken{{Name: "stale", Token: "stale-token", GroupKey: "stale", GroupName: "Stale", Enabled: true}},
		models:             []model.SiteModel{{GroupKey: "stale", ModelName: "stale-model", Source: "sync"}},
		status:             model.SiteExecutionStatusSuccess,
		message:            "stale sync",
	}
	if err := persistSyncSnapshot(ctx, account.ID, snapshot); err == nil {
		t.Fatal("expected stale snapshot to be rejected")
	}

	var reloaded model.SiteAccount
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloaded, account.ID).Error; err != nil {
		t.Fatalf("reload account: %v", err)
	}
	if reloaded.AccessToken != "newer-manual-token" || reloaded.SessionCookieEncrypted != "" || reloaded.CredentialRevision != 1 {
		t.Fatalf("stale snapshot overwrote newer credentials: %+v", reloaded)
	}
	if reloaded.LastSyncStatus != model.SiteExecutionStatusIdle || reloaded.LastSyncMessage != "newer credentials pending sync" {
		t.Fatalf("stale snapshot overwrote sync status: %+v", reloaded)
	}
	var groups []model.SiteUserGroup
	var tokens []model.SiteToken
	var models []model.SiteModel
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id = ?", account.ID).Find(&groups).Error; err != nil {
		t.Fatalf("load retained groups: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id = ?", account.ID).Find(&tokens).Error; err != nil {
		t.Fatalf("load retained tokens: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id = ?", account.ID).Find(&models).Error; err != nil {
		t.Fatalf("load retained models: %v", err)
	}
	containsGroup := func(key string) bool {
		for _, item := range groups {
			if item.GroupKey == key {
				return true
			}
		}
		return false
	}
	containsToken := func(value string) bool {
		for _, item := range tokens {
			if item.Token == value {
				return true
			}
		}
		return false
	}
	containsModel := func(name string) bool {
		for _, item := range models {
			if item.ModelName == name {
				return true
			}
		}
		return false
	}
	if !containsGroup("keep") || containsGroup("stale") ||
		!containsToken("keep-token") || containsToken("stale-token") ||
		!containsModel("keep-model") || containsModel("stale-model") {
		t.Fatalf("stale snapshot replaced projection: groups=%+v tokens=%+v models=%+v", groups, tokens, models)
	}
}

func TestUpdateAccountCheckinStateRejectsStaleCredentialRevision(t *testing.T) {
	ctx := setupProjectTestDB(t)
	_, account := createProjectionFixture(t, ctx)
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteAccount{}).Where("id = ?", account.ID).Updates(map[string]any{
		"access_token":         "newer-manual-token",
		"credential_revision":  account.CredentialRevision + 1,
		"last_checkin_status":  model.SiteExecutionStatusIdle,
		"last_checkin_message": "newer credentials pending checkin",
		"checkin_fail_streak":  0,
		"next_auto_checkin_at": nil,
	}).Error; err != nil {
		t.Fatalf("install newer credentials: %v", err)
	}

	err := updateAccountCheckinState(
		ctx,
		account,
		model.SiteExecutionStatusSuccess,
		"stale checkin",
		true,
		"session=stale-cookie",
	)
	if err == nil {
		t.Fatal("expected stale checkin result to be rejected")
	}

	var reloaded model.SiteAccount
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloaded, account.ID).Error; err != nil {
		t.Fatalf("reload account: %v", err)
	}
	if reloaded.AccessToken != "newer-manual-token" ||
		reloaded.CredentialRevision != account.CredentialRevision+1 ||
		reloaded.LastCheckinStatus != model.SiteExecutionStatusIdle ||
		reloaded.LastCheckinMessage != "newer credentials pending checkin" {
		t.Fatalf("stale checkin overwrote newer account state: %+v", reloaded)
	}
}

func TestUpdateAccountSyncStateRejectsStaleCredentialRevision(t *testing.T) {
	ctx := setupProjectTestDB(t)
	_, account := createProjectionFixture(t, ctx)
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteAccount{}).Where("id = ?", account.ID).Updates(map[string]any{
		"credential_revision": account.CredentialRevision + 1,
		"last_sync_status":    model.SiteExecutionStatusIdle,
		"last_sync_message":   "newer credentials pending sync",
	}).Error; err != nil {
		t.Fatalf("install newer credentials: %v", err)
	}

	err := updateAccountSyncState(
		ctx,
		account.ID,
		account.CredentialRevision,
		model.SiteExecutionStatusFailed,
		"stale sync failure",
	)
	if err == nil {
		t.Fatal("expected stale sync state to be rejected")
	}

	var reloaded model.SiteAccount
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloaded, account.ID).Error; err != nil {
		t.Fatalf("reload account: %v", err)
	}
	if reloaded.CredentialRevision != account.CredentialRevision+1 ||
		reloaded.LastSyncStatus != model.SiteExecutionStatusIdle ||
		reloaded.LastSyncMessage != "newer credentials pending sync" {
		t.Fatalf("stale sync state overwrote newer account state: %+v", reloaded)
	}
}

func TestPersistSyncSnapshotPreservesGroupMultiplier(t *testing.T) {
	ctx := setupProjectTestDB(t)
	_, account := createProjectionFixture(t, ctx)
	multiplier := 0.3
	group := model.SiteUserGroup{
		SiteAccountID: account.ID, GroupKey: "vip", Name: "VIP", Multiplier: &multiplier,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&group).Error; err != nil {
		t.Fatalf("create vip group: %v", err)
	}

	snapshot := &syncSnapshot{
		accessToken:        account.AccessToken,
		credentialRevision: account.CredentialRevision,
		groups:             []model.SiteUserGroup{{GroupKey: "vip", Name: "VIP Renamed"}},
		tokens: []model.SiteToken{
			{Name: "vip", Token: "key-vip", GroupKey: "vip", GroupName: "VIP", Enabled: true, Source: "sync"},
		},
		status:  model.SiteExecutionStatusSuccess,
		message: "ok",
	}
	if err := persistSyncSnapshot(ctx, account.ID, snapshot); err != nil {
		t.Fatalf("persistSyncSnapshot: %v", err)
	}

	var reloaded model.SiteUserGroup
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("site_account_id = ? AND group_key = ?", account.ID, "vip").
		First(&reloaded).Error; err != nil {
		t.Fatalf("load group: %v", err)
	}
	if reloaded.Multiplier == nil || *reloaded.Multiplier != multiplier {
		t.Fatalf("group multiplier was not preserved: %+v", reloaded)
	}
}

func TestPersistSyncSnapshotReplacesOnlyAuthoritativeGroups(t *testing.T) {
	ctx := setupProjectTestDB(t)
	_, account := createProjectionFixture(t, ctx)

	vipGroup := model.SiteUserGroup{SiteAccountID: account.ID, GroupKey: "vip", Name: "VIP"}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&vipGroup).Error; err != nil {
		t.Fatalf("create vip group failed: %v", err)
	}
	vipToken := model.SiteToken{SiteAccountID: account.ID, Name: "vip", Token: "key-vip", GroupKey: "vip", GroupName: "VIP", Enabled: true}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&vipToken).Error; err != nil {
		t.Fatalf("create vip token failed: %v", err)
	}
	vipModel := model.SiteModel{SiteAccountID: account.ID, GroupKey: "vip", ModelName: "gpt-4o-vip-old", Source: "sync", RouteType: model.SiteModelRouteTypeOpenAIChat, RouteSource: model.SiteModelRouteSourceSyncInferred}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&vipModel).Error; err != nil {
		t.Fatalf("create vip model failed: %v", err)
	}

	snapshot := &syncSnapshot{
		accessToken:        account.AccessToken,
		credentialRevision: account.CredentialRevision,
		groups: []model.SiteUserGroup{
			{GroupKey: model.SiteDefaultGroupKey, Name: model.SiteDefaultGroupName},
			{GroupKey: "vip", Name: "VIP"},
		},
		tokens: []model.SiteToken{
			{Name: "primary", Token: "key-primary-new", GroupKey: model.SiteDefaultGroupKey, GroupName: model.SiteDefaultGroupName, Enabled: true, Source: "sync"},
			{Name: "vip", Token: "key-vip-new", GroupKey: "vip", GroupName: "VIP", Enabled: true, Source: "sync"},
		},
		models: []model.SiteModel{
			{GroupKey: model.SiteDefaultGroupKey, ModelName: "gpt-4.1", Source: "sync", RouteType: model.SiteModelRouteTypeOpenAIChat, RouteSource: model.SiteModelRouteSourceSyncInferred},
		},
		groupResults: []siteGroupSyncResult{
			{GroupKey: model.SiteDefaultGroupKey, GroupName: model.SiteDefaultGroupName, HasKey: true, Status: siteGroupSyncStatusSynced, Authoritative: true, ModelCount: 1, Message: "同步到 1 个模型"},
			{GroupKey: "vip", GroupName: "VIP", HasKey: true, Status: siteGroupSyncStatusFailed, Authoritative: false, Message: "unauthorized"},
		},
		status:  model.SiteExecutionStatusPartial,
		message: "部分分组同步完成：更新 1 个分组，保留 1 个分组的历史投影",
	}

	if err := persistSyncSnapshot(ctx, account.ID, snapshot); err != nil {
		t.Fatalf("persistSyncSnapshot returned error: %v", err)
	}

	var models []model.SiteModel
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id = ?", account.ID).Order("group_key ASC, model_name ASC").Find(&models).Error; err != nil {
		t.Fatalf("query models failed: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected one refreshed default model and one preserved vip model, got %+v", models)
	}
	modelsByGroup := make(map[string][]string)
	for _, item := range models {
		modelsByGroup[item.GroupKey] = append(modelsByGroup[item.GroupKey], item.ModelName)
	}
	if len(modelsByGroup[model.SiteDefaultGroupKey]) != 1 || modelsByGroup[model.SiteDefaultGroupKey][0] != "gpt-4.1" {
		t.Fatalf("expected default group to be fully replaced, got %+v", modelsByGroup)
	}
	if len(modelsByGroup["vip"]) != 1 || modelsByGroup["vip"][0] != "gpt-4o-vip-old" {
		t.Fatalf("expected vip group to keep historical model, got %+v", modelsByGroup)
	}

	reloaded, err := op.SiteAccountGet(account.ID, context.Background())
	if err != nil {
		t.Fatalf("SiteAccountGet failed: %v", err)
	}
	if reloaded.LastSyncStatus != model.SiteExecutionStatusPartial {
		t.Fatalf("expected partial last_sync_status, got %q", reloaded.LastSyncStatus)
	}
	if reloaded.LastSyncMessage != snapshot.message {
		t.Fatalf("expected last_sync_message %q, got %q", snapshot.message, reloaded.LastSyncMessage)
	}

	var vipReloaded model.SiteUserGroup
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id = ? AND group_key = ?", account.ID, "vip").First(&vipReloaded).Error; err != nil {
		t.Fatalf("query vip group failed: %v", err)
	}
	if vipReloaded.ProjectionSuspended {
		t.Fatalf("expected failed vip group projection to keep historical projection active")
	}
	if vipReloaded.ModelSyncStatus != model.SiteGroupModelSyncStatusFailed {
		t.Fatalf("expected failed vip model sync status, got %q", vipReloaded.ModelSyncStatus)
	}
	if vipReloaded.ModelSyncFailureCount != 1 {
		t.Fatalf("expected vip failure count 1, got %d", vipReloaded.ModelSyncFailureCount)
	}
}

func TestPersistSyncSnapshotEmptySuspendsWithoutAdvancingSuccessTime(t *testing.T) {
	ctx := setupProjectTestDB(t)
	_, account := createProjectionFixture(t, ctx)

	previousSuccess := time.Unix(1700000000, 0)
	group := model.SiteUserGroup{
		SiteAccountID:          account.ID,
		GroupKey:               model.SiteDefaultGroupKey,
		Name:                   model.SiteDefaultGroupName,
		ModelSyncStatus:        model.SiteGroupModelSyncStatusSynced,
		LastModelSyncSuccessAt: &previousSuccess,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&group).Error; err != nil {
		t.Fatalf("create group failed: %v", err)
	}

	snapshot := &syncSnapshot{
		accessToken:        account.AccessToken,
		credentialRevision: account.CredentialRevision,
		groups:             []model.SiteUserGroup{{GroupKey: model.SiteDefaultGroupKey, Name: model.SiteDefaultGroupName}},
		tokens:             []model.SiteToken{{Name: "primary", Token: "key-primary", GroupKey: model.SiteDefaultGroupKey, GroupName: model.SiteDefaultGroupName, Enabled: true, Source: "sync"}},
		groupResults: []siteGroupSyncResult{
			{GroupKey: model.SiteDefaultGroupKey, GroupName: model.SiteDefaultGroupName, HasKey: true, Status: siteGroupSyncStatusEmpty, Authoritative: true, Message: "上游当前没有可用模型"},
		},
		status:  model.SiteExecutionStatusSuccess,
		message: "上游当前无可用模型，已清空历史模型",
	}
	if err := persistSyncSnapshot(ctx, account.ID, snapshot); err != nil {
		t.Fatalf("persistSyncSnapshot returned error: %v", err)
	}

	var reloaded model.SiteUserGroup
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id = ? AND group_key = ?", account.ID, model.SiteDefaultGroupKey).First(&reloaded).Error; err != nil {
		t.Fatalf("query reloaded group failed: %v", err)
	}
	if !reloaded.ProjectionSuspended {
		t.Fatalf("expected empty group projection to be suspended")
	}
	if reloaded.LastModelSyncSuccessAt == nil || !reloaded.LastModelSyncSuccessAt.Equal(previousSuccess) {
		t.Fatalf("expected empty sync to preserve last success time %v, got %v", previousSuccess, reloaded.LastModelSyncSuccessAt)
	}
}

// TestPersistSyncSnapshotKeepBlockDemotesKnown 阶段 2 X2 落点的回归锁（阶段 7 收口）：
// 站点停发倍率（快照无 multiplier）时 keep-block 保留旧值 5x、known 显式降 false（修订 12 选 B）。
func TestPersistSyncSnapshotKeepBlockDemotesKnown(t *testing.T) {
	ctx := setupProjectTestDB(t)
	_, account := createProjectionFixture(t, ctx)

	five := 5.0
	knownTrue := true
	oldGroup := model.SiteUserGroup{SiteAccountID: account.ID, GroupKey: "vip", Name: "VIP", Multiplier: &five, MultiplierKnown: &knownTrue}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&oldGroup).Error; err != nil {
		t.Fatalf("create old group failed: %v", err)
	}

	// 快照组无 multiplier（站点停发倍率）→ keep-block 保留数值 5、known 降 false
	snapshot := &syncSnapshot{
		accessToken:        account.AccessToken,
		credentialRevision: account.CredentialRevision,
		groups: []model.SiteUserGroup{
			{GroupKey: "vip", Name: "VIP"},
		},
		tokens: []model.SiteToken{
			{Name: "vip", Token: "key-vip", GroupKey: "vip", GroupName: "VIP", Enabled: true, Source: "sync"},
		},
		status:  model.SiteExecutionStatusSuccess,
		message: "ok",
	}
	if err := persistSyncSnapshot(ctx, account.ID, snapshot); err != nil {
		t.Fatalf("persistSyncSnapshot returned error: %v", err)
	}

	var reloaded model.SiteUserGroup
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id = ? AND group_key = ?", account.ID, "vip").First(&reloaded).Error; err != nil {
		t.Fatalf("query reloaded group failed: %v", err)
	}
	if reloaded.Multiplier == nil || *reloaded.Multiplier != 5 {
		t.Fatalf("expected multiplier 5 retained, got %+v", reloaded.Multiplier)
	}
	if reloaded.MultiplierKnown == nil || *reloaded.MultiplierKnown {
		t.Fatalf("expected multiplier_known demoted to false, got %+v", reloaded.MultiplierKnown)
	}
}
