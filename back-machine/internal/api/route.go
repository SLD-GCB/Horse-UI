package api

import (
	"encoding/json"
	"strings"

	"github.com/gin-gonic/gin"

	"horseui/back-machine/internal/model"
)

// routeConfig 可视化路由配置。
type routeConfig struct {
	Rules []map[string]any `json:"rules"`
	Final string           `json:"final"`
}

// getRoute 返回可视化路由配置。
func (a *App) getRoute(c *gin.Context) {
	out := routeConfig{Rules: []map[string]any{}}
	if raw := strings.TrimSpace(a.settings.Get(model.SettingRouteRules)); raw != "" {
		var rules []map[string]any
		if err := json.Unmarshal([]byte(raw), &rules); err == nil {
			out.Rules = rules
		}
	}
	out.Final = a.settings.Get(model.SettingRouteFinal)
	ok(c, out)
}

// updateRoute 保存可视化路由配置并应用。
func (a *App) updateRoute(c *gin.Context) {
	var req routeConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "配置格式错误")
		return
	}

	clean := make([]map[string]any, 0, len(req.Rules))
	for _, r := range req.Rules {
		if nr := normalizeRouteRule(r); len(nr) > 0 {
			clean = append(clean, nr)
		}
	}
	data, err := json.Marshal(clean)
	if err != nil {
		serverError(c, err)
		return
	}
	if err := a.settings.SetMany(map[string]string{
		model.SettingRouteRules: string(data),
		model.SettingRouteFinal: strings.TrimSpace(req.Final),
	}); err != nil {
		serverError(c, err)
		return
	}
	a.afterApply(c, gin.H{"rules": clean, "final": strings.TrimSpace(req.Final)})
}

// normalizeRouteRule 清理空字段，避免生成无效规则。
func normalizeRouteRule(r map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range r {
		switch val := v.(type) {
		case string:
			if strings.TrimSpace(val) == "" {
				continue
			}
			out[k] = strings.TrimSpace(val)
		case []any:
			if len(val) == 0 {
				continue
			}
			out[k] = val
		case nil:
			continue
		default:
			out[k] = v
		}
	}
	return out
}
