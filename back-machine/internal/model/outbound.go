package model

import "time"

// ProtoWireGuard 在 sing-box 中需渲染为顶层 endpoints（而非 outbounds）。
const ProtoWireGuard = "wireguard"

// Outbound 出站/链式转发节点。
type Outbound struct {
	ID     uint   `gorm:"primaryKey" json:"id"`
	Tag    string `gorm:"uniqueIndex;size:64" json:"tag"`
	Type   string `gorm:"size:32" json:"type"`
	Server string `gorm:"size:255" json:"server"`
	Port   int    `json:"port"`
	Remark string `gorm:"size:255" json:"remark"`
	Enable bool   `json:"enable"`

	// 协议专属字段（uuid/password/method/flow/security/alter_id/obfs/peers 等）。
	Config JSONText `gorm:"type:text" json:"config"`
	// 出站 TLS 配置。
	TLS JSONText `gorm:"type:text" json:"tls"`
	// 传输层配置（ws/grpc/http/httpupgrade）。
	Transport JSONText `gorm:"type:text" json:"transport"`
	// 多路复用配置。
	Multiplex JSONText `gorm:"type:text" json:"multiplex"`
	// 拨号/链式字段（detour/bind_interface/tcp_fast_open/domain_resolver，顶层平铺）。
	Dial JSONText `gorm:"type:text" json:"dial"`
	// 任意附加字段（兜底）。
	Extra JSONText `gorm:"type:text" json:"extra"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
