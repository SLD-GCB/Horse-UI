package model

import "time"

// Admin 面板管理员账号。
type Admin struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	Username   string `gorm:"uniqueIndex;size:64" json:"username"`
	Password   string `gorm:"size:128" json:"-"`
	TOTPSecret string `gorm:"size:64" json:"-"`
	// TokenVersion 令牌版本；改密/登出时递增，使旧令牌立即失效。
	TokenVersion int `gorm:"default:1" json:"-"`
	// MustChangePassword 首次生成的随机密码需强制修改。
	MustChangePassword bool      `gorm:"default:false" json:"must_change_password"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}
