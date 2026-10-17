// Package panelmenu 提供类似 s-ui 的终端交互式管理菜单。
// 菜单自身不启动 HTTP 服务：Linux 服务化安装时优先用 systemd 管理，
// 否则以「后台子进程 + PID 文件」的方式管理面板。
package panelmenu

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"horseui/back-machine/internal/model"
	"horseui/back-machine/internal/store"
	"horseui/back-machine/internal/svc"
	"horseui/back-machine/internal/sysinfo"
	"horseui/back-machine/internal/util"
)

// Options 菜单运行参数。
type Options struct {
	DataDir string
	Listen  string
	Version string
}

// Run 进入交互式菜单，直到用户选择退出。
func Run(opts Options) {
	if abs, err := filepath.Abs(opts.DataDir); err == nil {
		opts.DataDir = abs
	}
	cleanupOldBinary() // 清掉上次自更新留下的备份

	rd := bufio.NewReader(os.Stdin)
	for {
		printMenu(opts)
		line, err := rd.ReadString('\n')
		if err != nil {
			return // stdin 关闭（非交互场景），直接退出
		}
		switch strings.TrimSpace(line) {
		case "1":
			startPanel(opts)
		case "2":
			stopPanel(opts)
		case "3":
			restartPanel(opts)
		case "4":
			showStatus(opts)
		case "5":
			showAdmin(opts)
		case "6":
			resetPassword(opts)
		case "7":
			kernelMenu(opts, rd)
		case "8":
			showLogs(opts)
		case "9":
			updatePanel(opts, rd)
		case "10":
			uninstallPanel(opts, rd)
		case "11":
			tuneMenu(opts, rd)
		case "0", "q", "Q", "exit":
			return
		default:
			fmt.Println("  无效选项，请输入 0-11")
		}
		fmt.Print("\n按回车返回菜单 ...")
		if _, err := rd.ReadString('\n'); err != nil {
			return
		}
	}
}

func printMenu(opts Options) {
	fmt.Println()
	fmt.Println("============================================================")
	fmt.Printf("  HorseUI 管理菜单   v%s\n", opts.Version)
	fmt.Printf("  数据目录: %s\n", opts.DataDir)
	fmt.Printf("  监听地址: %s\n", opts.Listen)
	fmt.Printf("  管理方式: %s\n", manageMode())
	fmt.Println("------------------------------------------------------------")
	fmt.Println("   1. 启动面板")
	fmt.Println("   2. 停止面板")
	fmt.Println("   3. 重启面板")
	fmt.Println("   4. 查看状态与访问地址")
	fmt.Println("   5. 查看管理员账号与密码")
	fmt.Println("   6. 重置管理员密码（随机生成）")
	fmt.Println("   7. 内核管理（安装 / 更新 / 查看版本）")
	fmt.Println("   8. 查看日志")
	fmt.Println("   9. 更新面板")
	fmt.Println("  10. 卸载面板")
	fmt.Println("  11. 系统调优（弱网优化：UDP 缓冲 / BBR，可回滚）")
	fmt.Println("   0. 退出")
	fmt.Println("============================================================")
	fmt.Print("请输入选项: ")
}

func manageMode() string {
	if systemdAvailable() {
		return "systemd 服务（systemctl）"
	}
	return "本菜单后台进程"
}

// ---------------- 启停 ----------------

func startPanel(opts Options) {
	if panelAlive(opts) {
		fmt.Println("  面板已在运行，如需重启请选 3")
		return
	}
	if systemdAvailable() {
		if err := systemctlRun("start", "horseui"); err != nil {
			fmt.Println("  " + err.Error())
			return
		}
		waitAndReport(opts, "systemd 服务已启动")
		return
	}
	if readPID(opts.DataDir) > 0 {
		removePID(opts.DataDir) // 清掉陈旧的 PID 文件
	}

	self, err := os.Executable()
	if err != nil {
		fmt.Println("  无法定位可执行文件:", err)
		return
	}
	logFile := logPath(opts.DataDir)
	if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil {
		fmt.Println("  创建日志目录失败:", err)
		return
	}
	before := fileSize(logFile)
	lf, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Println("  打开日志文件失败:", err)
		return
	}
	defer lf.Close()

	cmd := exec.Command(self, "-run", "-data", opts.DataDir, "-listen", opts.Listen, "-open=false")
	cmd.Stdout = lf
	cmd.Stderr = lf
	cmd.SysProcAttr = detachAttrs()
	if err := cmd.Start(); err != nil {
		fmt.Println("  启动失败:", err)
		return
	}
	pid := cmd.Process.Pid
	_ = writePID(opts.DataDir, pid)
	_ = cmd.Process.Release()

	waitAndReport(opts, fmt.Sprintf("面板已启动（PID %d）", pid))
	// 把本次启动新增的日志里与密码有关的行回显出来（首次安装会打印初始密码）。
	for _, l := range newLines(logFile, before) {
		if strings.Contains(l, "密码:") {
			fmt.Println("  " + strings.TrimSpace(l))
		}
	}
	fmt.Printf("  完整日志: %s\n", logFile)
}

