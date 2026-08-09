package sitesync

import (
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

func TestSuccessfulSyncPreservesIndependentPolicyBlock(t *testing.T) {
	blockedAt := time.Now().Add(-time.Hour)
	existing := &model.SiteUserGroup{PolicyBlocked: true, PolicyBlockReason: "multiplier exceeds cap", PolicyBlockedAt: &blockedAt}
	group := model.SiteUserGroup{}
	applyPersistedGroupSyncState(&group, existing, siteGroupSyncResult{
		GroupKey: "default", Status: siteGroupSyncStatusSynced, Authoritative: true, ModelCount: 1,
	}, time.Now())
	if !group.PolicyBlocked || group.PolicyBlockReason != existing.PolicyBlockReason || group.PolicyBlockedAt == nil {
		t.Fatalf("successful sync cleared policy block: %+v", group)
	}
	if group.ProjectionSuspended {
		t.Fatal("successful sync left sync suspension set")
	}
}
