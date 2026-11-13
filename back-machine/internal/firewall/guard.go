package firewall

import (
	"context"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type portCapacity struct {
	baseline float64
	floor    float64
	ceiling  float64
	limit    float64
	over     int
	closedAt time.Time
}

// StartGuard 启动自动容量守护。只应在独立 fwd 进程里调用。
func (s *Service) StartGuard(ctx context.Context, protected map[int]bool) {
	if !s.Supported() {
		return
	}
	go s.guardLoop(ctx, protected)
}

func (s *Service) guardLoop(ctx context.Context, protected map[int]bool) {
	states := make(map[string]*portCapacity)
	autoClosed := make(map[string]bool)
	lastRecord := make(map[string]time.Time)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			settings := s.settings()
			if !settings.Enabled || !settings.AutoProtect {
				s.clearGuardSnapshot()
				continue
			}
			tcpCurrent := establishedByPort()
			udpCurrent := udpFlowsByPort()
			sourcesByPort := conntrackSources()
			healthy := systemHealthy()
			rows, err := s.Ports()
			if err != nil {
				continue
			}
			known := make(map[string]bool)
			protectedByRule := make(map[string]bool)
			allowByRule := make(map[string]bool)
			closedByRule := make(map[string]bool)
			now := time.Now()
			for _, row := range rows {
				key := guardKey(row.Protocol, row.Port)
				known[key] = true
				if row.Protect {
					protectedByRule[key] = true
				}
				if row.Action == "allow" {
					allowByRule[key] = true
					// 手动开放后清掉自动关闭状态，避免快照仍显示“已关端口”。
					if st := states[key]; st != nil && !st.closedAt.IsZero() {
						st.closedAt = time.Time{}
						st.over = 0
					}
					delete(autoClosed, key)
				}
				if row.Action == "close" {
					// 手动关闭和自动关闭都进入恢复计时，由自动保护策略统一决定何时重开。
					closedByRule[key] = true
					if states[key] == nil {
						protocol, port := splitGuardKey(key)
						if port > 0 {
							states[key] = newPortCapacity(protocol, 0)
						}
					}
					if st := states[key]; st != nil && st.closedAt.IsZero() {
						st.closedAt = now
					}
					if strings.HasPrefix(row.Comment, "自动：") {
						autoClosed[key] = true
					} else {
						delete(autoClosed, key)
					}
				}
			}
			observations := []struct {
				protocol string
				current  map[int]int
			}{
				{"tcp", tcpCurrent},
				{"udp", udpCurrent},
			}
			for _, obs := range observations {
				for port, count := range obs.current {
					key := guardKey(obs.protocol, port)
					// 只统计防火墙登记过的端口，排除临时出站端口。
					if port <= 0 || !known[key] {
						continue
					}
					if st := states[key]; st != nil && !st.closedAt.IsZero() {
						continue
					}
					if protectedByRule[key] || protected[port] {
						// 保护端口不自动关，但异常来源仍记录下来供人工确认。
						st := states[key]
						if st == nil {
							st = newPortCapacity(obs.protocol, count)
							states[key] = st
						}
						if float64(count) > st.limit {
							sources := sourcesByPort[obs.protocol][port]
							if len(sources) > 0 && now.Sub(lastRecord[key]) >= 30*time.Second {
								s.recordAttack(obs.protocol, port, count, st.limit, st.baseline, "watch", sources)
								lastRecord[key] = now
							}
						}
						continue
					}
					st := states[key]
					if st == nil {
						st = newPortCapacity(obs.protocol, count)
						states[key] = st
					}

					if !st.closedAt.IsZero() {
						continue
					}

					// 健康时缓慢上调，资源压力或超限时快速下调。
					if healthy && float64(count) < st.limit*0.7 {
						st.limit = math.Min(st.limit*1.08, st.ceiling)
					}
					if !healthy || float64(count) > st.limit {
						st.limit = math.Max(st.limit*0.75, st.floor)
					}

					if float64(count) > st.limit {
						st.over++
						sources := sourcesByPort[obs.protocol][port]
						if len(sources) > 0 && now.Sub(lastRecord[key]) >= 30*time.Second {
							s.recordAttack(obs.protocol, port, count, st.limit, st.baseline, "watch", sources)
							lastRecord[key] = now
						}
					} else {
						st.over = 0
					}
					if st.over >= 3 && float64(count) > st.floor {
						if err := s.ClosePort(port, obs.protocol, "自动：超过动态容量"); err == nil {
							autoClosed[key] = true
							st.closedAt = now
							st.over = 0
							sources := sourcesByPort[obs.protocol][port]
							s.recordAttack(obs.protocol, port, count, st.limit, st.baseline, "close", sources)
							lastRecord[key] = now
							s.logf("auto close %s/%d, current=%d limit=%.1f baseline=%.1f healthy=%v",
								obs.protocol, port, count, st.limit, st.baseline, healthy)
						}
					}

					// 基线缓慢跟随，只用于重算 floor/ceiling。
					st.baseline = st.baseline*0.95 + float64(count)*0.05
					if st.baseline < 1 {
						st.baseline = 1
					}
					st.floor = math.Max(st.baseline*2, 1)
					st.ceiling = math.Max(st.floor*4, 8)
					if st.limit > st.ceiling {
						st.limit = st.ceiling
					}
					if st.limit < st.floor {
						st.limit = st.floor
					}
				}
			}
			// 手动关闭和自动关闭统一恢复：冷却结束且连接回落后重开。
			for key, st := range states {
				if st.closedAt.IsZero() || !known[key] || protectedByRule[key] {
					continue
				}
				protocol, port := splitGuardKey(key)
				if port <= 0 || now.Sub(st.closedAt) < 5*time.Minute {
					continue
				}
				current := tcpCurrent[port]
				if protocol == "udp" {
					current = udpCurrent[port]
				}
				if float64(current) > st.floor*1.2 {
					continue
				}
				if err := s.OpenPort(port, protocol, "自动：容量恢复"); err == nil {
					delete(autoClosed, key)
					st.closedAt = time.Time{}
					st.over = 0
					st.limit = math.Max(st.floor*2, 2)
					s.recordAttack(protocol, port, current, st.limit, st.baseline, "reopen", nil)
					s.logf("auto reopen %s/%d, current=%d limit=%.1f", protocol, port, current, st.limit)
				}
			}
			// 允许端口即使当前没有连接，也要出现在容量快照里。
			for key := range allowByRule {
				if _, ok := states[key]; !ok {
					protocol, port := splitGuardKey(key)
					if port > 0 {
						states[key] = newPortCapacity(protocol, 0)
					}
				}
			}
			visible := make(map[string]bool)
			for key := range allowByRule {
				visible[key] = true
			}
			for key := range closedByRule {
				visible[key] = true
			}
			for key := range autoClosed {
				visible[key] = true
			}
			// 22/面板端口是 fwd 的硬保护端口，也要显示为常开。
			lockedPorts := make(map[string]bool)
			for key := range protectedByRule {
				lockedPorts[key] = true
			}
			for port, ok := range protected {
				if ok {
					lockedPorts[guardKey("tcp", port)] = true
					lockedPorts[guardKey("udp", port)] = true
				}
			}
			s.writeGuardSnapshot(healthy, states, tcpCurrent, udpCurrent, visible, lockedPorts)
		}
	}
}