func stopPanel(opts Options) {
	if pid := readPID(opts.DataDir); pid > 0 {
		if err := killTree(pid); err != nil {
			fmt.Printf("  停止 PID %d 失败: %v\n", pid, err)
			return
		}
		removePID(opts.DataDir)
		time.Sleep(900 * time.Millisecond)
		fmt.Printf("  已停止（PID %d）\n", pid)
		return
	}
	if systemdAvailable() {
		if err := systemctlRun("stop", "horseui"); err != nil {
			fmt.Println("  " + err.Error())
			return
		}
		fmt.Println("  已停止（systemd 服务）")
		return
	}
	if panelAlive(opts) {
		fmt.Println("  无法停止：面板不是本菜单启动的（可能以前台或其它方式运行）")
		return
	}
	fmt.Println("  面板未在运行")
}

func restartPanel(opts Options) {
	stopPanel(opts)
	startPanel(opts)
}

func waitAndReport(opts Options, okMsg string) {
	time.Sleep(2 * time.Second)
	if panelAlive(opts) {
		fmt.Println("  " + okMsg)
		fmt.Printf("  访问地址: %s\n", accessURL(opts, effectiveListen(opts)))
	} else {
		fmt.Println("  已发出启动命令，但端口暂未监听，请用「8. 查看日志」排查")
	}
}

// ---------------- 状态 ----------------

func showStatus(opts Options) {
	listen := effectiveListen(opts)
	if panelAlive(opts) {
		fmt.Println("  运行状态: 运行中")
	} else {
		fmt.Println("  运行状态: 已停止")
	}
	if pid := readPID(opts.DataDir); pid > 0 {
		fmt.Printf("  进程 PID : %d\n", pid)
	}
	fmt.Printf("  管理方式 : %s\n", manageMode())
	fmt.Printf("  数据目录 : %s\n", opts.DataDir)
	fmt.Printf("  监听地址 : %s\n", listen)

	db, settings, err := openStore(opts.DataDir)
	if err != nil {
		fmt.Printf("  配置读取 : %v\n", err)
		return
	}
	scheme := "http"
	if settings.Get(model.SettingPanelTLSEnable) == "1" {
		scheme = "https"
	}
	_, port := splitListen(listen)
	fmt.Println("  访问地址 :")
	if d := strings.TrimSpace(settings.Get(model.SettingPanelDomain)); d != "" {
		fmt.Printf("    %s://%s\n", scheme, d)
	}
	fmt.Printf("    %s://127.0.0.1:%s\n", scheme, port)
	for _, ifi := range sysinfo.Interfaces() {
		if ifi.Loopback {
			continue
		}
		for _, addr := range ifi.Addresses {
			fmt.Printf("    %s://%s:%s  (%s)\n", scheme, addr, port, ifi.Name)
		}
	}
	if settings.Get(model.SettingPanelTLSEnable) != "1" {
		fmt.Println("  提示     : 面板 HTTPS 未开启，建议在「面板设置」中开启")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	core := svc.NewCore(opts.DataDir, db, settings)
	if v, err := core.BoxVersion(ctx); err == nil && v != "" {
		fmt.Printf("  内核版本 : %s\n", v)
	} else {
		fmt.Println("  内核版本 : 未安装（可在菜单 7 安装）")
	}
}

// ---------------- 管理员账号 ----------------

func showAdmin(opts Options) {
	db, settings, err := openStore(opts.DataDir)
	if err != nil {
		fmt.Println("  " + err.Error())
		return
	}
	var admin model.Admin
	if err := db.Order("id").First(&admin).Error; err != nil {
		fmt.Println("  未找到管理员账号:", err)
		return
	}
	fmt.Printf("  账号: %s\n", admin.Username)

	pw := strings.TrimSpace(settings.Get(model.SettingAdminLastPassword))
	if pw != "" {
		fmt.Printf("  密码: %s\n", pw)
		fmt.Println("  说明: 这是面板最近一次「自动生成 / 重置」的密码；")
		fmt.Println("        若你已在面板网页上手动改过密码，此处会失效（改用选项 6 重置）")
	} else {
		fmt.Println("  密码: 不可查看（已由你在面板里手动修改过）")
		fmt.Println("  说明: 密码以 bcrypt 哈希存于数据库，无法反解出原文；")
		fmt.Println("        面板只在「自己生成」密码时留一份明文（settings 表 admin_last_password）。")
		fmt.Println("        要拿到可用密码，请用选项 6 重置为新的随机密码。")
	}
}

func resetPassword(opts Options) {
	db, settings, err := openStore(opts.DataDir)
	if err != nil {
		fmt.Println("  " + err.Error())
		return
	}
	var admin model.Admin
	if err := db.Order("id").First(&admin).Error; err != nil {
		fmt.Println("  未找到管理员账号:", err)
		return
	}
	pass := util.RandomPassword(16)
	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		fmt.Println("  生成密码失败:", err)
		return
	}
	admin.Password = string(hash)
	admin.TokenVersion = max(admin.TokenVersion, 1) + 1
	admin.MustChangePassword = false
	if err := db.Save(&admin).Error; err != nil {
		fmt.Println("  保存失败:", err)
		return
	}
	// 记一份明文，供「查看管理员账号与密码」读取。
	if settings != nil {
		_ = settings.Set(model.SettingAdminLastPassword, pass)
	}
	fmt.Println()
	fmt.Printf("  账号: %s\n", admin.Username)
	fmt.Printf("  新密码: %s\n", pass)
	fmt.Println("  请立即记录；旧登录会话将失效（运行中的面板 1 分钟内生效）")
}

