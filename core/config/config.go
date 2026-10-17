package config

import "encoding/json"

// Inbound sing-box 入站描述，Fields 为协议特有字段。
type Inbound struct {
	Type   string
	Tag    string
	Listen string
	Port   int
	Fields map[string]any
}

// Outbound sing-box 出站描述。
type Outbound struct {
	Type   string
	Tag    string
	Server string
	Port   int
	Fields map[string]any
}

// Experimental 实验性 API 配置。
type Experimental struct {
	ClashListen string   // Clash API 监听，空则不启用
	ClashSecret string   // Clash API 密钥
	V2RayListen string   // 统计 gRPC 监听，空或用户为空则不启用
	StatsUsers  []string // 需要统计的用户名（email）
	CachePath   string   // 缓存文件路径，空则不启用
}

// Input 配置生成输入。
type Input struct {
	LogLevel     string
	Sniff        bool
	Inbounds     []Inbound
	Outbounds    []Outbound
	Endpoints    []Outbound // WireGuard 等 endpoint 类型（渲染到顶层 endpoints）
	DNS          map[string]any
	Route        map[string]any
	Experimental Experimental
}

// Build 按输入生成 sing-box 配置 JSON。
func Build(in Input) ([]byte, error) {
	root := map[string]any{}

	level := in.LogLevel
	if level == "" {
		level = "info"
	}
	root["log"] = map[string]any{"level": level, "timestamp": true}

	if in.DNS != nil {
		root["dns"] = in.DNS
	}

	inbounds := make([]map[string]any, 0, len(in.Inbounds))
	for _, ib := range in.Inbounds {
		obj := map[string]any{"type": ib.Type, "tag": ib.Tag}
		if ib.Listen != "" {
			obj["listen"] = ib.Listen
		}
		if ib.Port > 0 {
			obj["listen_port"] = ib.Port
		}
		for k, v := range ib.Fields {
			obj[k] = v
		}
		inbounds = append(inbounds, obj)
	}
	root["inbounds"] = inbounds

	outbounds := []map[string]any{{"type": "direct", "tag": "direct"}}
	for _, ob := range in.Outbounds {
		obj := map[string]any{"type": ob.Type, "tag": ob.Tag}
		if ob.Server != "" {
			obj["server"] = ob.Server
		}
		if ob.Port > 0 {
			obj["server_port"] = ob.Port
		}
		for k, v := range ob.Fields {
			obj[k] = v
		}
		outbounds = append(outbounds, obj)
	}
	root["outbounds"] = outbounds

	if len(in.Endpoints) > 0 {
		endpoints := make([]map[string]any, 0, len(in.Endpoints))
		for _, ep := range in.Endpoints {
			obj := map[string]any{"type": ep.Type, "tag": ep.Tag}
			// endpoint 不使用顶层 server/server_port（WireGuard 的地址在 peers 中）。
			for k, v := range ep.Fields {
				obj[k] = v
			}
			endpoints = append(endpoints, obj)
		}
		root["endpoints"] = endpoints
	}

	root["route"] = buildRoute(in)

	exp := map[string]any{}
	if in.Experimental.ClashListen != "" {
		clash := map[string]any{"external_controller": in.Experimental.ClashListen}
		if in.Experimental.ClashSecret != "" {
			clash["secret"] = in.Experimental.ClashSecret
		}
		exp["clash_api"] = clash
	}
	if in.Experimental.V2RayListen != "" && len(in.Experimental.StatsUsers) > 0 {
		exp["v2ray_api"] = map[string]any{
			"listen": in.Experimental.V2RayListen,
			"stats":  map[string]any{"enabled": true, "users": in.Experimental.StatsUsers},
		}
	}
	if in.Experimental.CachePath != "" {
		exp["cache_file"] = map[string]any{"enabled": true, "path": in.Experimental.CachePath}
	}
	if len(exp) > 0 {
		root["experimental"] = exp
	}

	return json.MarshalIndent(root, "", "  ")
}

func buildRoute(in Input) map[string]any {
	route := map[string]any{}
	for k, v := range in.Route {
		route[k] = v
	}

	// 嗅探与 DNS 劫持必须排在最前，之后才是用户规则与广告拦截规则。
	rules := []any{}
	if in.Sniff {
		rules = append(rules, map[string]any{"action": "sniff"})
		rules = append(rules, map[string]any{"protocol": "dns", "action": "hijack-dns"})
	}
	if existing, ok := route["rules"].([]any); ok {
		rules = append(rules, existing...)
	}
	if len(rules) > 0 {
		route["rules"] = rules
	}

	if _, ok := route["final"]; !ok {
		route["final"] = "direct"
	}
	return route
}
