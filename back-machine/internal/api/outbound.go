package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"horseui/back-machine/internal/model"
	"horseui/back-machine/internal/svc"
	"horseui/core/stats"
)

func (a *App) listOutbounds(c *gin.Context) {
	var list []model.Outbound
	if err := a.db.Order("id").Find(&list).Error; err != nil {
		serverError(c, err)
		return
	}
	ok(c, list)
}

func (a *App) createOutbound(c *gin.Context) {
	var ob model.Outbound
	if err := c.ShouldBindJSON(&ob); err != nil {
		badRequest(c, err.Error())
		return
	}
	ob.ID = 0
	if err := a.prepareOutbound(&ob); err != nil {
		badRequest(c, err.Error())
		return
	}
	if err := a.db.Create(&ob).Error; err != nil {
		serverError(c, err)
		return
	}
	a.afterApply(c, gin.H{"outbound": ob})
}

// batchOutboundPayload 批量创建出站的请求体：以一份出站配置为模板。
type batchOutboundPayload struct {
	model.Outbound
	Count    int `json:"count"`
	PortStep int `json:"port_step"`
}

const batchOutboundLimit = 200

// batchCreateOutbounds 按模板批量创建出站，端口依次递增，标识支持 {n} 占位。
func (a *App) batchCreateOutbounds(c *gin.Context) {
	var req batchOutboundPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err.Error())
		return
	}
	if req.Count < 1 || req.Count > batchOutboundLimit {
		badRequest(c, fmt.Sprintf("单次批量创建数量需在 1-%d 之间", batchOutboundLimit))
		return
	}
	step := req.PortStep
	if step <= 0 {
		step = 1
	}
	if step > 1000 {
		badRequest(c, "端口递增步长不能超过 1000")
		return
	}
	base := req.Outbound
	if base.Type == model.ProtoWireGuard {
		badRequest(c, "WireGuard 的 peer 是固定端点，不支持批量创建；请改用分享链接导入或逐个创建")
		return
	}
	needPort := base.Type != "direct" && base.Type != "block"

	created := make([]model.Outbound, 0, req.Count)
	for i := 0; i < req.Count; i++ {
		ob := base
		ob.ID = 0
		ob.CreatedAt = time.Time{}
		ob.UpdatedAt = time.Time{}
		if needPort {
			ob.Port = base.Port + i*step
			if ob.Port > 65535 {
				a.deleteOutbounds(created)
				badRequest(c, fmt.Sprintf("第 %d 个出站端口超出 65535，请减小数量或步长", i+1))
				return
			}
		}
		ob.Tag = outboundBatchTag(base.Tag, base.Type, i+1, ob.Port)
		if err := a.prepareOutbound(&ob); err != nil {
			a.deleteOutbounds(created)
			badRequest(c, err.Error())
			return
		}
		if err := a.db.Create(&ob).Error; err != nil {
			a.deleteOutbounds(created)
			serverError(c, err)
			return
		}
		created = append(created, ob)
	}

	a.afterApply(c, gin.H{"outbounds": created, "count": len(created)})
}

// outboundBatchTag 生成批量出站的标识：
// 留空按「类型-端口」自动命名；{n} 替换为序号，否则追加 -序号。
func outboundBatchTag(base, typ string, index, port int) string {
	base = strings.TrimSpace(base)
	if base == "" {
		if port <= 0 {
			return fmt.Sprintf("out-%s-%d", typ, index)
		}
		return fmt.Sprintf("out-%s-%d", typ, port)
	}
	if strings.Contains(base, "{n}") {
		return strings.ReplaceAll(base, "{n}", strconv.Itoa(index))
	}
	return fmt.Sprintf("%s-%d", base, index)
}

// deleteOutbounds 回滚批量创建过程中已经写入的出站。
func (a *App) deleteOutbounds(outbounds []model.Outbound) {
	for _, ob := range outbounds {
		a.db.Delete(&model.Outbound{}, ob.ID)
	}
}

func (a *App) updateOutbound(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	var req model.Outbound
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err.Error())
		return
	}
	var ob model.Outbound
	if err := a.db.First(&ob, id).Error; err != nil {
		fail(c, 404, "出站不存在")
		return
	}
	req.ID = ob.ID
	req.CreatedAt = ob.CreatedAt
	if err := a.prepareOutbound(&req); err != nil {
		badRequest(c, err.Error())
		return
	}
	if err := a.db.Save(&req).Error; err != nil {
		serverError(c, err)
		return
	}
	a.afterApply(c, gin.H{"outbound": req})
}

func (a *App) deleteOutbound(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	if err := a.db.Delete(&model.Outbound{}, id).Error; err != nil {
		serverError(c, err)
		return
	}
	a.afterApply(c, gin.H{"deleted": id})
}

func (a *App) prepareOutbound(ob *model.Outbound) error {
	ob.Tag = strings.TrimSpace(ob.Tag)
	ob.Type = strings.TrimSpace(ob.Type)
	if ob.Type == "" {
		return errString("出站类型不能为空")
	}
	if ob.Tag == "" {
		ob.Tag = "out-" + strings.ToLower(ob.Type)
	}
	// direct / block 没有服务器参数，也不支持 detour；入库即清空，保持数据与内核一致。
	if ob.Type == "direct" || ob.Type == "block" {
		ob.Server = ""
		ob.Port = 0
		if ob.Dial != "" {
			dial := map[string]any{}
			if err := json.Unmarshal([]byte(ob.Dial), &dial); err == nil {
				if _, ok := dial["detour"]; ok {
					delete(dial, "detour")
					if b, err := json.Marshal(dial); err == nil {
						ob.Dial = model.JSONText(b)
					}
				}
			}
		}
	}
	var n int64
	q := a.db.Model(&model.Outbound{}).Where("tag = ?", ob.Tag)
	if ob.ID > 0 {
		q = q.Where("id <> ?", ob.ID)
	}
	if err := q.Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return errString("出站标识 " + ob.Tag + " 已存在")
	}
	return validateOutbound(ob)
}

