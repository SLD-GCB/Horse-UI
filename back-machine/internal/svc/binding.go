package svc

import (
	"gorm.io/gorm"

	"horseui/back-machine/internal/model"
)

// BoundClients 返回 入站ID -> 绑定到该入站的客户端ID 列表。
// 客户端的主入站（Client.InboundID）始终视为一条隐式绑定。
func BoundClients(db *gorm.DB) (map[uint][]uint, error) {
	var clients []model.Client
	if err := db.Find(&clients).Error; err != nil {
		return nil, err
	}

	index := make(map[uint]map[uint]bool)
	clientExists := make(map[uint]bool, len(clients))

	add := func(inboundID, clientID uint) {
		if index[inboundID] == nil {
			index[inboundID] = make(map[uint]bool)
		}
		index[inboundID][clientID] = true
	}

	for _, cl := range clients {
		clientExists[cl.ID] = true
		add(cl.InboundID, cl.ID)
	}

	var binds []model.ClientInbound
	if err := db.Find(&binds).Error; err != nil {
		return nil, err
	}
	for _, b := range binds {
		if !clientExists[b.ClientID] {
			continue
		}
		add(b.InboundID, b.ClientID)
	}

	out := make(map[uint][]uint, len(index))
	for inboundID, set := range index {
		ids := make([]uint, 0, len(set))
		for id := range set {
			ids = append(ids, id)
		}
		out[inboundID] = ids
	}
	return out, nil
}

// enabledClients 返回按 ID 排序的全部启用客户端。
func (c *Core) enabledClients() ([]model.Client, error) {
	var clients []model.Client
	if err := c.DB.Where("enable = ?", true).Order("id").Find(&clients).Error; err != nil {
		return nil, err
	}
	return clients, nil
}

// pickClients 按 ID 集合从有序列表中挑出客户端，保持原顺序。
func pickClients(all []model.Client, ids []uint) []model.Client {
	if len(ids) == 0 {
		return nil
	}
	set := make(map[uint]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	out := make([]model.Client, 0, len(ids))
	for _, cl := range all {
		if set[cl.ID] {
			out = append(out, cl)
		}
	}
	return out
}
