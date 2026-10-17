package svc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"horseui/back-machine/internal/model"
	"horseui/back-machine/internal/store"
	"horseui/core/box"
	"horseui/core/config"
	"horseui/core/process"
)

// Core 封装 sing-box 内核的配置生成、进程托管与生命周期管理。
type Core struct {
	DataDir  string
	DB       *gorm.DB
	Settings *store.Settings
	Box      *box.Manager
	Proc     *process.Supervisor

	mu       sync.Mutex
	errMu    sync.RWMutex
	lastHash string
	applyErr string
}

// NewCore 创建内核管理器。
func NewCore(dataDir string, db *gorm.DB, settings *store.Settings) *Core {
	boxDir := filepath.Join(dataDir, "bin")
	m := box.NewManager(boxDir)
	c := &Core{
		DataDir:  dataDir,
		DB:       db,
		Settings: settings,
		Box:      m,
	}
	c.Proc = process.New(m.Path(), c.ConfigPath(), 800)
	m.SetURLTemplate(settings.Get(model.SettingBoxURL))
	return c
}

// EffectiveStatsMode 解析实际生效的统计方式。
// auto 模式下探测内核是否带 with_v2ray_api，结果缓存到配置避免重复探测。
func (c *Core) EffectiveStatsMode() string {
	mode := c.Settings.GetDefault(model.SettingStatsMode, "auto")
	if mode != "auto" {
		return mode
	}
	if !c.Box.Installed() {
		return "clash"
	}
	switch c.Settings.Get(model.SettingStatsSupport) {
	case "1":
		return "v2ray"
	case "0":
		return "clash"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if c.Box.SupportsV2RayAPI(ctx) {
		_ = c.Settings.Set(model.SettingStatsSupport, "1")
		return "v2ray"
	}
	_ = c.Settings.Set(model.SettingStatsSupport, "0")
	return "clash"
}

// ConfigPath 返回内核配置文件路径。
func (c *Core) ConfigPath() string { return filepath.Join(c.DataDir, "config.json") }

// Generate 依据数据库生成 sing-box 配置 JSON。
func (c *Core) Generate() ([]byte, error) {
	var inbounds []model.Inbound
	if err := c.DB.Where("enable = ?", true).Order("id").Find(&inbounds).Error; err != nil {
		return nil, err
	}
	var outbounds []model.Outbound
	if err := c.DB.Where("enable = ?", true).Order("id").Find(&outbounds).Error; err != nil {
		return nil, err
	}

	in := config.Input{
		LogLevel: c.Settings.GetDefault(model.SettingLogLevel, "info"),
		Experimental: config.Experimental{
			ClashListen: c.Settings.Get(model.SettingClashListen),
			ClashSecret: c.Settings.Get(model.SettingClashSecret),
			CachePath:   filepath.Join(c.DataDir, "cache.db"),
		},
	}

	// 官方 sing-box 发行版不含 V2Ray API，auto 模式下会自动回退到 Clash API。
	if c.EffectiveStatsMode() == "v2ray" {
		in.Experimental.V2RayListen = c.Settings.Get(model.SettingV2RayListen)
	}

	if raw := c.Settings.Get(model.SettingDNS); raw != "" {
		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err == nil {
			in.DNS = m
		}
	}
	if raw := c.Settings.Get(model.SettingRoute); raw != "" {
		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err == nil {
			in.Route = m
		}
	}
	if in.Route == nil {
		in.Route = map[string]any{}
	}

	// 组装路由规则：广告拦截优先，其次可视化规则；rule_set 一并挂上。
	var routeRules []any
	var ruleSets []any
	if adRules, adSets := c.adblockRoute(); len(adRules) > 0 {
		routeRules = append(routeRules, adRules...)
		ruleSets = append(ruleSets, adSets...)
	}
	if raw := strings.TrimSpace(c.Settings.Get(model.SettingRouteRules)); raw != "" {
		var rules []any
		if err := json.Unmarshal([]byte(raw), &rules); err == nil && len(rules) > 0 {
			routeRules = append(routeRules, rules...)
		}
	}
	if len(routeRules) > 0 {
		in.Route["rules"] = routeRules
	}
	if len(ruleSets) > 0 {
		in.Route["rule_set"] = ruleSets
	}
	if final := strings.TrimSpace(c.Settings.Get(model.SettingRouteFinal)); final != "" {
		in.Route["final"] = final
	}

	sniff := false

	certMap := map[uint]model.Certificate{}
	var certs []model.Certificate
	if err := c.DB.Find(&certs).Error; err != nil {
		return nil, err
	}
	for _, ct := range certs {
		certMap[ct.ID] = ct
	}

	// 用户可通过绑定关系出现在多个入站的用户列表里（同一 UUID 多节点场景）。
	allClients, err := c.enabledClients()
	if err != nil {
		return nil, err
	}
	bindMap, err := BoundClients(c.DB)
	if err != nil {
		return nil, err
	}

	for _, ib := range inbounds {
		clients := pickClients(allClients, bindMap[ib.ID])
		var cert *model.Certificate
		if ib.CertID != nil {
			if ct, ok := certMap[*ib.CertID]; ok {
				cert = &ct
			}
		}
		ci, err := buildInbound(ib, clients, cert)
		if err != nil {
			return nil, fmt.Errorf("入站 [%s]: %w", ib.Tag, err)
		}
		in.Inbounds = append(in.Inbounds, ci)
		if ib.Sniff {
			sniff = true
		}
		for _, cl := range clients {
			in.Experimental.StatsUsers = append(in.Experimental.StatsUsers, cl.Name)
		}
	}
	in.Sniff = sniff

	for _, ob := range outbounds {
		co, err := buildOutbound(ob)
		if err != nil {
			return nil, fmt.Errorf("出站 [%s]: %w", ob.Tag, err)
		}
		// WireGuard 等 endpoint 类型渲染到顶层 endpoints。
		if ob.Type == model.ProtoWireGuard {
			in.Endpoints = append(in.Endpoints, co)
		} else {
			in.Outbounds = append(in.Outbounds, co)
		}
	}

	return config.Build(in)
}

// Apply 生成配置并写入磁盘；若内核在运行且配置有变化则重启使其生效。
// 配置会先通过 sing-box check 校验，校验失败不覆盖现有文件。
// 失败原因会记录在 LastApplyError 里，供面板持续展示（避免只弹一次提示就被忽略）。
func (c *Core) Apply() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	err := c.applyLocked()
	c.errMu.Lock()
	if err != nil {
		c.applyErr = err.Error()
	} else {
		c.applyErr = ""
	}
	c.errMu.Unlock()
	return err
}

