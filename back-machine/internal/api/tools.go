package api

import (
	"github.com/gin-gonic/gin"

	"horseui/back-machine/internal/svc"
	"horseui/back-machine/internal/util"
	"horseui/core/config"
)

// toolAdblockPreset 返回内置的常用广告域名与关键词列表。
func (a *App) toolAdblockPreset(c *gin.Context) {
	ok(c, gin.H{
		"domains":  svc.AdblockPresetDomains(),
		"keywords": svc.AdblockPresetKeywords(),
	})
}

func (a *App) toolUUID(c *gin.Context) {
	ok(c, gin.H{"uuid": util.NewUUID()})
}

func (a *App) toolReality(c *gin.Context) {
	var req struct {
		PrivateKey string `json:"private_key"`
	}
	_ = c.ShouldBindJSON(&req)

	if req.PrivateKey != "" {
		pub, err := config.RealityPublicFromPrivate(req.PrivateKey)
		if err != nil {
			badRequest(c, err.Error())
			return
		}
		ok(c, gin.H{"private_key": req.PrivateKey, "public_key": pub})
		return
	}

	priv, pub, err := config.RealityKeypair()
	if err != nil {
		serverError(c, err)
		return
	}
	ok(c, gin.H{"private_key": priv, "public_key": pub})
}

func (a *App) toolSSPassword(c *gin.Context) {
	var req struct {
		Method string `json:"method"`
	}
	_ = c.ShouldBindJSON(&req)
	if req.Method == "" {
		req.Method = "2022-blake3-aes-128-gcm"
	}
	pwd, err := config.SSPassword(req.Method)
	if err != nil {
		serverError(c, err)
		return
	}
	if pwd == "" {
		pwd = randomPassword()
	}
	ok(c, gin.H{"password": pwd})
}

func (a *App) toolShortID(c *gin.Context) {
	ok(c, gin.H{"short_id": config.ShortID()})
}

// toolPassword 生成 16 位随机客户端密码（大小写字母 + 数字，URL 安全）。
func (a *App) toolPassword(c *gin.Context) {
	ok(c, gin.H{"password": randomPassword()})
}

func (a *App) toolPresets(c *gin.Context) {
	ok(c, gin.H{
		"ss_methods":     config.PresetSSMethods(),
		"ss2022_methods": []string{"2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305"},
		"inbound_types":  []string{"vless", "vmess", "trojan", "shadowsocks", "tuic", "hysteria2", "skyhorse", "http"},
		"transports":     []string{"", "ws", "grpc", "httpupgrade", "http"},
		"congestion":     []string{"cubic", "new_reno", "bbr"},
		"obfs_types":     []string{"salamander", "gecko"},

		// 以下字段名与取值均已用 sing-box check 实测校验过。
		"tls_versions": []string{"1.0", "1.1", "1.2", "1.3"},
		"alpn_presets": []map[string]any{
			{"label": "h2 + http/1.1", "value": []string{"h2", "http/1.1"}},
			{"label": "h3", "value": []string{"h3"}},
			{"label": "h3 + h2", "value": []string{"h3", "h2"}},
		},
		"cipher_suites": []string{
			"TLS_AES_128_GCM_SHA256",
			"TLS_AES_256_GCM_SHA384",
			"TLS_CHACHA20_POLY1305_SHA256",
			"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
			"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
			"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384",
			"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
			"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256",
			"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256",
		},
		"client_auth": []map[string]string{
			{"label": "不请求（默认）", "value": ""},
			{"label": "request 请求客户端证书", "value": "request"},
			{"label": "require-any 要求任意证书", "value": "require-any"},
			{"label": "verify-if-given 提供则校验", "value": "verify-if-given"},
			{"label": "require-and-verify 要求并校验", "value": "require-and-verify"},
		},
		"ss_networks":     []string{"", "tcp", "udp"},
		"http_methods":    []string{"GET", "POST"},
		"bbr_profiles":    []string{"conservative", "standard", "aggressive"},
		"reality_maxdiff": []string{"", "30s", "1m", "5m", "10m"},

		// 出站相关（字段名与取值同样经过 sing-box check 实测）。
		"outbound_types":      []string{"direct", "block", "socks", "http", "shadowsocks", "vmess", "vless", "trojan", "hysteria2", "tuic", "skyhorse", "wireguard"},
		"outbound_transports": []string{"", "ws", "grpc", "http", "httpupgrade"},
		"vmess_securities":    []string{"auto", "aes-128-gcm", "chacha20-poly1305", "none", "zero"},
		"socks_versions":      []string{"5", "4a"},
		"tuic_relay_modes":    []string{"native", "quic"},
		"multiplex_protocols": []string{"smux", "yamux", "h2mux"},
		"utls_fingerprints":   []string{"chrome", "firefox", "edge", "safari", "360", "qq", "ios", "android", "random", "randomized"},
	})
}
