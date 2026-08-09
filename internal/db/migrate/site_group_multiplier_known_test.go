package migrate

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMigrateSiteGroupMultiplierKnownBackfill(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&legacySiteUserGroupMultiplierKnown{}); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}

	// 迁移前插入测试数据（legacy 结构，无 multiplier_known 列）
	// group_key 互不相同——迁移内 AutoMigrate 会建 idx_site_account_group 唯一索引
	rows := []legacySiteUserGroupMultiplierKnown{
		{ID: 1, SiteAccountID: 1, GroupKey: "vip", Multiplier: f64(5), RawPayload: `{"data":{"groups":[{"group_key":"vip","rate_multiplier":5}]}}`},
		{ID: 2, SiteAccountID: 1, GroupKey: "std", Multiplier: f64(1), RawPayload: `{"data":{"groups":[{"group_key":"std"}]}}`},
		{ID: 3, SiteAccountID: 1, GroupKey: "none", Multiplier: nil, RawPayload: `{"data":{}}`},
		{ID: 4, SiteAccountID: 1, GroupKey: "gold", Multiplier: f64(2), RawPayload: `{"data":{"groups":[{"group_key":"gold","rate_multiplier":3}]}}`},
		{ID: 5, SiteAccountID: 2, GroupKey: "vip2", Multiplier: f64(5), RawPayload: `{"data":{"groups":[{"group_key":"other"}]}}`},
		{ID: 6, SiteAccountID: 2, GroupKey: "empty", Multiplier: f64(5), RawPayload: ``},
		{ID: 7, SiteAccountID: 2, GroupKey: "free", Multiplier: f64(0), RawPayload: `{"data":{"groups":[{"group_key":"free","rate_multiplier":0}]}}`},
		{ID: 8, SiteAccountID: 3, GroupKey: "broken", Multiplier: f64(5), RawPayload: `{invalid json`},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed legacy rows: %v", err)
	}

	// 正向控制断言（v2 V5）：证明用例 D 的 false 是「值不匹配」而非「解析失败」——
	// 解析器必须能从 payload 解析出该组倍率 3。
	if v, ok := model.StoredSiteGroupMultiplier(`{"data":{"groups":[{"group_key":"gold","rate_multiplier":3}]}}`, "gold"); !ok || v != 3 {
		t.Fatalf("positive control: StoredSiteGroupMultiplier(gold) = (%v, %v), want (3, true)", v, ok)
	}

	if err := migrateSiteGroupMultiplierKnown(db); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	if !db.Migrator().HasColumn(&model.SiteUserGroup{}, "multiplier_known") {
		t.Fatal("multiplier_known column was not added")
	}

	// 行存活：迁移后行数不变（SQLite recreateTable 路径下也应保数据）
	var count int64
	if err := db.Table("site_user_groups").Count(&count).Error; err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != int64(len(rows)) {
		t.Fatalf("row count changed: got %d want %d", count, len(rows))
	}

	// 逐行断言 multiplier_known
	expected := map[string]bool{
		"vip":    true,  // 真值自证同值（≠1）
		"std":    false, // S1 编造 1x：multiplier==1 一律 false
		"none":   false, // 无倍率
		"gold":   false, // 值不匹配（payload 解析到 3，列 2）
		"vip2":   false, // 真值 + payload 缺该组（跨源形态）
		"empty":  false, // payload 空
		"free":   true,  // 真 0x 保值（0≠1 且自证同值 0）
		"broken": false, // 坏 payload 不 panic、不阻塞，标 false
	}
	var got []model.SiteUserGroup
	if err := db.Select("group_key", "multiplier_known").Find(&got).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	for _, g := range got {
		want, ok := expected[g.GroupKey]
		if !ok {
			t.Fatalf("unexpected group %q", g.GroupKey)
		}
		if g.MultiplierKnown == nil {
			t.Fatalf("group %q multiplier_known is nil, want %v", g.GroupKey, want)
		}
		if *g.MultiplierKnown != want {
			t.Fatalf("group %q multiplier_known = %v, want %v", g.GroupKey, *g.MultiplierKnown, want)
		}
	}

	// 幂等：重复运行只补 NULL 行（本次全表无 NULL），值不变不报错
	if err := migrateSiteGroupMultiplierKnown(db); err != nil {
		t.Fatalf("re-run migration failed: %v", err)
	}
	var after []model.SiteUserGroup
	if err := db.Select("group_key", "multiplier_known").Find(&after).Error; err != nil {
		t.Fatalf("read back after re-run: %v", err)
	}
	for _, g := range after {
		want := expected[g.GroupKey]
		if g.MultiplierKnown == nil || *g.MultiplierKnown != want {
			t.Fatalf("group %q multiplier_known changed after re-run: got %v want %v", g.GroupKey, g.MultiplierKnown, want)
		}
	}
}

type legacySiteUserGroupMultiplierKnown struct {
	ID            int      `gorm:"primaryKey"`
	SiteAccountID int      `gorm:"not null"`
	GroupKey      string   `gorm:"size:128;not null"`
	Multiplier    *float64 `json:"group_multiplier,omitempty"`
	RawPayload    string
}

func (legacySiteUserGroupMultiplierKnown) TableName() string { return "site_user_groups" }

func f64(v float64) *float64 { return &v }
