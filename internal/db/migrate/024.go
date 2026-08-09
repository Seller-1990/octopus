package migrate

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 24,
		Up:      migrateSiteUserGroupPolicyState,
	})
}

func migrateSiteUserGroupPolicyState(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.SiteUserGroup{}) {
		return nil
	}
	return db.AutoMigrate(&model.SiteUserGroup{})
}