// conntrackSources 按协议/本地端口统计来源 IP，优先用于异常时展示。
func conntrackSources() map[string]map[int]map[string]int {
	out := make(map[string]map[int]map[string]int)
	raw, err := os.ReadFile("/proc/net/nf_conntrack")
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		protocol := fields[2]
		if protocol != "tcp" && protocol != "udp" {
			continue
		}
		src, dport := "", 0
		for _, field := range fields {
			switch {
			case strings.HasPrefix(field, "src="):
				src = strings.TrimPrefix(field, "src=")
			case strings.HasPrefix(field, "dport="):
				dport, _ = strconv.Atoi(strings.TrimPrefix(field, "dport="))
			}
		}
		if src == "" || dport <= 0 || dport > 65535 {
			continue
		}
		byPort := out[protocol]
		if byPort == nil {
			byPort = make(map[int]map[string]int)
			out[protocol] = byPort
		}
		byIP := byPort[dport]
		if byIP == nil {
			byIP = make(map[string]int)
			byPort[dport] = byIP
		}
		byIP[src]++
	}
	return out
}

func newPortCapacity(protocol string, count int) *portCapacity {
	base := math.Max(float64(count), 1)
	headroom := 4.0
	minLimit := 8.0
	minFloor := 4.0
	if protocol == "udp" {
		// QUIC 测速/多流会短时并发，UDP 给更高初始容量。
		minLimit = 256
		minFloor = 128
	}
	return &portCapacity{
		baseline: base,
		floor:    math.Max(base*2, minFloor),
		ceiling:  math.Max(base*headroom*2, minLimit*2),
		limit:    math.Max(base*headroom, minLimit),
	}
}

