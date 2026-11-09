package api

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"horseui/back-machine/internal/ipcare"
	"horseui/back-machine/internal/model"
)

const ipCareTokenPrefix = "enc:v1:"

func (a *App) ipCareTokenKey() ([]byte, error) {
	path := filepath.Join(a.opts.DataDir, "ipcare_token.key")
	if raw, err := os.ReadFile(path); err == nil && len(raw) == 32 {
		return raw, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(a.opts.DataDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

func (a *App) sealIPCareToken(token string) (string, error) {
	key, err := a.ipCareTokenKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(token), nil)
	return ipCareTokenPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

func (a *App) openIPCareToken(stored string) (string, error) {
	stored = strings.TrimSpace(stored)
	if stored == "" {
		return "", nil
	}
	if !strings.HasPrefix(stored, ipCareTokenPrefix) {
		// 兼容旧版本明文令牌，下次保存时自动迁移。
		return stored, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, ipCareTokenPrefix))
	if err != nil {
		return "", err
	}
	key, err := a.ipCareTokenKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("IP 养护令牌数据损坏")
	}
	nonce, data := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// ensureIPCareToken 返回专用令牌，不存在时生成。
func (a *App) ensureIPCareToken(regenerate bool) (string, error) {
	if !regenerate {
		if stored := strings.TrimSpace(a.settings.Get(model.SettingIPCareToken)); stored != "" {
			token, err := a.openIPCareToken(stored)
			if err != nil {
				return "", err
			}
			if !strings.HasPrefix(stored, ipCareTokenPrefix) {
				if sealed, err := a.sealIPCareToken(token); err == nil {
					_ = a.settings.Set(model.SettingIPCareToken, sealed)
				}
			}
			return token, nil
		}
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	sealed, err := a.sealIPCareToken(token)
	if err != nil {
		return "", err
	}
	if err := a.settings.Set(model.SettingIPCareToken, sealed); err != nil {
		return "", err
	}
	return token, nil
}

// ipCareAuth 接受管理员 JWT 或 IP 养护专用令牌。
func (a *App) ipCareAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		raw := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		stored := strings.TrimSpace(a.settings.Get(model.SettingIPCareToken))
		token, err := a.openIPCareToken(stored)
		if err == nil && token != "" && raw != "" &&
			subtle.ConstantTimeCompare([]byte(raw), []byte(token)) == 1 {
			c.Next()
			return
		}
		a.authMiddleware()(c)
	}
}

// ipCareTokenInfo 返回专用令牌。
func (a *App) ipCareTokenInfo(c *gin.Context) {
	token, err := a.ensureIPCareToken(false)
	if err != nil {
		serverError(c, err)
		return
	}
	ok(c, gin.H{"token": token})
}

// ipCareTokenRotate 重新生成专用令牌。
func (a *App) ipCareTokenRotate(c *gin.Context) {
	token, err := a.ensureIPCareToken(true)
	if err != nil {
		serverError(c, err)
		return
	}
	ok(c, gin.H{"token": token})
}

// ipCareStatus 返回 IP 养护状态。
func (a *App) ipCareStatus(c *gin.Context) {
	st, err := a.ipCare.Status()
	if err != nil {
		serverError(c, err)
		return
	}
	ok(c, st)
}

// ipCareHistory 返回检测历史。
func (a *App) ipCareHistory(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	list, err := a.ipCare.History(limit)
	if err != nil {
		serverError(c, err)
		return
	}
	ok(c, gin.H{"items": list})
}

// ipCareCheck 手动执行一次检测。
func (a *App) ipCareCheck(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	res, err := a.ipCare.Check(ctx)
	if errors.Is(err, ipcare.ErrBusy) {
		fail(c, http.StatusConflict, "已有任务在运行")
		return
	}
	if err != nil {
		// 检测失败也返回结果，前端展示 last_error，而不是只弹 HTTP 错误。
		ok(c, res)
		return
	}
	if res.IsCN {
		go func() {
			autoCtx, cancel := context.WithTimeout(context.Background(), 95*time.Second)
			defer cancel()
			a.ipCare.MaybeAutoWarm(autoCtx, res)
		}()
	}
	ok(c, res)
}

// ipCareWarm 手动执行一次纠偏。
func (a *App) ipCareWarm(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()
	job, err := a.ipCare.Warm(ctx)
	if errors.Is(err, ipcare.ErrBusy) {
		fail(c, http.StatusConflict, "已有任务在运行")
		return
	}
	if err != nil {
		ok(c, job)
		return
	}
	ok(c, job)
}

// ipCareWarmHistory 返回纠偏历史。
func (a *App) ipCareWarmHistory(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	list, err := a.ipCare.WarmHistory(limit)
	if err != nil {
		serverError(c, err)
		return
	}
	ok(c, gin.H{"items": list})
}

// ipCareSettings 返回设置。
func (a *App) ipCareSettings(c *gin.Context) {
	settings, err := a.ipCare.GetSettings()
	if err != nil {
		serverError(c, err)
		return
	}
	ok(c, settings)
}

// ipCareUpdateSettings 保存设置。
func (a *App) ipCareUpdateSettings(c *gin.Context) {
	var req model.IPCareSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "设置格式错误")
		return
	}
	settings, err := a.ipCare.SaveSettings(req)
	if err != nil {
		badRequest(c, err.Error())
		return
	}
	ok(c, settings)
}
