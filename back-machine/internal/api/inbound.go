package api

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"

	"horseui/back-machine/internal/model"
	"horseui/core/config"
)

// inboundRow 入站列表行，附带用户数量。
type inboundRow struct {
	model.Inbound
	ClientCount int64 `json:"client_count"`
}

func (a *App) listInbounds(c *gin.Context) {
	var list []model.Inbound
	if err := a.db.Order("id").Find(&list).Error; err != nil {
		serverError(c, err)
		return
	}
	rows := make([]inboundRow, 0, len(list))
	for _, ib := range list {
		var n int64
		a.db.Model(&model.Client{}).Where("inbound_id = ?", ib.ID).Count(&n)
		rows = append(rows, inboundRow{Inbound: ib, ClientCount: n})
	}
	ok(c, rows)
}

func (a *App) getInbound(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	var ib model.Inbound
	if err := a.db.First(&ib, id).Error; err != nil {
		fail(c, 404, "入站不存在")
		return
	}
	var clients []model.Client
	a.db.Where("inbound_id = ?", ib.ID).Order("id").Find(&clients)
	ok(c, gin.H{"inbound": ib, "clients": clients})
}

func (a *App) createInbound(c *gin.Context) {
	var ib model.Inbound
	if err := c.ShouldBindJSON(&ib); err != nil {
		badRequest(c, err.Error())
		return
	}
	ib.ID = 0
	if err := a.prepareInbound(&ib); err != nil {
		badRequest(c, err.Error())
		return
	}
	if err := a.db.Create(&ib).Error; err != nil {
		serverError(c, err)
		return
	}
	a.afterApply(c, gin.H{"inbound": ib})
}

func (a *App) updateInbound(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	var req model.Inbound
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err.Error())
		return
	}
	var ib model.Inbound
	if err := a.db.First(&ib, id).Error; err != nil {
		fail(c, 404, "入站不存在")
		return
	}
	req.ID = ib.ID
	req.CreatedAt = ib.CreatedAt
	if err := a.prepareInbound(&req); err != nil {
		badRequest(c, err.Error())
		return
	}
	if err := a.db.Omit("Clients").Save(&req).Error; err != nil {
		serverError(c, err)
		return
	}
	a.afterApply(c, gin.H{"inbound": req})
}

func (a *App) deleteInbound(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	if err := a.db.Where("inbound_id = ?", id).Delete(&model.Client{}).Error; err != nil {
		serverError(c, err)
		return
	}
	a.db.Where("inbound_id = ?", id).Delete(&model.ClientInbound{})
	if err := a.db.Delete(&model.Inbound{}, id).Error; err != nil {
		serverError(c, err)
		return
	}
	a.afterApply(c, gin.H{"deleted": id})
}

// prepareInbound 校验并补全入站配置默认值。
func (a *App) prepareInbound(ib *model.Inbound) error {
	switch ib.Type {
	case model.ProtoVLESS, model.ProtoVMess, model.ProtoTrojan,
		model.ProtoShadowsocks, model.ProtoTUIC, model.ProtoHysteria2, model.ProtoHTTP:
	default:
		return fmt.Errorf("不支持的协议类型: %s", ib.Type)
	}

	if ib.Port <= 0 || ib.Port > 65535 {
		return fmt.Errorf("端口必须在 1-65535 之间")
	}
	// TLS 强制：除 Shadowsocks 外，所有协议都必须启用 TLS 或 Reality。
	if ib.Type != model.ProtoShadowsocks && !inboundHasTLS(ib) {
		return fmt.Errorf("协议 %s 必须启用 TLS 或 Reality（该协议无内置加密，明文传输会被窃听）", ib.Type)
	}
	// 非 Reality 时必须有证书来源，否则生成配置会因缺少证书而失败。
	if ib.Type != model.ProtoShadowsocks && !inboundHasReality(ib) && !inboundHasCertSource(ib) {
		return fmt.Errorf("协议 %s 需要证书：请选择面板证书、填写证书路径，或改用 Reality", ib.Type)
	}
	if ib.Listen == "" {
		ib.Listen = "::"
	}
	if strings.TrimSpace(ib.Tag) == "" {
		ib.Tag = fmt.Sprintf("%s-%d", ib.Type, ib.Port)
	}

	// tag 唯一性校验。
	var n int64
	q := a.db.Model(&model.Inbound{}).Where("tag = ?", ib.Tag)
	if ib.ID > 0 {
		q = q.Where("id <> ?", ib.ID)
	}
	if err := q.Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("入站标识 %s 已存在", ib.Tag)
	}

	// Shadowsocks 默认方法与服务端密钥。
	if ib.Type == model.ProtoShadowsocks {
		if ib.Method == "" {
			ib.Method = "2022-blake3-aes-128-gcm"
		}
		if ib.ServerPassword == "" {
			pwd, err := config.SSPassword(ib.Method)
			if err != nil {
				return err
			}
			if pwd == "" {
				pwd = randomPassword()
			}
			ib.ServerPassword = pwd
		}
	}

	if ib.Type == model.ProtoHysteria2 && ib.ObfsPassword != "" && ib.ObfsType == "" {
		ib.ObfsType = "salamander"
	}
	return nil
}

// inboundHasTLS 判断入站是否已启用 TLS 或 Reality。
func inboundHasTLS(ib *model.Inbound) bool {
	if jsonObjNonEmpty(ib.Reality) != nil {
		return true
	}
	t := jsonObjNonEmpty(ib.TLS)
	if t == nil {
		return false
	}
	if enabled, ok := t["enabled"].(bool); ok && !enabled {
		return false
	}
	return true
}

// jsonObjNonEmpty 解析 JSON 文本为对象；空值、非法值或空对象返回 nil。
func jsonObjNonEmpty(j model.JSONText) map[string]any {
	s := strings.TrimSpace(string(j))
	if s == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil || len(m) == 0 {
		return nil
	}
	return m
}

// inboundHasReality 判断入站是否配置了 Reality。
func inboundHasReality(ib *model.Inbound) bool {
	return jsonObjNonEmpty(ib.Reality) != nil
}

// inboundHasCertSource 判断入站是否已有证书来源（面板证书引用或 TLS 中的证书路径）。
func inboundHasCertSource(ib *model.Inbound) bool {
	if ib.CertID != nil && *ib.CertID > 0 {
		return true
	}
	t := jsonObjNonEmpty(ib.TLS)
	if t == nil {
		return false
	}
	cp, _ := t["certificate_path"].(string)
	return strings.TrimSpace(cp) != ""
}