func guardKey(protocol string, port int) string {
	return protocol + "/" + strconv.Itoa(port)
}

// systemHealthy 读取负载、内存、conntrack，决定容量上限是升还是降。
func systemHealthy() bool {
	if raw, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(raw))
		if len(fields) > 0 {
			if load, err := strconv.ParseFloat(fields[0], 64); err == nil {
				if load > float64(runtime.NumCPU())*0.8 {
					return false
				}
			}
		}
	}
	if raw, err := os.ReadFile("/proc/meminfo"); err == nil {
		var total, avail float64
		for _, line := range strings.Split(string(raw), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			value, _ := strconv.ParseFloat(fields[1], 64)
			switch fields[0] {
			case "MemTotal:":
				total = value
			case "MemAvailable:":
				avail = value
			}
		}
		if total > 0 && avail/total < 0.15 {
			return false
		}
	}
	countRaw, err1 := os.ReadFile("/proc/sys/net/netfilter/nf_conntrack_count")
	maxRaw, err2 := os.ReadFile("/proc/sys/net/netfilter/nf_conntrack_max")
	if err1 == nil && err2 == nil {
		count, _ := strconv.ParseFloat(strings.TrimSpace(string(countRaw)), 64)
		max, _ := strconv.ParseFloat(strings.TrimSpace(string(maxRaw)), 64)
		if max > 0 && count/max > 0.85 {
			return false
		}
	}
	return true
}

// establishedByPort 从 /proc/net/tcp{,6} 统计 ESTABLISHED 连接数。
func establishedByPort() map[int]int {
	out := make(map[int]int)
	for _, file := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		raw, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(raw), "\n")[1:] {
			fields := strings.Fields(line)
			if len(fields) < 4 || fields[3] != "01" {
				continue
			}
			local := fields[1]
			idx := strings.LastIndex(local, ":")
			if idx < 0 {
				continue
			}
			port64, err := strconv.ParseInt(local[idx+1:], 16, 32)
			if err != nil || port64 <= 0 || port64 > 65535 {
				continue
			}
			out[int(port64)]++
		}
	}
	return out
}

// udpFlowsByPort 从 conntrack 统计 UDP 流；没有 conntrack 时返回空。
func udpFlowsByPort() map[int]int {
	out := make(map[int]int)
	raw, err := os.ReadFile("/proc/net/nf_conntrack")
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[2] != "udp" {
			continue
		}
		seen := make(map[int]bool)
		for _, field := range fields {
			if !strings.HasPrefix(field, "dport=") {
				continue
			}
			port, err := strconv.Atoi(strings.TrimPrefix(field, "dport="))
			if err != nil || port <= 0 || port > 65535 || seen[port] {
				continue
			}
			seen[port] = true
			out[port]++
		}
	}
	return out
}
