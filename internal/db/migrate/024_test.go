package migrate

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMigrateSiteUserGroupPolicyStateAddsIndependentColumns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&legacySiteUserGroupPolicyState{}); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	if err := migrateSiteUserGroupPolicyState(db); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	if !db.Migrator().HasColumn(&model.SiteUserGroup{}, "policy_blocked") ||
		!db.Migrator().HasColumn(&model.SiteUserGroup{}, "policy_block_reason") ||
		!db.Migrator().HasColumn(&model.SiteUserGroup{}, "policy_blocked_at") {
		t.Fatal("policy state columns were not added")
	}
}

type legacySiteUserGroupPolicyState struct {
	ID            int `gorm:"primaryKey"`
	SiteAccountID int `gorm:"not null"`
	GroupKey      string
}

func (legacySiteUserGroupPolicyState) TableName() string { return "site_user_groups" }
