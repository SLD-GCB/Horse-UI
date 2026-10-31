package svc

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"horseui/back-machine/internal/model"
	"horseui/core/config"
)

// TLSSettings 入站 TLS/Reality 参数摘要。
type TLSSettings struct {
	Enabled     bool
	Reality     bool
	ServerName  string
	PublicKey   string
	ShortID     string
	Fingerprint string
	ALPN        string
}

// TransportSettings 入站传输层参数摘要。
type TransportSettings struct {
	Type        string // tcp / ws / grpc / http / httpupgrade
	Path        string
	Host        string
	ServiceName string
}

func getString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func getMap(m map[string]any, key string) map[string]any {
	if v, ok := m[key]; ok {
		if mm, ok := v.(map[string]any); ok {
			return mm
		}
	}
	return nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// ParseTLS 解析入站的 TLS/Reality 参数。
func ParseTLS(ib model.Inbound) TLSSettings {
	ts := TLSSettings{Fingerprint: "chrome"}
	tls := parseObj(ib.TLS)
	reality := parseObj(ib.Reality)

	if len(tls) > 0 {
		ts.Enabled = true
		ts.ServerName = getString(tls, "server_name")
		if alpn, ok := tls["alpn"].([]any); ok {
			parts := make([]string, 0, len(alpn))
			for _, a := range alpn {
				if s, ok := a.(string); ok {
					parts = append(parts, s)
				}
			}
			ts.ALPN = strings.Join(parts, ",")
		}
		if utls := getMap(tls, "utls"); utls != nil {
			if fp := getString(utls, "fingerprint"); fp != "" {
				ts.Fingerprint = fp
			}
		}
	}

	if len(reality) > 0 {
		ts.Enabled = true
		ts.Reality = true
		if ts.ServerName == "" {
			ts.ServerName = getString(getMap(reality, "handshake"), "server")
		}
		if pk := getString(reality, "private_key"); pk != "" {
			if pub, err := config.RealityPublicFromPrivate(pk); err == nil {
				ts.PublicKey = pub
			}
		}
		switch v := reality["short_id"].(type) {
		case []any:
			if len(v) > 0 {
				if s, ok := v[0].(string); ok {
					ts.ShortID = s
				}
			}
		case string:
			ts.ShortID = v
		}
	}
	return ts
}

// ParseTransport 解析入站的传输层参数。
func ParseTransport(ib model.Inbound) TransportSettings {
	ts := TransportSettings{Type: "tcp"}
	tr := parseObj(ib.Transport)
	if len(tr) == 0 {
		return ts
	}
	switch t := getString(tr, "type"); t {
	case "ws":
		ts.Type = "ws"
		ts.Path = getString(tr, "path")
		if h := getMap(tr, "headers"); h != nil {
			ts.Host = getString(h, "Host", "host")
		}
	case "grpc":
		ts.Type = "grpc"
		ts.ServiceName = getString(tr, "service_name")
	case "http":
		ts.Type = "http"
		ts.Path = getString(tr, "path")
		if hosts, ok := tr["host"].([]any); ok && len(hosts) > 0 {
			if s, ok := hosts[0].(string); ok {
				ts.Host = s
			}
		}
	case "httpupgrade":
		ts.Type = "httpupgrade"
		ts.Path = getString(tr, "path")
		ts.Host = getString(tr, "host")
	default:
		if t != "" {
			ts.Type = t
		}
	}
	return ts
}

func applyTransportQuery(q url.Values, tr TransportSettings) {
	switch tr.Type {
	case "ws", "http", "httpupgrade":
		if tr.Path != "" {
			q.Set("path", tr.Path)
		}
		if tr.Host != "" {
			q.Set("host", tr.Host)
		}
	case "grpc":
		if tr.ServiceName != "" {
			q.Set("serviceName", tr.ServiceName)
		}
	}
}

// BuildShareLink 生成单客户端分享链接（v2rayN/sing-box 通用）。
func BuildShareLink(server string, ib model.Inbound, cl model.Client) string {
	tls := ParseTLS(ib)
	tr := ParseTransport(ib)
	name := url.PathEscape(cl.Name)

	switch ib.Type {
	case model.ProtoVLESS:
		q := url.Values{}
		q.Set("type", tr.Type)
		switch {
		case tls.Reality:
			q.Set("security", "reality")
		case tls.Enabled:
			q.Set("security", "tls")
		default:
			q.Set("security", "none")
		}
		if tls.ServerName != "" {
			q.Set("sni", tls.ServerName)
		}
		if tls.Reality {
			if tls.PublicKey != "" {
				q.Set("pbk", tls.PublicKey)
			}
			if tls.ShortID != "" {
				q.Set("sid", tls.ShortID)
			}
			if tls.Fingerprint != "" {
				q.Set("fp", tls.Fingerprint)
			}
		}
		applyTransportQuery(q, tr)
		if cl.Flow != "" {
			q.Set("flow", cl.Flow)
		}
		if tls.ALPN != "" {
			q.Set("alpn", tls.ALPN)
		}
		if ib.Insecure {
			q.Set("allowInsecure", "1")
		}
		return fmt.Sprintf("vless://%s@%s:%d?%s#%s", cl.UUID, server, ib.Port, q.Encode(), name)

	case model.ProtoVMess:
		obj := map[string]any{
			"v": "2", "ps": cl.Name, "add": server, "port": strconv.Itoa(ib.Port),
			"id": cl.UUID, "aid": "0", "scy": "auto",
			"net": tr.Type, "type": "none", "host": tr.Host, "path": tr.Path,
		}
		if tls.Enabled {
			obj["tls"] = "tls"
		}
		if tls.ServerName != "" {
			obj["sni"] = tls.ServerName
		}
		if tls.ALPN != "" {
			obj["alpn"] = tls.ALPN
		}
		if ib.Insecure {
			obj["allowInsecure"] = true
		}
		if tr.Type == "grpc" {
			obj["path"] = tr.ServiceName
		}
		b, _ := json.Marshal(obj)
		return "vmess://" + base64.StdEncoding.EncodeToString(b)

	case model.ProtoTrojan:
		q := url.Values{}
		q.Set("type", tr.Type)
		if tls.Enabled {
			q.Set("security", "tls")
		}
		if tls.ServerName != "" {
			q.Set("sni", tls.ServerName)
		}
		if tls.ALPN != "" {
			q.Set("alpn", tls.ALPN)
		}
		applyTransportQuery(q, tr)
		if ib.Insecure {
			q.Set("allowInsecure", "1")
		}
		return fmt.Sprintf("trojan://%s@%s:%d?%s#%s", url.QueryEscape(cl.Password), server, ib.Port, q.Encode(), name)

	case model.ProtoShadowsocks:
		userinfo := base64.RawURLEncoding.EncodeToString([]byte(ib.Method + ":" + cl.Password))
		return fmt.Sprintf("ss://%s@%s:%d#%s", userinfo, server, ib.Port, name)

	case model.ProtoHysteria2:
		q := url.Values{}
		if tls.ServerName != "" {
			q.Set("sni", tls.ServerName)
		}
		if ib.ObfsPassword != "" {
			q.Set("obfs", orDefault(ib.ObfsType, "salamander"))
			q.Set("obfs-password", ib.ObfsPassword)
		}
		if tls.ALPN != "" {
			q.Set("alpn", tls.ALPN)
		}
		if ib.Insecure {
			q.Set("insecure", "1")
		}
		return fmt.Sprintf("hysteria2://%s@%s:%d?%s#%s", url.QueryEscape(cl.Password), server, ib.Port, q.Encode(), name)

	case model.ProtoSkyHorse:
		return ""

	case model.ProtoTUIC:
		q := url.Values{}
		if ib.CongestionControl != "" {
			q.Set("congestion_control", ib.CongestionControl)
		}
		if tls.ServerName != "" {
			q.Set("sni", tls.ServerName)
		}
		if tls.ALPN != "" {
			q.Set("alpn", tls.ALPN)
		}
		if ib.Insecure {
			q.Set("insecure", "1")
		}
		return fmt.Sprintf("tuic://%s:%s@%s:%d?%s#%s",
			cl.UUID, url.QueryEscape(cl.Password), server, ib.Port, q.Encode(), name)
	}
	return ""
}

func applyClashTransport(p map[string]any, tr TransportSettings) {
	switch tr.Type {
	case "ws":
		p["network"] = "ws"
		ws := map[string]any{"path": tr.Path}
		if tr.Host != "" {
			ws["headers"] = map[string]any{"Host": tr.Host}
		}
		p["ws-opts"] = ws
	case "grpc":
		p["network"] = "grpc"
		p["grpc-opts"] = map[string]any{"grpc-service-name": tr.ServiceName}
	case "http":
		p["network"] = "http"
		opts := map[string]any{"path": []string{orDefault(tr.Path, "/")}}
		if tr.Host != "" {
			opts["headers"] = map[string]any{"Host": []string{tr.Host}}
		}
		p["http-opts"] = opts
	case "httpupgrade":
		p["network"] = "httpupgrade"
		opts := map[string]any{"path": tr.Path}
		if tr.Host != "" {
			opts["host"] = tr.Host
		}
		p["httpupgrade-opts"] = opts
	}
}

// BuildClashProxy 生成 Clash(Meta) 代理节点配置。
func BuildClashProxy(server string, ib model.Inbound, cl model.Client) map[string]any {
	p := buildClashProxy(server, ib, cl)
	if p != nil && ib.Insecure {
		p["skip-cert-verify"] = true
	}
	return p
}

func buildClashProxy(server string, ib model.Inbound, cl model.Client) map[string]any {
	tls := ParseTLS(ib)
	tr := ParseTransport(ib)

	switch ib.Type {
	case model.ProtoVLESS:
		p := map[string]any{"name": cl.Name, "type": "vless", "server": server, "port": ib.Port, "uuid": cl.UUID, "udp": true}
		if tls.Enabled {
			p["tls"] = true
		}
		if tls.ServerName != "" {
			p["servername"] = tls.ServerName
		}
		if tls.ALPN != "" {
			p["alpn"] = strings.Split(tls.ALPN, ",")
		}
		if tls.Reality {
			p["reality-opts"] = map[string]any{"public-key": tls.PublicKey, "short-id": tls.ShortID}
			p["client-fingerprint"] = orDefault(tls.Fingerprint, "chrome")
		}
		if cl.Flow != "" {
			p["flow"] = cl.Flow
		}
		applyClashTransport(p, tr)
		return p

	case model.ProtoVMess:
		p := map[string]any{
			"name": cl.Name, "type": "vmess", "server": server, "port": ib.Port,
			"uuid": cl.UUID, "alterId": 0, "cipher": "auto", "udp": true,
		}
		if tls.Enabled {
			p["tls"] = true
		}
		if tls.ServerName != "" {
			p["servername"] = tls.ServerName
		}
		applyClashTransport(p, tr)
		return p

	case model.ProtoTrojan:
		p := map[string]any{"name": cl.Name, "type": "trojan", "server": server, "port": ib.Port, "password": cl.Password, "udp": true}
		if tls.ServerName != "" {
			p["sni"] = tls.ServerName
		}
		applyClashTransport(p, tr)
		return p

	case model.ProtoShadowsocks:
		return map[string]any{
			"name": cl.Name, "type": "ss", "server": server, "port": ib.Port,
			"cipher": ib.Method, "password": cl.Password, "udp": true,
		}

	case model.ProtoHysteria2:
		p := map[string]any{"name": cl.Name, "type": "hysteria2", "server": server, "port": ib.Port, "password": cl.Password}
		if tls.ServerName != "" {
			p["sni"] = tls.ServerName
		}
		if ib.ObfsPassword != "" {
			p["obfs"] = orDefault(ib.ObfsType, "salamander")
			p["obfs-password"] = ib.ObfsPassword
		}
		// 弱网：把服务端带宽下发给客户端，客户端据此启用 Brutal（丢包不减速）。
		// 注意 Mihomo 用的是字符串形式的 up / down，不是 up_mbps / down_mbps。
		if ib.UpMbps > 0 {
			p["up"] = fmt.Sprintf("%d Mbps", ib.UpMbps)
		}
		if ib.DownMbps > 0 {
			p["down"] = fmt.Sprintf("%d Mbps", ib.DownMbps)
		}
		return p

	case model.ProtoSkyHorse:
		return nil

	case model.ProtoTUIC:
		p := map[string]any{
			"name": cl.Name, "type": "tuic", "server": server, "port": ib.Port,
			"uuid": cl.UUID, "password": cl.Password, "udp-relay-mode": "native",
		}
		if ib.CongestionControl != "" {
			p["congestion-controller"] = ib.CongestionControl
		}
		if tls.ServerName != "" {
			p["sni"] = tls.ServerName
		}
		if tls.ALPN != "" {
			p["alpn"] = strings.Split(tls.ALPN, ",")
		}
		return p
	}
	return nil
}

// BuildSingboxOutbound 生成 sing-box 客户端出站配置。
func BuildSingboxOutbound(server string, ib model.Inbound, cl model.Client) map[string]any {
	o := buildSingboxOutbound(server, ib, cl)
	if o != nil && ib.Insecure {
		if tls, ok := o["tls"].(map[string]any); ok {
			tls["insecure"] = true
		}
	}
	return o
}

func buildSingboxOutbound(server string, ib model.Inbound, cl model.Client) map[string]any {
	tls := ParseTLS(ib)
	tr := ParseTransport(ib)

	tlsObj := func(extra map[string]any) map[string]any {
		if !tls.Enabled {
			return nil
		}
		o := map[string]any{"enabled": true}
		if tls.ServerName != "" {
			o["server_name"] = tls.ServerName
		}
		if tls.ALPN != "" {
			o["alpn"] = strings.Split(tls.ALPN, ",")
		}
		if tls.Reality {
			o["reality"] = map[string]any{"enabled": true, "public_key": tls.PublicKey, "short_id": tls.ShortID}
			o["utls"] = map[string]any{"enabled": true, "fingerprint": orDefault(tls.Fingerprint, "chrome")}
		}
		for k, v := range extra {
			o[k] = v
		}
		return o
	}

	transportObj := func() map[string]any {
		switch tr.Type {
		case "ws":
			o := map[string]any{"type": "ws", "path": tr.Path}
			if tr.Host != "" {
				o["headers"] = map[string]any{"Host": tr.Host}
			}
			return o
		case "grpc":
			return map[string]any{"type": "grpc", "service_name": tr.ServiceName}
		case "http":
			o := map[string]any{"type": "http", "path": tr.Path}
			if tr.Host != "" {
				o["host"] = []string{tr.Host}
			}
			return o
		case "httpupgrade":
			o := map[string]any{"type": "httpupgrade", "path": tr.Path}
			if tr.Host != "" {
				o["host"] = tr.Host
			}
			return o
		}
		return nil
	}

	base := func(typ string) map[string]any {
		return map[string]any{"type": typ, "tag": cl.Name, "server": server, "server_port": ib.Port}
	}

	switch ib.Type {
	case model.ProtoVLESS:
		o := base("vless")
		o["uuid"] = cl.UUID
		if cl.Flow != "" {
			o["flow"] = cl.Flow
		}
		if t := tlsObj(nil); t != nil {
			o["tls"] = t
		}
		if t := transportObj(); t != nil {
			o["transport"] = t
		}
		return o

	case model.ProtoVMess:
		o := base("vmess")
		o["uuid"] = cl.UUID
		o["security"] = "auto"
		o["alter_id"] = 0
		if t := tlsObj(nil); t != nil {
			o["tls"] = t
		}
		if t := transportObj(); t != nil {
			o["transport"] = t
		}
		return o

	case model.ProtoTrojan:
		o := base("trojan")
		o["password"] = cl.Password
		if t := tlsObj(nil); t != nil {
			o["tls"] = t
		}
		if t := transportObj(); t != nil {
			o["transport"] = t
		}
		return o

	case model.ProtoShadowsocks:
		o := base("shadowsocks")
		o["method"] = ib.Method
		o["password"] = cl.Password
		return o

	case model.ProtoHysteria2:
		o := base("hysteria2")
		o["password"] = cl.Password
		if ib.ObfsPassword != "" {
			o["obfs"] = map[string]any{"type": orDefault(ib.ObfsType, "salamander"), "password": ib.ObfsPassword}
		}
		// 弱网：sing-box 客户端用整数 up_mbps / down_mbps，填了才会启用 Brutal。
		if ib.UpMbps > 0 {
			o["up_mbps"] = ib.UpMbps
		}
		if ib.DownMbps > 0 {
			o["down_mbps"] = ib.DownMbps
		}
		if t := tlsObj(nil); t != nil {
			o["tls"] = t
		}
		return o

	case model.ProtoSkyHorse:
		o := base("skyhorse")
		o["uuid"] = cl.UUID
		o["password"] = cl.Password
		o["obfs_password"] = ib.ObfsPassword
		if extra := parseObj(ib.Extra); extra != nil {
			if v, ok := extra["obfs_rotation_seconds"]; ok {
				o["obfs_rotation_seconds"] = v
			}
			if v, ok := extra["obfs_tolerance_seconds"]; ok {
				o["obfs_tolerance_seconds"] = v
			}
		}
		if ib.UpMbps > 0 {
			o["up_mbps"] = ib.UpMbps
		}
		if ib.DownMbps > 0 {
			o["down_mbps"] = ib.DownMbps
		}
		if t := tlsObj(nil); t != nil {
			o["tls"] = t
		}
		return o
	case model.ProtoTUIC:
		o := base("tuic")
		o["uuid"] = cl.UUID
		o["password"] = cl.Password
		if ib.CongestionControl != "" {
			o["congestion_control"] = ib.CongestionControl
		}
		if t := tlsObj(nil); t != nil {
			o["tls"] = t
		}
		return o
	}
	return nil
}

// nodeName 生成节点名：单入站保持用户名，多入站追加入站标识以免重名。
func nodeName(base string, ib model.Inbound, multi bool) string {
	if !multi {
		return base
	}
	return base + "-" + ib.Tag
}

// BuildShareLinks 生成该用户在全部生效入站上的分享链接（一个用户多节点）。
func BuildShareLinks(server string, ibs []model.Inbound, cl model.Client) []string {
	multi := len(ibs) > 1
	out := make([]string, 0, len(ibs))
	for _, ib := range ibs {
		c := cl
		c.Name = nodeName(cl.Name, ib, multi)
		if l := BuildShareLink(server, ib, c); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// BehaviorMask 行为抹除参数：把 TCP 系协议的客户端出站统一改成多路复用。
// 目的不是省 RTT，而是抹掉「连接数 / 每秒建连数 / 反复 TLS 握手」这三个最容易被 QoS 盯上的指纹。
type BehaviorMask struct {
	Enabled    bool
	MaxStreams int  // 每连接并发流上限，<=0 用默认值
	Padding    bool // 填充帧长，抹掉包长分布指纹
}

func (m BehaviorMask) streams() int {
	if m.MaxStreams > 0 {
		return m.MaxStreams
	}
	return 8
}

// muxSupported 只有 TCP 系协议能加多路复用；QUIC 系（hysteria2 / tuic）本来就把流复用在一条连接上，
// HTTP 代理没有对应的多路复用扩展。
func muxSupported(protocol string) bool {
	switch protocol {
	// sing-box 的协议名是 shadowsocks，Clash / Mihomo 用的是 ss。
	case "vless", "vmess", "trojan", "shadowsocks", "ss":
		return true
	}
	return false
}

// applySingboxMux 给 sing-box 客户端出站加 multiplex（行为抹除）。
func applySingboxMux(out map[string]any, m BehaviorMask) {
	if !m.Enabled || out == nil {
		return
	}
	if t, _ := out["type"].(string); !muxSupported(t) {
		return
	}
	obj := map[string]any{
		"enabled":     true,
		"protocol":    "smux",
		"max_streams": m.streams(),
	}
	if m.Padding {
		obj["padding"] = true
	}
	out["multiplex"] = obj
}

// applyClashSmux 给 Clash(Meta) / mihomo 的代理节点加 smux（行为抹除）。
func applyClashSmux(proxy map[string]any, m BehaviorMask) {
	if !m.Enabled || proxy == nil {
		return
	}
	if t, _ := proxy["type"].(string); !muxSupported(t) {
		return
	}
	obj := map[string]any{"enabled": true, "protocol": "smux"}
	if m.Padding {
		obj["padding"] = true
	}
	proxy["smux"] = obj
}

// BuildSingboxClientConfig 生成可直接导入 sing-box 客户端的完整配置文件。
// 包含 tun + 本地 mixed 入站、DNS、路由与出站，开箱即用。
// 多入站时在出站里追加一个 urltest 自动选速分组（tag 仍为 proxy）。
func BuildSingboxClientConfig(server string, ibs []model.Inbound, cl model.Client, mask BehaviorMask) map[string]any {
	multi := len(ibs) > 1
	nodes := make([]any, 0, len(ibs))
	tags := make([]string, 0, len(ibs))
	for _, ib := range ibs {
		c := cl
		c.Name = nodeName(cl.Name, ib, multi)
		ob := BuildSingboxOutbound(server, ib, c)
		if ob == nil {
			continue
		}
		tag := "proxy"
		if multi {
			tag = c.Name
		}
		ob["tag"] = tag
		applySingboxMux(ob, mask)
		nodes = append(nodes, ob)
		tags = append(tags, tag)
	}
	if len(nodes) == 0 {
		return nil
	}

	outbounds := append([]any{}, nodes...)
	if multi {
		outbounds = append(outbounds, map[string]any{
			"type":      "urltest",
			"tag":       "proxy",
			"outbounds": tags,
			"url":       "http://www.gstatic.com/generate_204",
			"interval":  "3m",
			"tolerance": 50,
		})
	}
	outbounds = append(outbounds, map[string]any{"type": "direct", "tag": "direct"})

	return map[string]any{
		"log": map[string]any{
			"level":     "info",
			"timestamp": true,
		},
		"dns": map[string]any{
			"servers": []any{
				// 系统解析器：用于引导解析代理服务器域名，避免与代理出站形成循环。
				map[string]any{"type": "local", "tag": "dns-direct"},
				map[string]any{"type": "https", "tag": "dns-proxy", "server": "1.1.1.1", "detour": "proxy"},
			},
			"final":    "dns-proxy",
			"strategy": "prefer_ipv4",
		},
		"inbounds": []any{
			map[string]any{
				"type":         "tun",
				"tag":          "tun-in",
				"address":      []string{"172.19.0.1/30", "fdfe:dcba:9876::1/126"},
				"mtu":          9000,
				"auto_route":   true,
				"strict_route": true,
				"stack":        "mixed",
			},
			map[string]any{
				"type":        "mixed",
				"tag":         "mixed-in",
				"listen":      "127.0.0.1",
				"listen_port": 2080,
			},
		},
		"outbounds": outbounds,
		"route": map[string]any{
			"rules": []any{
				map[string]any{"action": "sniff"},
				map[string]any{"protocol": "dns", "action": "hijack-dns"},
				map[string]any{"ip_is_private": true, "outbound": "direct"},
			},
			"final":                   "proxy",
			"auto_detect_interface":   true,
			"default_domain_resolver": "dns-direct",
		},
		"experimental": map[string]any{
			"cache_file": map[string]any{"enabled": true},
		},
	}
}

// BuildClashConfig 生成可直接导入 Clash.Meta / mihomo 的完整配置文件。
// 多入站时追加一个 url-test 自动测速分组 AUTO，并放进 PROXY 组。
func BuildClashConfig(server string, ibs []model.Inbound, cl model.Client, mask BehaviorMask) map[string]any {
	multi := len(ibs) > 1
	proxies := make([]any, 0, len(ibs))
	names := make([]string, 0, len(ibs))
	for _, ib := range ibs {
		c := cl
		c.Name = nodeName(cl.Name, ib, multi)
		p := BuildClashProxy(server, ib, c)
		if p == nil {
			continue
		}
		applyClashSmux(p, mask)
		proxies = append(proxies, p)
		names = append(names, c.Name)
	}
	if len(proxies) == 0 {
		return nil
	}

	groups := make([]any, 0, 2)
	selectItems := append([]string{}, names...)
	if multi {
		groups = append(groups, map[string]any{
			"name":      "AUTO",
			"type":      "url-test",
			"proxies":   names,
			"url":       "http://www.gstatic.com/generate_204",
			"interval":  300,
			"tolerance": 50,
		})
		selectItems = append([]string{"AUTO"}, selectItems...)
	}
	groups = append(groups, map[string]any{
		"name":    "PROXY",
		"type":    "select",
		"proxies": selectItems,
	})

	return map[string]any{
		"mixed-port":   7890,
		"allow-lan":    false,
		"mode":         "rule",
		"log-level":    "info",
		"proxies":      proxies,
		"proxy-groups": groups,
		"rules": []string{
			"GEOIP,CN,DIRECT",
			"MATCH,PROXY",
		},
	}
}
