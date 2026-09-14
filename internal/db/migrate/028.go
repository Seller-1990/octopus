package migrate

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 2026091401, // 必须 > 2026082001（027.go）
		Up:      migrateSiteModelPriceQuotePollutedMultiplier,
	})
}

// migrateSiteModelPriceQuotePollutedMultiplier 一次性修复 F01 的存量污染行。
//
// 背景：F01 修复（2026-09-13）前的 SiteModelPriceManualUpsert 无条件把
// GroupMultiplierKnown 置 true——管理员「只填 token 单价、不填倍率」这一最
// 常见操作会把 (multiplier=0, known=true) 落库，下游计费按 0 成本、选路
// lowest-cost 被拉偏。代码修复只堵新增，本迁移治理存量。
//
// 判定：manual_override=true 且 known=true 且 multiplier=0 的行是污染特征
// （UI 表单从不发送 group_multiplier_known，见逻辑对抗者复核）。极少数通过
// 裸 API 显式声明「免费」的行会被一并重置为按 1x 计，需重新确认——相对
// 于污染持续生效的静默成本归零，这是正确的默认取舍。
//
// 幂等：只改命中污染签名的行，重跑无副作用。
func migrateSiteModelPriceQuotePollutedMultiplier(conn *gorm.DB) error {
	if conn == nil {
		return fmt.Errorf("db is nil")
	}
	if !conn.Migrator().HasTable(&model.SiteModelPriceQuote{}) {
		return nil
	}
	result := conn.Model(&model.SiteModelPriceQuote{}).
		Where("manual_override = ? AND group_multiplier_known = ? AND group_multiplier = ?", true, true, 0).
		Update("group_multiplier_known", false)
	if result.Error != nil {
		return fmt.Errorf("reset polluted group multiplier known: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		fmt.Printf("migration 2026091401: reset %d polluted price quote rows (multiplier=0+known=true)\n", result.RowsAffected)
	}
	return nil
}
