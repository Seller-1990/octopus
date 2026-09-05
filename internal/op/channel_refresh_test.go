package op

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func setupChannelOpTestDB(t *testing.T) context.Context {
	t.Helper()
	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}
	channelCache.Clear()
	channelKeyCache.Clear()
	channelKeyCacheNeedUpdateLock.Lock()
	channelKeyCacheNeedUpdate = make(map[int]struct{})
	channelKeyCacheNeedUpdateLock.Unlock()
	dbPath := filepath.Join(t.TempDir(), "octopus-channel-test.db")
	if err := dbpkg.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() {
		channelCache.Clear()
		channelKeyCache.Clear()
		_ = dbpkg.Close()
	})
	return context.Background()
}

func createChannelWithKey(t *testing.T, ctx context.Context, name string) (model.Channel, model.ChannelKey) {
	t.Helper()
	channel := model.Channel{
		Name:      name,
		BaseUrls:  []model.BaseUrl{{URL: "https://upstream.example/v1"}},
		Keys:      []model.ChannelKey{{ChannelKey: "sk-test-key", Enabled: true}},
		AutoSync:  false,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&channel).Error; err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := channelRefreshCache(ctx); err != nil {
		t.Fatalf("channelRefreshCache: %v", err)
	}
	if len(channel.Keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(channel.Keys))
	}
	return channel, channel.Keys[0]
}

// 复现 F06:未落库的使用增量在配置刷新后不得被 DB 旧快照覆盖。
func TestChannelRefreshPreservesPendingRecordedCost(t *testing.T) {
	ctx := setupChannelOpTestDB(t)
	channel, key := createChannelWithKey(t, ctx, "cost-preserved")

	if err := ChannelKeyRecordUse(model.ChannelKey{
		ID:               key.ID,
		ChannelID:        channel.ID,
		StatusCode:       200,
		LastUseTimeStamp: time.Now().Unix(),
	}, 5); err != nil {
		t.Fatalf("record use: %v", err)
	}

	renamed := "renamed-channel"
	if _, err := ChannelUpdate(&model.ChannelUpdateRequest{ID: channel.ID, Name: &renamed}, ctx); err != nil {
		t.Fatalf("channel update: %v", err)
	}

	cached, ok := channelKeyCache.Get(key.ID)
	if !ok {
		t.Fatal("key cache miss after refresh")
	}
	if cached.TotalCost != 5 {
		t.Fatalf("refresh erased pending cost: cache TotalCost = %v, want 5", cached.TotalCost)
	}

	if err := ChannelKeySaveDB(ctx); err != nil {
		t.Fatalf("save db: %v", err)
	}
	var persisted model.ChannelKey
	if err := dbpkg.GetDB().WithContext(ctx).First(&persisted, key.ID).Error; err != nil {
		t.Fatalf("load key: %v", err)
	}
	if persisted.TotalCost != 5 {
		t.Fatalf("flush persisted erased cost: DB TotalCost = %v, want 5", persisted.TotalCost)
	}
}

// 刷新查询失败时,旧缓存(含未落库增量)必须原样保留,flush 仍可落库。
func TestChannelRefreshFailureKeepsCachedState(t *testing.T) {
	ctx := setupChannelOpTestDB(t)
	channel, key := createChannelWithKey(t, ctx, "refresh-failure")

	if err := ChannelKeyRecordUse(model.ChannelKey{
		ID:               key.ID,
		ChannelID:        channel.ID,
		StatusCode:       200,
		LastUseTimeStamp: time.Now().Unix(),
	}, 5); err != nil {
		t.Fatalf("record use: %v", err)
	}

	failedCtx, cancel := context.WithCancel(ctx)
	cancel()
	renamed := "renamed-should-fail"
	if _, err := ChannelUpdate(&model.ChannelUpdateRequest{ID: channel.ID, Name: &renamed}, failedCtx); err == nil {
		t.Fatal("channel update with canceled context must fail")
	}

	cached, ok := channelKeyCache.Get(key.ID)
	if !ok {
		t.Fatal("key cache was evicted by failed refresh")
	}
	if cached.TotalCost != 5 {
		t.Fatalf("failed refresh disturbed cache: TotalCost = %v, want 5", cached.TotalCost)
	}

	// 用有效 ctx 完成 flush:失败的刷新不得阻塞后续落库。
	if err := ChannelKeySaveDB(ctx); err != nil {
		t.Fatalf("save db: %v", err)
	}
	var persisted model.ChannelKey
	if err := dbpkg.GetDB().WithContext(ctx).First(&persisted, key.ID).Error; err != nil {
		t.Fatalf("load key: %v", err)
	}
	if persisted.TotalCost != 5 {
		t.Fatalf("flush after failed refresh lost cost: DB TotalCost = %v, want 5", persisted.TotalCost)
	}
}
