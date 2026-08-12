package migrate

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 2026081001, // 必须 > 现有最大 2026080304（migrate.go 按 Version 升序执行）
		Up:      migrateSiteGroupMultiplierKnown,
	})
}

// migrateSiteGroupMultiplierKnown 为 site_user_groups 回填 multiplier_known 列。
// 规则（v3，吸收逻辑对抗者复审）：存量行一律标 false（暂定）。
//
// 早期方案曾用 raw_payload「自证」判 true，但该证明是重言式——multiplier 本就
// 解析自 raw_payload，同步写入行必然自证成功、pricing 写入行必然失败，证明不了
// 额外事实。标 false 只影响过渡期（暂定行豁免 cap + 前端「暂定」徽标），站点
// 下次同步 / pricing 刷新即由阶段 2 写路径升级为真值。
//
// 只回填 multiplier_known IS NULL 的行：失败重试只补未回填行，不回退已回填值；
// 版本化一次性执行，成功记录后（migrate.go 按 Version 跳过 SUCCESS）不再重跑。
func migrateSiteGroupMultiplierKnown(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.SiteUserGroup{}) {
		return nil
	}
	// 幂等保险：主 AutoMigrate（db.go models 列表含 SiteUserGroup）已加列，此处 no-op。
	if err := db.AutoMigrate(&model.SiteUserGroup{}); err != nil {
		return fmt.Errorf("auto migrate site_user_groups: %w", err)
	}
	if err := db.Model(&model.SiteUserGroup{}).
		Where("multiplier_known IS NULL").
		Update("multiplier_known", false).Error; err != nil {
		return fmt.Errorf("backfill multiplier_known: %w", err)
	}
	return nil
}
