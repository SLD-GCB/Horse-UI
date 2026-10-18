package api

import (
	"fmt"
	"strconv"
	"strings"
	"time"

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

// batchClientPayload 批量创建用户的请求体。
type batchClientPayload struct {
	InboundID  uint       `json:"inbound_id"`
	InboundIDs []uint     `json:"inbound_ids"`
	Count      int        `json:"count"`
	NamePrefix string     `json:"name_prefix"`
	NameStart  int        `json:"name_start"` // 0 表示自动接续已有编号
	Flow       string     `json:"flow"`
	QuotaBytes int64      `json:"quota_bytes"`
	ExpireAt   *time.Time `json:"expire_at"`
	Enable     *bool      `json:"enable"`
	Remark     string     `json:"remark"`
}

const batchClientLimit = 200

// batchCreateClients 批量创建用户，最后只应用一次内核配置。
func (a *App) batchCreateClients(c *gin.Context) {
	var req batchClientPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err.Error())
		return
	}
	if req.InboundID == 0 {
		badRequest(c, "必须指定主入站")
		return
	}
	if req.Count < 1 || req.Count > batchClientLimit {
		badRequest(c, fmt.Sprintf("单次批量创建数量需在 1-%d 之间", batchClientLimit))
		return
	}
	var ib model.Inbound
	if err := a.db.First(&ib, req.InboundID).Error; err != nil {
		badRequest(c, "主入站不存在")
		return
	}

	prefix := strings.TrimSpace(req.NamePrefix)
	if prefix == "" {
		prefix = "user-"
	}

	start := req.NameStart
	if start <= 0 {
		start = a.nextClientIndex(prefix)
	}
	if start < 1 {
		start = 1
	}
	width := len(strconv.Itoa(start + req.Count - 1))
	if width < 2 {
		width = 2
	}

	enable := true
	if req.Enable != nil {
		enable = *req.Enable
	}

	created := make([]model.Client, 0, req.Count)
	for i := 0; i < req.Count; i++ {
		name := fmt.Sprintf("%s%0*d", prefix, width, start+i)
		if len([]rune(name)) > 64 {
			a.deleteClients(created)
			badRequest(c, "生成的用户名超过 64 个字符，请缩短前缀")
			return
		}
		cl := model.Client{
			InboundID:  req.InboundID,
			Name:       name,
			Flow:       strings.TrimSpace(req.Flow),
			Enable:     enable,
			QuotaBytes: req.QuotaBytes,
			ExpireAt:   req.ExpireAt,
			Remark:     req.Remark,
		}
		if err := a.prepareClient(&ib, &cl); err != nil {
			a.deleteClients(created)
			badRequest(c, err.Error())
			return
		}
		if err := a.db.Create(&cl).Error; err != nil {
			a.deleteClients(created)
			serverError(c, err)
			return
		}
		if err := a.syncClientBindings(cl.ID, req.InboundIDs); err != nil {
			created = append(created, cl)
			a.deleteClients(created)
			serverError(c, err)
			return
		}
		created = append(created, cl)
	}

	a.afterApply(c, gin.H{"clients": created, "count": len(created)})
}

// nextClientIndex 自动接续同前缀用户的最大编号，避免重复创建时撞名。
func (a *App) nextClientIndex(prefix string) int {
	var names []string
	// 用 substr 比较前缀，避免 LIKE 把前缀里的 % 和 _ 当成通配符。
	a.db.Model(&model.Client{}).
		Where("substr(name, 1, ?) = ?", len([]rune(prefix)), prefix).
		Pluck("name", &names)
	max := 0
	for _, name := range names {
		suffix := strings.TrimPrefix(name, prefix)
		if suffix == name || suffix == "" {
			continue
		}
		n, err := strconv.Atoi(suffix)
		if err != nil || n <= max {
			continue
		}
		max = n
	}
	return max + 1
}

// deleteClients 回滚批量创建过程中已经写入的用户。
func (a *App) deleteClients(clients []model.Client) {
	for _, cl := range clients {
		a.db.Where("client_id = ?", cl.ID).Delete(&model.ClientInbound{})
		a.db.Where("client_id = ?", cl.ID).Delete(&model.ClientStat{})
		a.db.Delete(&model.Client{}, cl.ID)
	}
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