// outboundConfig 解析协议专属字段。
func outboundConfig(ob *model.Outbound) map[string]any {
	m := map[string]any{}
	if ob.Config != "" {
		_ = json.Unmarshal([]byte(ob.Config), &m)
	}
	return m
}

func cfgString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// validateOutbound 按类型校验必填字段，错误信息精确到字段。
func validateOutbound(ob *model.Outbound) error {
	cfg := outboundConfig(ob)

	switch ob.Type {
	case "direct", "block":
		// 无服务器参数。
	case model.ProtoWireGuard:
		if cfgString(cfg, "private_key") == "" {
			return errString("WireGuard 需要 private_key")
		}
		peers, _ := cfg["peers"].([]any)
		if len(peers) == 0 {
			return errString("WireGuard 需要至少一个 peer（含 public_key 与 allowed_ips）")
		}
	default:
		if strings.TrimSpace(ob.Server) == "" {
			return errString("服务器地址不能为空")
		}
		if ob.Port <= 0 || ob.Port > 65535 {
			return errString("端口需在 1-65535 之间")
		}
	}

	switch ob.Type {
	case "vless", "vmess", "tuic":
		if cfgString(cfg, "uuid") == "" {
			return errString(ob.Type + " 需要 uuid")
		}
	case "trojan", "hysteria2":
		if cfgString(cfg, "password") == "" {
			return errString(ob.Type + " 需要 password")
		}
	case "shadowsocks":
		if cfgString(cfg, "method") == "" {
			return errString("shadowsocks 需要 method")
		}
		if cfgString(cfg, "password") == "" {
			return errString("shadowsocks 需要 password")
		}
	}
	if ob.Type == "tuic" && cfgString(cfg, "password") == "" {
		return errString("tuic 需要 password")
	}

	// hysteria2 / tuic 出站强制 TLS。
	if ob.Type == "hysteria2" || ob.Type == "tuic" {
		tls := map[string]any{}
		if ob.TLS != "" {
			_ = json.Unmarshal([]byte(ob.TLS), &tls)
		}
		if enabled, _ := tls["enabled"].(bool); !enabled {
			return errString(ob.Type + " 出站必须启用 TLS")
		}
	}
	return nil
}

// testOutbound 通过 Clash API 测试出站延迟（需内核运行中）。
func (a *App) testOutbound(c *gin.Context) {
	var req struct {
		ID  uint   `json:"id"`
		Tag string `json:"tag"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "参数错误")
		return
	}
	tag := strings.TrimSpace(req.Tag)
	if tag == "" && req.ID > 0 {
		var ob model.Outbound
		if err := a.db.First(&ob, req.ID).Error; err != nil {
			fail(c, 404, "出站不存在")
			return
		}
		tag = ob.Tag
	}
	if tag == "" {
		badRequest(c, "缺少出站标识")
		return
	}
	if !a.core.Proc.Running() {
		fail(c, 400, "内核未运行，无法测速")
		return
	}

	testURL := strings.TrimSpace(a.settings.GetDefault(model.SettingTestURL, stats.DefaultTestURL))
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	// 查询超时设得比 Clash 客户端的 HTTP 超时(5s)短，让内核先返回自己的超时结果。
	delay, err := a.clashClient().ProxyDelay(ctx, tag, testURL, 3000)
	if err != nil {
		msg := "测速失败：" + err.Error()
		// 内核里没有该出站时补上可操作的原因（停用 / 配置没应用 / 内核可见出站列表）。
		if strings.Contains(err.Error(), "HTTP 404") {
			msg += "。" + a.explainMissingProxy(ctx, tag)
		}
		fail(c, 502, msg)
		return
	}
	ok(c, gin.H{"tag": tag, "delay": delay})
}

// explainMissingProxy 内核里查不到某个出站时，给出原因与内核当前可见的出站列表。
func (a *App) explainMissingProxy(ctx context.Context, tag string) string {
	var ob model.Outbound
	found := a.db.Where("tag = ?", tag).First(&ob).Error == nil

	var b strings.Builder
	switch {
	case found && !ob.Enable:
		b.WriteString("该出站在面板里是「停用」状态，不会写进内核配置，请先启用")
	case found:
		b.WriteString("该出站已启用但内核里没有它，通常是配置没成功应用（生成或校验失败），")
		b.WriteString("请到「系统与内核」查看当前配置与内核日志")
	default:
		b.WriteString("数据库里找不到该出站（标识 " + tag + "）")
	}

	if names, err := a.clashClient().ProxyNames(ctx); err == nil && len(names) > 0 {
		const shown = 12
		b.WriteString("。内核当前可见的出站：")
		if len(names) > shown {
			b.WriteString(strings.Join(names[:shown], "、"))
			b.WriteString(fmt.Sprintf("…（共 %d 个）", len(names)))
		} else {
			b.WriteString(strings.Join(names, "、"))
		}
	}
	return b.String()
}

// parseOutboundLink 解析分享链接为出站字段（供「从分享链接导入」使用）。
func (a *App) parseOutboundLink(c *gin.Context) {
	var req struct {
		Link string `json:"link"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Link) == "" {
		badRequest(c, "请提供分享链接")
		return
	}
	ob, err := svc.ParseShareLink(req.Link)
	if err != nil {
		badRequest(c, err.Error())
		return
	}
	ok(c, ob)
}
