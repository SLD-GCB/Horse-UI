package svc

import (
	"encoding/json"
	"fmt"
	"strings"

	"horseui/back-machine/internal/model"
	"horseui/core/config"
)

func parseObj(j model.JSONText) map[string]any {
	if j == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(j), &m); err != nil || len(m) == 0 {
		return nil
	}
	return m
}

// buildInbound 将入站模型转换为 sing-box 入站描述。cert 非空时使用其证书文件。
func buildInbound(ib model.Inbound, clients []model.Client, cert *model.Certificate) (config.Inbound, error) {
	out := config.Inbound{
		Type:   ib.Type,
		Tag:    ib.Tag,
		Listen: ib.Listen,
		Port:   ib.Port,
		Fields: map[string]any{},
	}
	f := out.Fields

	// TLS 与 Reality。
	tls := parseObj(ib.TLS)
	reality := parseObj(ib.Reality)
	if len(reality) > 0 {
		if tls == nil {
			tls = map[string]any{}
		}
		tls["enabled"] = true
		r := map[string]any{"enabled": true}
		for k, v := range reality {
			r[k] = v
		}
		tls["reality"] = r
	}
	if ib.Type != model.ProtoShadowsocks && cert != nil && cert.CertPath != "" {
		if tls == nil {
			tls = map[string]any{}
		}
		tls["enabled"] = true
		tls["certificate_path"] = cert.CertPath
		tls["key_path"] = cert.KeyPath
	}
	if ib.Type != model.ProtoShadowsocks && tls != nil {
		f["tls"] = tls
	}
	// TLS 强制：除 Shadowsocks 外所有协议都必须启用 TLS 或 Reality；
	// 非 Reality 时还必须提供证书，否则内核会因缺少证书而拒绝启动。
	if ib.RequireTLS() {
		if tls == nil {
			return out, fmt.Errorf("协议 %s 必须配置 TLS 或 Reality", ib.Type)
		}
		if enabled, ok := tls["enabled"].(bool); ok && !enabled {
			return out, fmt.Errorf("协议 %s 必须启用 TLS", ib.Type)
		}
		if _, hasReality := tls["reality"]; !hasReality {
			cp, _ := tls["certificate_path"].(string)
			if strings.TrimSpace(cp) == "" {
				return out, fmt.Errorf("协议 %s 需要证书：请在「TLS」中选择面板证书或填写证书路径，或改用 Reality", ib.Type)
			}
		}
	}

	switch ib.Type {
	case model.ProtoVLESS:
		users := make([]map[string]any, 0, len(clients))
		for _, c := range clients {
			u := map[string]any{"name": c.Name, "uuid": c.UUID}
			if c.Flow != "" {
				u["flow"] = c.Flow
			}
			users = append(users, u)
		}
		f["users"] = users

	case model.ProtoVMess:
		users := make([]map[string]any, 0, len(clients))
		for _, c := range clients {
			users = append(users, map[string]any{
				"name": c.Name, "uuid": c.UUID, "alterId": 0,
			})
		}
		f["users"] = users

	case model.ProtoTrojan:
		users := make([]map[string]any, 0, len(clients))
		for _, c := range clients {
			users = append(users, map[string]any{"name": c.Name, "password": c.Password})
		}
		f["users"] = users

	case model.ProtoShadowsocks:
		f["method"] = ib.Method
		if ib.ServerPassword != "" {
			f["password"] = ib.ServerPassword
		}
		if len(clients) > 0 {
			users := make([]map[string]any, 0, len(clients))
			for _, c := range clients {
				users = append(users, map[string]any{"name": c.Name, "password": c.Password})
			}
			f["users"] = users
		}

	case model.ProtoTUIC:
		users := make([]map[string]any, 0, len(clients))
		for _, c := range clients {
			users = append(users, map[string]any{
				"name": c.Name, "uuid": c.UUID, "password": c.Password,
			})
		}
		f["users"] = users
		if ib.CongestionControl != "" {
			f["congestion_control"] = ib.CongestionControl
		}

	case model.ProtoHysteria2:
		users := make([]map[string]any, 0, len(clients))
		for _, c := range clients {
			users = append(users, map[string]any{"name": c.Name, "password": c.Password})
		}
		f["users"] = users
		if ib.UpMbps > 0 {
			f["up_mbps"] = ib.UpMbps
		}
		if ib.DownMbps > 0 {
			f["down_mbps"] = ib.DownMbps
		}
		if ib.ObfsPassword != "" {
			ot := ib.ObfsType
			if ot == "" {
				ot = "salamander"
			}
			f["obfs"] = map[string]any{"type": ot, "password": ib.ObfsPassword}
		}
		if ib.Masquerade != "" {
			f["masquerade"] = ib.Masquerade
		}

	case model.ProtoSkyHorse:
		users := make([]map[string]any, 0, len(clients))
		for _, c := range clients {
			users = append(users, map[string]any{"name": c.Name, "uuid": c.UUID, "password": c.Password})
		}
		f["users"] = users
		if ib.ObfsPassword != "" {
			f["obfs_password"] = ib.ObfsPassword
		}
		if ib.UpMbps > 0 {
			f["up_mbps"] = ib.UpMbps
		}
		if ib.DownMbps > 0 {
			f["down_mbps"] = ib.DownMbps
		}

	case model.ProtoHTTP:
		users := make([]map[string]any, 0, len(clients))
		for _, c := range clients {
			users = append(users, map[string]any{
				"username": c.Name, "password": c.Password,
			})
		}
		f["users"] = users

	default:
		return out, fmt.Errorf("不支持的协议类型: %s", ib.Type)
	}

	// 传输层：仅 VLESS / VMess / Trojan 支持。
	if tr := parseObj(ib.Transport); tr != nil && ib.SupportsTransport() {
		f["transport"] = tr
	}

	// 协议特有/高级选项最后合并，便于扩展新字段而无需改表结构。
	// 受保护的基础字段不允许被覆盖。
	if extra := parseObj(ib.Extra); extra != nil {
		for k, v := range extra {
			switch k {
			case "type", "tag", "listen", "listen_port":
				continue
			}
			f[k] = v
		}
	}

	return out, nil
}