// ---------------- 内核 ----------------

func kernelMenu(opts Options, rd *bufio.Reader) {
	db, settings, err := openStore(opts.DataDir)
	if err != nil {
		fmt.Println("  " + err.Error())
		return
	}
	core := svc.NewCore(opts.DataDir, db, settings)
	ctx := context.Background()

	if v, err := core.BoxVersion(ctx); err == nil && v != "" {
		fmt.Printf("  当前内核版本: %s\n", v)
	} else {
		fmt.Println("  当前内核: 未安装")
	}
	if panelAlive(opts) {
		fmt.Println("  注意: 面板正在运行，更新内核前建议先停止面板（菜单 2）")
	}
	fmt.Print("  输入要安装的版本（回车=最新版，0=取消）: ")
	line, err := rd.ReadString('\n')
	if err != nil {
		return
	}
	ver := strings.TrimSpace(line)
	if ver == "0" {
		return
	}
	fmt.Println("  正在下载安装（GitHub 较慢时请耐心等待）...")
	if err := core.InstallCore(ctx, ver); err != nil {
		fmt.Println("  安装失败:", err)
		return
	}
	if v, err := core.BoxVersion(ctx); err == nil {
		fmt.Printf("  内核安装完成，当前版本: %s\n", v)
	} else {
		fmt.Println("  内核安装完成")
	}
}

// ---------------- 日志 ----------------

func showLogs(opts Options) {
	file := logPath(opts.DataDir)
	b, err := os.ReadFile(file)
	if err != nil {
		fmt.Println("  暂无日志文件")
		if systemdAvailable() {
			fmt.Println("  服务方式运行，请用： journalctl -u horseui -n 80 --no-pager")
		} else {
			fmt.Println("  （日志仅在通过本菜单启动面板时写入该文件）")
		}
		return
	}
	lines := strings.Split(strings.TrimRight(string(b), "\r\n"), "\n")
	const maxLines = 60
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	fmt.Printf("  ---- %s（末 %d 行）----\n", file, len(lines))
	for _, l := range lines {
		fmt.Println("  " + strings.TrimRight(l, "\r"))
	}
	if systemdAvailable() {
		fmt.Println("  提示: systemd 方式运行时完整日志见 journalctl -u horseui -n 80 --no-pager")
	}
}

// ---------------- 更新 ----------------

func updatePanel(opts Options, rd *bufio.Reader) {
	self, err := selfPath()
	if err != nil {
		fmt.Println("  无法定位当前程序:", err)
		return
	}
	fmt.Printf("  当前程序: %s\n", self)
	def := filepath.Join(filepath.Dir(self), "horseui.new")
	fmt.Printf("  新版本文件路径（回车用 %s，输入 0 取消）:\n  > ", def)
	line, err := rd.ReadString('\n')
	if err != nil {
		return
	}
	p := strings.TrimSpace(line)
	if p == "0" {
		fmt.Println("  已取消")
		return
	}
	if p == "" {
		p = def
	}
	if err := looksLikeBinary(p); err != nil {
		fmt.Println("  文件校验失败: " + err.Error())
		return
	}

	wasRunning := panelAlive(opts)
	if wasRunning {
		fmt.Println("  先停止面板 ...")
		stopPanel(opts)
	}

	if err := replaceSelf(p); err != nil {
		fmt.Println("  更新失败: " + err.Error())
		if wasRunning {
			startPanel(opts)
		}
		return
	}
	fmt.Println("  程序文件已更新。")

	if wasRunning {
		fmt.Println("  重新启动面板 ...")
		startPanel(opts)
	}
	fmt.Println("  注意: 当前菜单仍是旧版本进程，退出后重新运行即可使用新版本。")
}

