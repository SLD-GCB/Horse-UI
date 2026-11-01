package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"horseui/back-machine/internal/model"
	"horseui/back-machine/internal/store"
	"horseui/back-machine/internal/totp"
	"horseui/back-machine/internal/util"
)

type loginReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	// Code 可选：面板启用 2FA 后，登录页可以直接带上 6 位动态码或恢复码。
	Code string `json:"code"`
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
	if admin.TOTPEnabled {
		code := strings.TrimSpace(req.Code)
		if code != "" {
			if !a.verifySecondFactor(admin, code) {
				a.loginFails.Fail(userKey, a.loginMaxFails())
				a.recordAudit(model.AuditLog{
					Username: admin.Username, IP: c.ClientIP(), Method: c.Request.Method,
					Path: c.FullPath(), Action: "login", Status: http.StatusUnauthorized,
					Detail: "动态码错误", CreatedAt: time.Now(),
				})
				unauthorized(c, "动态码错误或已使用")
				return
			}
			if err := a.db.Save(admin).Error; err != nil {
				serverError(c, err)
				return
			}
			a.loginFails.Reset(userKey)
			a.issueSession(c, admin, req.Password)
			return
		}
		challenge, err := a.signTokenWithPurpose(admin.Username, max(admin.TokenVersion, 1), 5*time.Minute, "2fa")
		if err != nil {
			serverError(c, err)
			return
		}
		a.recordAudit(model.AuditLog{
			Username: admin.Username, IP: c.ClientIP(), Method: c.Request.Method,
			Path: c.FullPath(), Action: "login", Status: http.StatusOK,
			Detail: "密码通过，等待 2FA", CreatedAt: time.Now(),
		})
		ok(c, gin.H{"two_factor_required": true, "challenge": challenge})
		return
	}

	a.loginFails.Reset(userKey)
	a.issueSession(c, admin, req.Password)
}

// issueSession 签发正式会话并返回登录结果。
func (a *App) issueSession(c *gin.Context, admin *model.Admin, password string) {
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
		"weak_password": util.IsWeakPassword(password),
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

// ---------------------------------------------------------------------------
// 两步验证（TOTP）

type login2FAReq struct {
	Challenge string `json:"challenge" binding:"required"`
	Code      string `json:"code" binding:"required"`
}

type totpPasswordReq struct {
	Password string `json:"password" binding:"required"`
}

type totpCodeReq struct {
	Code string `json:"code" binding:"required"`
}

type totpDisableReq struct {
	Password string `json:"password" binding:"required"`
	Code     string `json:"code" binding:"required"`
}

// handleLogin2FA 校验动态码或恢复码，签发正式会话。
func (a *App) handleLogin2FA(c *gin.Context) {
	if !a.loginLimit.Allow("ip:" + c.ClientIP()) {
		fail(c, http.StatusTooManyRequests, "请求过于频繁，请稍后再试")
		return
	}
	var req login2FAReq
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "请输入动态码")
		return
	}
	claims, err := a.parseToken(req.Challenge)
	if err != nil || claims.Purpose != "2fa" {
		unauthorized(c, "验证已过期，请重新登录")
		return
	}
	admin, err := store.FindAdmin(a.db, claims.Username)
	if err != nil {
		serverError(c, err)
		return
	}
	if admin == nil || !admin.TOTPEnabled || admin.TOTPSecret == "" {
		unauthorized(c, "未启用两步验证")
		return
	}

	userKey := "user:" + strings.ToLower(admin.Username)
	if ok, retry := a.loginFails.Allowed(userKey); !ok {
		fail(c, http.StatusTooManyRequests, fmt.Sprintf("验证失败次数过多，请 %d 分钟后再试", int(retry.Minutes())+1))
		return
	}

	code := strings.TrimSpace(req.Code)
	passed := a.verifySecondFactor(admin, code)
	if !passed {
		a.loginFails.Fail(userKey, a.loginMaxFails())
		a.recordAudit(model.AuditLog{
			Username: admin.Username, IP: c.ClientIP(), Method: c.Request.Method,
			Path: c.FullPath(), Action: "login_2fa", Status: http.StatusUnauthorized,
			Detail: "动态码错误", CreatedAt: time.Now(),
		})
		unauthorized(c, "动态码错误或已使用")
		return
	}
	if err := a.db.Save(admin).Error; err != nil {
		serverError(c, err)
		return
	}
	a.loginFails.Reset(userKey)
	a.recordAudit(model.AuditLog{
		Username: admin.Username, IP: c.ClientIP(), Method: c.Request.Method,
		Path: c.FullPath(), Action: "login_2fa", Status: http.StatusOK,
		Detail: "两步验证通过", CreatedAt: time.Now(),
	})
	a.issueSession(c, admin, "")
}

// handleTOTPStatus 返回当前 2FA 状态。
func (a *App) handleTOTPStatus(c *gin.Context) {
	admin, err := store.FindAdmin(a.db, c.GetString("username"))
	if err != nil {
		serverError(c, err)
		return
	}
	if admin == nil {
		unauthorized(c, "未登录")
		return
	}
	ok(c, gin.H{
		"enabled":         admin.TOTPEnabled,
		"recovery_codes":  recoveryCount(admin.TOTPRecovery),
		"pending_secret":  admin.TOTPSecret != "" && !admin.TOTPEnabled,
	})
}

