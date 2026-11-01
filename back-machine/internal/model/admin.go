package model

import "time"

// Admin 面板管理员账号。
type Admin struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	Username   string `gorm:"uniqueIndex;size:64" json:"username"`
	Password   string `gorm:"size:128" json:"-"`
	TOTPSecret string `gorm:"size:64" json:"-"`
	// TOTPEnabled 是否已启用两步验证。
	TOTPEnabled bool `gorm:"default:false" json:"-"`
	// TOTPRecovery 恢复码的 SHA-256 列表（JSON 数组），只用一次。
	TOTPRecovery string `gorm:"size:2048" json:"-"`
	// TOTPLastStep 最近一次成功使用的时间步，防止同一个码重放。
	TOTPLastStep int64 `gorm:"default:0" json:"-"`
	// TokenVersion 令牌版本；改密/登出时递增，使旧令牌立即失效。
	TokenVersion int `gorm:"default:1" json:"-"`
	// MustChangePassword 首次生成的随机密码需强制修改。
	MustChangePassword bool      `gorm:"default:false" json:"must_change_password"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}
