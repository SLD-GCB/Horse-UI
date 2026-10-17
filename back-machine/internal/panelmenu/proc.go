package panelmenu

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const pidFileName = "horseui.pid"

func pidPath(dataDir string) string { return filepath.Join(dataDir, pidFileName) }

// logPath 菜单启动的面板日志文件（子进程 stdout/stderr 重定向到此）。
func logPath(dataDir string) string { return filepath.Join(dataDir, "logs", "horseui.log") }

// IsTerminal 判断标准输入是否为交互终端（用于决定是否显示菜单）。
func IsTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func readPID(dataDir string) int {
	b, err := os.ReadFile(pidPath(dataDir))
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func writePID(dataDir string, pid int) error {
	return os.WriteFile(pidPath(dataDir), []byte(strconv.Itoa(pid)), 0o644)
}

func removePID(dataDir string) { _ = os.Remove(pidPath(dataDir)) }

// normalizeHost 把通配监听地址转换为可连接的地址。
func normalizeHost(host string) string {
	switch host {
	case "", "0.0.0.0":
		return "127.0.0.1"
	case "::", "[::]":
		return "::1"
	}
	return host
}

// splitListen 拆分监听地址为 host / port，容错常见写法。
func splitListen(listen string) (string, string) {
	listen = strings.TrimSpace(listen)
	host, port, err := net.SplitHostPort(listen)
	if err == nil {
		return strings.Trim(host, "[]"), port
	}
	if strings.HasPrefix(listen, ":") {
		return "", strings.TrimPrefix(listen, ":")
	}
	return "", "8080"
}

func fileSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// looksLikeBinary 粗略校验文件是不是可执行程序（ELF 或 Windows PE）。
func looksLikeBinary(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("无法读取该文件: %w", err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return fmt.Errorf("这是一个目录，不是文件")
	}
	if fi.Size() < 1024*1024 {
		return fmt.Errorf("文件过小（%d 字节），不像是面板程序", fi.Size())
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(f, head); err != nil {
		return err
	}
	switch {
	case head[0] == 0x7f && head[1] == 'E' && head[2] == 'L' && head[3] == 'F':
		return nil // Linux ELF
	case head[0] == 'M' && head[1] == 'Z':
		return nil // Windows PE
	default:
		return fmt.Errorf("不是可识别的可执行文件（ELF/PE）")
	}
}

// selfPath 返回当前可执行文件的真实路径（解析软链接）。
func selfPath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved, nil
	}
	return p, nil
}

// replaceSelf 用 newPath 覆盖当前正在运行的程序。
// Windows 与 Linux 都允许重命名运行中的可执行文件，因此先把旧文件改名做备份再写入。
func replaceSelf(newPath string) error {
	self, err := selfPath()
	if err != nil {
		return err
	}
	newAbs, _ := filepath.Abs(newPath)
	if newAbs == self {
		return fmt.Errorf("新文件与当前程序是同一个文件")
	}

	backup := self + ".old"
	_ = os.Remove(backup)
	if err := os.Rename(self, backup); err != nil {
		return fmt.Errorf("备份当前程序失败（是否有写权限？）: %w", err)
	}
	if err := copyFile(newPath, self); err != nil {
		_ = os.Rename(backup, self) // 失败回滚
		return err
	}
	return os.Chmod(self, 0o755)
}

// cleanupOldBinary 清理上次自更新留下的备份（运行中的旧映像此时已可删除）。
func cleanupOldBinary() {
	self, err := selfPath()
	if err != nil {
		return
	}
	_ = os.Remove(self + ".old")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
