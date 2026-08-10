package migrate

import (
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 2026081002, // 必须 > 现有最大 2026081001（site_group_multiplier_known.go）
		Up:      migrateCanonicalModelVision,
	})
}

// migrateCanonicalModelVision 为存量 CanonicalModel 回填 vision_capable 列。
// 迁移在 InitDB 时执行（此时 models.dev 数据必然未加载——任务在 server 启动后异步拉取），
// 故此处只能按「模型名后缀规则」回填（5v/vision/vl 等显式后缀）；
// models.dev 来源的回填由运行时 CatalogSync 的存量回填分支承担（catalog.go `VisionCapable == nil`）。
// 只回填 vision_capable IS NULL 的行（失败重试幂等）。
func migrateCanonicalModelVision(conn *gorm.DB) error {
	if conn == nil {
		return fmt.Errorf("db is nil")
	}
	if !conn.Migrator().HasTable(&model.CanonicalModel{}) {
		return nil
	}
	if err := conn.AutoMigrate(&model.CanonicalModel{}); err != nil {
		return fmt.Errorf("auto migrate canonical_models: %w", err)
	}
	var canonicals []model.CanonicalModel
	if err := conn.Select("id", "name").Where("vision_capable IS NULL").Find(&canonicals).Error; err != nil {
		return fmt.Errorf("query canonical_models: %w", err)
	}
	return conn.Transaction(func(tx *gorm.DB) error {
		for i := range canonicals {
			c := &canonicals[i]
			if !visionBySuffix(c.Name) {
				continue // 无显式视觉后缀 → 保持 nil（未知），留给运行时 models.dev 回填
			}
			v := true
			if err := tx.Model(&model.CanonicalModel{}).
				Where("id = ? AND vision_capable IS NULL", c.ID).
				Update("vision_capable", v).Error; err != nil {
				return fmt.Errorf("update canonical %d vision_capable: %w", c.ID, err)
			}
		}
		return nil
	})
}

// visionBySuffix 模型名显式视觉后缀判定（与 op.resolveVisionCapable 的兜底规则同源，
// 迁移不依赖 op 包避免循环依赖）。使用 modelvendor 时仅用于探测（本函数独立实现）。
func visionBySuffix(name string) bool {
	lower := strings.ToLower(name)
	for _, suffix := range []string{"5v", "vision", "-vl", "-vlx", "omni", "visual"} {
		if strings.Contains(lower, suffix) {
			return true
		}
	}
	return false
}
