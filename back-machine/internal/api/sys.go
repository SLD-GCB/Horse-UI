package api

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"horseui/back-machine/internal/model"
	"horseui/back-machine/internal/sysinfo"
)

var appStartTime = time.Now()

// panelAddress 面板自身的访问信息。
type panelAddress struct {
	Current string   `json:"current"` // 当前访问地址
	Domain  string   `json:"domain"`  // 用户设置的面板域名（可能为空）
	Listen  string   `json:"listen"`  // 当前生效的监听地址
	Scheme  string   `json:"scheme"`
	Port    string   `json:"port"`
	URLs    []string `json:"urls"` // 候选访问地址
}

// panelAddressInfo 汇总面板可访问地址：自定义域名 + 本机各网卡 + 回环。
func (a *App) panelAddressInfo(c *gin.Context) panelAddress {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	_, port := splitHostPort(a.effectiveListen())

	pa := panelAddress{Scheme: scheme, Port: port, URLs: []string{}, Listen: a.effectiveListen()}
	seen := map[string]bool{}
	add := func(u string) {
		if u == "" || seen[u] {
			return
		}
		seen[u] = true
		pa.URLs = append(pa.URLs, u)
	}

	// 自定义域名（优先展示）。
	if d := strings.TrimSpace(a.settings.Get(model.SettingPanelDomain)); d != "" {
		d = strings.TrimPrefix(d, "http://")
		d = strings.TrimPrefix(d, "https://")
		d = strings.TrimSuffix(d, "/")
		pa.Domain = d
		add("https://" + d)
		add("http://" + d)
	}

	// 本机各网卡（非回环、仅 IPv4，避免 IPv6 拼接问题）。
	for _, nic := range sysinfo.Interfaces() {
		if nic.Loopback {
			continue
		}
		for _, addr := range nic.Addresses {
			if strings.Contains(addr, ":") {
				continue
			}
			add(fmt.Sprintf("%s://%s:%s", scheme, addr, port))
		}
	}
	add(fmt.Sprintf("%s://127.0.0.1:%s", scheme, port))

	pa.Current = scheme + "://" + c.Request.Host
	return pa
}

func (a *App) sysInfo(c *gin.Context) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	info := gin.H{
		"version":    a.opts.Version,
		"os":         runtime.GOOS,
		"arch":       runtime.GOARCH,
		"go_version": runtime.Version(),
		"cpus":       runtime.NumCPU(),
		"uptime":     int64(time.Since(appStartTime).Seconds()),
		"mem_alloc":  ms.Alloc,
		"mem_sys":    ms.Sys,
		"goroutines": runtime.NumGoroutine(),
		"core":       a.core.Status(c.Request.Context()),
		"panel":      a.panelAddressInfo(c),
	}
	if host, err := os.Hostname(); err == nil {
		info["hostname"] = host
	}
	ok(c, info)
}

func (a *App) coreStatus(c *gin.Context) {
	ok(c, a.core.Status(c.Request.Context()))
}

func (a *App) coreInstall(c *gin.Context) {
	var req struct {
		Version string `json:"version"`
	}
	_ = c.ShouldBindJSON(&req)

	if err := a.core.InstallCore(c.Request.Context(), req.Version); err != nil {
		serverError(c, err)
		return
	}
	applyErr := a.core.Apply()
	startErr := a.core.StartCore()
	ok(c, gin.H{
		"core":        a.core.Status(c.Request.Context()),
		"apply_error": errStr(applyErr),
		"start_error": errStr(startErr),
	})
}

func (a *App) coreStart(c *gin.Context) {
	if err := a.core.StartCore(); err != nil {
		serverError(c, err)
		return
	}
	ok(c, a.core.Status(c.Request.Context()))
}

func (a *App) coreStop(c *gin.Context) {
	if err := a.core.StopCore(); err != nil {
		serverError(c, err)
		return
	}
	ok(c, a.core.Status(c.Request.Context()))
}

func (a *App) coreRestart(c *gin.Context) {
	if err := a.core.Apply(); err != nil {
		serverError(c, err)
		return
	}
	if err := a.core.RestartCore(); err != nil {
		serverError(c, err)
		return
	}
	ok(c, a.core.Status(c.Request.Context()))
}

func (a *App) coreLogs(c *gin.Context) {
	ok(c, gin.H{"lines": a.core.Proc.Logs()})
}

func (a *App) coreLogsClear(c *gin.Context) {
	a.core.Proc.ClearLogs()
	ok(c, nil)
}

func (a *App) coreConfig(c *gin.Context) {
	data, err := a.core.Generate()
	if err != nil {
		serverError(c, err)
		return
	}
	ok(c, gin.H{"config": string(data)})
}
