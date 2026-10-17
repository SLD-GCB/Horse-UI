package api

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"

	"horseui/back-machine/internal/model"
	"horseui/back-machine/internal/svc"
	"horseui/back-machine/internal/sysinfo"
	"horseui/back-machine/internal/util"
)

// batchErr 批量操作里单条的失败信息。
type batchErr struct {
	Target  string `json:"target"`
	Message string `json:"message"`
}

// pickFreePorts 从 start 起找 n 个可用端口（跳过本机监听与已有入站）。
func (a *App) pickFreePorts(start, n int) ([]int, error) {
	if start <= 0 {
		start = parseIntDefault(a.settings.GetDefault(model.SettingPortStart, "10000"), 10000)
	}
	end := parseIntDefault(a.settings.GetDefault(model.SettingPortEnd, "60000"), 60000)
	if end < start || end > 65535 {
		end = 65535
	}

	occupied := map[int]bool{}
	for p := range sysinfo.ListeningPorts() {
		occupied[p] = true
	}
	var existing []model.Inbound
	a.db.Find(&existing)
	for _, ib := range existing {
		occupied[ib.Port] = true
	}

	out := make([]int, 0, n)
	for p := start; p <= end && len(out) < n; p++ {
		if occupied[p] {
			continue
		}
		occupied[p] = true
		out = append(out, p)
	}
	if len(out) < n {
		return nil, fmt.Errorf("可用端口不足：需要 %d 个，只找到 %d 个（可在「面板设置 → 端口池区间」里放宽）", n, len(out))
	}
	return out, nil
}

// batchCreateInbounds 按协议组合一次性创建多条入站。
// 采用「逐条创建 + 汇总失败原因」的方式：某一条失败不影响其他条，方便部分成功后再补。
func (a *App) batchCreateInbounds(c *gin.Context) {
	var req struct {
		Types     []string `json:"types"`
		PortStart int      `json:"port_start"`
		TagPrefix string   `json:"tag_prefix"`
		Listen    string   `json:"listen"`
		CertID    *uint    `json:"cert_id"`
		WeakNet   bool     `json:"weak_net"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err.Error())
		return
	}

	// 去重并过滤空值。
	types := make([]string, 0, len(req.Types))
	seen := map[string]bool{}
	for _, t := range req.Types {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		types = append(types, t)
	}
	if len(types) == 0 {
		badRequest(c, "请至少选择一种协议")
		return
	}
	if len(types) > 20 {
		badRequest(c, "一次最多创建 20 条入站")
		return
	}

	var cert model.Certificate
	if req.CertID != nil && *req.CertID > 0 {
		if err := a.db.First(&cert, *req.CertID).Error; err != nil {
			badRequest(c, "所选证书不存在")
			return
		}
	}

	ports, err := a.pickFreePorts(req.PortStart, len(types))
	if err != nil {
		badRequest(c, err.Error())
		return
	}

	prefix := strings.TrimSpace(req.TagPrefix)
	created := make([]model.Inbound, 0, len(types))
	var errs []batchErr

	for i, tp := range types {
		ib := model.Inbound{
			Type:   tp,
			Enable: true,
			Listen: req.Listen,
			Port:   ports[i],
			Sniff:  true,
			Remark: "批量创建",
		}
		if ib.Listen == "" {
			ib.Listen = "::"
		}
		if prefix != "" {
			ib.Tag = prefix + "-" + tp
		}

		// 统一证书：除 Shadowsocks 外都必须有 TLS（或 Reality）。
		if tp != model.ProtoShadowsocks && req.CertID != nil && *req.CertID > 0 {
			ib.CertID = req.CertID
			ib.Insecure = cert.Kind == model.CertKindSelfSigned
			if cert.Domain != "" {
				ib.TLS = model.JSONText(fmt.Sprintf(`{"enabled":true,"server_name":%q}`, cert.Domain))
			} else {
				ib.TLS = model.JSONText(`{"enabled":true}`)
			}
		}

		// 弱网预置：只做该协议真正有效的项。
		if req.WeakNet {
			switch tp {
			case model.ProtoHysteria2:
				ib.ObfsType = "salamander"
				ib.ObfsPassword = util.RandomAlnum(20)
			case model.ProtoTUIC:
				ib.CongestionControl = "bbr"
			case model.ProtoVLESS, model.ProtoVMess, model.ProtoTrojan:
				ib.Extra = model.JSONText(`{"tcp_fast_open":true}`)
			}
		}

		if err := a.prepareInbound(&ib); err != nil {
			errs = append(errs, batchErr{Target: tp, Message: err.Error()})
			continue
		}
		if err := a.db.Create(&ib).Error; err != nil {
			errs = append(errs, batchErr{Target: tp, Message: err.Error()})
			continue
		}
		created = append(created, ib)
	}

	if len(created) == 0 {
		msg := "全部创建失败"
		if len(errs) > 0 {
			msg += "：" + errs[0].Message
		}
		badRequest(c, msg)
		return
	}
	a.afterApply(c, gin.H{
		"created":  len(created),
		"inbounds": created,
		"errors":   errs,
	})
}

// batchInboundOp 批量启用 / 停用 / 删除 / 重排端口。
func (a *App) batchInboundOp(c *gin.Context) {
	var req struct {
		IDs       []uint `json:"ids"`
		Action    string `json:"action"`
		PortStart int    `json:"port_start"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err.Error())
		return
	}
	if len(req.IDs) == 0 {
		badRequest(c, "请先选择入站")
		return
	}

	changed := 0
	switch req.Action {
	case "enable", "disable":
		on := req.Action == "enable"
		if err := a.db.Model(&model.Inbound{}).Where("id IN ?", req.IDs).Update("enable", on).Error; err != nil {
			serverError(c, err)
			return
		}
		changed = len(req.IDs)

	case "delete":
		for _, id := range req.IDs {
			if err := a.db.Where("inbound_id = ?", id).Delete(&model.Client{}).Error; err != nil {
				serverError(c, err)
				return
			}
			a.db.Where("inbound_id = ?", id).Delete(&model.ClientInbound{})
			if err := a.db.Delete(&model.Inbound{}, id).Error; err != nil {
				serverError(c, err)
				return
			}
			changed++
		}

	case "renumber":
		// 按顺序重排端口，tag 保持不变（改 tag 会让已下发的订阅失配）。
		ports, err := a.pickFreePorts(req.PortStart, len(req.IDs))
		if err != nil {
			badRequest(c, err.Error())
			return
		}
		for i, id := range req.IDs {
			if err := a.db.Model(&model.Inbound{}).Where("id = ?", id).Update("port", ports[i]).Error; err != nil {
				serverError(c, err)
				return
			}
			changed++
		}

	default:
		badRequest(c, "action 只能是 enable / disable / delete / renumber")
		return
	}

	a.afterApply(c, gin.H{"changed": changed})
}

