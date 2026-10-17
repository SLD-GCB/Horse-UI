package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"horseui/back-machine/internal/model"
	"horseui/back-machine/internal/store"
	"horseui/back-machine/internal/util"
)

type loginReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (a *App) handleLogin(c *gin.Context) {
	// 每 IP 速率限制。
	if !a.loginLimit.Allow("ip:" + c.ClientIP()) {
		fail(c, http.StatusTooManyRequests, "请求过于频繁，请稍后再试")
		return
	}

	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "请输入用户名和密码")
		return
	}

	// 账号级失败锁定。
	userKey := "user:" + strings.ToLower(req.Username)
	if ok, retry := a.loginFails.Allowed(userKey); !ok {
		fail(c, http.StatusTooManyRequests, fmt.Sprintf("登录失败次数过多，请 %d 分钟后再试", int(retry.Minutes())+1))
		return
	}

	admin, err := store.FindAdmin(a.db, req.Username)
	if err != nil {
		serverError(c, err)
		return
	}
	if admin == nil || bcrypt.CompareHashAndPassword([]byte(admin.Password), []byte(req.Password)) != nil {
		a.loginFails.Fail(userKey, a.loginMaxFails())
		a.recordAudit(model.AuditLog{
			Username: req.Username, IP: c.ClientIP(), Method: c.Request.Method,
			Path: c.FullPath(), Action: "login", Status: http.StatusUnauthorized,
			Detail: "密码错误", CreatedAt: time.Now(),
		})
		unauthorized(c, "用户名或密码错误")
		return
	}
	a.loginFails.Reset(userKey)

	ttl := time.Duration(a.sessionTTLHours()) * time.Hour
	token, err := a.signToken(admin.Username, max(admin.TokenVersion, 1), ttl)
	if err != nil {
		serverError(c, err)
		return
	}
	a.setAdminState(admin)
	a.recordAudit(model.AuditLog{
		Username: admin.Username, IP: c.ClientIP(), Method: c.Request.Method,
		Path: c.FullPath(), Action: "login", Status: http.StatusOK,
		Detail: "登录成功", CreatedAt: time.Now(),
	})
	ok(c, gin.H{
		"token":         token,
		"username":      admin.Username,
		"version":       a.opts.Version,
		"must_change":   admin.MustChangePassword,
		"weak_password": util.IsWeakPassword(req.Password),
	})
}

func (a *App) handleProfile(c *gin.Context) {
	ok(c, gin.H{
		"username":    c.GetString("username"),
		"version":     a.opts.Version,
		"must_change": a.currentAdmin().MustChange,
	})
}

// handleLogout 递增令牌版本，使该账号已签发的所有令牌立即失效。
func (a *App) handleLogout(c *gin.Context) {
	if admin, err := store.FindAdmin(a.db, c.GetString("username")); err == nil && admin != nil {
		admin.TokenVersion = max(admin.TokenVersion, 1) + 1
		if err := a.db.Save(admin).Error; err == nil {
			a.setAdminState(admin)
		}
	}
	ok(c, nil)
}

type changePwdReq struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

// handleChangePassword 修改密码：校验强度、递增令牌版本，并返回新令牌保持当前会话。
func (a *App) handleChangePassword(c *gin.Context) {
	var req changePwdReq
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "请填写原密码和新密码")
		return
	}
	if err := util.ValidatePassword(req.NewPassword); err != nil {
		badRequest(c, err.Error())
		return
	}
	admin, err := store.FindAdmin(a.db, c.GetString("username"))
	if err != nil {
		serverError(c, err)
		return
	}
	if admin == nil {
		unauthorized(c, "账号不存在")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(admin.Password), []byte(req.OldPassword)) != nil {
		badRequest(c, "原密码错误")
		return
	}
	if req.NewPassword == req.OldPassword {
		badRequest(c, "新密码不能与原密码相同")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		serverError(c, err)
		return
	}
	admin.Password = string(hash)
	admin.TokenVersion = max(admin.TokenVersion, 1) + 1
	admin.MustChangePassword = false
	if err := a.db.Save(admin).Error; err != nil {
		serverError(c, err)
		return
	}
	// 用户自定义的密码不再留存明文，清掉「最近生成的密码」记录。
	_ = a.settings.Set(model.SettingAdminLastPassword, "")
	a.setAdminState(admin)

	ttl := time.Duration(a.sessionTTLHours()) * time.Hour
	token, err := a.signToken(admin.Username, admin.TokenVersion, ttl)
	if err != nil {
		serverError(c, err)
		return
	}
	ok(c, gin.H{"token": token})
}
