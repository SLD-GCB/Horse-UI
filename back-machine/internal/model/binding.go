package model

// ClientInbound 用户与入站的绑定关系。
// 用户绑定多个入站时，这些入站的流量会合并计入该用户用量；
// 若一个入站被多个用户绑定，则按绑定人数均摊。
type ClientInbound struct {
	ClientID  uint `gorm:"primaryKey" json:"client_id"`
	InboundID uint `gorm:"primaryKey" json:"inbound_id"`
}
