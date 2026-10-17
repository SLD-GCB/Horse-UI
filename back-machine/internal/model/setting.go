package model

// Setting 面板与内核的键值型配置。
type Setting struct {
	Key   string `gorm:"primaryKey;size:64" json:"key"`
	Value string `gorm:"type:text" json:"value"`
}

// 预置配置键。
const (
	SettingJWTSecret    = "jwt_secret"       // 面板登录令牌密钥
	SettingSubHost      = "sub_host"         // 订阅链接对外域名，如 https://example.com
	SettingSubPath      = "sub_path"         // 订阅路径前缀，默认 /sub
	SettingClashListen  = "clash_listen"     // clash api 监听地址，默认 127.0.0.1:9090
	SettingClashSecret  = "clash_secret"     // clash api 密钥
	SettingV2RayListen  = "v2ray_listen"     // 统计 gRPC 监听地址，默认 127.0.0.1:9091
	SettingLogLevel     = "log_level"        // 内核日志级别
	SettingDNS          = "dns"              // 原始 DNS 配置 JSON
	SettingRoute        = "route"            // 原始 route 配置 JSON
	SettingBoxVersion   = "box_version"      // 当前 sing-box 版本
	SettingACMEEmail    = "acme_email"       // ACME 账户邮箱
	SettingStatsMode    = "stats_mode"       // 流量统计方式：auto（默认）/ clash / v2ray / off
	SettingStatsSupport = "stats_support"    // 内核是否支持 V2Ray API（探测结果缓存）
	SettingBoxURL       = "box_download_url" // 自定义内核下载地址模板
	SettingPortStart    = "port_pool_start"  // 端口池起始
	SettingPortEnd      = "port_pool_end"    // 端口池结束

	// ---- 路由可视化 ----
	SettingRouteRules = "route_rules" // 可视化路由规则（JSON 数组，顺序即匹配顺序）
	SettingRouteFinal = "route_final" // 默认出站 tag

	// ---- 测速 ----
	SettingTestURL = "test_url" // 节点测速地址（入站/出站测速共用）

	// ---- 面板自身 ----
	SettingPanelDomain = "panel_domain" // 面板对外域名/地址（仅用于展示与链接生成）
	SettingPanelListen = "panel_listen" // 面板监听地址（覆盖启动参数 -listen，重启生效）

	// ---- 广告过滤 ----
	SettingAdblockEnable   = "adblock_enable"   // "1" 启用广告过滤
	SettingAdblockDomains  = "adblock_domains"  // 广告域名后缀（每行一个）
	SettingAdblockKeywords = "adblock_keywords" // 域名关键词（逗号/换行分隔）
	SettingAdblockRegex    = "adblock_regex"    // 域名正则（每行一个）
	SettingAdblockIPs      = "adblock_ips"      // IP/CIDR 黑名单（每行一个）
	SettingAdblockURL      = "adblock_url"      // 可选：远程规则集 URL（.srs）

	// ---- 安全相关 ----
	SettingPanelTLSEnable   = "panel_tls_enable"   // 面板 HTTPS 开关："1" 启用
	SettingPanelTLSCertID   = "panel_tls_cert_id"  // 面板 HTTPS 使用的证书 ID
	SettingPanelHTTPListen  = "panel_http_listen"  // 明文监听（用于 308 跳转到 HTTPS），空=关闭
	SettingCORSOrigins      = "cors_origins"       // CORS 白名单，逗号分隔，空=仅同源
	SettingSessionTTLHours  = "session_ttl_hours"  // 登录会话有效期（小时）
	SettingLoginMaxFails    = "login_max_fails"    // 连续登录失败锁定阈值
	SettingSubUAWhitelist   = "sub_ua_whitelist"   // 订阅 UA 子串白名单，空=不限制
	SettingPanelIPWhitelist = "panel_ip_whitelist" // 面板访问 IP/CIDR 白名单，空=不限制
	SettingMaxBodyBytes     = "max_body_bytes"     // 请求体上限（字节）

	// ---- 行为抹除（抗识别）----
	// 对全部 TCP 系协议（vless/vmess/trojan/shadowsocks）的客户端出站统一开启多路复用，
	// 把「连接数 / 每秒建连数 / 反复 TLS 握手」这三个最容易被 QoS 盯上的行为指纹压掉。
	SettingBehaviorMask        = "behavior_mask"         // "1" 启用
	SettingBehaviorMaskStreams = "behavior_mask_streams" // 每连接并发流上限，默认 8
	SettingBehaviorMaskPadding = "behavior_mask_padding" // "1" 填充帧长（默认开）

	// SettingAdminLastPassword 面板最近一次「自动生成」的管理员密码，供终端菜单查看。
	// 仅在面板自己生成密码时写入（新库初始化 / 弱口令重置 / 菜单重置）；
	// 用户在界面上手动改密后会被清空——bcrypt 哈希无法反解，所以不存原文就查不到。
	// 该键属于 protectedSettings，不会通过 API 外泄。
	SettingAdminLastPassword = "admin_last_password"
)
