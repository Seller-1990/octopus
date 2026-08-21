package model

import "time"

// SiteCheckinLog 记录一次站点账号签到的执行结果。
type SiteCheckinLog struct {
	ID            int64               `json:"id" gorm:"primaryKey;autoIncrement"`
	SiteID        int                 `json:"site_id" gorm:"not null;index"`
	SiteAccountID int                 `json:"site_account_id" gorm:"not null;index"`
	Status        SiteExecutionStatus `json:"status" gorm:"type:varchar(16);not null"`
	Message       string              `json:"message,omitempty"`
	Reward        string              `json:"reward,omitempty"`
	LatencyMS     int64               `json:"latency_ms" gorm:"default:0"`
	Source        string              `json:"source" gorm:"type:varchar(16);not null;default:'manual'"`
	CreatedAt     time.Time           `json:"created_at"`
}
