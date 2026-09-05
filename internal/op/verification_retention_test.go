package op

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func setupVerificationTestDB(t *testing.T) context.Context {
	t.Helper()
	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}
	dbPath := filepath.Join(t.TempDir(), "octopus-verify-retention-test.db")
	if err := dbpkg.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	clearSiteChannelBindingCache()
	clearHeaderPolicyCache()
	t.Cleanup(func() {
		_ = dbpkg.Close()
	})
	return context.Background()
}

// TestVerificationRetentionCleanupDeletesOnlyExpiredPastCutoff 覆盖 N3：
// 只删除 expires_at 超出保留期的会话/任务行；未过期行（无论终态与否）
// 与保留期内的过期行都必须保留。
func TestVerificationRetentionCleanupDeletesOnlyExpiredPastCutoff(t *testing.T) {
	ctx := setupVerificationTestDB(t)
	db := dbpkg.GetDB().WithContext(ctx)
	now := time.Now()

	old := now.AddDate(0, 0, -10)       // 10 天前过期 → 应删除
	fresh := now.AddDate(0, 0, -3)      // 3 天前过期 → 保留期内，保留
	future := now.Add(30 * time.Minute) // 未过期 → 保留

	sessions := []model.VerificationSession{
		{ID: 1, SiteID: 1, Status: model.VerificationSessionExpired, ExpiresAt: old},
		{ID: 2, SiteID: 1, Status: model.VerificationSessionRevoked, ExpiresAt: old},
		{ID: 3, SiteID: 1, Status: model.VerificationSessionExpired, ExpiresAt: fresh},
		{ID: 4, SiteID: 1, Status: model.VerificationSessionPending, ExpiresAt: future},
	}
	tasks := []model.VerificationTask{
		{ID: 1, SessionID: 1, Status: model.VerificationTaskExpired, ExpiresAt: old, TargetURL: "https://a"},
		{ID: 2, SessionID: 2, Status: model.VerificationTaskCanceled, ExpiresAt: old, TargetURL: "https://a"},
		{ID: 3, SessionID: 3, Status: model.VerificationTaskExpired, ExpiresAt: fresh, TargetURL: "https://a"},
		{ID: 4, SessionID: 4, Status: model.VerificationTaskPending, ExpiresAt: future, TargetURL: "https://a"},
	}
	for i := range sessions {
		if err := db.Create(&sessions[i]).Error; err != nil {
			t.Fatalf("create session %d: %v", sessions[i].ID, err)
		}
	}
	for i := range tasks {
		if err := db.Create(&tasks[i]).Error; err != nil {
			t.Fatalf("create task %d: %v", tasks[i].ID, err)
		}
	}

	deleted, err := VerificationRetentionCleanup(ctx, now, 7)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	// 4 行超出 7 天保留期（会话 1/2 + 任务 1/2）；会话 3/4 与任务 3/4 保留
	if deleted != 4 {
		t.Fatalf("deleted = %d, want 4 (2 sessions + 2 tasks past cutoff, others kept)", deleted)
	}

	var sessionIDs, taskIDs []int64
	if err := db.Model(&model.VerificationSession{}).Pluck("id", &sessionIDs).Error; err != nil {
		t.Fatalf("pluck sessions: %v", err)
	}
	if err := db.Model(&model.VerificationTask{}).Pluck("id", &taskIDs).Error; err != nil {
		t.Fatalf("pluck tasks: %v", err)
	}
	wantSessions := []int64{3, 4}
	wantTasks := []int64{3, 4}
	if len(sessionIDs) != len(wantSessions) || sessionIDs[0] != 3 || sessionIDs[1] != 4 {
		t.Fatalf("remaining sessions = %v, want %v", sessionIDs, wantSessions)
	}
	if len(taskIDs) != len(wantTasks) || taskIDs[0] != 3 || taskIDs[1] != 4 {
		t.Fatalf("remaining tasks = %v, want %v", taskIDs, wantTasks)
	}
}
