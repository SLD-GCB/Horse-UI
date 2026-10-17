package stats

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// ClashClient 访问 sing-box 内置的 Clash API。
type ClashClient struct {
	base   string
	secret string
	http   *http.Client
}

// NewClashClient 创建 Clash API 客户端，addr 形如 127.0.0.1:9090。
func NewClashClient(addr, secret string) *ClashClient {
	base := addr
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	return &ClashClient{
		base:   strings.TrimSuffix(base, "/"),
		secret: secret,
		http:   &http.Client{Timeout: 5 * time.Second},
	}
}

// Connection 一条活动连接。
type Connection struct {
	ID       string
	User     string // 仅带 with_v2ray_api 的内核或部分实现会返回
	Inbound  string // 入站 tag
	SourceIP string // 客户端来源 IP（运营商侧看到的「一个用户」）
	Host     string // 目标域名；IP 直连时回退为目的地 IP
	Start    time.Time
	Upload   int64
	Download int64
}

func (c *ClashClient) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("clash api %s 返回 HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Connections 拉取当前全部活动连接。
func (c *ClashClient) Connections(ctx context.Context) ([]Connection, error) {
	var raw struct {
		Connections []json.RawMessage `json:"connections"`
	}
	if err := c.get(ctx, "/connections", &raw); err != nil {
		return nil, err
	}

	out := make([]Connection, 0, len(raw.Connections))
	for _, item := range raw.Connections {
		var conn struct {
			ID       string         `json:"id"`
			Upload   int64          `json:"upload"`
			Download int64          `json:"download"`
			Start    string         `json:"start"`
			Metadata map[string]any `json:"metadata"`
		}
		if err := json.Unmarshal(item, &conn); err != nil {
			continue
		}
		out = append(out, Connection{
			ID:       conn.ID,
			User:     metadataUser(conn.Metadata),
			Inbound:  metadataInbound(conn.Metadata),
			SourceIP: metadataString(conn.Metadata, "sourceIP"),
			Host:     metadataHost(conn.Metadata),
			Start:    parseStart(conn.Start),
			Upload:   conn.Upload,
			Download: conn.Download,
		})
	}
	return out, nil
}

// Version 读取 Clash API 版本，可用于连通性探测。
func (c *ClashClient) Version(ctx context.Context) (string, error) {
	var out struct {
		Version string `json:"version"`
	}
	if err := c.get(ctx, "/version", &out); err != nil {
		return "", err
	}
	return out.Version, nil
}

// DefaultTestURL 延迟测试使用的目标地址。
const DefaultTestURL = "http://www.gstatic.com/generate_204"

// ProxyDelay 通过 Clash API 测试指定出站的延迟（毫秒）。
func (c *ClashClient) ProxyDelay(ctx context.Context, name, testURL string, timeoutMS int) (int, error) {
	if testURL == "" {
		testURL = DefaultTestURL
	}
	if timeoutMS <= 0 {
		timeoutMS = 5000
	}
	path := fmt.Sprintf("/proxies/%s/delay?url=%s&timeout=%d",
		url.PathEscape(name), url.QueryEscape(testURL), timeoutMS)

	var out struct {
		Delay   int    `json:"delay"`
		Message string `json:"message"`
	}
	if err := c.get(ctx, path, &out); err != nil {
		return 0, err
	}
	if out.Message != "" {
		return 0, fmt.Errorf("%s", out.Message)
	}
	return out.Delay, nil
}

// ProxyNames 返回 Clash API 当前可见的代理/出站名（升序）。
// 用于诊断测速时「内核里没有该出站」的情况。
func (c *ClashClient) ProxyNames(ctx context.Context) ([]string, error) {
	var raw struct {
		Proxies map[string]json.RawMessage `json:"proxies"`
	}
	if err := c.get(ctx, "/proxies", &raw); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(raw.Proxies))
	for name := range raw.Proxies {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// metadataUser 兼容不同版本的用户字段名。
func metadataUser(m map[string]any) string {
	if m == nil {
		return ""
	}
	for _, k := range []string{"user", "inboundUser", "inbound_user"} {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// metadataString 读取 metadata 里的字符串字段。
func metadataString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

// metadataHost 目标域名；IP 直连（没有域名）时回退到目的地 IP。
func metadataHost(m map[string]any) string {
	if h := metadataString(m, "host"); h != "" {
		return h
	}
	return metadataString(m, "destinationIP")
}

// parseStart 解析 Clash API 的连接建立时间。
func parseStart(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

// metadataInbound 从 sing-box 的 type 字段解析入站 tag（形如 "vless/e2e-vless"）。
func metadataInbound(m map[string]any) string {
	if m == nil {
		return ""
	}
	v, ok := m["type"]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	if i := strings.Index(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// Delta 某次采样的流量增量。
type Delta struct {
	Up   int64
	Down int64
}

// Sample 一次采样的结果。
type Sample struct {
	Users    map[string]Delta // 用户名 -> 增量（内核不返回用户时为空）
	Inbounds map[string]Delta // 入站 tag -> 增量
}

type connSnapshot struct {
	user    string
	inbound string
	up      int64
	down    int64
}

// Tracker 依据活动连接列表计算各用户的流量增量。
// Clash API 只返回当前活动连接，因此需要高频采样并累计差值。
type Tracker struct {
	seen map[string]connSnapshot
}

// NewTracker 创建跟踪器。
func NewTracker() *Tracker {
	return &Tracker{seen: make(map[string]connSnapshot)}
}

// Reset 清空已见连接（内核重启后调用）。
func (t *Tracker) Reset() {
	t.seen = make(map[string]connSnapshot)
}

// Observe 用当前连接列表计算增量。
func (t *Tracker) Observe(conns []Connection) Sample {
	sample := Sample{
		Users:    make(map[string]Delta),
		Inbounds: make(map[string]Delta),
	}
	current := make(map[string]struct{}, len(conns))

	for _, c := range conns {
		if c.ID == "" {
			continue
		}
		current[c.ID] = struct{}{}

		prev, ok := t.seen[c.ID]
		var dUp, dDown int64
		if ok && prev.user == c.User && prev.inbound == c.Inbound {
			dUp, dDown = c.Upload-prev.up, c.Download-prev.down
		} else {
			// 新连接（或归属变更）：当前累计值即为该连接已产生的流量。
			dUp, dDown = c.Upload, c.Download
		}
		if dUp < 0 {
			dUp = c.Upload
		}
		if dDown < 0 {
			dDown = c.Download
		}

		if dUp > 0 || dDown > 0 {
			if c.User != "" {
				d := sample.Users[c.User]
				d.Up += dUp
				d.Down += dDown
				sample.Users[c.User] = d
			}
			if c.Inbound != "" {
				d := sample.Inbounds[c.Inbound]
				d.Up += dUp
				d.Down += dDown
				sample.Inbounds[c.Inbound] = d
			}
		}
		t.seen[c.ID] = connSnapshot{user: c.User, inbound: c.Inbound, up: c.Upload, down: c.Download}
	}

	// 连接已关闭，其流量已在上面的增量中计入，移除快照即可。
	for id := range t.seen {
		if _, ok := current[id]; !ok {
			delete(t.seen, id)
		}
	}
	return sample
}
