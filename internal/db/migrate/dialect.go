package migrate

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// migrationDialect 返回小写方言名（sqlite/mysql/postgres），供裸 SQL DDL
// 按方言分支。历史迁移直接写 SQLite 风格 DDL：MySQL 拒绝 TEXT 列字面量
// DEFAULT（ERROR 1101），PostgreSQL 不存在 DATETIME 类型（004.go 在 PG 上
// 启动必失败）——B250913-07/08。
func migrationDialect(db *gorm.DB) string {
	if db == nil {
		return "sqlite"
	}
	return strings.ToLower(db.Dialector.Name())
}

// textColumnDDL 带字面量默认值的字符串列 DDL。MySQL 的 TEXT/BLOB 列不能
// 携带字面量 DEFAULT，改用 VARCHAR(191)（与全库索引列长度约定一致）。
func textColumnDDL(db *gorm.DB, column, defaultValue string) string {
	if migrationDialect(db) == "mysql" {
		return fmt.Sprintf("%s VARCHAR(191) NOT NULL DEFAULT '%s'", column, defaultValue)
	}
	return fmt.Sprintf("%s TEXT NOT NULL DEFAULT '%s'", column, defaultValue)
}

// datetimeColumnDDL 时间戳列 DDL。PostgreSQL 无 DATETIME，用 TIMESTAMP；
// SQLite/MySQL 均接受 DATETIME。
func datetimeColumnDDL(db *gorm.DB, column string) string {
	if migrationDialect(db) == "postgres" {
		return fmt.Sprintf("%s TIMESTAMP", column)
	}
	return fmt.Sprintf("%s DATETIME", column)
}
