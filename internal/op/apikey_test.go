package op

import (
	"context"
	"math"
	"path/filepath"
	"sync"
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func setupAPIKeyOpTestDB(t *testing.T) context.Context {
	t.Helper()
	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}
	apiKeyCache.Clear()
	apiKeyIDMap.Clear()

	dbPath := filepath.Join(t.TempDir(), "octopus-apikey-test.db")
	if err := dbpkg.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	clearHeaderPolicyCache()
	t.Cleanup(func() {
		apiKeyCache.Clear()
		apiKeyIDMap.Clear()
		_ = dbpkg.Close()
	})
	return context.Background()
}

func TestComputeNextQuotaResetUsesLocalMidnight(t *testing.T) {
	location := time.FixedZone("UTC+8", 8*60*60)
	now := time.Date(2026, time.August, 6, 23, 30, 0, 0, location)
	tests := map[string]time.Time{
		"daily":   time.Date(2026, time.August, 7, 0, 0, 0, 0, location),
		"weekly":  time.Date(2026, time.August, 10, 0, 0, 0, 0, location),
		"monthly": time.Date(2026, time.September, 1, 0, 0, 0, 0, location),
	}
	for period, want := range tests {
		t.Run(period, func(t *testing.T) {
			if got := computeNextQuotaReset(period, now); got != want.Unix() {
				t.Fatalf("computeNextQuotaReset(%q) = %s, want %s", period, time.Unix(got, 0).In(location), want)
			}
		})
	}
}

func TestAPIKeyCreateAndUpdateManageQuotaState(t *testing.T) {
	ctx := setupAPIKeyOpTestDB(t)
	key := model.APIKey{
		Name:        "quota-key",
		APIKey:      "sk-octopus-quota-test",
		Enabled:     true,
		QuotaLimit:  10,
		QuotaPeriod: "daily",
		QuotaUsed:   99,
	}
	if err := APIKeyCreate(&key, ctx); err != nil {
		t.Fatalf("APIKeyCreate failed: %v", err)
	}
	if key.QuotaUsed != 0 || key.QuotaResetAt == 0 {
		t.Fatalf("create quota state = used %v reset %d", key.QuotaUsed, key.QuotaResetAt)
	}

	if err := APIKeyIncrementQuotaUsed(ctx, key.ID, 2.5); err != nil {
		t.Fatalf("APIKeyIncrementQuotaUsed failed: %v", err)
	}
	updated := key
	updated.Name = "quota-key-updated"
	updated.QuotaUsed = 1000
	if err := APIKeyUpdate(&updated, ctx); err != nil {
		t.Fatalf("APIKeyUpdate failed: %v", err)
	}
	if math.Abs(updated.QuotaUsed-2.5) > 1e-9 {
		t.Fatalf("same-period update overwrote usage: got %v", updated.QuotaUsed)
	}

	updated.QuotaPeriod = "weekly"
	if err := APIKeyUpdate(&updated, ctx); err != nil {
		t.Fatalf("APIKeyUpdate period change failed: %v", err)
	}
	if updated.QuotaUsed != 0 || updated.QuotaResetAt == 0 {
		t.Fatalf("period change did not reset quota: used %v reset %d", updated.QuotaUsed, updated.QuotaResetAt)
	}
}

func TestAPIKeyIncrementQuotaUsedIsConcurrentSafe(t *testing.T) {
	ctx := setupAPIKeyOpTestDB(t)
	key := model.APIKey{Name: "concurrent", APIKey: "sk-octopus-concurrent", Enabled: true, QuotaLimit: 10}
	if err := APIKeyCreate(&key, ctx); err != nil {
		t.Fatalf("APIKeyCreate failed: %v", err)
	}

	const workers = 50
	var wait sync.WaitGroup
	errors := make(chan error, workers)
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errors <- APIKeyIncrementQuotaUsed(ctx, key.ID, 0.1)
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent increment failed: %v", err)
		}
	}

	cached, err := APIKeyGet(key.ID, ctx)
	if err != nil {
		t.Fatalf("APIKeyGet failed: %v", err)
	}
	if math.Abs(cached.QuotaUsed-5) > 1e-9 {
		t.Fatalf("cached quota_used = %v, want 5", cached.QuotaUsed)
	}
	var persisted model.APIKey
	if err := dbpkg.GetDB().WithContext(ctx).First(&persisted, key.ID).Error; err != nil {
		t.Fatalf("reload API key failed: %v", err)
	}
	if math.Abs(persisted.QuotaUsed-5) > 1e-9 {
		t.Fatalf("persisted quota_used = %v, want 5", persisted.QuotaUsed)
	}
}

func TestAPIKeyDeleteRemovesLookupMapping(t *testing.T) {
	ctx := setupAPIKeyOpTestDB(t)
	key := model.APIKey{Name: "delete", APIKey: "sk-octopus-delete", Enabled: true}
	if err := APIKeyCreate(&key, ctx); err != nil {
		t.Fatalf("APIKeyCreate failed: %v", err)
	}
	if err := APIKeyDelete(key.ID, ctx); err != nil {
		t.Fatalf("APIKeyDelete failed: %v", err)
	}
	if _, err := APIKeyGetByAPIKey(key.APIKey, ctx); err == nil {
		t.Fatal("deleted API key remains available through API-key lookup")
	}
}
