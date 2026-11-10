package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func (a *App) firewallStatus(c *gin.Context) {
	ok(c, a.firewall.Status())
}

func (a *App) firewallPorts(c *gin.Context) {
	rows, err := a.firewall.Ports()
	if err != nil {
		serverError(c, err)
		return
	}
	ok(c, rows)
}

func (a *App) firewallCreatePort(c *gin.Context) {
	var req struct {
		Port     int    `json:"port"`
		Protocol string `json:"protocol"`
		Action   string `json:"action"`
		Comment  string `json:"comment"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数不正确")
		return
	}
	var err error
	switch strings.ToLower(req.Action) {
	case "allow", "open":
		err = a.firewall.OpenPort(req.Port, strings.ToLower(req.Protocol), req.Comment)
	case "close", "deny":
		err = a.firewall.ClosePort(req.Port, strings.ToLower(req.Protocol), req.Comment)
	default:
		fail(c, http.StatusBadRequest, "动作只能是 allow/close")
		return
	}
	if err != nil {
		serverError(c, err)
		return
	}
	ok(c, gin.H{"ok": true})
}

func (a *App) firewallDeletePort(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		fail(c, http.StatusBadRequest, "端口规则 id 不正确")
		return
	}
	if err := a.firewall.DeletePort(uint(id)); err != nil {
		serverError(c, err)
		return
	}
	ok(c, gin.H{"ok": true})
}

func (a *App) firewallAttackSources(c *gin.Context) {
	rows, err := a.firewall.AttackSources(queryLimit(c))
	if err != nil {
		serverError(c, err)
		return
	}
	ok(c, gin.H{"items": rows})
}

func (a *App) firewallAttackLogs(c *gin.Context) {
	rows, err := a.firewall.AttackLogs(queryLimit(c))
	if err != nil {
		serverError(c, err)
		return
	}
	ok(c, gin.H{"items": rows})
}

func queryLimit(c *gin.Context) int {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	return limit
}

func (a *App) firewallBans(c *gin.Context) {
	rows, err := a.firewall.Bans()
	if err != nil {
		serverError(c, err)
		return
	}
	ok(c, rows)
}

func (a *App) firewallBan(c *gin.Context) {
	var req struct {
		IP        string `json:"ip"`
		Reason    string `json:"reason"`
		Permanent bool   `json:"permanent"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数不正确")
		return
	}
	if strings.TrimSpace(req.IP) == "" {
		fail(c, http.StatusBadRequest, "IP 不能为空")
		return
	}
	if err := a.firewall.Ban(req.IP, req.Reason, "manual", req.Permanent); err != nil {
		serverError(c, err)
		return
	}
	ok(c, gin.H{"ok": true})
}

func (a *App) firewallUnban(c *gin.Context) {
	if err := a.firewall.Unban(c.Param("ip")); err != nil {
		serverError(c, err)
		return
	}
	ok(c, gin.H{"ok": true})
}

func (a *App) firewallDiscover(c *gin.Context) {
	added, err := a.firewall.Discover()
	if err != nil {
		serverError(c, err)
		return
	}
	if err := a.firewall.Apply(); err != nil {
		serverError(c, err)
		return
	}
	ok(c, gin.H{"added": added})
}

func (a *App) firewallApply(c *gin.Context) {
	if err := a.firewall.Apply(); err != nil {
		serverError(c, err)
		return
	}
	ok(c, gin.H{"ok": true})
}

func (a *App) firewallRollback(c *gin.Context) {
	if err := a.firewall.Rollback(); err != nil {
		serverError(c, err)
		return
	}
	ok(c, gin.H{"ok": true})
}

func (a *App) firewallLogs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("lines", "200"))
	lines, err := a.firewall.Logs(limit)
	if err != nil {
		serverError(c, err)
		return
	}
	ok(c, gin.H{"lines": lines})
}

func (a *App) firewallProtectPort(c *gin.Context) {
	var req struct {
		Port     int    `json:"port"`
		Protocol string `json:"protocol"`
		Protect  bool   `json:"protect"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数不正确")
		return
	}
	if err := a.firewall.SetPortProtect(req.Port, strings.ToLower(req.Protocol), req.Protect); err != nil {
		serverError(c, err)
		return
	}
	ok(c, gin.H{"ok": true})
}

func (a *App) firewallGuardState(c *gin.Context) {
	ok(c, a.firewall.GuardState())
}

func (a *App) firewallSetEnabled(c *gin.Context) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数不正确")
		return
	}
	if err := a.firewall.SetEnabled(req.Enabled); err != nil {
		serverError(c, err)
		return
	}
	ok(c, gin.H{"ok": true})
}

func (a *App) firewallSetAutoProtect(c *gin.Context) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数不正确")
		return
	}
	if err := a.firewall.SetAutoProtect(req.Enabled); err != nil {
		serverError(c, err)
		return
	}
	ok(c, gin.H{"ok": true})
}
