package api

import (
	"github.com/gin-gonic/gin"

	"horseui/back-machine/internal/sysinfo"
)

// tuneReport 系统调优体检（只读，不修改任何东西）。
func (a *App) tuneReport(c *gin.Context) {
	ok(c, sysinfo.Detect(a.opts.DataDir))
}

// tuneApply 执行系统调优：action=apply 应用 / revert 回滚。
// 调优不在面板启动关键路径上，失败只影响本次调用。
func (a *App) tuneApply(c *gin.Context) {
	var req struct {
		Action string `json:"action"`
	}
	_ = c.ShouldBindJSON(&req)

	var (
		rep sysinfo.TuneReport
		err error
	)
	switch req.Action {
	case "apply":
		rep, err = sysinfo.Apply(a.opts.DataDir)
	case "revert":
		rep, err = sysinfo.Revert(a.opts.DataDir)
	default:
		badRequest(c, "action 只能是 apply 或 revert")
		return
	}
	if err != nil {
		badRequest(c, err.Error())
		return
	}
	ok(c, rep)
}

// tuneBBR 全局 BBR 开关（设置页滑块）：enabled=true 开启，false 恢复 cubic + fq_codel。
func (a *App) tuneBBR(c *gin.Context) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "参数格式错误")
		return
	}
	rep, err := sysinfo.SetBBR(a.opts.DataDir, req.Enabled)
	if err != nil {
		badRequest(c, err.Error())
		return
	}
	ok(c, rep)
}
