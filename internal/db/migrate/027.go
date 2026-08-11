package migrate

import (
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 2026081101, // 必须 > 2026081002（026.go）
		Up:      migrateCanonicalModelCapabilities,
	})
}

// migrateCanonicalModelCapabilities 为存量 CanonicalModel 回填 capabilities 位图。
// 迁移在 InitDB 时执行（models.dev 必然未加载——任务在 server 启动后异步拉取），
// 故此处只能按模型名后缀规则回填多模态/推理两维（语音/生图无后缀启发式，留给
// 运行时 CatalogSync 的 LookupCapabilities 预填）。
// 兼容旧字段：同步回填 vision_capable（由多模态位派生，保持双字段一致）。
// 只回填 capabilities IS NULL 的行（失败重试幂等）。
func migrateCanonicalModelCapabilities(conn *gorm.DB) error {
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
	if err := conn.Select("id", "name", "vision_capable").Where("capabilities IS NULL").Find(&canonicals).Error; err != nil {
		return fmt.Errorf("query canonical_models: %w", err)
	}
	return conn.Transaction(func(tx *gorm.DB) error {
		for i := range canonicals {
			c := &canonicals[i]
			caps := capabilitiesBySuffix(c.Name)
			// P1-1 修复：若 vision_capable 非 nil（已有注册表/历史证据），不得用后缀猜测覆盖。
			// 后缀只用于「vision 从未回填」的行（VisionCapable == nil 时补齐多模态位）。
			if c.VisionCapable != nil {
				if *c.VisionCapable {
					caps |= uint8(model.CapMultimodal)
				}
				// vision=false（明确不支持）：保留，不因后缀反转（证据优先于命名猜测）
			}
			if caps == 0 {
				continue // 无可推断能力 → 保持 nil，留给运行时 models.dev 预填
			}
			vision := caps&uint8(model.CapMultimodal) != 0
			updates := map[string]any{"capabilities": caps}
			// 仅在 vision 从未回填时才写 vision_capable（避免覆盖已有证据值）
			if c.VisionCapable == nil {
				updates["vision_capable"] = vision
			}
			if err := tx.Model(&model.CanonicalModel{}).
				Where("id = ? AND capabilities IS NULL", c.ID).
				Updates(updates).Error; err != nil {
				return fmt.Errorf("update canonical %d capabilities: %w", c.ID, err)
			}
		}
		return nil
	})
}

// capabilitiesBySuffix 模型名显式后缀能力推断（与 op.resolveCapabilities 的兜底规则同源，
// 迁移不依赖 op 包避免循环依赖）。仅多模态/推理可后缀推断。
func capabilitiesBySuffix(name string) uint8 {
	lower := strings.ToLower(name)
	var caps uint8
	for _, suffix := range []string{"5v", "vision", "-vl", "-vlx", "omni", "visual"} {
		if strings.Contains(lower, suffix) {
			caps |= uint8(model.CapMultimodal)
			break
		}
	}
	for _, suffix := range []string{"reasoning", "-r1", "thinking"} {
		if strings.Contains(lower, suffix) {
			caps |= uint8(model.CapReasoning)
			break
		}
	}
	return caps
}