// batchOutboundOp 批量启用 / 停用 / 删除出站。
func (a *App) batchOutboundOp(c *gin.Context) {
	var req struct {
		IDs    []uint `json:"ids"`
		Action string `json:"action"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err.Error())
		return
	}
	if len(req.IDs) == 0 {
		badRequest(c, "请先选择出站")
		return
	}

	switch req.Action {
	case "enable", "disable":
		on := req.Action == "enable"
		if err := a.db.Model(&model.Outbound{}).Where("id IN ?", req.IDs).Update("enable", on).Error; err != nil {
			serverError(c, err)
			return
		}
	case "delete":
		if err := a.db.Where("id IN ?", req.IDs).Delete(&model.Outbound{}).Error; err != nil {
			serverError(c, err)
			return
		}
	default:
		badRequest(c, "action 只能是 enable / disable / delete")
		return
	}

	a.afterApply(c, gin.H{"changed": len(req.IDs)})
}

// batchImportOutbounds 粘贴多行分享链接，批量创建出站。
func (a *App) batchImportOutbounds(c *gin.Context) {
	var req struct {
		Links string `json:"links"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err.Error())
		return
	}

	lines := make([]string, 0, 8)
	for _, line := range strings.Split(req.Links, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		badRequest(c, "请粘贴至少一条分享链接")
		return
	}
	if len(lines) > 100 {
		badRequest(c, "一次最多导入 100 条")
		return
	}

	created := make([]model.Outbound, 0, len(lines))
	var errs []batchErr

	for i, line := range lines {
		name := fmt.Sprintf("第 %d 行", i+1)
		ob, err := svc.ParseShareLink(line)
		if err != nil {
			errs = append(errs, batchErr{Target: name, Message: err.Error()})
			continue
		}
		ob.ID = 0
		if err := a.prepareOutbound(ob); err != nil {
			errs = append(errs, batchErr{Target: name, Message: err.Error()})
			continue
		}
		if err := a.db.Create(ob).Error; err != nil {
			errs = append(errs, batchErr{Target: name, Message: err.Error()})
			continue
		}
		created = append(created, *ob)
	}

	if len(created) == 0 {
		msg := "全部导入失败"
		if len(errs) > 0 {
			msg += "：" + errs[0].Message
		}
		badRequest(c, msg)
		return
	}
	a.afterApply(c, gin.H{
		"created":   len(created),
		"outbounds": created,
		"errors":    errs,
	})
}
