package api

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"horseui/back-machine/internal/svc"
)

// parseID 解析路径中的自增 ID。
func parseID(c *gin.Context) (uint, bool) {
	v, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		badRequest(c, "无效的 ID")
		return 0, false
	}
	return uint(v), true
}

// afterApply 应用内核配置，并把可能的错误作为警告返回给前端。
func (a *App) afterApply(c *gin.Context, payload gin.H) {
	if err := a.core.Apply(); err != nil {
		payload["apply_error"] = err.Error()
	}
	ok(c, payload)
}

// coreStatusOf 返回内核状态快照。
func (a *App) coreStatusOf(c *gin.Context) svc.CoreStatus {
	return a.core.Status(c.Request.Context())
}
