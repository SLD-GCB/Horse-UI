package model

import "time"

// AuditLog 敏感操作审计记录。
type AuditLog struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Username  string    `gorm:"size:64;index" json:"username"`
	IP        string    `gorm:"size:64" json:"ip"`
	Method    string    `gorm:"size:8" json:"method"`
	Path      string    `gorm:"size:255;index" json:"path"`
	Action    string    `gorm:"size:128" json:"action"`
	Status    int       `json:"status"`
	Detail    string    `gorm:"size:255" json:"detail"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}
