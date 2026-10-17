package sysinfo

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 端口探测结果缓存，避免频繁执行系统命令。
var (
	portCacheMu   sync.Mutex
	portCacheAt   time.Time
	portCacheData map[int][]string
	portCacheTTL  = 3 * time.Second
)

// ListeningPorts 返回本机正在监听的端口，值为协议列表（tcp/udp）。
func ListeningPorts() map[int][]string {
	portCacheMu.Lock()
	defer portCacheMu.Unlock()
	if portCacheData != nil && time.Since(portCacheAt) < portCacheTTL {
		return portCacheData
	}

	var result map[int][]string
	if runtime.GOOS == "linux" {
		result = procListeningPorts()
	}
	if len(result) == 0 {
		result = netstatListeningPorts()
	}
	portCacheData = result
	portCacheAt = time.Now()
	return result
}

// procListeningPorts 解析 Linux 的 /proc/net/{tcp,tcp6,udp,udp6}。
func procListeningPorts() map[int][]string {
	out := map[int][]string{}
	files := []struct {
		path       string
		proto      string
		onlyListen bool
	}{
		{"/proc/net/tcp", "tcp", true},
		{"/proc/net/tcp6", "tcp6", true},
		{"/proc/net/udp", "udp", false},
		{"/proc/net/udp6", "udp6", false},
	}

	for _, f := range files {
		data, err := os.ReadFile(f.path)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for _, line := range lines[1:] {
			fields := strings.Fields(line)
			if len(fields) < 6 {
				continue
			}
			state := fields[3]
			// TCP 只看 LISTEN(0A)，UDP 只看已绑定(07)
			if f.onlyListen && state != "0A" {
				continue
			}
			if !f.onlyListen && state != "07" {
				continue
			}
			i := strings.LastIndex(fields[1], ":")
			if i < 0 {
				continue
			}
			p, err := strconv.ParseInt(fields[1][i+1:], 16, 32)
			if err != nil || p <= 0 {
				continue
			}
			out[int(p)] = appendUnique(out[int(p)], f.proto)
		}
	}
	return out
}

// netstatPath 解析 netstat 可执行文件路径。
// Windows 上 netstat.exe 在 System32 下，但某些环境的 PATH 可能不完整，因此回退到绝对路径。
func netstatPath() string {
	if p, err := exec.LookPath("netstat"); err == nil {
		return p
	}
	if runtime.GOOS == "windows" {
		root := os.Getenv("SystemRoot")
		if root == "" {
			root = `C:\Windows`
		}
		p := filepath.Join(root, "System32", "netstat.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// netstatListeningPorts 解析 netstat -an 输出（Windows / macOS / 无 proc 的 Linux）。
func netstatListeningPorts() map[int][]string {
	out := map[int][]string{}

	bin := netstatPath()
	if bin == "" {
		return out
	}
	args := []string{"-an"}
	if runtime.GOOS == "windows" {
		args = []string{"-ano"}
	}
	data, err := exec.Command(bin, args...).Output()
	if err != nil {
		return out
	}

	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		proto := strings.ToLower(fields[0])
		switch {
		case strings.HasPrefix(proto, "tcp"):
			proto = "tcp"
			if !strings.Contains(strings.ToUpper(line), "LISTEN") {
				continue
			}
		case strings.HasPrefix(proto, "udp"):
			proto = "udp"
		default:
			continue
		}

		// 第一个形如 addr:port 的列即本地地址
		for _, f := range fields[1:] {
			p, ok := extractPort(f)
			if !ok {
				continue
			}
			if p > 0 {
				out[p] = appendUnique(out[p], proto)
			}
			break
		}
	}
	return out
}

func extractPort(s string) (int, bool) {
	if s == "" || s == "*" || s == "*:*" || s == "*:*:*" {
		return 0, false
	}
	i := strings.LastIndex(s, ":")
	if i < 0 || i == len(s)-1 {
		return 0, false
	}
	p, err := strconv.Atoi(s[i+1:])
	if err != nil {
		return 0, false
	}
	return p, true
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// Bindable 尝试在 0.0.0.0 上同时绑定 TCP 与 UDP，判断端口是否真的可用。
func Bindable(port int) bool {
	if port <= 0 || port > 65535 {
		return false
	}
	addr := fmt.Sprintf("0.0.0.0:%d", port)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	_ = ln.Close()

	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		return false
	}
	_ = pc.Close()
	return true
}

// FreePorts 从 [start,end] 中挑出前 limit 个可用端口。
// occupied 为已知被占用的端口集合，会先被跳过以减少绑定探测次数。
func FreePorts(start, end, limit int, occupied map[int]bool) []int {
	if start < 1 {
		start = 1
	}
	if end > 65535 {
		end = 65535
	}
	if start > end {
		return nil
	}
	out := make([]int, 0, limit)
	for p := start; p <= end && len(out) < limit; p++ {
		if occupied[p] {
			continue
		}
		if !Bindable(p) {
			continue
		}
		out = append(out, p)
	}
	return out
}
