package model

import "time"

// Client 入站下的用户（可独立计费/限速/到期）。
type Client struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	InboundID uint   `gorm:"index" json:"inbound_id"`
	Name      string `gorm:"size:64;index" json:"name"`
	UUID      string `gorm:"size:64" json:"uuid"`
	Password  string `gorm:"size:256" json:"password"`
	Flow      string `gorm:"size:32" json:"flow"`
	Enable    bool   `json:"enable"`

	QuotaBytes int64 `json:"quota_bytes"` // 0 表示不限流量
	UsedUp     int64 `json:"used_up"`
	UsedDown   int64 `json:"used_down"`

	// 内核上报的累计原始值（内核重启会归零），用于计算增量。
	RawUp   int64 `gorm:"default:0" json:"-"`
	RawDown int64 `gorm:"default:0" json:"-"`

	ExpireAt *time.Time `json:"expire_at"`
	SubToken string     `gorm:"size:64;uniqueIndex" json:"sub_token"`
	Remark   string     `gorm:"size:255" json:"remark"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Used 已用总流量。
func (c Client) Used() int64 { return c.UsedUp + c.UsedDown }

// Expired 是否已过期。
func (c Client) Expired() bool {
	if c.ExpireAt == nil {
		return false
	}
	return time.Now().After(*c.ExpireAt)
}

// OverQuota 是否超出配额。
func (c Client) OverQuota() bool {
	return c.QuotaBytes > 0 && c.Used() >= c.QuotaBytes
}

// Active 是否可用（启用且未过期且未超配额）。
func (c Client) Active() bool {
	return c.Enable && !c.Expired() && !c.OverQuota()
}

// ClientStat 用户流量采样点，用于仪表盘图表。
type ClientStat struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ClientID  uint      `gorm:"index" json:"client_id"`
	Up        int64     `json:"up"`
	Down      int64     `json:"down"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}
