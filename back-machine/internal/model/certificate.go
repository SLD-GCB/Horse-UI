package model

import "time"

// 证书类型。
const (
	CertKindACME       = "acme"       // Let's Encrypt 等 ACME 签发
	CertKindSelfSigned = "selfsigned" // 面板本地自签
)

// Certificate 证书记录。
type Certificate struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	Kind        string     `gorm:"size:16;default:'acme'" json:"kind"`
	Domain      string     `gorm:"size:255;uniqueIndex" json:"domain"`
	Provider    string     `gorm:"size:32" json:"provider"` // acme: http/cloudflare/...；selfsigned: 留空
	Credentials JSONText   `gorm:"type:text" json:"credentials"`
	CertPath    string     `gorm:"size:512" json:"cert_path"`
	KeyPath     string     `gorm:"size:512" json:"key_path"`
	AutoRenew   bool       `json:"auto_renew"`
	NotBefore   *time.Time `json:"not_before"`
	NotAfter    *time.Time `json:"not_after"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}
