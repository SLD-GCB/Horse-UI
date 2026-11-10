package api

import (
	"context"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"horseui/back-machine/internal/firewall"
	"horseui/back-machine/internal/ipcare"
	"horseui/back-machine/internal/model"
	"horseui/back-machine/internal/store"
	"horseui/back-machine/internal/svc"
	"horseui/back-machine/internal/webui"
	"horseui/core/stats"
)

// Options 面板启动参数。
type Options struct {
	DataDir string
	Listen  string
	Version string
	// Open 启动后自动打开浏览器（Windows 双击运行时默认开启）。
	Open bool
}

// App 面板应用实例。
type App struct {
	opts     Options
	db       *gorm.DB
	settings *store.Settings
	core     *svc.Core
	ipCare   *ipcare.Service
	firewall *firewall.Service
	engine   *gin.Engine

	statsMu        sync.Mutex
	statsCli       *stats.Client
	statsAddr      string
	clashCli       *stats.ClashClient
	clashAddr      string
	clashSecret    string
	clashSeen      *stats.Tracker
	pending        map[string]stats.Delta
	pendingInbound map[string]stats.Delta

	// 安全相关。
	ipAllow    *ipAllowList
	auth       atomic.Pointer[adminState]
	loginLimit *rateLimiter
	loginFails *failGuard
	subLimit   *rateLimiter
	auditCh    chan model.AuditLog
	panelTLS   *panelTLS
}

// New 创建面板应用。
func New(opts Options) (*App, error) {
	if opts.DataDir == "" {
		return nil, fmt.Errorf("data dir is empty")
	}
	abs, err := filepath.Abs(opts.DataDir)
	if err != nil {
		return nil, err
	}
	opts.DataDir = abs
	if err := ensureDataDirs(opts.DataDir); err != nil {
		return nil, err
	}

	db, err := store.Open(opts.DataDir)
	if err != nil {
		return nil, err
	}
	if err := store.Migrate(db); err != nil {
		return nil, err
	}
	settings, err := store.NewSettings(db)
	if err != nil {
		return nil, err
	}
	if err := store.Seed(db, settings); err != nil {
		return nil, err
	}

	a := &App{
		opts:      opts,
		db:        db,
		settings:  settings,
		core:      svc.NewCore(opts.DataDir, db, settings),
		ipCare:    ipcare.New(db, opts.DataDir),
		firewall:  firewall.New(db, opts.DataDir),
		clashSeen: stats.NewTracker(),
		pending:   make(map[string]stats.Delta),

		pendingInbound: make(map[string]stats.Delta),

		ipAllow:    &ipAllowList{},
		loginLimit: newRateLimiter(10, time.Minute),
		loginFails: newFailGuard(15 * time.Minute),
		subLimit:   newRateLimiter(60, time.Minute),
		auditCh:    make(chan model.AuditLog, 1024),
		panelTLS:   &panelTLS{},
	}

	gin.SetMode(gin.ReleaseMode)
	a.engine = gin.New()
	// 不信任任何代理头，ClientIP 取真实远端地址。
	_ = a.engine.SetTrustedProxies(nil)
	a.engine.Use(gin.Recovery(), a.securityHeadersMiddleware(), a.corsMiddleware(), a.bodyLimitMiddleware())
	a.routes()
	a.registerStatic()
	// 首次启动自动生成面板自签证书（证书表为空时），保证 HTTPS 开箱可用。
	if err := a.ensurePanelCert(); err != nil {
		fmt.Printf("[horseui] 自动生成面板证书失败: %v\n", err)
	}
	if err := a.loadAdminState(); err != nil {
		return nil, err
	}
	// 检测管理员是否仍在使用弱默认口令，是则重置为随机强密码。
	a.ensureAdminPassword()
	a.startAudit()
	return a, nil
}

// Run 启动内核、后台任务与 HTTP 服务。
func (a *App) Run() error {
	if a.core.Box.Installed() {
		// 配置生成/校验失败时仍用上一次的配置文件启动，避免面板重启后节点直接全掉。
		if err := a.core.Apply(); err != nil {
			fmt.Printf("[horseui] 配置生成或校验失败，将沿用上次配置启动: %v\n", err)
		}
		if err := a.core.StartCore(); err != nil {
			fmt.Printf("[horseui] 内核启动失败: %v\n", err)
		}
	} else {
		fmt.Printf("[horseui] 未检测到 sing-box 内核，请在面板中安装\n")
	}

	a.startTasks()
	a.ipCare.Start(context.Background())
	if port := listenPort(a.opts.Listen); port > 0 {
		_ = a.firewall.EnsurePort(port, "tcp", "面板端口（启动保护）")
	}
	_ = a.firewall.EnsurePort(22, "tcp", "SSH（启动保护）")
	a.firewall.Start(context.Background())
	return a.startHTTP()
}

