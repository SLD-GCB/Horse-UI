package api

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"horseui/back-machine/internal/model"
	"horseui/back-machine/internal/sysinfo"
)

// panelPortRow 面板入站占用的端口。
type panelPortRow struct {
	Port int    `json:"port"`
	Tag  string `json:"tag"`
	Type string `json:"type"`
}

// listenPortRow 本机正在监听的端口。
type listenPortRow struct {
	Port  int      `json:"port"`
	Proto []string `json:"proto"`
	Panel bool     `json:"panel"`
}

// portPool 返回本机网卡、端口占用情况与可用端口池。
func (a *App) portPool(c *gin.Context) {
	start := parseIntDefault(a.settings.GetDefault(model.SettingPortStart, "10000"), 10000)
	end := parseIntDefault(a.settings.GetDefault(model.SettingPortEnd, "60000"), 60000)

	listening := sysinfo.ListeningPorts()

	var inbounds []model.Inbound
	a.db.Order("port").Find(&inbounds)

	panelPorts := make(map[int]bool, len(inbounds))
	occupied := make(map[int]bool, len(listening)+len(inbounds))
	for p := range listening {
		occupied[p] = true
	}
	panel := make([]panelPortRow, 0, len(inbounds))
	lastPort := 0
	for _, ib := range inbounds {
		panel = append(panel, panelPortRow{Port: ib.Port, Tag: ib.Tag, Type: ib.Type})
		panelPorts[ib.Port] = true
		occupied[ib.Port] = true
		if ib.Port > lastPort {
			lastPort = ib.Port
		}
		for _, port := range inboundPortRange(ib) {
			panelPorts[port] = true
			occupied[port] = true
			if port > lastPort {
				lastPort = port
			}
		}
	}
	nextStart, nextEnd := nextFreeRange(start, end, 10, occupied, lastPort)

	listenRows := make([]listenPortRow, 0, len(listening))
	for p, protos := range listening {
		listenRows = append(listenRows, listenPortRow{Port: p, Proto: protos, Panel: panelPorts[p]})
	}
	sort.Slice(listenRows, func(i, j int) bool { return listenRows[i].Port < listenRows[j].Port })

	free := sysinfo.FreePorts(start, end, 60, occupied)
	suggested := 0
	if len(free) > 0 {
		suggested = free[0]
	}

	ok(c, gin.H{
		"range":      gin.H{"start": start, "end": end},
		"interfaces": sysinfo.Interfaces(),
		"listening":  listenRows,
		"panel":      panel,
		"free":       free,
		"suggested":  suggested,
		"next_range": gin.H{"start": nextStart, "end": nextEnd},
	})
}

func nextFreeRange(start, end, size int, occupied map[int]bool, after int) (int, int) {
	if size <= 0 {
		size = 1
	}
	search := func(from int) (int, int) {
		for candidate := from; candidate+size-1 <= end; candidate++ {
			ok := true
			for port := candidate; port < candidate+size; port++ {
				if occupied[port] {
					ok = false
					break
				}
			}
			if ok {
				return candidate, candidate + size - 1
			}
		}
		return 0, 0
	}
	from := start
	if after >= start && after < end {
		from = after + 1
	}
	if first, last := search(from); first != 0 {
		return first, last
	}
	return search(start)
}

func parseIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}

// inboundPortRange 返回入站声明占用的额外端口，支持 start:end、start-end 和逗号列表。
func inboundPortRange(ib model.Inbound) []int {
	raw := strings.TrimSpace(string(ib.Extra))
	if raw == "" || raw == "null" {
		return nil
	}
	var extra map[string]any
	if err := json.Unmarshal([]byte(raw), &extra); err != nil {
		return nil
	}
	text := ""
	for _, key := range []string{"port_range", "listen_port_range"} {
		if value, ok := extra[key].(string); ok && strings.TrimSpace(value) != "" {
			text = value
			break
		}
	}
	if text == "" {
		return nil
	}
	seen := make(map[int]bool)
	var ports []int
	add := func(port int) {
		if port < 1 || port > 65535 || seen[port] || len(ports) >= 256 {
			return
		}
		seen[port] = true
		ports = append(ports, port)
	}
	for _, part := range strings.FieldsFunc(text, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	}) {
		start, end, ok := strings.Cut(part, ":")
		if !ok {
			start, end, ok = strings.Cut(part, "-")
		}
		if ok {
			first, err1 := strconv.Atoi(start)
			last, err2 := strconv.Atoi(end)
			if err1 != nil || err2 != nil {
				continue
			}
			if first > last {
				first, last = last, first
			}
			for port := first; port <= last; port++ {
				add(port)
			}
			continue
		}
		if port, err := strconv.Atoi(part); err == nil {
			add(port)
		}
	}
	return ports
}
