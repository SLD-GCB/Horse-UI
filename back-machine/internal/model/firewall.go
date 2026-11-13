package model

// FirewallSettings 防火墙单行设置。
type FirewallSettings struct {
	ID          uint  `gorm:"primaryKey" json:"id"`
	Enabled     bool  `json:"enabled"`
	AutoImport  bool  `json:"auto_import"`
	AutoProtect bool  `json:"auto_protect"`
	UpdatedAt   int64 `json:"updated_at"`
}

// TableName 固定表名。
func (FirewallSettings) TableName() string { return "firewall_settings" }

// FirewallPort 端口规则。Action: allow / close。
type FirewallPort struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	Port      int    `gorm:"index" json:"port"`
	Protocol  string `gorm:"size:8" json:"protocol"` // tcp / udp
	Action    string `gorm:"size:8" json:"action"`   // allow / close
	Source    string `gorm:"size:64" json:"source"`  // any / cidr
	Comment   string `gorm:"size:255" json:"comment"`
	Auto      bool   `json:"auto"`
	InboundID uint   `gorm:"index" json:"inbound_id"`
	Protect   bool   `json:"protect"` // 锁定当前状态：开放则常开，关闭则常关
	Enabled   bool   `json:"enabled"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// TableName 固定表名。
func (FirewallPort) TableName() string { return "firewall_ports" }

// FirewallBan IP 黑名单。
type FirewallBan struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	IP        string `gorm:"size:64;index" json:"ip"`
	Reason    string `gorm:"size:255" json:"reason"`
	Source    string `gorm:"size:16" json:"source"` // manual / attack / scan
	Permanent bool   `json:"permanent"`
	CreatedAt int64  `json:"created_at"`
}

// TableName 固定表名。
func (FirewallBan) TableName() string { return "firewall_bans" }

// FirewallAttackSource 异常来源聚合，供面板人工确认后封禁。
type FirewallAttackSource struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	IP        string `gorm:"size:64;index:idx_fw_source,unique" json:"ip"`
	Port      int    `gorm:"index:idx_fw_source,unique" json:"port"`
	Protocol  string `gorm:"size:8;index:idx_fw_source,unique" json:"protocol"`
	Count     int    `json:"count"`
	FirstSeen int64  `json:"first_seen"`
	LastSeen  int64  `json:"last_seen"`
	Banned    bool   `json:"banned"`
}

// TableName 固定表名。
func (FirewallAttackSource) TableName() string { return "firewall_attack_sources" }

// FirewallAttackLog 动态容量判定的攻击事件。
type FirewallAttackLog struct {
	ID        uint    `gorm:"primaryKey" json:"id"`
	Port      int     `json:"port"`
	Protocol  string  `gorm:"size:8" json:"protocol"`
	Current   int     `json:"current"`
	Limit     float64 `json:"limit"`
	Baseline  float64 `json:"baseline"`
	Action    string  `gorm:"size:16" json:"action"` // watch / close / reopen
	Sources   string  `gorm:"type:text" json:"sources"`
	CreatedAt int64   `gorm:"index" json:"created_at"`
}

// TableName 固定表名。
func (FirewallAttackLog) TableName() string { return "firewall_attack_logs" }
