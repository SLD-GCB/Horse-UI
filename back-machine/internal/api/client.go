package api

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"

	"horseui/back-machine/internal/model"
	"horseui/back-machine/internal/util"
)

// clientRow 客户端列表行，附带所属入站与绑定入站。
type clientRow struct {
	model.Client
	InboundTag  string `json:"inbound_tag"`
	InboundType string `json:"inbound_type"`
	InboundIDs  []uint `json:"inbound_ids"`
}

// clientPayload 客户端创建/更新请求体。
type clientPayload struct {
	model.Client
	InboundIDs []uint `json:"inbound_ids"`
}

// clientBindings 返回 客户端ID -> 显式绑定的入站ID 列表。
func (a *App) clientBindings() map[uint][]uint {
	out := map[uint][]uint{}
	var binds []model.ClientInbound
	a.db.Find(&binds)
	for _, b := range binds {
		out[b.ClientID] = append(out[b.ClientID], b.InboundID)
	}
	return out
}

// syncClientBindings 覆盖写入某个客户端的绑定关系。
func (a *App) syncClientBindings(clientID uint, inboundIDs []uint) error {
	if err := a.db.Where("client_id = ?", clientID).Delete(&model.ClientInbound{}).Error; err != nil {
		return err
	}
	seen := map[uint]bool{}
	for _, iid := range inboundIDs {
		if iid == 0 || seen[iid] {
			continue
		}
		seen[iid] = true
		if err := a.db.Create(&model.ClientInbound{ClientID: clientID, InboundID: iid}).Error; err != nil {
			return err
		}
	}
	return nil
}

// effectiveInboundIDs 返回客户端的全部生效入站（主入站 + 显式绑定）。
func effectiveInboundIDs(cl model.Client, bindings map[uint][]uint) []uint {
	out := []uint{cl.InboundID}
	seen := map[uint]bool{cl.InboundID: true}
	for _, id := range bindings[cl.ID] {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func (a *App) listClients(c *gin.Context) {
	q := a.db.Order("id")
	if ibID := c.Query("inbound_id"); ibID != "" {
		q = q.Where("inbound_id = ? OR id IN (SELECT client_id FROM client_inbounds WHERE inbound_id = ?)", ibID, ibID)
	}
	var list []model.Client
	if err := q.Find(&list).Error; err != nil {
		serverError(c, err)
		return
	}

	inboundMap := map[uint]model.Inbound{}
	var inbounds []model.Inbound
	a.db.Find(&inbounds)
	for _, ib := range inbounds {
		inboundMap[ib.ID] = ib
	}
	bindings := a.clientBindings()

	rows := make([]clientRow, 0, len(list))
	for _, cl := range list {
		ib := inboundMap[cl.InboundID]
		rows = append(rows, clientRow{
			Client:      cl,
			InboundTag:  ib.Tag,
			InboundType: ib.Type,
			InboundIDs:  effectiveInboundIDs(cl, bindings),
		})
	}
	ok(c, rows)
}

func (a *App) createClient(c *gin.Context) {
	var req clientPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err.Error())
		return
	}
	cl := req.Client
	if cl.InboundID == 0 {
		badRequest(c, "必须指定主入站")
		return
	}
	var ib model.Inbound
	if err := a.db.First(&ib, cl.InboundID).Error; err != nil {
		badRequest(c, "主入站不存在")
		return
	}
	cl.ID = 0
	if err := a.prepareClient(&ib, &cl); err != nil {
		badRequest(c, err.Error())
		return
	}
	if err := a.db.Create(&cl).Error; err != nil {
		serverError(c, err)
		return
	}
	if err := a.syncClientBindings(cl.ID, req.InboundIDs); err != nil {
		serverError(c, err)
		return
	}
	a.afterApply(c, gin.H{"client": cl})
}

