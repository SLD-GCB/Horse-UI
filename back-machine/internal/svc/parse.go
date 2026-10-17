package svc

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"horseui/back-machine/internal/model"
)

// ParseShareLink 解析分享链接为出站模型（供「从分享链接导入」使用）。
func ParseShareLink(link string) (*model.Outbound, error) {
	link = strings.TrimSpace(link)
	if link == "" {
		return nil, fmt.Errorf("链接为空")
	}
	scheme := link
	if i := strings.Index(link, "://"); i >= 0 {
		scheme = strings.ToLower(link[:i])
	}
	switch scheme {
	case "vless":
		return parseVLESS(link)
	case "vmess":
		return parseVMess(link)
	case "trojan":
		return parseTrojan(link)
	case "ss":
		return parseShadowsocks(link)
	case "hysteria2", "hy2":
		return parseHysteria2(link)
	case "tuic":
		return parseTUIC(link)
	case "socks", "socks5":
		return parseSocks(link)
	case "http", "https":
		return parseHTTPOut(link)
	}
	return nil, fmt.Errorf("暂不支持该链接类型: %s", scheme)
}

// ---- 通用小工具（避免与包内已有函数重名）----

func toJSONText(v any) model.JSONText {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return model.JSONText(b)
}

func linkDecodeB64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding,
	} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, fmt.Errorf("base64 解码失败")
}

func linkInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(n))
		return i
	}
	return 0
}

func linkBool(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "1" || s == "true" || s == "yes"
}

