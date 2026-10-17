package api

import (
	"github.com/gin-gonic/gin"

	"horseui/back-machine/internal/model"
	"horseui/back-machine/internal/svc"
)

// inboundTrafficRow 入站流量行。
type inboundTrafficRow struct {
	Tag     string `json:"tag"`
	Up      int64  `json:"up"`
	Down    int64  `json:"down"`
	Bytes   int64  `json:"bytes"`
	Clients int    `json:"clients"` // 绑定的用户数
}

// statsSummary 返回当前统计方式与各入站累计流量。
func (a *App) statsSummary(c *gin.Context) {
	mode := a.core.EffectiveStatsMode()

	var inbounds []model.Inbound
	a.db.Order("(used_up + used_down) desc").Find(&inbounds)

	bindMap, _ := svc.BoundClients(a.db)

	rows := make([]inboundTrafficRow, 0, len(inbounds))
	for _, ib := range inbounds {
		rows = append(rows, inboundTrafficRow{
			Tag:     ib.Tag,
			Up:      ib.UsedUp,
			Down:    ib.UsedDown,
			Bytes:   ib.UsedUp + ib.UsedDown,
			Clients: len(bindMap[ib.ID]),
		})
	}

	ok(c, gin.H{
		"mode":     mode,
		"per_user": mode == "v2ray",
		"split":    mode != "v2ray",
		"inbounds": rows,
	})
}
