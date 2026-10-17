//go:build !windows

package sysinfo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// tuneConfFile 收敛到独立文件，绝不 sed 污染 /etc/sysctl.conf。
const tuneConfFile = "/etc/sysctl.d/99-horseui.conf"

func tuneSupported() bool { return true }

func tuneConfPath() string { return tuneConfFile }

// tuneReadSysctl 读 /proc/sys 下的当前值；参数不存在时返回 false（由调用方跳过）。
func tuneReadSysctl(key string) (string, bool) {
	b, err := os.ReadFile("/proc/sys/" + strings.ReplaceAll(key, ".", "/"))
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}

// tuneBBRAvailable 内核是否可用 bbr；不可用时尝试 modprobe tcp_bbr。
func tuneBBRAvailable() bool {
	available := func() bool {
		b, err := os.ReadFile("/proc/sys/net/ipv4/tcp_available_congestion_control")
		return err == nil && strings.Contains(string(b), "bbr")
	}
	if available() {
		return true
	}
	modprobe, err := exec.LookPath("modprobe")
	if err != nil {
		return false
	}
	if err := exec.Command(modprobe, "tcp_bbr").Run(); err != nil {
		return false
	}
	return available()
}

// tuneInContainer 容器里 net.* 多为命名空间参数，且 /proc/sys 常为只读。
func tuneInContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if b, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		s := string(b)
		if strings.Contains(s, "docker") || strings.Contains(s, "kubepods") || strings.Contains(s, "containerd") {
			return true
		}
	}
	return false
}

// tuneCanWrite 需要 root 且 /etc/sysctl.d 真的可写（容器里常见只读挂载）。
func tuneCanWrite() bool {
	if os.Geteuid() != 0 {
		return false
	}
	dir := filepath.Dir(tuneConfFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	f, err := os.CreateTemp(dir, ".horseui-tune-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

func tuneWriteConf(content string) error {
	if err := os.MkdirAll(filepath.Dir(tuneConfFile), 0o755); err != nil {
		return err
	}
	tmp := tuneConfFile + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, tuneConfFile)
}

func tuneRemoveConf() error {
	err := os.Remove(tuneConfFile)
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	return err
}

// tuneSetValue 直接写 /proc/sys（等价于 sysctl -w），不依赖服务器上有没有 sysctl 命令。
func tuneSetValue(key, value string) error {
	return os.WriteFile("/proc/sys/"+strings.ReplaceAll(key, ".", "/"), []byte(value+"\n"), 0o644)
}

// tuneMemMB 物理内存（MB），用于按内存自适应建议值。
func tuneMemMB() int64 {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kb / 1024
	}
	return 0
}
