package op

import (
	"context"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

// SiteCheckinLogAdd 写入一条签到执行日志。
func SiteCheckinLogAdd(ctx context.Context, logEntry model.SiteCheckinLog) error {
	return db.GetDB().WithContext(ctx).Create(&logEntry).Error
}

// SiteCheckinLogList 返回指定账号最近的签到日志。
func SiteCheckinLogList(ctx context.Context, accountID int, limit int) ([]model.SiteCheckinLog, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	var logs []model.SiteCheckinLog
	if err := db.GetDB().WithContext(ctx).
		Where("site_account_id = ?", accountID).
		Order("id DESC").
		Limit(limit).
		Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}
