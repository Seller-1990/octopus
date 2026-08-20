package migrate

import (
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 2026082001,
		Up:      migrateChannelTLSFingerprintColumn,
	})
}

func migrateChannelTLSFingerprintColumn(db *gorm.DB) error {
	if err := db.AutoMigrate(&model.Channel{}); err != nil {
		return err
	}

	return db.Model(&model.Channel{}).
		Where("tls_fingerprint IS NULL").
		Update("tls_fingerprint", "").Error
}
