package api

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	qrcode "github.com/skip2/go-qrcode"
	"gopkg.in/yaml.v3"

	"horseui/back-machine/internal/model"
	"horseui/back-machine/internal/svc"
)

// hostOnly 去掉主机串中的端口部分。
func hostOnly(s string) string {
	if h, _, err := net.SplitHostPort(s); err == nil {
		return h
	}
	return s
}

// subUAAllowed 校验订阅请求的 User-Agent 是否命中白名单（配置为空时不限制）。
func (a *App) subUAAllowed(ua string) bool {
	raw := strings.TrimSpace(a.settings.Get(model.SettingSubUAWhitelist))
	if raw == "" {
		return true
	}
	lua := strings.ToLower(ua)
	for _, p := range strings.Split(raw, ",") {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" && strings.Contains(lua, p) {
			return true
		}
	}
	return false
}

// serverAddress 返回分享链接使用的对外服务器地址。
func (a *App) serverAddress(c *gin.Context) string {
	s := strings.TrimSpace(a.settings.Get(model.SettingSubHost))
	if s == "" {
		return hostOnly(c.Request.Host)
	}
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimSuffix(s, "/")
	return s
}

// subURL 返回某个客户端的完整订阅地址。
func (a *App) subURL(c *gin.Context, cl model.Client) string {
	base := strings.TrimSpace(a.settings.Get(model.SettingSubHost))
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if base == "" {
		base = scheme + "://" + c.Request.Host
	} else if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = scheme + "://" + base
	}
	base = strings.TrimSuffix(base, "/")
	return base + a.subPath() + "/" + cl.SubToken
}

// clientInbounds 返回该客户端的全部生效入站：主入站排第一，其余为额外绑定（按 ID 升序）。
// 订阅/分享链接据此下发该用户绑定的所有节点。
func (a *App) clientInbounds(cl model.Client) []model.Inbound {
	inboundMap := map[uint]model.Inbound{}
	var inbounds []model.Inbound
	a.db.Find(&inbounds)
	for _, ib := range inbounds {
		inboundMap[ib.ID] = ib
	}

	seen := map[uint]bool{cl.InboundID: true}
	rest := make([]uint, 0, 4)
	for _, id := range a.clientBindings()[cl.ID] {
		if seen[id] {
			continue
		}
		seen[id] = true
		rest = append(rest, id)
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i] < rest[j] })

	ids := append([]uint{cl.InboundID}, rest...)
	out := make([]model.Inbound, 0, len(ids))
	for _, id := range ids {
		if ib, ok := inboundMap[id]; ok {
			out = append(out, ib)
		}
	}
	return out
}

// behaviorMask 读取「行为抹除」设置：对全部 TCP 系协议统一开启多路复用。
// 目的是抹掉「连接数 / 建连频率 / 反复 TLS 握手」的行为指纹，而不是省 RTT。
func (a *App) behaviorMask() svc.BehaviorMask {
	if strings.TrimSpace(a.settings.Get(model.SettingBehaviorMask)) != "1" {
		return svc.BehaviorMask{}
	}
	m := svc.BehaviorMask{Enabled: true, MaxStreams: 8, Padding: true}
	if v, err := strconv.Atoi(strings.TrimSpace(a.settings.Get(model.SettingBehaviorMaskStreams))); err == nil && v >= 1 && v <= 128 {
		m.MaxStreams = v
	}
	if strings.TrimSpace(a.settings.Get(model.SettingBehaviorMaskPadding)) == "0" {
		m.Padding = false
	}
	return m
}

// clientLinks 返回单个客户端的分享链接、订阅地址与二维码（需登录）。
func (a *App) clientLinks(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	var cl model.Client
	if err := a.db.First(&cl, id).Error; err != nil {
		fail(c, 404, "用户不存在")
		return
	}
	ibs := a.clientInbounds(cl)
	if len(ibs) == 0 {
		fail(c, 404, "该用户没有可用的入站")
		return
	}

	server := a.serverAddress(c)
	mask := a.behaviorMask()
	links := svc.BuildShareLinks(server, ibs, cl)

	// 二维码只承载第一个节点（一个二维码放不下多个节点，多节点请用订阅地址）。
	qr := ""
	if len(links) > 0 {
		if png, err := qrcode.Encode(links[0], qrcode.Medium, 256); err == nil {
			qr = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
		}
	}

	ok(c, gin.H{
		"link":    strings.Join(links, "\n"),
		"links":   links,
		"nodes":   len(links),
		"sub_url": a.subURL(c, cl),
		"qrcode":  qr,
		"clash":   svc.BuildClashConfig(server, ibs, cl, mask),
		"singbox": svc.BuildSingboxClientConfig(server, ibs, cl, mask),
	})
}

// handleSubscription 订阅分发端点（无需鉴权）。
func (a *App) handleSubscription(c *gin.Context) {
	// 限流，避免被爬取或暴力枚举。
	if !a.subLimit.Allow("sub:" + c.ClientIP()) {
		c.String(http.StatusTooManyRequests, "too many requests")
		return
	}
	// 可选的 User-Agent 白名单。
	if !a.subUAAllowed(c.GetHeader("User-Agent")) {
		c.String(http.StatusForbidden, "forbidden")
		return
	}

	token := c.Param("token")

	var cl model.Client
	if err := a.db.Where("sub_token = ?", token).First(&cl).Error; err != nil {
		c.String(http.StatusNotFound, "subscription not found")
		return
	}
	if !cl.Active() {
		c.String(http.StatusForbidden, "subscription disabled or expired")
		return
	}
	ibs := a.clientInbounds(cl)
	if len(ibs) == 0 {
		c.String(http.StatusNotFound, "no available inbound")
		return
	}

	server := a.serverAddress(c)
	mask := a.behaviorMask()

	switch strings.ToLower(c.Query("target")) {
	case "clash", "clashmeta", "meta":
		cfg := svc.BuildClashConfig(server, ibs, cl, mask)
		if cfg == nil {
			c.String(http.StatusBadRequest, "unsupported protocol")
			return
		}
		b, err := yaml.Marshal(cfg)
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		c.Data(http.StatusOK, "text/yaml; charset=utf-8", b)

	case "singbox", "sing-box":
		cfg := svc.BuildSingboxClientConfig(server, ibs, cl, mask)
		if cfg == nil {
			c.String(http.StatusBadRequest, "unsupported protocol")
			return
		}
		b, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		c.Data(http.StatusOK, "application/json; charset=utf-8", b)

	default:
		links := svc.BuildShareLinks(server, ibs, cl)
		if len(links) == 0 {
			c.String(http.StatusBadRequest, "unsupported protocol")
			return
		}
		body := base64.StdEncoding.EncodeToString([]byte(strings.Join(links, "\n")))
		c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(body))
	}
}
