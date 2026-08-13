package model

type APIKey struct {
	ID              int     `json:"id" gorm:"primaryKey"`
	Name            string  `json:"name" gorm:"not null"`
	APIKey          string  `json:"api_key" gorm:"not null"`
	Enabled         bool    `json:"enabled" gorm:"default:true"`
	ExpireAt        int64   `json:"expire_at,omitempty"`
	MaxCost         float64 `json:"max_cost,omitempty"`
	MaxRPM          int     `json:"max_rpm,omitempty"`
	SupportedModels string  `json:"supported_models,omitempty"`
	ToolsOnly       bool    `json:"tools_only,omitempty"`                         // 仅 tools：勾选后该 key 全部请求只走支持 tools 的渠道模型（跳过 supports_tools=false）
	VisionBridge    bool    `json:"vision_bridge,omitempty" gorm:"default:false"` // 视觉桥 key 级开关：允许此 key 的含图请求在纯文本通道上替换为 VLM 描述（还需全局开关）
	QuotaLimit      float64 `json:"quota_limit" gorm:"default:0"`
	QuotaPeriod     string  `json:"quota_period" gorm:"size:16;default:'monthly'"`
	QuotaUsed       float64 `json:"quota_used" gorm:"default:0"`
	QuotaResetAt    int64   `json:"quota_reset_at" gorm:"default:0"`
}
