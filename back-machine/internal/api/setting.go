package api

import (
	"github.com/gin-gonic/gin"

	"horseui/back-machine/internal/model"
)

// protectedSettings 不允许通过接口读取或修改的配置键。
var protectedSettings = map[string]bool{
	model.SettingJWTSecret:         true,
	model.SettingAdminLastPassword: true,
}

func (a *App) getSettings(c *gin.Context) {
	m := a.settings.All()
	for k := range protectedSettings {
		delete(m, k)
	}
	ok(c, m)
}

func (a *App) updateSettings(c *gin.Context) {
	var req map[string]string
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "配置格式错误")
		return
	}
	// 过滤受保护键后一次性批量写入（值未变化的项会被跳过）。
	clean := make(map[string]string, len(req))
	for k, v := range req {
		if protectedSettings[k] {
			continue
		}
		clean[k] = v
	}
	if err := a.settings.SetMany(clean); err != nil {
		serverError(c, err)
		return
	}
	m := a.settings.All()
	for k := range protectedSettings {
		delete(m, k)
	}
	a.afterApply(c, gin.H{"settings": m})
}
