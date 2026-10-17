package api

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"horseui/back-machine/internal/model"
)

// 连接数诊断的参考阈值。运营商策略差异很大，这里只用来判断「明显偏高」。
const (
	connWatchConns  = 200 // 单个来源 IP 的并发连接数
	connHighConns   = 500 // 超过它基本可以确定在按连接数限速
	connWatchPerMin = 120 // 单个来源 IP 最近一分钟新建连接数（约 2/s）
	connHighPerMin  = 300 // 约 5/s
)

// connRow 单个来源 IP 的连接统计。
type connRow struct {
	SourceIP   string   `json:"source_ip"`
	Conns      int      `json:"conns"`
	NewLastMin int      `json:"new_last_min"`
	Level      string   `json:"level"` // ok / watch / high
	TopHosts   []string `json:"top_hosts"`
}

// connReport 连接数诊断结果。
type connReport struct {
	Supported  bool      `json:"supported"`
	Reason     string    `json:"reason"`
	Total      int       `json:"total"`
	HasStart   bool      `json:"has_start"` // 内核是否返回了连接建立时间（否则新建连接数不可用）
	MaskOn     bool      `json:"mask_on"`   // 行为抹除当前是否已开启
	WatchConns int       `json:"watch_conns"`
	HighConns  int       `json:"high_conns"`
	UpdatedAt  time.Time `json:"updated_at"`
	Users      []connRow `json:"users"`
	Advice     string    `json:"advice"`
}

// statsConns 连接数诊断：按来源 IP 统计并发连接数与建连速率。
// 依据是「高并发 TCP 被运营商按连接数 / CPS 限速」这一常见现象，
// 用来判断线路变慢到底是连接数被限、还是带宽/BDP 不足。
func (a *App) statsConns(c *gin.Context) {
	rep := connReport{
		WatchConns: connWatchConns,
		HighConns:  connHighConns,
		MaskOn:     strings.TrimSpace(a.settings.Get(model.SettingBehaviorMask)) == "1",
		UpdatedAt:  time.Now(),
		Users:      []connRow{},
	}

	if !a.core.Proc.Running() {
		rep.Reason = "内核未运行，读不到活动连接"
		ok(c, rep)
		return
	}

	conns, err := a.clashClient().Connections(context.Background())
	if err != nil {
		rep.Reason = "Clash API 不可用（可能未启用 clash_api）：" + err.Error()
		ok(c, rep)
		return
	}

	rep.Supported = true
	rep.Total = len(conns)

	type agg struct {
		conns   int
		newLast int
		hosts   map[string]int
	}
	byIP := make(map[string]*agg, len(conns))
	now := time.Now()
	for _, cn := range conns {
		ip := cn.SourceIP
		if ip == "" {
			ip = "未知来源"
		}
		g := byIP[ip]
		if g == nil {
			g = &agg{hosts: map[string]int{}}
			byIP[ip] = g
		}
		g.conns++
		if !cn.Start.IsZero() {
			rep.HasStart = true
			if now.Sub(cn.Start) <= time.Minute {
				g.newLast++
			}
		}
		host := cn.Host
		if host == "" {
			host = "未知目标"
		}
		g.hosts[host]++
	}

	rows := make([]connRow, 0, len(byIP))
	for ip, g := range byIP {
		rows = append(rows, connRow{
			SourceIP:   ip,
			Conns:      g.conns,
			NewLastMin: g.newLast,
			Level:      connLevel(g.conns, g.newLast),
			TopHosts:   topHosts(g.hosts, 3),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Conns != rows[j].Conns {
			return rows[i].Conns > rows[j].Conns
		}
		return rows[i].NewLastMin > rows[j].NewLastMin
	})
	rep.Users = rows
	rep.Advice = connAdvice(rows, rep.MaskOn)
	ok(c, rep)
}

// connLevel 按并发连接数与建连速率给出风险等级。
func connLevel(conns, newLastMin int) string {
	switch {
	case conns >= connHighConns || newLastMin >= connHighPerMin:
		return "high"
	case conns >= connWatchConns || newLastMin >= connWatchPerMin:
		return "watch"
	default:
		return "ok"
	}
}

// connAdvice 根据最差的那一行给出可执行的建议。
func connAdvice(rows []connRow, maskOn bool) string {
	worst := "ok"
	for _, r := range rows {
		if r.Level == "high" {
			worst = "high"
			break
		}
		if r.Level == "watch" {
			worst = "watch"
		}
	}

	switch worst {
	case "high":
		if maskOn {
			return "有来源 IP 的连接数 / 建连速率明显偏高。行为抹除已开启，可适当调小「每连接并发流」让多路复用更集中；若仍然慢，说明这条线路更可能是按 UDP 或按流量限速，建议改用 Hysteria2 并配服务端 Brutal。"
		}
		return "有来源 IP 的连接数 / 建连速率明显偏高，很可能正在被按「连接数 / 建连频率」限速。建议到「面板设置 → 行为抹除」开启全协议多路复用，或让用户改用 Hysteria2（QUIC 单连接复用流，天生没有这个指纹）。"
	case "watch":
		return "连接数偏高但还没到高危，多标签浏览、下载器都会推高它。如果同时明显变慢，可以先开行为抹除观察效果。"
	default:
		return "连接数在正常范围内。如果依然慢，更可能是带宽 / BDP 不足或线路丢包，而不是连接数被限。"
	}
}

// topHosts 取出现次数最多的前 n 个目标（便于定位是哪个应用在刷连接）。
func topHosts(m map[string]int, n int) []string {
	type kv struct {
		host  string
		count int
	}
	list := make([]kv, 0, len(m))
	for h, c := range m {
		list = append(list, kv{host: h, count: c})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].count != list[j].count {
			return list[i].count > list[j].count
		}
		return list[i].host < list[j].host
	})
	if len(list) > n {
		list = list[:n]
	}
	out := make([]string, 0, len(list))
	for _, it := range list {
		out = append(out, it.host)
	}
	return out
}
