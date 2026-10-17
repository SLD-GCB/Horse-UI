package api

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"

	"horseui/back-machine/internal/model"
	"horseui/back-machine/internal/svc"
	"horseui/back-machine/internal/sysinfo"
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

// batchCloneInbound 用同一份完整入站配置创建 N 条。
// 模板来自「新增入站」的完整表单，
// 所以 TLS / Reality / 传输 / 协议选项（包括高级 JSON）都会一并复制，只是端口顺延、标识加序号。
func (a *App) batchCloneInbound(c *gin.Context) {
	var req struct {
		Inbound model.Inbound `json:"inbound"`
		Count   int           `json:"count"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err.Error())
		return
	}

	count := req.Count
	if count <= 0 {
		count = 1
	}
	if count > 50 {
		badRequest(c, "一次最多创建 50 条入站")
		return
	}

	tpl := req.Inbound
	if strings.TrimSpace(tpl.Type) == "" {
		badRequest(c, "请选择协议类型")
		return
	}
	if tpl.Port <= 0 || tpl.Port > 65535 {
		badRequest(c, "请填写有效端口")
		return
	}

	ports, err := a.pickFreePorts(tpl.Port, count)
	if err != nil {
		badRequest(c, err.Error())
		return
	}

	baseTag := strings.TrimSpace(tpl.Tag)
	if baseTag == "" {
		baseTag = fmt.Sprintf("%s-%d", tpl.Type, tpl.Port)
	}

	created := make([]model.Inbound, 0, count)
	var errs []batchErr

	for i := 0; i < count; i++ {
		ib := tpl
		ib.ID = 0
		ib.Port = ports[i]
		if count > 1 {
			ib.Tag = fmt.Sprintf("%s-%d", baseTag, i+1)
		} else {
			ib.Tag = baseTag
		}

		if err := a.prepareInbound(&ib); err != nil {
			errs = append(errs, batchErr{Target: ib.Tag, Message: err.Error()})
			continue
		}
		if err := a.db.Create(&ib).Error; err != nil {
			errs = append(errs, batchErr{Target: ib.Tag, Message: err.Error()})
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
