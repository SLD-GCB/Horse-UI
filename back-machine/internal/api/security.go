package api

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"horseui/back-machine/internal/model"
)

// 面板默认的安全响应头。CSP 允许内联样式以满足 Element Plus 运行时注入。
const panelCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; " +
	"frame-ancestors 'none'; base-uri 'self'; form-action 'self'"

// securityHeadersMiddleware 为所有响应补充安全相关响应头。
func (a *App) securityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		h.Set("Content-Security-Policy", panelCSP)
		if c.Request.TLS != nil {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		c.Next()
	}
}

// bodyLimitMiddleware 限制请求体大小，防止超大 JSON 打爆内存。
func (a *App) bodyLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := int64(2 << 20) // 2MB
		if v := a.settings.Get(model.SettingMaxBodyBytes); v != "" {
			if p, err := strconv.ParseInt(v, 10, 64); err == nil && p > 0 {
				limit = p
			}
		}
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}
		c.Next()
	}
}

// ipWhitelistMiddleware 限制面板 API 的访问来源；白名单为空时不做限制。
func (a *App) ipWhitelistMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := strings.TrimSpace(a.settings.Get(model.SettingPanelIPWhitelist))
		if raw == "" {
			c.Next()
			return
		}
		if err := a.ipAllow.ensure(raw); err != nil {
			// 配置解析失败时放行并告警，避免把自己锁在面板外。
			fmt.Printf("[horseui] IP 白名单解析失败，已放行: %v\n", err)
			c.Next()
			return
		}
		if !a.ipAllow.allowed(c.ClientIP()) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 40302, "msg": "来源 IP 不在白名单内"})
			return
		}
		c.Next()
	}
}

// ipAllowList 缓存已解析的 IP/CIDR 白名单。
type ipAllowList struct {
	mu   sync.RWMutex
	raw  string
	nets []*net.IPNet
	ips  []net.IP
}

// ensure 在原始配置变化时重新解析。
func (l *ipAllowList) ensure(raw string) error {
	l.mu.RLock()
	same := l.raw == raw
	l.mu.RUnlock()
	if same {
		return nil
	}
	nets, ips, err := parseIPList(raw)
	if err != nil {
		return err
	}
	l.mu.Lock()
	l.raw, l.nets, l.ips = raw, nets, ips
	l.mu.Unlock()
	return nil
}

func (l *ipAllowList) allowed(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	for _, n := range l.nets {
		if n.Contains(ip) {
			return true
		}
	}
	for _, x := range l.ips {
		if x.Equal(ip) {
			return true
		}
	}
	return false
}

func parseIPList(raw string) ([]*net.IPNet, []net.IP, error) {
	var nets []*net.IPNet
	var ips []net.IP
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "/") {
			_, n, err := net.ParseCIDR(part)
			if err != nil {
				return nil, nil, fmt.Errorf("无效 CIDR: %s", part)
			}
			nets = append(nets, n)
			continue
		}
		ip := net.ParseIP(part)
		if ip == nil {
			return nil, nil, fmt.Errorf("无效 IP: %s", part)
		}
		ips = append(ips, ip)
	}
	return nets, ips, nil
}

// corsOrigins 解析 CORS 白名单。
func (a *App) corsOrigins() []string {
	raw := strings.TrimSpace(a.settings.Get(model.SettingCORSOrigins))
	if raw == "" {
		return nil
	}
	out := make([]string, 0, 4)
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// corsMiddleware 仅对白名单来源放行跨域，避免反射任意 Origin。
func (a *App) corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			allowed := false
			for _, o := range a.corsOrigins() {
				if strings.EqualFold(o, origin) || o == "*" {
					allowed = true
					break
				}
			}
			if allowed {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Access-Control-Allow-Credentials", "true")
				c.Header("Vary", "Origin")
				c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
				c.Header("Access-Control-Allow-Headers", "Authorization,Content-Type")
			}
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
		}
		c.Next()
	}
}
