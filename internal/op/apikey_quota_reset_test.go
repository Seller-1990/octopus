package op

import (
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

// 复现 F07:A、B 双请求都看到到期快照时,A 重置后新周期计费,B 再重置不得把新周期用量清零。
func TestAPIKeyResetQuotaIsIdempotentWithinCycle(t *testing.T) {
	ctx := setupAPIKeyOpTestDB(t)
	key := model.APIKey{
		Name:      "reset-idem",
		APIKey:    "sk-octopus-reset-idem",
		Enabled:   true,
		QuotaLimit: 100,
	}
	if err := APIKeyCreate(&key, ctx); err != nil {
		t.Fatalf("create key: %v", err)
	}
	now := time.Now()
	yesterday := now.AddDate(0, 0, -1).Unix()
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.APIKey{}).Where("id = ?", key.ID).
		Update("quota_reset_at", yesterday).Error; err != nil {
		t.Fatalf("expire quota: %v", err)
	}

	// 请求 A:带着过期快照重置,新周期开始。
	currentA, err := APIKeyResetQuota(ctx, key.ID, yesterday, now)
	if err != nil {
		t.Fatalf("reset A: %v", err)
	}
	if currentA.QuotaUsed != 0 {
		t.Fatalf("reset A should clear usage, got %v", currentA.QuotaUsed)
	}
	if err := APIKeyIncrementQuotaUsed(ctx, key.ID, 5); err != nil {
		t.Fatalf("increment: %v", err)
	}

	// 请求 B:仍持 A 之前的过期快照,必须放弃重置,不得清零 A 周期内已计费用。
	currentB, err := APIKeyResetQuota(ctx, key.ID, yesterday, now)
	if err != nil {
		t.Fatalf("reset B: %v", err)
	}
	if currentB.QuotaUsed != 5 {
		t.Fatalf("duplicate reset erased new-cycle usage: QuotaUsed = %v, want 5", currentB.QuotaUsed)
	}
	if currentB.QuotaResetAt != currentA.QuotaResetAt {
		t.Fatalf("duplicate reset must not roll the cycle boundary back: %d != %d", currentB.QuotaResetAt, currentA.QuotaResetAt)
	}
	var persisted model.APIKey
	if err := dbpkg.GetDB().WithContext(ctx).First(&persisted, key.ID).Error; err != nil {
		t.Fatalf("load key: %v", err)
	}
	if persisted.QuotaUsed != 5 {
		t.Fatalf("DB usage erased by duplicate reset: %v, want 5", persisted.QuotaUsed)
	}
}

// 过期快照重置时,周期参数以锁内 DB 当前值为准,不得被调用方旧快照覆盖。
func TestAPIKeyResetQuotaDoesNotOverridePeriodChange(t *testing.T) {
	ctx := setupAPIKeyOpTestDB(t)
	key := model.APIKey{
		Name:      "reset-period",
		APIKey:    "sk-octopus-reset-period",
		Enabled:   true,
		QuotaLimit: 100,
	}
	if err := APIKeyCreate(&key, ctx); err != nil {
		t.Fatalf("create key: %v", err)
	}
	now := time.Now()
	yesterday := now.AddDate(0, 0, -1).Unix()
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.APIKey{}).Where("id = ?", key.ID).
		Updates(map[string]interface{}{"quota_reset_at": yesterday, "quota_period": "monthly"}).Error; err != nil {
		t.Fatalf("expire quota: %v", err)
	}

	// 调用方快照声称周期还是 daily(改周期前取得的),DB 已是 monthly:重置必须沿用 DB 的 monthly。
	current, err := APIKeyResetQuota(ctx, key.ID, yesterday, now)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if current.QuotaPeriod != "monthly" {
		t.Fatalf("stale caller period overwrote DB period: %q, want monthly", current.QuotaPeriod)
	}
	if current.QuotaUsed != 0 {
		t.Fatalf("matched reset should clear usage, got %v", current.QuotaUsed)
	}
	if current.QuotaResetAt <= now.Unix() {
		t.Fatalf("monthly reset boundary should be in the future, got %d", current.QuotaResetAt)
	}
}
