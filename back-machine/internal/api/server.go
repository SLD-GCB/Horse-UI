package api

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"horseui/back-machine/internal/model"
)

// panelTLS 持有面板 HTTPS 证书，供 tls.Config.GetCertificate 热读取。
type panelTLS struct {
	mu   sync.RWMutex
	cert *tls.Certificate
	id   uint // 当前证书对应的 model.Certificate ID
}

func (p *panelTLS) load(certPath, keyPath string) error {
	c, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.cert = &c
	p.mu.Unlock()
	return nil
}

func (p *panelTLS) setID(id uint) {
	p.mu.Lock()
	p.id = id
	p.mu.Unlock()
}

func (p *panelTLS) currentID() uint {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.id
}

func (p *panelTLS) has() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cert != nil
}

// GetCertificate 每次 TLS 握手读取当前证书，实现续签后免重启生效。
func (p *panelTLS) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.cert == nil {
		return nil, fmt.Errorf("面板证书未加载")
	}
	return p.cert, nil
}

// loadPanelCert 依据设置从数据库读取并加载面板证书。
func (a *App) loadPanelCert() error {
	raw := strings.TrimSpace(a.settings.Get(model.SettingPanelTLSCertID))
	if raw == "" {
		return fmt.Errorf("未选择面板证书")
	}
	id, err := strconv.Atoi(raw)
	if err != nil {
		return fmt.Errorf("面板证书 ID 无效")
	}
	var cert model.Certificate
	if err := a.db.First(&cert, id).Error; err != nil {
		return fmt.Errorf("面板证书不存在")
	}
	if cert.CertPath == "" || cert.KeyPath == "" {
		return fmt.Errorf("证书文件路径为空")
	}
	if err := a.panelTLS.load(cert.CertPath, cert.KeyPath); err != nil {
		// 加载失败时保留旧证书，避免一次异常导致面板失联。
		return err
	}
	a.panelTLS.setID(uint(id))
	return nil
}

// reloadPanelCertIf 指定证书正用于面板 HTTPS 时，热加载新文件（续签后自动生效）。
func (a *App) reloadPanelCertIf(certID uint) {
	if a.settings.Get(model.SettingPanelTLSEnable) != "1" {
		return
	}
	if a.panelTLS.currentID() != certID {
		return
	}
	if err := a.loadPanelCert(); err != nil {
		fmt.Printf("[horseui] 面板证书热加载失败（保留旧证书）: %v\n", err)
	}
}

// effectiveListen 返回实际使用的监听地址：设置项优先，其次启动参数。
func (a *App) effectiveListen() string {
	if v := strings.TrimSpace(a.settings.Get(model.SettingPanelListen)); v != "" {
		return v
	}
	return a.opts.Listen
}

// startHTTP 启动面板 HTTP(S) 服务。
func (a *App) startHTTP() error {
	addr := a.effectiveListen()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// 不锁死：设置项里的地址不可用时回退到启动参数，保证面板仍能访问。
		if addr != a.opts.Listen {
			fmt.Printf("[horseui] 无法监听 %s（%v），已回退到 %s\n", addr, err, a.opts.Listen)
			addr = a.opts.Listen
			ln, err = net.Listen("tcp", addr)
		}
		if err != nil {
			return err
		}
	}

	srv := &http.Server{
		Handler:           a.engine,
		ReadHeaderTimeout: 15 * time.Second,
	}

	tlsEnabled := a.settings.Get(model.SettingPanelTLSEnable) == "1"
	if tlsEnabled {
		if err := a.loadPanelCert(); err != nil {
			// 安全降级：证书不可用时回落明文，绝不因证书问题启动失败。
			fmt.Printf("[horseui] 面板 HTTPS 证书加载失败，已回落明文 HTTP: %v\n", err)
			tlsEnabled = false
		}
	}

	if !tlsEnabled {
		url := "http://" + panelHostPort(addr)
		fmt.Printf("[horseui] 面板已启动: %s\n", url)
		a.maybeOpenBrowser(url)
		return srv.Serve(ln)
	}

	srv.TLSConfig = &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: a.panelTLS.GetCertificate,
	}
	a.startHTTPRedirect(addr)
	url := "https://" + panelHostPort(addr)
	fmt.Printf("[horseui] 面板已启动: %s\n", url)
	a.maybeOpenBrowser(url)
	// 证书由 GetCertificate 提供，证书文件参数留空。
	return srv.ServeTLS(ln, "", "")
}

// startHTTPRedirect 在配置的明文地址上把请求 308 跳转到 HTTPS。
func (a *App) startHTTPRedirect(httpsAddr string) {
	httpAddr := strings.TrimSpace(a.settings.Get(model.SettingPanelHTTPListen))
	if httpAddr == "" {
		return
	}
	_, httpsPort := splitHostPort(httpsAddr)
	go func() {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			target := "https://" + net.JoinHostPort(hostOnly(r.Host), httpsPort) + r.URL.RequestURI()
			http.Redirect(w, r, target, http.StatusPermanentRedirect)
		})
		srv := &http.Server{Addr: httpAddr, Handler: handler, ReadHeaderTimeout: 15 * time.Second}
		if err := srv.ListenAndServe(); err != nil {
			fmt.Printf("[horseui] 明文跳转服务异常: %v\n", err)
		}
	}()
}

func (a *App) maybeOpenBrowser(url string) {
	if !a.opts.Open {
		return
	}
	go func() {
		time.Sleep(1200 * time.Millisecond)
		openBrowser(url)
	}()
}

// openBrowser 用系统默认浏览器打开地址。
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func splitHostPort(addr string) (string, string) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, "8080"
	}
	return host, port
}

// panelHostPort 把 0.0.0.0/:: 之类的监听地址转换为可访问地址。
func panelHostPort(addr string) string {
	host, port := splitHostPort(addr)
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}
