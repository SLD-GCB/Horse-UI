package store

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"horseui/back-machine/internal/model"
	"horseui/back-machine/internal/util"
)

// DefaultAdminUser 首次启动时创建的管理员用户名。
// 初始密码为随机生成，仅在首次创建时打印一次。
const DefaultAdminUser = "admin"

// Seed 初始化默认配置与管理员账号。
func Seed(db *gorm.DB, settings *Settings) error {
	// 默认配置项。
	defaults := map[string]string{
		model.SettingJWTSecret:   util.RandomHex(32),
		model.SettingClashListen: "127.0.0.1:9090",
		model.SettingClashSecret: util.RandomHex(16),
		model.SettingV2RayListen: "127.0.0.1:9091",
		model.SettingLogLevel:    "info",
		model.SettingSubPath:     "/sub",
		model.SettingStatsMode:   "auto",
		model.SettingPortStart:   "10000",
		model.SettingPortEnd:     "60000",
		// 安全默认值：面板 HTTPS 默认开启（首次启动会自动生成自签证书）。
		model.SettingPanelTLSEnable:  "1",
		model.SettingSessionTTLHours: "24",
		model.SettingLoginMaxFails:   "5",
		model.SettingMaxBodyBytes:    "2097152",
		// 测速默认地址。
		model.SettingTestURL: "http://www.gstatic.com/generate_204",
		// 行为抹除：默认关闭（开启后全部 TCP 协议走多路复用，下载类场景可能变慢）。
		model.SettingBehaviorMask:        "0",
		model.SettingBehaviorMaskStreams: "8",
		model.SettingBehaviorMaskPadding: "1",
	}
	for k, v := range defaults {
		if err := settings.SetDefault(k, v); err != nil {
			return err
		}
	}

	// 默认管理员：仅全新库时创建，密码由面板随机生成（含大小写字母、数字、符号）并只打印一次。
	var count int64
	if err := db.Model(&model.Admin{}).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		pass := util.RandomPassword(16)
		hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		admin := model.Admin{
			Username:     DefaultAdminUser,
			Password:     string(hash),
			TokenVersion: 1,
		}
		if err := db.Create(&admin).Error; err != nil {
			return err
		}
		// 记录本次生成的密码，供终端菜单「查看管理员账号与密码」读取（bcrypt 不可反解）。
		if err := settings.Set(model.SettingAdminLastPassword, pass); err != nil {
			return err
		}
		fmt.Printf("\n[horseui] ============================================\n")
		fmt.Printf("[horseui] 初始管理员账号: %s\n", DefaultAdminUser)
		fmt.Printf("[horseui] 初始密码: %s\n", pass)
		fmt.Printf("[horseui] 此密码仅显示一次，请立即记录。\n")
		fmt.Printf("[horseui] ============================================\n\n")
	}
	return nil
}

// FindAdmin 按用户名查询管理员。
func FindAdmin(db *gorm.DB, username string) (*model.Admin, error) {
	var a model.Admin
	err := db.Where("username = ?", username).First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}
