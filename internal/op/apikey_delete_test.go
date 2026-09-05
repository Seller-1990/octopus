package op

import (
	"strings"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

// TestAPIKeyDeleteMissingRowPreservesStats 覆盖 F15 回归：
// 缓存与 DB 不一致（缓存有 key、DB 行已被外部删除）时，删除必须报
// "not found" 且不得先行清掉该 key 的统计（旧序 StatsAPIKeyDel 在
// DELETE 之前执行，会导致键仍在但统计已丢）。
func TestAPIKeyDeleteMissingRowPreservesStats(t *testing.T) {
	ctx := setupAPIKeyOpTestDB(t)

	key := model.APIKey{ID: 1, Name: "stale-cache", APIKey: "sk-octopus-stale-f15", Enabled: true}
	apiKeyCache.Set(key.ID, key)
	apiKeyIDMap.Set(key.APIKey, key.ID)
	t.Cleanup(func() {
		apiKeyCache.Del(key.ID)
		apiKeyIDMap.Del(key.APIKey)
	})

	statsAPIKeyCache.Set(key.ID, model.StatsAPIKey{APIKeyID: key.ID})
	t.Cleanup(func() {
		statsAPIKeyCache.Del(key.ID)
	})

	err := APIKeyDelete(key.ID, ctx)
	if err == nil {
		t.Fatal("expected error when DB row is missing (stale cache)")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got: %v", err)
	}
	if _, ok := statsAPIKeyCache.Get(key.ID); !ok {
		t.Fatal("stats must survive a failed delete (row missing in DB)")
	}
	if _, ok := apiKeyCache.Get(key.ID); !ok {
		t.Fatal("cache entry should survive when the DB delete affected no rows")
	}
}

// TestAPIKeyDeleteRemovesStatsAfterRow 验证正常删除路径：
// 行删除成功后 stats 与缓存一并清理。
func TestAPIKeyDeleteRemovesStatsAfterRow(t *testing.T) {
	ctx := setupAPIKeyOpTestDB(t)

	key := model.APIKey{ID: 2, Name: "real-key", APIKey: "sk-octopus-real-f15", Enabled: true}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&key).Error; err != nil {
		t.Fatalf("create api key: %v", err)
	}
	apiKeyCache.Set(key.ID, key)
	apiKeyIDMap.Set(key.APIKey, key.ID)
	statsAPIKeyCache.Set(key.ID, model.StatsAPIKey{APIKeyID: key.ID})
	t.Cleanup(func() {
		apiKeyCache.Del(key.ID)
		apiKeyIDMap.Del(key.APIKey)
		statsAPIKeyCache.Del(key.ID)
	})

	if err := APIKeyDelete(key.ID, ctx); err != nil {
		t.Fatalf("APIKeyDelete: %v", err)
	}
	if _, ok := statsAPIKeyCache.Get(key.ID); ok {
		t.Fatal("stats should be removed after successful delete")
	}
	if _, ok := apiKeyCache.Get(key.ID); ok {
		t.Fatal("cache entry should be removed after successful delete")
	}
	var count int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.APIKey{}).Where("id = ?", key.ID).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("DB row still present, count = %d", count)
	}
}
