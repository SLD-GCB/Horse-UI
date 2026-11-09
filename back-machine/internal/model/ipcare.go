package model

// IPCareSettings IP 养护的用户设置，单行记录（id=1）。
type IPCareSettings struct {
	ID                    uint   `gorm:"primaryKey" json:"id"`
	TargetRegion          string `gorm:"size:8" json:"target_region"`
	DetectMode            string `gorm:"size:16" json:"detect_mode"`
	DetectIntervalSeconds int    `json:"detect_interval_seconds"`
	WarmMode              string `gorm:"size:16" json:"warm_mode"`
	NotifyCN              bool   `json:"notify_cn"`
	NotifyFail            bool   `json:"notify_fail"`
	NextCheckAt           int64  `json:"next_check_at"`
	UpdatedAt             int64  `json:"updated_at"`
}

// TableName 固定表名，避免 GORM 复数化变化。
func (IPCareSettings) TableName() string { return "ip_care_settings" }

// IPCareCheck 一次 Google 区域检测结果。
type IPCareCheck struct {
	ID           uint   `gorm:"primaryKey" json:"id"`
	IP           string `gorm:"size:64" json:"ip"`
	GoogleRegion string `gorm:"size:8" json:"google_region"`
	TargetRegion string `gorm:"size:8" json:"target_region"`
	IsCN         bool   `json:"is_cn"`
	TrustScore   int    `json:"trust_score"`
	LatencyMs    int64  `json:"latency_ms"`
	Error        string `gorm:"type:text" json:"error"`
	CreatedAt    int64  `gorm:"index" json:"created_at"`
}

// TableName 固定表名。
func (IPCareCheck) TableName() string { return "ip_care_checks" }

// IPCareJob 一次纠偏（温养）任务。
type IPCareJob struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	Type       string `gorm:"size:16" json:"type"`
	Status     string `gorm:"size:16" json:"status"`
	Requests   int    `json:"requests"`
	Error      string `gorm:"type:text" json:"error"`
	CreatedAt  int64  `gorm:"index" json:"created_at"`
	FinishedAt int64  `json:"finished_at"`
}

// TableName 固定表名。
func (IPCareJob) TableName() string { return "ip_care_jobs" }
