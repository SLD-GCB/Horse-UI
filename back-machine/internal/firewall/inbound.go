package firewall

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"horseui/back-machine/internal/model"
)

// SyncInbounds 把已启用入站的端口和端口段同步到防火墙。
// 自动规则带 InboundID：删除节点后可只移除它自己的端口，不影响共享端口。
func (s *Service) SyncInbounds() (int, error) {
	var inbounds []model.Inbound
	if err := s.db.Where("enable = ?", true).Find(&inbounds).Error; err != nil {
		return 0, err
	}
	type want struct {
		inboundID uint
		comment   string
	}
	desired := make(map[string]want)
	for _, ib := range inbounds {
		comment := "节点：" + ib.Tag
		if ib.Remark != "" {
			comment = "节点：" + ib.Remark
		}
		for _, protocol := range inboundProtocols(ib.Type) {
			for _, port := range inboundPorts(ib) {
				key := fmt.Sprintf("%s/%d", protocol, port)
				if _, exists := desired[key]; !exists {
					desired[key] = want{inboundID: ib.ID, comment: comment}
				}
			}
		}
	}

	existing, err := s.Ports()
	if err != nil {
		return 0, err
	}
	added := 0
	for _, row := range existing {
		key := fmt.Sprintf("%s/%d", row.Protocol, row.Port)
		if _, ok := desired[key]; ok {
			// 老版本自动导入的节点端口，补上归属，后续删节点才能收口。
			if row.Auto && row.InboundID == 0 && row.Action != "close" {
				row.InboundID = desired[key].inboundID
				row.Comment = desired[key].comment
				row.UpdatedAt = time.Now().Unix()
				_ = s.db.Save(&row).Error
			}
			continue
		}
		if row.Auto && row.InboundID > 0 {
			if err := s.db.Delete(&model.FirewallPort{}, row.ID).Error; err != nil {
				return added, err
			}
		}
	}

	for key, item := range desired {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) != 2 {
			continue
		}
		protocol := parts[0]
		port, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		n, err := s.ensurePort(port, protocol, item.comment, item.inboundID)
		if err != nil {
			return added, err
		}
		added += n
	}
	return added, nil
}

func inboundProtocols(kind string) []string {
	switch kind {
	case model.ProtoTUIC, model.ProtoHysteria2, model.ProtoSkyHorse:
		return []string{"udp"}
	default:
		return []string{"tcp"}
	}
}

func inboundPorts(ib model.Inbound) []int {
	ports := make(map[int]bool)
	if ib.Port > 0 && ib.Port <= 65535 {
		ports[ib.Port] = true
	}
	var extra map[string]any
	if len(ib.Extra) > 0 {
		_ = json.Unmarshal([]byte(ib.Extra), &extra)
	}
	for _, key := range []string{"port_range", "listen_port_range", "server_ports", "ports"} {
		addPortValue(ports, extra[key])
	}
	out := make([]int, 0, len(ports))
	for port := range ports {
		out = append(out, port)
	}
	// 端口段可能很大，限制单入站同步数量，避免误配置撑爆集合。
	if len(out) > 1000 {
		out = out[:1000]
	}
	return out
}

func addPortValue(ports map[int]bool, value any) {
	switch v := value.(type) {
	case string:
		for _, token := range strings.FieldsFunc(v, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\n' || r == '\t'
		}) {
			addPortToken(ports, token)
		}
	case []any:
		for _, item := range v {
			addPortValue(ports, item)
		}
	case []string:
		for _, item := range v {
			addPortValue(ports, item)
		}
	case float64:
		port := int(v)
		if port > 0 && port <= 65535 {
			ports[port] = true
		}
	}
}

func addPortToken(ports map[int]bool, token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	if strings.Contains(token, "-") {
		parts := strings.SplitN(token, "-", 2)
		start, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		end, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err1 != nil || err2 != nil {
			return
		}
		if start > end {
			start, end = end, start
		}
		if start < 1 {
			start = 1
		}
		if end > 65535 {
			end = 65535
		}
		for port := start; port <= end; port++ {
			ports[port] = true
			if len(ports) >= 1000 {
				return
			}
		}
		return
	}
	port, err := strconv.Atoi(token)
	if err == nil && port > 0 && port <= 65535 {
		ports[port] = true
	}
}

// ensurePort 与 EnsurePort 相同，但返回新增数量。
func (s *Service) ensurePort(port int, protocol, comment string, inboundID uint) (int, error) {
	if err := validatePort(port, protocol); err != nil {
		return 0, err
	}
	var count int64
	s.db.Model(&model.FirewallPort{}).
		Where("port = ? AND protocol = ?", port, protocol).
		Count(&count)
	if count > 0 {
		return 0, nil
	}
	now := time.Now().Unix()
	row := model.FirewallPort{
		Port:      port,
		Protocol:  protocol,
		Action:    "allow",
		Source:    "any",
		Comment:   comment,
		Auto:      true,
		InboundID: inboundID,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	return 1, s.db.Create(&row).Error
}