func linkSplit(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func linkFirst(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// linkHostPort 从 URL 提取主机与端口。
func linkHostPort(u *url.URL, defPort string) (string, int) {
	host := u.Hostname()
	p := u.Port()
	if p == "" {
		p = defPort
	}
	port, _ := strconv.Atoi(p)
	return host, port
}

// linkTag 用 fragment 作为备注/tag，缺省用前缀+主机。
func linkTag(u *url.URL, prefix, host string) string {
	if f := strings.TrimSpace(u.Fragment); f != "" {
		if d, err := url.QueryUnescape(f); err == nil {
			f = d
		}
		return f
	}
	return prefix + "-" + host
}

// buildTLSFromQuery 依据分享链接 query 构造出站 TLS。
func buildTLSFromQuery(q url.Values) map[string]any {
	security := strings.ToLower(q.Get("security"))
	if security == "" || security == "none" {
		return nil
	}
	tls := map[string]any{"enabled": true}
	if sni := linkFirst(q.Get("sni"), q.Get("peer")); sni != "" {
		tls["server_name"] = sni
	}
	if alpn := q.Get("alpn"); alpn != "" {
		tls["alpn"] = linkSplit(alpn)
	}
	if linkBool(q.Get("allowInsecure")) || linkBool(q.Get("insecure")) {
		tls["insecure"] = true
	}
	if fp := q.Get("fp"); fp != "" {
		tls["utls"] = map[string]any{"enabled": true, "fingerprint": fp}
	}
	if security == "reality" {
		r := map[string]any{"enabled": true}
		if pbk := q.Get("pbk"); pbk != "" {
			r["public_key"] = pbk
		}
		if sid := q.Get("sid"); sid != "" {
			r["short_id"] = sid
		}
		// reality 客户端必须启用 uTLS（内核强制要求）。
		if tls["utls"] == nil {
			tls["utls"] = map[string]any{"enabled": true, "fingerprint": "chrome"}
		}
		tls["reality"] = r
	}
	return tls
}

// buildTransportFromQuery 依据分享链接 query 构造传输层。
func buildTransportFromQuery(q url.Values) map[string]any {
	switch strings.ToLower(q.Get("type")) {
	case "ws":
		tr := map[string]any{"type": "ws"}
		if p := q.Get("path"); p != "" {
			tr["path"] = p
		}
		if h := q.Get("host"); h != "" {
			tr["headers"] = map[string]any{"Host": h}
		}
		return tr
	case "grpc":
		tr := map[string]any{"type": "grpc"}
		if sn := linkFirst(q.Get("serviceName"), q.Get("servicename")); sn != "" {
			tr["service_name"] = sn
		}
		return tr
	case "http", "h2":
		tr := map[string]any{"type": "http"}
		if h := q.Get("host"); h != "" {
			tr["host"] = linkSplit(h)
		}
		if p := q.Get("path"); p != "" {
			tr["path"] = p
		}
		return tr
	case "httpupgrade":
		tr := map[string]any{"type": "httpupgrade"}
		if p := q.Get("path"); p != "" {
			tr["path"] = p
		}
		if h := q.Get("host"); h != "" {
			tr["host"] = h
		}
		return tr
	}
	return nil
}

// ---- 各协议 ----

func parseVLESS(link string) (*model.Outbound, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	host, port := linkHostPort(u, "443")
	uuid := ""
	if u.User != nil {
		uuid = u.User.Username()
	}
	if uuid == "" {
		return nil, fmt.Errorf("vless 链接缺少 uuid")
	}
	q := u.Query()
	cfg := map[string]any{"uuid": uuid}
	if f := q.Get("flow"); f != "" {
		cfg["flow"] = f
	}
	if pe := linkFirst(q.Get("packetEncoding"), q.Get("packet_encoding")); pe != "" {
		cfg["packet_encoding"] = pe
	}
	ob := &model.Outbound{
		Type: "vless", Server: host, Port: port,
		Tag: linkTag(u, "vless", host), Enable: true, Config: toJSONText(cfg),
	}
	if tls := buildTLSFromQuery(q); tls != nil {
		ob.TLS = toJSONText(tls)
	}
	if tr := buildTransportFromQuery(q); tr != nil {
		ob.Transport = toJSONText(tr)
	}
	return ob, nil
}

func parseVMess(link string) (*model.Outbound, error) {
	raw := strings.TrimPrefix(link, "vmess://")
	if i := strings.Index(raw, "#"); i >= 0 {
		raw = raw[:i]
	}
	dec, err := linkDecodeB64(raw)
	if err != nil {
		return nil, fmt.Errorf("vmess 链接 base64 解析失败")
	}
	var m map[string]any
	if err := json.Unmarshal(dec, &m); err != nil {
		return nil, fmt.Errorf("vmess 链接 JSON 解析失败")
	}
	host := getString(m, "add")
	port := linkInt(m["port"])
	id := getString(m, "id")
	if host == "" || id == "" {
		return nil, fmt.Errorf("vmess 链接缺少服务器或 uuid")
	}
	cfg := map[string]any{"uuid": id}
	if scy := getString(m, "scy"); scy != "" {
		cfg["security"] = scy
	}
	if aid := linkInt(m["aid"]); aid > 0 {
		cfg["alter_id"] = aid
	}
	ob := &model.Outbound{
		Type: "vmess", Server: host, Port: port,
		Tag:    linkFirst(getString(m, "ps"), "vmess-"+host),
		Enable: true, Config: toJSONText(cfg),
	}

	if strings.EqualFold(getString(m, "tls"), "tls") {
		tls := map[string]any{"enabled": true}
		if sni := getString(m, "sni"); sni != "" {
			tls["server_name"] = sni
		}
		if alpn := getString(m, "alpn"); alpn != "" {
			tls["alpn"] = linkSplit(alpn)
		}
		if fp := getString(m, "fp"); fp != "" {
			tls["utls"] = map[string]any{"enabled": true, "fingerprint": fp}
		}
		ob.TLS = toJSONText(tls)
	}

	switch strings.ToLower(getString(m, "net")) {
	case "ws":
		tr := map[string]any{"type": "ws"}
		if p := getString(m, "path"); p != "" {
			tr["path"] = p
		}
		if h := getString(m, "host"); h != "" {
			tr["headers"] = map[string]any{"Host": h}
		}
		ob.Transport = toJSONText(tr)
	case "grpc":
		tr := map[string]any{"type": "grpc"}
		if sn := getString(m, "path"); sn != "" {
			tr["service_name"] = sn
		}
		ob.Transport = toJSONText(tr)
	case "http", "h2":
		tr := map[string]any{"type": "http"}
		if h := getString(m, "host"); h != "" {
			tr["host"] = linkSplit(h)
		}
		if p := getString(m, "path"); p != "" {
			tr["path"] = p
		}
		ob.Transport = toJSONText(tr)
	case "httpupgrade":
		tr := map[string]any{"type": "httpupgrade"}
		if p := getString(m, "path"); p != "" {
			tr["path"] = p
		}
		if h := getString(m, "host"); h != "" {
			tr["host"] = h
		}
		ob.Transport = toJSONText(tr)
	}
	return ob, nil
}

func parseTrojan(link string) (*model.Outbound, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	host, port := linkHostPort(u, "443")
	pw := ""
	if u.User != nil {
		pw = u.User.Username()
	}
	if pw == "" {
		return nil, fmt.Errorf("trojan 链接缺少密码")
	}
	q := u.Query()
	ob := &model.Outbound{
		Type: "trojan", Server: host, Port: port,
		Tag: linkTag(u, "trojan", host), Enable: true,
		Config: toJSONText(map[string]any{"password": pw}),
	}
	tls := buildTLSFromQuery(q)
	if tls == nil {
		tls = map[string]any{"enabled": true}
	}
	if sni := q.Get("sni"); sni != "" {
		tls["server_name"] = sni
	}
	if linkBool(q.Get("allowInsecure")) {
		tls["insecure"] = true
	}
	ob.TLS = toJSONText(tls)
	if tr := buildTransportFromQuery(q); tr != nil {
		ob.Transport = toJSONText(tr)
	}
	return ob, nil
}

func parseShadowsocks(link string) (*model.Outbound, error) {
	raw := strings.TrimPrefix(link, "ss://")
	frag := ""
	if i := strings.Index(raw, "#"); i >= 0 {
		frag = raw[i+1:]
		raw = raw[:i]
	}
	if i := strings.Index(raw, "?"); i >= 0 {
		raw = raw[:i]
	}

	var method, password, host string
	var port int

	if strings.Contains(raw, "@") {
		// SIP002：base64(method:password)@host:port
		at := strings.LastIndex(raw, "@")
		cred := raw[:at]
		dec, err := linkDecodeB64(cred)
		if err != nil {
			dec = []byte(cred) // 少数客户端用明文
		}
		mp := strings.SplitN(string(dec), ":", 2)
		if len(mp) != 2 {
			return nil, fmt.Errorf("ss 链接凭据解析失败")
		}
		method, password = mp[0], mp[1]
		h, p, err := net.SplitHostPort(raw[at+1:])
		if err != nil {
			return nil, fmt.Errorf("ss 链接服务器解析失败")
		}
		host = h
		port, _ = strconv.Atoi(p)
	} else {
		// 旧格式：base64(method:password@host:port)
		dec, err := linkDecodeB64(raw)
		if err != nil {
			return nil, fmt.Errorf("ss 链接解析失败")
		}
		s := string(dec)
		at := strings.LastIndex(s, "@")
		if at < 0 {
			return nil, fmt.Errorf("ss 链接解析失败")
		}
		mp := strings.SplitN(s[:at], ":", 2)
		if len(mp) != 2 {
			return nil, fmt.Errorf("ss 链接凭据解析失败")
		}
		method, password = mp[0], mp[1]
		h, p, err := net.SplitHostPort(s[at+1:])
		if err != nil {
			return nil, fmt.Errorf("ss 链接服务器解析失败")
		}
		host = h
		port, _ = strconv.Atoi(p)
	}

	tag := "ss-" + host
	if frag != "" {
		if d, err := url.QueryUnescape(frag); err == nil {
			frag = d
		}
		tag = frag
	}
	cfg := map[string]any{"method": method, "password": password}
	return &model.Outbound{
		Type: "shadowsocks", Server: host, Port: port,
		Tag: tag, Enable: true, Config: toJSONText(cfg),
	}, nil
}

func parseHysteria2(link string) (*model.Outbound, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	host, port := linkHostPort(u, "443")
	pw := ""
	if u.User != nil {
		pw = u.User.Username()
		if p, ok := u.User.Password(); ok && p != "" {
			pw = p
		}
	}
	q := u.Query()
	cfg := map[string]any{}
	if pw != "" {
		cfg["password"] = pw
	}
	if obfs := q.Get("obfs"); obfs != "" {
		o := map[string]any{"type": obfs}
		if op := linkFirst(q.Get("obfs-password"), q.Get("obfs_password")); op != "" {
			o["password"] = op
		}
		cfg["obfs"] = o
	}
	ob := &model.Outbound{
		Type: "hysteria2", Server: host, Port: port,
		Tag: linkTag(u, "hy2", host), Enable: true, Config: toJSONText(cfg),
	}
	tls := map[string]any{"enabled": true}
	if sni := q.Get("sni"); sni != "" {
		tls["server_name"] = sni
	}
	if alpn := q.Get("alpn"); alpn != "" {
		tls["alpn"] = linkSplit(alpn)
	}
	if linkBool(q.Get("insecure")) {
		tls["insecure"] = true
	}
	ob.TLS = toJSONText(tls)
	return ob, nil
}

func parseTUIC(link string) (*model.Outbound, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	host, port := linkHostPort(u, "443")
	uuid, pw := "", ""
	if u.User != nil {
		uuid = u.User.Username()
		if p, ok := u.User.Password(); ok {
			pw = p
		}
	}
	if uuid == "" {
		return nil, fmt.Errorf("tuic 链接缺少 uuid")
	}
	q := u.Query()
	cfg := map[string]any{"uuid": uuid}
	if pw != "" {
		cfg["password"] = pw
	}
	if cc := q.Get("congestion_control"); cc != "" {
		cfg["congestion_control"] = cc
	}
	if rm := q.Get("udp_relay_mode"); rm != "" {
		cfg["udp_relay_mode"] = rm
	}
	ob := &model.Outbound{
		Type: "tuic", Server: host, Port: port,
		Tag: linkTag(u, "tuic", host), Enable: true, Config: toJSONText(cfg),
	}
	tls := map[string]any{"enabled": true}
	if sni := q.Get("sni"); sni != "" {
		tls["server_name"] = sni
	}
	if alpn := q.Get("alpn"); alpn != "" {
		tls["alpn"] = linkSplit(alpn)
	}
	if linkBool(q.Get("allow_insecure")) || linkBool(q.Get("insecure")) {
		tls["insecure"] = true
	}
	ob.TLS = toJSONText(tls)
	return ob, nil
}

func parseSocks(link string) (*model.Outbound, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	host, port := linkHostPort(u, "1080")
	cfg := map[string]any{"version": "5"}
	if u.User != nil {
		if n := u.User.Username(); n != "" {
			cfg["username"] = n
		}
		if p, ok := u.User.Password(); ok && p != "" {
			cfg["password"] = p
		}
	}
	return &model.Outbound{
		Type: "socks", Server: host, Port: port,
		Tag: linkTag(u, "socks", host), Enable: true, Config: toJSONText(cfg),
	}, nil
}

func parseHTTPOut(link string) (*model.Outbound, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	host, port := linkHostPort(u, "8080")
	cfg := map[string]any{}
	if u.User != nil {
		if n := u.User.Username(); n != "" {
			cfg["username"] = n
		}
		if p, ok := u.User.Password(); ok && p != "" {
			cfg["password"] = p
		}
	}
	ob := &model.Outbound{
		Type: "http", Server: host, Port: port,
		Tag: linkTag(u, "http", host), Enable: true, Config: toJSONText(cfg),
	}
	if u.Scheme == "https" {
		ob.TLS = toJSONText(map[string]any{"enabled": true})
	}
	return ob, nil
}
