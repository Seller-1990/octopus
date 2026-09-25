package migrate

import (
	"fmt"

	"gorm.io/gorm"
)

// idxSitePriceCompareModel 支撑价格对比面板的点查：
// WHERE LOWER(model_name) = ? AND status='valid' AND observed_at > ?
// SQLite/PostgreSQL 用表达式索引（与查询谓词精确匹配）；MySQL 函数索引
// 语法随版本差异大，退化为普通列索引（报价表万行级，低频面板可接受）。
const idxSitePriceCompareModel = "idx_site_model_price_quotes_model_lower_observed"

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 2026092501,
		Up:      migrateSitePriceCompareIndex,
	})
}

func migrateSitePriceCompareIndex(conn *gorm.DB) error {
	if conn == nil {
		return fmt.Errorf("db is nil")
	}
	if !conn.Migrator().HasTable("site_model_price_quotes") {
		return nil
	}
	if conn.Migrator().HasIndex("site_model_price_quotes", idxSitePriceCompareModel) {
		return nil
	}
	switch conn.Name() {
	case "sqlite", "postgres":
		return conn.Exec(
			"CREATE INDEX " + idxSitePriceCompareModel +
				" ON site_model_price_quotes(LOWER(model_name), observed_at)",
		).Error
	default:
		return conn.Exec(
			"CREATE INDEX " + idxSitePriceCompareModel +
				" ON site_model_price_quotes(model_name, observed_at)",
		).Error
	}
}
