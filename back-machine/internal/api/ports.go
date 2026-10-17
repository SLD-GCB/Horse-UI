package api

import (
	"sort"
	"strconv"

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
	for _, ib := range inbounds {
		panel = append(panel, panelPortRow{Port: ib.Port, Tag: ib.Tag, Type: ib.Type})
		panelPorts[ib.Port] = true
		occupied[ib.Port] = true
	}

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
	})
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