// handleTOTPSetup 生成新的 TOTP 密钥，等待用户扫码并确认。
func (a *App) handleTOTPSetup(c *gin.Context) {
	var req totpPasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "请输入当前密码")
		return
	}
	admin, err := store.FindAdmin(a.db, c.GetString("username"))
	if err != nil {
		serverError(c, err)
		return
	}
	if admin == nil {
		unauthorized(c, "未登录")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(admin.Password), []byte(req.Password)) != nil {
		unauthorized(c, "密码错误")
		return
	}
	secret, err := totp.GenerateSecret()
	if err != nil {
		serverError(c, err)
		return
	}
	admin.TOTPSecret = secret
	admin.TOTPEnabled = false
	admin.TOTPLastStep = 0
	admin.TOTPRecovery = ""
	if err := a.db.Save(admin).Error; err != nil {
		serverError(c, err)
		return
	}
	ok(c, gin.H{
		"secret": secret,
		"uri":    totp.URI(secret, "HorseUI", admin.Username),
	})
}

// handleTOTPEnable 校验动态码后启用 2FA，并返回一次性恢复码。
func (a *App) handleTOTPEnable(c *gin.Context) {
	var req totpCodeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "请输入动态码")
		return
	}
	admin, err := store.FindAdmin(a.db, c.GetString("username"))
	if err != nil {
		serverError(c, err)
		return
	}
	if admin == nil || admin.TOTPSecret == "" {
		badRequest(c, "请先生成密钥")
		return
	}
	step, okCode := totp.Validate(admin.TOTPSecret, req.Code, time.Now(), 1)
	if !okCode {
		badRequest(c, "动态码错误")
		return
	}
	plain, hashes := generateRecoveryCodes(10)
	admin.TOTPEnabled = true
	admin.TOTPLastStep = step
	admin.TOTPRecovery = hashes
	if err := a.db.Save(admin).Error; err != nil {
		serverError(c, err)
		return
	}
	a.recordAudit(model.AuditLog{
		Username: admin.Username, IP: c.ClientIP(), Method: c.Request.Method,
		Path: c.FullPath(), Action: "2fa_enable", Status: http.StatusOK,
		Detail: "启用两步验证", CreatedAt: time.Now(),
	})
	ok(c, gin.H{"recovery_codes": plain})
}

// handleTOTPDisable 关闭 2FA，需要密码 + 动态码/恢复码。
func (a *App) handleTOTPDisable(c *gin.Context) {
	var req totpDisableReq
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "请输入密码和动态码")
		return
	}
	admin, err := store.FindAdmin(a.db, c.GetString("username"))
	if err != nil {
		serverError(c, err)
		return
	}
	if admin == nil || !admin.TOTPEnabled {
		badRequest(c, "未启用两步验证")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(admin.Password), []byte(req.Password)) != nil {
		unauthorized(c, "密码错误")
		return
	}
	code := strings.TrimSpace(req.Code)
	passed := false
	if strings.Contains(code, "-") {
		passed = a.consumeRecovery(admin, code)
	} else if _, ok := totp.Validate(admin.TOTPSecret, code, time.Now(), 1); ok {
		passed = true
	}
	if !passed {
		badRequest(c, "动态码错误")
		return
	}
	admin.TOTPSecret = ""
	admin.TOTPEnabled = false
	admin.TOTPRecovery = ""
	admin.TOTPLastStep = 0
	if err := a.db.Save(admin).Error; err != nil {
		serverError(c, err)
		return
	}
	a.recordAudit(model.AuditLog{
		Username: admin.Username, IP: c.ClientIP(), Method: c.Request.Method,
		Path: c.FullPath(), Action: "2fa_disable", Status: http.StatusOK,
		Detail: "关闭两步验证", CreatedAt: time.Now(),
	})
	ok(c, nil)
}

// verifySecondFactor 校验动态码或恢复码；动态码会记录时间步防止重放。
func (a *App) verifySecondFactor(admin *model.Admin, code string) bool {
	code = strings.TrimSpace(code)
	if code == "" {
		return false
	}
	if strings.Contains(code, "-") {
		return a.consumeRecovery(admin, code)
	}
	step, ok := totp.Validate(admin.TOTPSecret, code, time.Now(), 1)
	if !ok || step <= admin.TOTPLastStep {
		return false
	}
	admin.TOTPLastStep = step
	return true
}

func hashRecovery(code string) string {	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(code))))
	return hex.EncodeToString(sum[:])
}

func generateRecoveryCodes(n int) ([]string, string) {
	plain := make([]string, 0, n)
	hashes := make([]string, 0, n)
	for i := 0; i < n; i++ {
		buf := make([]byte, 4)
		if _, err := rand.Read(buf); err != nil {
			continue
		}
		code := fmt.Sprintf("%02x%02x-%02x%02x", buf[0], buf[1], buf[2], buf[3])
		plain = append(plain, code)
		hashes = append(hashes, hashRecovery(code))
	}
	data, _ := json.Marshal(hashes)
	return plain, string(data)
}

func recoveryCount(raw string) int {
	var hashes []string
	if raw == "" || json.Unmarshal([]byte(raw), &hashes) != nil {
		return 0
	}
	return len(hashes)
}

// consumeRecovery 校验并删除一个恢复码。
func (a *App) consumeRecovery(admin *model.Admin, code string) bool {
	var hashes []string
	if admin.TOTPRecovery == "" || json.Unmarshal([]byte(admin.TOTPRecovery), &hashes) != nil {
		return false
	}
	target := hashRecovery(code)
	for i, h := range hashes {
		if len(h) == len(target) && subtle.ConstantTimeCompare([]byte(h), []byte(target)) == 1 {
			hashes = append(hashes[:i], hashes[i+1:]...)
			data, _ := json.Marshal(hashes)
			admin.TOTPRecovery = string(data)
			return true
		}
	}
	return false
}
