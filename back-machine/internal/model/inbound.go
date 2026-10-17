package model

import "time"

// 入站协议类型。
const (
	ProtoVLESS       = "vless"
	ProtoVMess       = "vmess"
	ProtoTrojan      = "trojan"
	ProtoShadowsocks = "shadowsocks"
	ProtoTUIC        = "tuic"
	ProtoHysteria2   = "hysteria2"
	ProtoHTTP        = "http"
)

// Inbound 入站（服务端监听）配置。
type Inbound struct {
	ID            uint   `gorm:"primaryKey" json:"id"`
	Tag           string `gorm:"uniqueIndex;size:64" json:"tag"`
	Type          string `gorm:"size:32;index" json:"type"`
	Remark        string `gorm:"size:255" json:"remark"`
	Enable        bool   `json:"enable"`
	Listen        string `gorm:"size:64" json:"listen"`
	Port          int    `json:"port"`
	Sniff         bool   `json:"sniff"`
	SniffOverride bool   `json:"sniff_override"`

	// 通用配置片段（原始 JSON）。
	TLS       JSONText `gorm:"type:text" json:"tls"`
	Reality   JSONText `gorm:"type:text" json:"reality"`
	Transport JSONText `gorm:"type:text" json:"transport"`
	// Extra 协议特有/高级选项，直接合并进入站对象（type/tag/listen 等受保护字段除外）。
	Extra JSONText `gorm:"type:text" json:"extra"`

	// Shadowsocks 专用。
	Method         string `gorm:"size:64" json:"method"`
	ServerPassword string `gorm:"size:256" json:"server_password"`

	// Hysteria2 / TUIC 专用。
	UpMbps            int    `json:"up_mbps"`
	DownMbps          int    `json:"down_mbps"`
	ObfsType          string `gorm:"size:32" json:"obfs_type"`
	ObfsPassword      string `gorm:"size:128" json:"obfs_password"`
	CongestionControl string `gorm:"size:32" json:"congestion_control"`
	Masquerade        string `gorm:"size:255" json:"masquerade"`

	// 证书引用（指向 Certificate 表）。
	CertID *uint `json:"cert_id"`
	// Insecure 使用自签证书时置为 true：下发的客户端配置会跳过证书校验。
	// 该字段只影响订阅内容，不会写入内核配置。
	Insecure bool `json:"insecure"`

	// 该入站累计流量（由统计任务写入）。
	UsedUp   int64 `gorm:"default:0" json:"used_up"`
	UsedDown int64 `gorm:"default:0" json:"used_down"`

	// 内核上报的累计原始值，用于计算增量（仅 V2Ray 统计方式使用）。
	RawUp   int64 `gorm:"default:0" json:"-"`
	RawDown int64 `gorm:"default:0" json:"-"`

	Clients   []Client  `gorm:"foreignKey:InboundID" json:"clients,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RequireTLS 判断协议是否强制启用 TLS。
// 除 Shadowsocks（自带 AEAD 加密）外，所有协议都必须启用 TLS 或 Reality：
// VLESS 等协议本身不含加密，不配 TLS 即为明文传输，会被人中间人窃听。
func (i Inbound) RequireTLS() bool {
	return i.Type != ProtoShadowsocks
}

// SupportsTransport 判断协议是否支持 transport 字段。
// HTTP 入站本身就是 HTTP 服务，Shadowsocks 使用自身加密，QUIC 类协议也不支持。
func (i Inbound) SupportsTransport() bool {
	switch i.Type {
	case ProtoVLESS, ProtoVMess, ProtoTrojan:
		return true
	default:
		return false
	}
}
