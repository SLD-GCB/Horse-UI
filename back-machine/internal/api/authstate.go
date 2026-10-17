package api

import (
	"fmt"
	"strconv"

	"golang.org/x/crypto/bcrypt"

	"horseui/back-machine/internal/model"
	"horseui/back-machine/internal/util"
)

// adminState 管理员认证状态的内存缓存，避免每次请求查库。
type adminState struct {
	Username   string
	TokenVer   int
	MustChange bool
}

// loadAdminState 启动时从数据库载入管理员状态。
func (a *App) loadAdminState() error {
	var admin model.Admin
	if err := a.db.Order("id").First(&admin).Error; err != nil {
		a.auth.Store(&adminState{})
		return nil
	}
	a.setAdminState(&admin)
	return nil
}

// setAdminState 用数据库记录刷新缓存。
func (a *App) setAdminState(admin *model.Admin) {
	if admin == nil {
		return
	}
	a.auth.Store(&adminState{
		Username:   admin.Username,
		TokenVer:   max(admin.TokenVersion, 1),
		MustChange: admin.MustChangePassword,
	})
}

// currentAdmin 返回当前缓存的管理员状态（永不为 nil）。
func (a *App) currentAdmin() *adminState {
	if s := a.auth.Load(); s != nil {
		return s
	}
	return &adminState{}
}

// refreshAdminState 从数据库重新加载（定时兜底，覆盖外部直接改库的情况）。
func (a *App) refreshAdminState() {
	var admin model.Admin
	if err := a.db.Order("id").First(&admin).Error; err != nil {
		return
	}
	a.setAdminState(&admin)
}

// ensureAdminPassword 启动时检查管理员口令。
// 若仍是已知弱默认口令（如 admin/admin），直接重置为面板随机生成的 16 位强密码并打印一次。
// 因为管理菜单（选项 5）可随时再次重置，不会造成永久锁死。
func (a *App) ensureAdminPassword() {
	var admin model.Admin
	if err := a.db.Order("id").First(&admin).Error; err != nil {
		return
	}
	weak := false
	for _, w := range util.WeakPasswords() {
		if bcrypt.CompareHashAndPassword([]byte(admin.Password), []byte(w)) == nil {
			weak = true
			break
		}
	}
	if !weak {
		return
	}
	pass := util.RandomPassword(16)
	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		return
	}
	admin.Password = string(hash)
	admin.TokenVersion = max(admin.TokenVersion, 1) + 1
	admin.MustChangePassword = false
	if err := a.db.Save(&admin).Error; err != nil {
		return
	}
	_ = a.settings.Set(model.SettingAdminLastPassword, pass)
	a.setAdminState(&admin)
	fmt.Printf("\n[horseui] 检测到管理员仍在使用弱默认密码，已重置为面板随机生成的密码：\n")
	fmt.Printf("[horseui] 管理员密码: %s\n", pass)
	fmt.Printf("[horseui] 该密码仅显示一次，请立即记录（可通过管理菜单第 5 项再次重置）\n\n")
}

// sessionTTLHours 会话有效期（小时）。
func (a *App) sessionTTLHours() int {
	if v := a.settings.Get(model.SettingSessionTTLHours); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 24*365 {
			return n
		}
	}
	return 24
}

// loginMaxFails 登录失败锁定阈值。
func (a *App) loginMaxFails() int {
	if v := a.settings.Get(model.SettingLoginMaxFails); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			return n
		}
	}
	return 5
}