// LastApplyError 返回最近一次 Apply 的错误信息（空串表示最近一次成功）。
func (c *Core) LastApplyError() string {
	c.errMu.RLock()
	defer c.errMu.RUnlock()
	return c.applyErr
}

// applyLocked 执行真正的生成 / 校验 / 落盘 / 重启（调用方需持有 mu）。
func (c *Core) applyLocked() error {
	data, err := c.Generate()
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])

	if c.Box.Installed() {
		tmp := c.ConfigPath() + ".check"
		if err := os.WriteFile(tmp, data, 0o644); err != nil {
			return err
		}
		out, err := c.Box.Check(context.Background(), tmp)
		_ = os.Remove(tmp)
		if err != nil {
			return fmt.Errorf("配置校验失败: %v: %s", err, out)
		}
	}

	if hash == c.lastHash && c.Proc.Running() {
		return nil
	}

	if err := os.WriteFile(c.ConfigPath(), data, 0o644); err != nil {
		return err
	}
	c.lastHash = hash

	if c.Proc.Running() {
		return c.Proc.Restart()
	}
	return nil
}

// StartCore 启动内核进程（如已安装）。
func (c *Core) StartCore() error {
	if !c.Box.Installed() {
		return fmt.Errorf("sing-box 未安装")
	}
	return c.Proc.Start()
}

// StopCore 停止内核进程。
func (c *Core) StopCore() error { return c.Proc.Stop() }

// RestartCore 重启内核进程。
func (c *Core) RestartCore() error {
	if !c.Box.Installed() {
		return fmt.Errorf("sing-box 未安装")
	}
	return c.Proc.Restart()
}

// InstallCore 下载并安装指定版本内核。
func (c *Core) InstallCore(ctx context.Context, version string) error {
	// 先停掉内核，避免 Windows 上无法覆盖运行中的 exe。
	_ = c.Proc.Stop()

	c.Box.SetURLTemplate(c.Settings.Get(model.SettingBoxURL))

	if version == "" {
		v, err := c.Box.LatestVersion(ctx)
		if err != nil {
			return err
		}
		version = v
	}
	if err := c.Box.Install(ctx, version); err != nil {
		return err
	}
	c.lastHash = ""
	// 内核更换后需重新探测 V2Ray API 支持情况。
	_ = c.Settings.Set(model.SettingStatsSupport, "")
	return c.Settings.Set(model.SettingBoxVersion, version)
}

// BoxVersion 返回已安装内核版本。
func (c *Core) BoxVersion(ctx context.Context) (string, error) {
	if !c.Box.Installed() {
		return "", nil
	}
	return c.Box.Version(ctx)
}

// CoreStatus 内核状态快照。
type CoreStatus struct {
	Installed  bool   `json:"installed"`
	Version    string `json:"version"`
	Running    bool   `json:"running"`
	ConfigPath string `json:"config_path"`
	// ApplyError 最近一次配置应用失败的原因（空串表示最近一次成功）。
	ApplyError string `json:"apply_error"`
}

// Status 返回内核状态。
func (c *Core) Status(ctx context.Context) CoreStatus {
	st := CoreStatus{
		Installed:  c.Box.Installed(),
		Running:    c.Proc.Running(),
		ConfigPath: c.ConfigPath(),
		ApplyError: c.LastApplyError(),
	}
	if st.Installed {
		if v, err := c.Box.Version(ctx); err == nil {
			st.Version = v
		}
	}
	return st
}