// buildOutbound 将出站模型转换为 sing-box 出站描述。
// buildOutbound 将出站模型转换为 sing-box 出站描述。
// 合并顺序：Config → TLS → Transport → Multiplex → Dial → Extra（后者覆盖同名键）。
func buildOutbound(ob model.Outbound) (config.Outbound, error) {
	out := config.Outbound{
		Type:   ob.Type,
		Tag:    ob.Tag,
		Server: ob.Server,
		Port:   ob.Port,
		Fields: map[string]any{},
	}
	// direct / block 不接受 server / server_port 字段（内核会报 unknown field）。
	// 老数据可能残留 server，这里统一忽略，避免生成非法配置。
	if ob.Type == "direct" || ob.Type == "block" {
		out.Server = ""
		out.Port = 0
	}
	// 协议专属字段直接平铺。
	if cfg := parseObj(ob.Config); cfg != nil {
		for k, v := range cfg {
			out.Fields[k] = v
		}
	}
	// 结构化块（嵌套对象）。
	if tls := parseObj(ob.TLS); tls != nil {
		out.Fields["tls"] = tls
	}
	if tr := parseObj(ob.Transport); tr != nil {
		out.Fields["transport"] = tr
	}
	if mux := parseObj(ob.Multiplex); mux != nil {
		out.Fields["multiplex"] = mux
	}
	// 拨号字段为顶层平铺。
	if dial := parseObj(ob.Dial); dial != nil {
		for k, v := range dial {
			// direct / block 不支持 detour（内核报 "not supported in direct context"），
			// 老数据或接口直传时统一忽略，避免生成非法配置。
			if k == "detour" && (ob.Type == "direct" || ob.Type == "block") {
				continue
			}
			out.Fields[k] = v
		}
	}
	if extra := parseObj(ob.Extra); extra != nil {
		for k, v := range extra {
			out.Fields[k] = v
		}
	}
	return out, nil
}
