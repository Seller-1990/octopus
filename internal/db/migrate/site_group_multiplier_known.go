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
// 规则（阶段 1 v2，吸收实施前审查）：
//   - multiplier IS NULL → false（无倍率）
//   - multiplier == 1 → false（S1 编造 1x 集中在 1，真 1x 标 false 无害、靠下次同步升级）
//   - multiplier != 1 且 raw_payload 自证同值 → true（双源一致）
//   - 其余 → false（无法证实标 false，保守方向：多 false 只豁免 cap，多 true 才误拦）
//
// 只回填 multiplier_known IS NULL 的行。注意：本迁移是版本化一次性回填——
// 成功记录后（migrate.go 按 Version 跳过 SUCCESS）生产环境不再重跑；
// 「IS NULL 限定」用于失败重试场景（Up 报错 → 记 Failed → 下次启动重跑，只补
// 未回填行，不回退已回填值）。同步写路径（storage.go 全删重建）当前不携带本列，
// 回填值在站点下次同步后失效；持续性 known 维护由阶段 2 写路径（storage.go /
// site_pricing.go / 创建路径显式写 &false）承担。
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
	var groups []model.SiteUserGroup
	if err := db.Select("id", "group_key", "multiplier", "raw_payload").
		Where("multiplier_known IS NULL").
		Find(&groups).Error; err != nil {
		return fmt.Errorf("query site_user_groups: %w", err)
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for i := range groups {
			g := &groups[i]
			known := false
			if g.Multiplier != nil && *g.Multiplier != 1 {
				if v, ok := model.StoredSiteGroupMultiplier(g.RawPayload, g.GroupKey); ok && v == *g.Multiplier {
					known = true
				}
			}
			if err := tx.Model(&model.SiteUserGroup{}).
				Where("id = ? AND multiplier_known IS NULL", g.ID).
				Update("multiplier_known", known).Error; err != nil {
				return fmt.Errorf("update group %d multiplier_known: %w", g.ID, err)
			}
		}
		return nil
	})
}
