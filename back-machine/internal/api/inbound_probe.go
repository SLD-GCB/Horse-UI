package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/proxy"

	"horseui/back-machine/internal/model"
	"horseui/back-machine/internal/svc"
	"horseui/core/stats"
)

// testInbound 对入站做真实代理测速：
// 临时启动一个 sing-box 客户端（本地 SOCKS 入站 → 该节点出站），经它访问测速地址并计时。
func (a *App) testInbound(c *gin.Context) {
	id, okID := parseID(c)
	if !okID {
		return
	}
	var ib model.Inbound
	if err := a.db.First(&ib, id).Error; err != nil {
		fail(c, 404, "入站不存在")
		return
	}
	if !ib.Enable {
		fail(c, 400, "该入站已停用")
		return
	}
	if !a.core.Box.Installed() {
		fail(c, 400, "内核未安装，无法测速")
		return
	}
	if !a.core.Proc.Running() {
		fail(c, 400, "内核未运行，无法测速")
		return
	}

	// 取一个启用用户作为测速凭据。
	var cl model.Client
	if err := a.db.Where("inbound_id = ? AND enable = ?", ib.ID, true).Order("id").First(&cl).Error; err != nil {
		fail(c, 400, "该入站没有可用用户，无法测速")
		return
	}

	testURL := strings.TrimSpace(a.settings.GetDefault(model.SettingTestURL, stats.DefaultTestURL))
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	delay, err := a.probeInbound(ctx, ib, cl, testURL)
	if err != nil {
		fail(c, 502, "测速失败："+err.Error())
		return
	}
	ok(c, gin.H{"id": ib.ID, "tag": ib.Tag, "delay": delay})
}

// probeInbound 生成临时客户端配置、启动客户端并经 SOCKS5 访问 testURL，返回耗时（毫秒）。
func (a *App) probeInbound(ctx context.Context, ib model.Inbound, cl model.Client, testURL string) (int, error) {
	ob := svc.BuildSingboxOutbound(probeHost(ib.Listen), ib, cl)
	if ob == nil {
		return 0, fmt.Errorf("该入站类型暂不支持测速")
	}
	tag, _ := ob["tag"].(string)
	if tag == "" {
		tag = "proxy"
		ob["tag"] = tag
	}

	port, err := freeLocalPort()
	if err != nil {
		return 0, err
	}

	cfg := map[string]any{
		"log": map[string]any{"level": "error"},
		"inbounds": []any{
			map[string]any{"type": "socks", "tag": "probe-in", "listen": "127.0.0.1", "listen_port": port},
		},
		"outbounds": []any{ob},
		"route":     map[string]any{"final": tag},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return 0, err
	}

	tmpDir := filepath.Join(a.opts.DataDir, "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return 0, err
	}
	cfgFile := filepath.Join(tmpDir, fmt.Sprintf("probe-%d-%d.json", ib.ID, time.Now().UnixNano()))
	if err := os.WriteFile(cfgFile, data, 0o600); err != nil {
		return 0, err
	}
	defer os.Remove(cfgFile)

	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, a.core.Box.Path(), "run", "-c", cfgFile)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("启动测速客户端失败: %w", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_, _ = cmd.Process.Wait()
	}()

	if err := waitPort(ctx, port, 4*time.Second); err != nil {
		return 0, fmt.Errorf("测速客户端未就绪: %s", lastLine(out.String()))
	}

	dialer, err := proxy.SOCKS5("tcp", fmt.Sprintf("127.0.0.1:%d", port), nil, proxy.Direct)
	if err != nil {
		return 0, err
	}
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			},
			DisableKeepAlives: true,
		},
		Timeout: 8 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	start := time.Now()
	resp, err := client.Get(testURL)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return int(time.Since(start).Milliseconds()), nil
}

// probeHost 把监听地址归一化为可直连的地址。
func probeHost(listen string) string {
	h := strings.Trim(strings.TrimSpace(listen), "[]")
	switch h {
	case "", "::", "0.0.0.0", "*", "::1":
		return "127.0.0.1"
	}
	return h
}

// freeLocalPort 取一个本机空闲端口。
func freeLocalPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// waitPort 轮询等待本地端口可连接。
func waitPort(ctx context.Context, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(120 * time.Millisecond)
	}
	return fmt.Errorf("等待端口 %d 超时", port)
}

// lastLine 取输出末尾一行用于错误提示。
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) == 0 {
		return ""
	}
	l := strings.TrimSpace(lines[len(lines)-1])
	if len(l) > 160 {
		l = l[:160]
	}
	return l
}