// ---------------- 卸载 ----------------

func uninstallPanel(opts Options, rd *bufio.Reader) {
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		fmt.Println("  提示: Linux 下卸载需要 root 权限（请用 sudo horseui 重新运行）")
	}
	fmt.Println()
	fmt.Println("  即将卸载 HorseUI 面板，请选择卸载方式：")
	fmt.Println("    wipe  → 卸载程序，并彻底删除数据（数据库 / 证书 / 节点配置 / 内核）")
	fmt.Println("    keep  → 只卸载程序，保留数据（以后重装可恢复）")
	fmt.Print("  请输入（其它任意输入 = 取消）: ")
	line, err := rd.ReadString('\n')
	if err != nil {
		return
	}
	mode := strings.ToLower(strings.TrimSpace(line))
	if mode != "wipe" && mode != "keep" {
		fmt.Println("  已取消")
		return
	}

	// 停止面板（含 systemd 服务）
	if pid := readPID(opts.DataDir); pid > 0 {
		_ = killTree(pid)
		removePID(opts.DataDir)
	}
	if systemdAvailable() {
		_ = systemctlRun("stop", "horseui")
	}

	done, err := platformUninstall()
	if err != nil {
		fmt.Println("  卸载出错: " + err.Error())
	}
	for _, d := range done {
		fmt.Println("  " + d)
	}

	if mode == "wipe" {
		if err := os.RemoveAll(opts.DataDir); err != nil {
			fmt.Printf("  删除数据目录失败: %v\n", err)
		} else {
			fmt.Printf("  已删除数据目录 %s\n", opts.DataDir)
		}
	} else {
		fmt.Printf("  已保留数据目录 %s\n", opts.DataDir)
	}
	fmt.Println("  卸载完成，再见。")
	fmt.Print("\n按回车退出 ...")
	_, _ = rd.ReadString('\n')
	os.Exit(0)
}

// ---------------- 辅助 ----------------

func openStore(dataDir string) (*gorm.DB, *store.Settings, error) {
	dbFile := filepath.Join(dataDir, "horseui.db")
	if _, err := os.Stat(dbFile); err != nil {
		return nil, nil, fmt.Errorf("未找到数据库 %s，请先用菜单 1 启动一次面板", dbFile)
	}
	db, err := store.Open(dataDir)
	if err != nil {
		return nil, nil, err
	}
	s, err := store.NewSettings(db)
	if err != nil {
		return nil, nil, err
	}
	return db, s, nil
}

// effectiveListen 返回面板实际监听地址（设置项优先，其次启动参数）。
func effectiveListen(opts Options) string {
	_, settings, err := openStore(opts.DataDir)
	if err == nil {
		if v := strings.TrimSpace(settings.Get(model.SettingPanelListen)); v != "" {
			return v
		}
	}
	return opts.Listen
}

// panelAlive 判断面板是否在运行。
// HTTPS 时完成一次完整 TLS 握手，避免在面板日志里留下难看的 "TLS handshake error"。
func panelAlive(opts Options) bool {
	listen := effectiveListen(opts)
	host, port := splitListen(listen)
	if port == "" {
		return false
	}
	addr := net.JoinHostPort(normalizeHost(host), port)
	conn, err := net.DialTimeout("tcp", addr, 1500*time.Millisecond)
	if err != nil {
		return false
	}
	defer conn.Close()
	if panelHTTPS(opts) {
		tc := tls.Client(conn, &tls.Config{InsecureSkipVerify: true, ServerName: normalizeHost(host)})
		_ = tc.SetDeadline(time.Now().Add(2 * time.Second))
		_ = tc.Handshake() // 端口已连通即视为存活，握手结果仅用于消除日志噪音
	}
	return true
}

func panelHTTPS(opts Options) bool {
	_, settings, err := openStore(opts.DataDir)
	return err == nil && settings.Get(model.SettingPanelTLSEnable) == "1"
}

func accessURL(opts Options, listen string) string {
	_, settings, err := openStore(opts.DataDir)
	scheme := "http"
	if err == nil && settings.Get(model.SettingPanelTLSEnable) == "1" {
		scheme = "https"
	}
	host, port := splitListen(listen)
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("%s://%s:%s", scheme, host, port)
}

func newLines(file string, offset int64) []string {
	b, err := os.ReadFile(file)
	if err != nil || int64(len(b)) <= offset {
		return nil
	}
	part := strings.TrimRight(string(b[offset:]), "\r\n")
	if part == "" {
		return nil
	}
	return strings.Split(part, "\n")
}
