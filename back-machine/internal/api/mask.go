package api

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"

	"horseui/back-machine/internal/model"
	"horseui/back-machine/internal/util"
)

// maskUDP 给所有未配置混淆的 Hysteria2 入站启用加密混淆。
// sing-box 里唯一能真正抹掉 UDP/QUIC 特征的手段就是 Hysteria2 的 obfs：
// 它把 QUIC 报文整体打乱，DPI 既看不到 QUIC 结构，也拿不到包长分布。TUIC 没有等价字段。
// 该操作会改变服务端配置，旧订阅必须重新导入，所以只由用户显式触发。
func (a *App) maskUDP(c *gin.Context) {
	var hy2 []model.Inbound
	if err := a.db.Where("type = ?", model.ProtoHysteria2).Find(&hy2).Error; err != nil {
		serverError(c, err)
		return
	}

	changed := 0
	for i := range hy2 {
		ib := &hy2[i]
		if strings.TrimSpace(ib.ObfsPassword) != "" {
			continue
		}
		obfsType := strings.TrimSpace(ib.ObfsType)
		if obfsType == "" {
			obfsType = "salamander"
		}
		if err := a.db.Model(&model.Inbound{}).Where("id = ?", ib.ID).Updates(map[string]any{
			"obfs_type":     obfsType,
			"obfs_password": util.RandomAlnum(20),
		}).Error; err != nil {
			serverError(c, err)
			return
		}
		changed++
	}

	var tuicCount int64
	a.db.Model(&model.Inbound{}).Where("type = ?", model.ProtoTUIC).Count(&tuicCount)

	msg := "所有 Hysteria2 入站都已启用混淆，无需改动"
	if changed > 0 {
		msg = fmt.Sprintf("已为 %d 个 Hysteria2 入站启用混淆，客户端需重新导入订阅", changed)
	}
	if tuicCount > 0 {
		msg += fmt.Sprintf("；另有 %d 个 TUIC 入站无法混淆（sing-box 未提供该字段）", tuicCount)
	}

	payload := gin.H{"changed": changed, "tuic": tuicCount, "message": msg}
	if changed == 0 {
		ok(c, payload)
		return
	}
	a.afterApply(c, payload)
}