func (a *App) updateClient(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	var req clientPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err.Error())
		return
	}
	var cl model.Client
	if err := a.db.First(&cl, id).Error; err != nil {
		fail(c, 404, "用户不存在")
		return
	}
	var ib model.Inbound
	if err := a.db.First(&ib, cl.InboundID).Error; err != nil {
		badRequest(c, "主入站不存在")
		return
	}

	upd := req.Client
	upd.ID = cl.ID
	upd.InboundID = cl.InboundID
	upd.CreatedAt = cl.CreatedAt
	upd.UsedUp = cl.UsedUp
	upd.UsedDown = cl.UsedDown
	upd.RawUp = cl.RawUp
	upd.RawDown = cl.RawDown
	if upd.SubToken == "" {
		upd.SubToken = cl.SubToken
	}
	if err := a.prepareClient(&ib, &upd); err != nil {
		badRequest(c, err.Error())
		return
	}
	if err := a.db.Save(&upd).Error; err != nil {
		serverError(c, err)
		return
	}
	if err := a.syncClientBindings(upd.ID, req.InboundIDs); err != nil {
		serverError(c, err)
		return
	}
	a.afterApply(c, gin.H{"client": upd})
}

func (a *App) deleteClient(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	if err := a.db.Delete(&model.Client{}, id).Error; err != nil {
		serverError(c, err)
		return
	}
	a.db.Where("client_id = ?", id).Delete(&model.ClientStat{})
	a.db.Where("client_id = ?", id).Delete(&model.ClientInbound{})
	a.afterApply(c, gin.H{"deleted": id})
}

func (a *App) resetClientTraffic(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	if err := a.db.Model(&model.Client{}).Where("id = ?", id).
		Updates(map[string]any{"used_up": 0, "used_down": 0, "raw_up": 0, "raw_down": 0}).Error; err != nil {
		serverError(c, err)
		return
	}
	a.db.Where("client_id = ?", id).Delete(&model.ClientStat{})
	var cl model.Client
	a.db.First(&cl, id)
	ok(c, cl)
}

func (a *App) rotateClientSecret(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	var cl model.Client
	if err := a.db.First(&cl, id).Error; err != nil {
		fail(c, 404, "用户不存在")
		return
	}
	var ib model.Inbound
	if err := a.db.First(&ib, cl.InboundID).Error; err != nil {
		badRequest(c, "主入站不存在")
		return
	}
	cl.UUID = ""
	cl.Password = ""
	cl.SubToken = ""
	if err := fillClientSecrets(ib, &cl); err != nil {
		serverError(c, err)
		return
	}
	if err := a.db.Save(&cl).Error; err != nil {
		serverError(c, err)
		return
	}
	a.afterApply(c, gin.H{"client": cl})
}

// resetSubToken 仅重置订阅令牌（使旧订阅地址失效），不影响连接凭据。
func (a *App) resetSubToken(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	var cl model.Client
	if err := a.db.First(&cl, id).Error; err != nil {
		fail(c, 404, "用户不存在")
		return
	}
	cl.SubToken = util.RandomToken()
	if err := a.db.Save(&cl).Error; err != nil {
		serverError(c, err)
		return
	}
	ok(c, gin.H{"sub_token": cl.SubToken, "sub_url": a.subURL(c, cl)})
}

// prepareClient 校验并补全客户端配置。
func (a *App) prepareClient(ib *model.Inbound, cl *model.Client) error {
	cl.Name = strings.TrimSpace(cl.Name)
	if cl.Name == "" {
		cl.Name = "user-" + randomPassword()[:6]
	}

	// 用户名全局唯一（同时作为内核统计名）。
	var n int64
	q := a.db.Model(&model.Client{}).Where("name = ?", cl.Name)
	if cl.ID > 0 {
		q = q.Where("id <> ?", cl.ID)
	}
	if err := q.Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("用户名 %s 已存在（需全局唯一）", cl.Name)
	}

	if cl.QuotaBytes < 0 {
		return fmt.Errorf("流量配额不能为负数")
	}
	return fillClientSecrets(*ib, cl)
}