// DataDir 返回运行数据目录。
func (a *App) DataDir() string { return a.opts.DataDir }

func (a *App) subPath() string {
	p := strings.TrimSpace(a.settings.GetDefault(model.SettingSubPath, "/sub"))
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return strings.TrimSuffix(p, "/")
}

func (a *App) routes() {
	// 健康检查。
	a.engine.GET("/api/ping", func(c *gin.Context) {
		ok(c, gin.H{"name": "HorseUI", "version": a.opts.Version})
	})

	api := a.engine.Group("/api", a.ipWhitelistMiddleware())

	// 无需鉴权。
	api.POST("/auth/login", a.handleLogin)
	api.POST("/auth/login/2fa", a.handleLogin2FA)

	// 需要鉴权。
	auth := api.Group("")
	auth.Use(a.authMiddleware(), a.auditMiddleware())
	{
		auth.GET("/auth/profile", a.handleProfile)
		auth.POST("/auth/logout", a.handleLogout)
		auth.POST("/auth/password", a.handleChangePassword)
		auth.GET("/auth/2fa", a.handleTOTPStatus)
		auth.POST("/auth/2fa/setup", a.handleTOTPSetup)
		auth.POST("/auth/2fa/enable", a.handleTOTPEnable)
		auth.POST("/auth/2fa/disable", a.handleTOTPDisable)

		auth.GET("/inbounds", a.listInbounds)
		auth.POST("/inbounds", a.createInbound)
		auth.POST("/inbounds/batch", a.batchCreateInbounds)
		auth.POST("/inbounds/:id/test", a.testInbound)
		auth.GET("/inbounds/:id", a.getInbound)
		auth.PUT("/inbounds/:id", a.updateInbound)
		auth.DELETE("/inbounds/:id", a.deleteInbound)

		auth.GET("/clients", a.listClients)
		auth.POST("/clients", a.createClient)
		auth.POST("/clients/batch", a.batchCreateClients)
		auth.PUT("/clients/:id", a.updateClient)
		auth.DELETE("/clients/:id", a.deleteClient)
		auth.POST("/clients/:id/reset-traffic", a.resetClientTraffic)
		auth.POST("/clients/:id/rotate", a.rotateClientSecret)
		auth.POST("/clients/:id/reset-sub-token", a.resetSubToken)
		auth.GET("/clients/:id/links", a.clientLinks)

		auth.GET("/outbounds", a.listOutbounds)
		auth.POST("/outbounds", a.createOutbound)
		auth.POST("/outbounds/batch", a.batchCreateOutbounds)
		auth.POST("/outbounds/test", a.testOutbound)
		auth.POST("/outbounds/parse", a.parseOutboundLink)
		auth.PUT("/outbounds/:id", a.updateOutbound)
		auth.DELETE("/outbounds/:id", a.deleteOutbound)

		auth.GET("/settings", a.getSettings)
		auth.PUT("/settings", a.updateSettings)

		auth.GET("/route", a.getRoute)
		auth.PUT("/route", a.updateRoute)

		auth.GET("/certs", a.listCerts)
		auth.POST("/certs", a.createCert)
		auth.POST("/certs/:id/renew", a.renewCert)
		auth.DELETE("/certs/:id", a.deleteCert)
		auth.GET("/cert-providers", a.certProviders)
		auth.GET("/panelcert", a.panelCertInfo)

		auth.GET("/stats/summary", a.statsSummary)
		auth.GET("/stats/conns", a.statsConns)
		auth.GET("/ports", a.portPool)
		auth.GET("/audit", a.listAudit)

		auth.GET("/ip-care/token", a.ipCareTokenInfo)
		auth.POST("/ip-care/token/rotate", a.ipCareTokenRotate)

		auth.GET("/firewall/status", a.firewallStatus)
		auth.GET("/firewall/ports", a.firewallPorts)
		auth.POST("/firewall/ports", a.firewallCreatePort)
		auth.POST("/firewall/ports/protect", a.firewallProtectPort)
		auth.DELETE("/firewall/ports/:id", a.firewallDeletePort)
		auth.GET("/firewall/bans", a.firewallBans)
		auth.GET("/firewall/attack-sources", a.firewallAttackSources)
		auth.GET("/firewall/attack-logs", a.firewallAttackLogs)
		auth.POST("/firewall/bans", a.firewallBan)
		auth.DELETE("/firewall/bans/:ip", a.firewallUnban)
		auth.POST("/firewall/discover", a.firewallDiscover)
		auth.POST("/firewall/apply", a.firewallApply)
		auth.POST("/firewall/rollback", a.firewallRollback)
		auth.POST("/firewall/enabled", a.firewallSetEnabled)
		auth.POST("/firewall/auto-protect", a.firewallSetAutoProtect)
		auth.GET("/firewall/logs", a.firewallLogs)
		auth.GET("/firewall/guard", a.firewallGuardState)

		auth.GET("/sys/info", a.sysInfo)
		auth.GET("/sys/core", a.coreStatus)
		auth.POST("/sys/core/install", a.coreInstall)
		auth.POST("/sys/core/start", a.coreStart)
		auth.POST("/sys/core/stop", a.coreStop)
		auth.POST("/sys/core/restart", a.coreRestart)
		auth.GET("/sys/logs", a.coreLogs)
		auth.DELETE("/sys/logs", a.coreLogsClear)
		auth.GET("/sys/config", a.coreConfig)

		// 系统调优（弱网优化）：只读体检 + apply / revert。
		auth.GET("/sys/tune", a.tuneReport)
		auth.POST("/sys/tune", a.tuneApply)
		// 全局 BBR 开关（设置页滑块）。
		auth.POST("/sys/bbr", a.tuneBBR)
		// UDP 特征抹除：给未配置混淆的 Hysteria2 入站批量启用 obfs。
		auth.POST("/sys/mask-udp", a.maskUDP)

		auth.GET("/tools/presets", a.toolPresets)
		auth.GET("/tools/adblock-preset", a.toolAdblockPreset)
		auth.POST("/tools/uuid", a.toolUUID)
		auth.POST("/tools/reality", a.toolReality)
		auth.POST("/tools/ss-password", a.toolSSPassword)
		auth.POST("/tools/short-id", a.toolShortID)
		auth.POST("/tools/password", a.toolPassword)
	}

	// IP 养护：管理员 JWT 或专用令牌。
	ipcareAPI := api.Group("/ip-care", a.ipCareAuth())
	{
		ipcareAPI.GET("/status", a.ipCareStatus)
		ipcareAPI.GET("/history", a.ipCareHistory)
		ipcareAPI.GET("/warm/history", a.ipCareWarmHistory)
		ipcareAPI.GET("/settings", a.ipCareSettings)
		ipcareAPI.PUT("/settings", a.ipCareUpdateSettings)
		ipcareAPI.POST("/check", a.ipCareCheck)
		ipcareAPI.POST("/warm", a.ipCareWarm)
		// HorseBox 只读监控：复用 IP 养护令牌，不开放管理动作。
		ipcareAPI.GET("/firewall/status", a.firewallStatus)
		ipcareAPI.GET("/firewall/guard", a.firewallGuardState)
		ipcareAPI.GET("/firewall/logs", a.firewallLogs)
		ipcareAPI.GET("/firewall/attack-sources", a.firewallAttackSources)
		ipcareAPI.GET("/firewall/attack-logs", a.firewallAttackLogs)
		ipcareAPI.GET("/system/metrics", a.appMetrics)
	}

	// 订阅分发（无需鉴权）。
	sub := a.engine.Group(a.subPath())
	sub.GET("/:token", a.handleSubscription)
}

// registerStatic 托管内嵌的前端静态资源，并支持 SPA 回退。
func (a *App) registerStatic() {
	sub, err := webui.Dist()
	if err != nil {
		return
	}
	fileServer := http.FileServer(http.FS(sub))
	subPrefix := a.subPath() + "/"

	a.engine.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path
		if strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, subPrefix) {
			fail(c, http.StatusNotFound, "not found")
			return
		}
		name := strings.TrimPrefix(p, "/")
		if name != "" {
			if _, err := fs.Stat(sub, name); err == nil {
				fileServer.ServeHTTP(c.Writer, c.Request)
				return
			}
		}
		index, err := fs.ReadFile(sub, "index.html")
		if err != nil {
			fail(c, http.StatusNotFound, "not found")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", index)
	})
}

func ensureDataDirs(root string) error {
	for _, sub := range []string{"", "certs", "logs", "bin"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func listenPort(addr string) int {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	value, err := strconv.Atoi(port)
	if err != nil || value <= 0 || value > 65535 {
		return 0
	}
	return value
}
