package sysinfo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// 系统调优分组。
const (
	TuneGroupUDP  = "udp"  // QUIC 类（hysteria2 / tuic）最吃这块
	TuneGroupTCP  = "tcp"  // TCP 系入站（vless / vmess / trojan）与出站连接
	TuneGroupConn = "conn" // 连接队列与 fd 上限
	TuneGroupFS   = "fs"
)

// tuneKeyCongestion BBR 需要单独检测可用性，不在内核里就直接跳过。
const tuneKeyCongestion = "net.ipv4.tcp_congestion_control"

// tuneKeyQdisc BBR 的标配队列规则。
const tuneKeyQdisc = "net.core.default_qdisc"

// tuneBackupFile 首次应用前把原始值备份到数据目录，回滚时用它还原。
const tuneBackupFile = "sysctl-backup.json"

// tuneCmp 比较方式，决定「当前值是否已达标」。
type tuneCmp int

const (
	cmpEq tuneCmp = iota // 必须相等（枚举 / 开关）
	cmpGe                // 当前 >= 建议 即达标（缓冲 / 队列 / 上限类）
	cmpLe                // 当前 <= 建议 即达标（超时 / 回收类）
)

// TuneItem 单个内核参数的体检结果。
type TuneItem struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Group   string `json:"group"`
	Current string `json:"current"`
	Suggest string `json:"suggest"`
	OK      bool   `json:"ok"`
	Safe    bool   `json:"safe"`
	Risk    string `json:"risk"`
	Note    string `json:"note"`

	cmp tuneCmp
}

// TuneReport 系统调优的体检 / 应用结果。
type TuneReport struct {
	Supported    bool       `json:"supported"`
	CanApply     bool       `json:"can_apply"`
	Applied      bool       `json:"applied"`
	BBRAvailable bool       `json:"bbr_available"`
	MemMB        int64      `json:"mem_mb"`
	InContainer  bool       `json:"in_container"`
	ConfPath     string     `json:"conf_path"`
	Items        []TuneItem `json:"items"`
	Warnings     []string   `json:"warnings"`
}

// tuneParam 参数表的静态描述；建议值按内存自适应。
type tuneParam struct {
	key   string
	label string
	group string
	cmp   tuneCmp
	safe  bool
	risk  string
	note  string

	suggest func(memMB int64) string
}

// suggestedBufMax QUIC 用户态 socket 的收发缓冲上限按内存分档，别在小内存机上开太大。
func suggestedBufMax(memMB int64) string {
	switch {
	case memMB < 1024:
		return "4194304" // 4M
	case memMB < 2048:
		return "8388608" // 8M
	default:
		return "16777216" // 16M
	}
}

// suggestedUDPMem 只作为建议，不纳入一键应用：算错会伤全局。
func suggestedUDPMem(memMB int64) string {
	switch {
	case memMB < 1024:
		return "32768 65536 131072"
	case memMB < 2048:
		return "65536 131072 262144"
	default:
		return "131072 262144 524288"
	}
}

func fixed(v string) func(int64) string { return func(int64) string { return v } }

// tuneParams 参数表。顺序即前端展示顺序。
func tuneParams() []tuneParam {
	return []tuneParam{
		// ---- UDP / QUIC：弱网下最值钱的一组 ----
		{key: "net.core.rmem_max", label: "socket 接收缓冲上限", group: TuneGroupUDP, cmp: cmpGe, safe: true, suggest: suggestedBufMax, note: "QUIC（hysteria2 / tuic）在用户态收发，缓冲上限直接决定高延迟高丢包下的吞吐"},
		{key: "net.core.wmem_max", label: "socket 发送缓冲上限", group: TuneGroupUDP, cmp: cmpGe, safe: true, suggest: suggestedBufMax, note: "同上，发送方向"},
		{key: "net.core.rmem_default", label: "socket 默认接收缓冲", group: TuneGroupUDP, cmp: cmpGe, safe: true, suggest: fixed("1048576"), note: "未显式 setsockopt 的 socket 用它"},
		{key: "net.core.wmem_default", label: "socket 默认发送缓冲", group: TuneGroupUDP, cmp: cmpGe, safe: true, suggest: fixed("1048576"), note: "同上，发送方向"},
		{key: "net.ipv4.udp_rmem_min", label: "UDP 每 socket 接收下限", group: TuneGroupUDP, cmp: cmpGe, safe: true, suggest: fixed("16384"), note: "防止单个 UDP socket 被压得过小"},
		{key: "net.ipv4.udp_wmem_min", label: "UDP 每 socket 发送下限", group: TuneGroupUDP, cmp: cmpGe, safe: true, suggest: fixed("16384"), note: "同上，发送方向"},
		{key: "net.ipv4.udp_mem", label: "UDP 全局内存页（min pressure max）", group: TuneGroupUDP, cmp: cmpGe, safe: false, risk: "mid", suggest: suggestedUDPMem, note: "全局参数：小内存机或容器里算错会挤压其他服务，默认只给建议不应用"},

		// ---- TCP：帮 TCP 系入站与 sing-box 的出站连接 ----
		{key: tuneKeyCongestion, label: "拥塞控制算法", group: TuneGroupTCP, cmp: cmpEq, safe: true, suggest: fixed("bbr"), note: "BBR 对 TCP 有效；QUIC 类协议走用户态拥塞控制，不受它影响"},
		{key: "net.core.default_qdisc", label: "默认排队规则", group: TuneGroupTCP, cmp: cmpEq, safe: true, suggest: fixed("fq"), note: "BBR 的标准搭配，抑制 bufferbloat"},
		{key: "net.ipv4.tcp_slow_start_after_idle", label: "空闲后不重置 cwnd", group: TuneGroupTCP, cmp: cmpLe, safe: true, suggest: fixed("0"), note: "连接空闲后继续用原窗口，避免每次重新慢启动"},
		{key: "net.ipv4.tcp_mtu_probing", label: "PMTU 黑洞探测", group: TuneGroupTCP, cmp: cmpGe, safe: true, suggest: fixed("1"), note: "链路 MTU 不一致 / 隧道场景尤其重要"},
		{key: "net.ipv4.tcp_fastopen", label: "TCP Fast Open", group: TuneGroupTCP, cmp: cmpGe, safe: true, suggest: fixed("3"), note: "3 = 客户端与服务端都开启，握手省一次 RTT"},
		{key: "net.ipv4.tcp_tw_reuse", label: "复用 TIME_WAIT", group: TuneGroupTCP, cmp: cmpGe, safe: true, suggest: fixed("1"), note: "防出站源端口耗尽（转发场景常见）"},
		{key: "net.ipv4.tcp_fin_timeout", label: "FIN_WAIT2 超时", group: TuneGroupTCP, cmp: cmpLe, safe: true, suggest: fixed("15"), note: "加速端口回收"},
		{key: "net.ipv4.tcp_keepalive_time", label: "保活探测间隔", group: TuneGroupTCP, cmp: cmpLe, safe: true, suggest: fixed("600"), note: "更快回收断掉的死连接"},
		{key: "net.ipv4.ip_local_port_range", label: "本地端口范围", group: TuneGroupTCP, cmp: cmpGe, safe: false, risk: "mid", suggest: fixed("10240 65000"), note: "全局参数：会占用低端口，默认只给建议"},

		// ---- 连接队列与 fd ----
		{key: "net.core.somaxconn", label: "accept 队列上限", group: TuneGroupConn, cmp: cmpGe, safe: true, suggest: fixed("8192"), note: "并发连接多时避免握手被丢"},
		{key: "net.ipv4.tcp_max_syn_backlog", label: "SYN 队列上限", group: TuneGroupConn, cmp: cmpGe, safe: true, suggest: fixed("8192"), note: "高延迟下握手慢，队列小了会被丢"},
		{key: "net.core.netdev_max_backlog", label: "网卡收包队列", group: TuneGroupConn, cmp: cmpGe, safe: true, suggest: fixed("16384"), note: "大流量下减少内核丢包"},
		{key: "fs.file-max", label: "系统 fd 上限", group: TuneGroupFS, cmp: cmpGe, safe: true, suggest: fixed("2097152"), note: "连接数上限的兜底（进程级 limit 另由 systemd 设置）"},
	}
}

// tuneCompare 判断当前值是否已达标。多段值（如 ip_local_port_range）逐段比较。
func tuneCompare(current, suggest string, cmp tuneCmp) bool {
	cs := strings.Fields(current)
	ss := strings.Fields(suggest)
	if len(cs) == 0 || len(cs) != len(ss) {
		return false
	}
	for i := range ss {
		c, err1 := strconv.ParseInt(cs[i], 10, 64)
		s, err2 := strconv.ParseInt(ss[i], 10, 64)
		if err1 != nil || err2 != nil {
			// 非数字（bbr / fq 之类）只能相等比较
			if cmp != cmpEq || !strings.EqualFold(cs[i], ss[i]) {
				return false
			}
			continue
		}
		switch cmp {
		case cmpGe:
			if c < s {
				return false
			}
		case cmpLe:
			if c > s {
				return false
			}
		default:
			if c != s {
				return false
			}
		}
	}
	return true
}

// Detect 汇总当前内核参数与建议值，不修改任何东西。
func Detect(dataDir string) TuneReport {
	rep := TuneReport{
		Supported: tuneSupported(),
		ConfPath:  tuneConfPath(),
		Items:     []TuneItem{},
		Warnings:  []string{},
	}
	if !rep.Supported {
		rep.Warnings = append(rep.Warnings, "当前系统不支持内核参数调优：仅 Linux 支持，Windows 下请在 Linux 服务器上操作")
		return rep
	}

	rep.MemMB = tuneMemMB()
	rep.BBRAvailable = tuneBBRAvailable()
	rep.InContainer = tuneInContainer()
	rep.CanApply = tuneCanWrite()
	rep.Applied = tuneConfApplied()

	for _, p := range tuneParams() {
		cur, found := tuneReadSysctl(p.key)
		if !found {
			continue // 本内核没有这个参数，不展示也不写
		}
		sg := p.suggest(rep.MemMB)
		if sg == "" {
			continue
		}
		it := TuneItem{
			Key:     p.key,
			Label:   p.label,
			Group:   p.group,
			Current: strings.TrimSpace(cur),
			Suggest: sg,
			Safe:    p.safe,
			Risk:    p.risk,
			Note:    p.note,
			cmp:     p.cmp,
		}
		it.OK = tuneCompare(it.Current, sg, p.cmp)
		if p.key == tuneKeyCongestion && !rep.BBRAvailable {
			it.OK = true
			it.Safe = false
			it.Note = "内核未启用 bbr 模块，已跳过（可尝试 modprobe tcp_bbr 后重试）"
		}
		rep.Items = append(rep.Items, it)
	}

	rep.Warnings = append(rep.Warnings, tuneWarnings(rep)...)
	return rep
}

func tuneConfApplied() bool {
	p := tuneConfPath()
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

func tuneWarnings(rep TuneReport) []string {
	var ws []string
	if rep.InContainer {
		ws = append(ws, "检测到容器环境：net.* 多为命名空间参数，调优可能只影响本容器，/proc/sys 也可能只读")
	}
	if !rep.BBRAvailable {
		ws = append(ws, "内核当前未启用 bbr，tcp_congestion_control 已跳过")
	}
	if !rep.CanApply {
		ws = append(ws, "当前权限无法写入 "+rep.ConfPath+"（需要 root），只能查看建议值")
	}
	var skipped []string
	for _, it := range rep.Items {
		if !it.Safe && it.Risk == "mid" {
			skipped = append(skipped, it.Key)
		}
	}
	if len(skipped) > 0 {
		ws = append(ws, "以下参数风险较高、默认不随一键应用下发，需要时请手工设置："+strings.Join(skipped, "、"))
	}
	return ws
}

// sysctlKV 一条待写入的配置。
type sysctlKV struct {
	key string
	val string
}

func renderSysctlConf(kvs []sysctlKV) string {
	var b strings.Builder
	b.WriteString("# 由 HorseUI「系统调优」自动生成。\n")
	b.WriteString("# 删除本文件、或在面板里点「回滚调优」即可还原。请勿手工在此追加其他配置。\n")
	b.WriteString("# 生成时间：" + time.Now().Format("2006-01-02 15:04:05") + "\n")
	for _, p := range kvs {
		b.WriteString(p.key + " = " + p.val + "\n")
	}
	return b.String()
}

// readTuneConf 读取收敛文件里已经写入的参数（忽略注释与非法行）。
func readTuneConf() map[string]string {
	out := map[string]string{}
	p := tuneConfPath()
	if p == "" {
		return out
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out
}

// writeTuneConfMap 按参数表重写收敛文件（键排序，输出稳定）。
func writeTuneConfMap(m map[string]string) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	kvs := make([]sysctlKV, 0, len(keys))
	for _, k := range keys {
		kvs = append(kvs, sysctlKV{key: k, val: m[k]})
	}
	return tuneWriteConf(renderSysctlConf(kvs))
}

// SetBBR 全局开关 BBR：同时设置拥塞控制算法与默认队列规则（fq 是 BBR 的标配），
// 并持久化到收敛文件。关闭时恢复为内核默认的 cubic + fq_codel。
// 只影响 TCP 系协议；QUIC 系（hysteria2 / tuic）走用户态拥塞控制，不受它影响。
func SetBBR(dataDir string, enable bool) (TuneReport, error) {
	rep := Detect(dataDir)
	if !rep.Supported {
		return rep, fmt.Errorf("当前系统不支持内核参数调优（仅 Linux 支持）")
	}
	if !rep.CanApply {
		return rep, fmt.Errorf("无权修改 %s，请用 root 运行", rep.ConfPath)
	}
	if enable && !rep.BBRAvailable {
		return rep, fmt.Errorf("内核未启用 bbr 模块，无法开启（可先 modprobe tcp_bbr 再试）")
	}

	cc, qdisc := "cubic", "fq_codel"
	if enable {
		cc, qdisc = "bbr", "fq"
	}

	params := readTuneConf()
	params[tuneKeyCongestion] = cc
	// 只在当前内核真的有该键时才写队列规则，避免 sysctl 报未知键。
	hasQdisc := false
	if _, ok := tuneReadSysctl(tuneKeyQdisc); ok {
		params[tuneKeyQdisc] = qdisc
		hasQdisc = true
	}

	var failed []string
	if err := tuneSetValue(tuneKeyCongestion, cc); err != nil {
		failed = append(failed, tuneKeyCongestion)
	}
	if hasQdisc {
		if err := tuneSetValue(tuneKeyQdisc, qdisc); err != nil {
			failed = append(failed, tuneKeyQdisc)
		}
	}
	if err := writeTuneConfMap(params); err != nil {
		return rep, fmt.Errorf("写入 %s 失败：%w", rep.ConfPath, err)
	}

	out := Detect(dataDir)
	if len(failed) > 0 {
		out.Warnings = append(out.Warnings,
			"以下参数在当前内核上写入失败（配置已保存，重启后生效）："+strings.Join(failed, "、"))
	}
	return out, nil
}

// Apply 把达标参数写入收敛文件，并让当前内核立即生效；首次调用会备份原始值。
// 调优不在面板启动关键路径上：失败只影响本次调用，不会影响面板运行。
func Apply(dataDir string) (TuneReport, error) {
	rep := Detect(dataDir)
	if !rep.Supported {
		return rep, fmt.Errorf("当前系统不支持内核参数调优（仅 Linux 支持）")
	}
	if !rep.CanApply {
		return rep, fmt.Errorf("无权写入 %s，请用 root 运行", rep.ConfPath)
	}

	var kvs []sysctlKV
	for _, it := range rep.Items {
		if !it.Safe || it.OK {
			continue
		}
		kvs = append(kvs, sysctlKV{key: it.Key, val: it.Suggest})
	}
	if len(kvs) == 0 {
		rep.Warnings = append(rep.Warnings, "当前内核参数均已达标，无需改动")
		return rep, nil
	}

	// 只备份一次，保证回滚回到「HorseUI 动手之前」的状态。
	if err := backupSysctlOnce(dataDir, rep.Items); err != nil {
		rep.Warnings = append(rep.Warnings, "备份原始值失败："+err.Error())
	}

	var failed []string
	for _, p := range kvs {
		if err := tuneSetValue(p.key, p.val); err != nil {
			failed = append(failed, p.key)
		}
	}
	if err := tuneWriteConf(renderSysctlConf(kvs)); err != nil {
		return rep, fmt.Errorf("写入 %s 失败：%w", rep.ConfPath, err)
	}
	if len(failed) > 0 {
		rep.Warnings = append(rep.Warnings,
			"以下参数在当前内核上写入失败（常见于容器只读 /proc/sys），配置已保存、重启后生效："+strings.Join(failed, "、"))
	}
	rep.Warnings = append(rep.Warnings, "缓冲类参数只对新建 socket 生效：请到「系统与内核」重启一次内核进程。")

	out := Detect(dataDir)
	out.Warnings = append(rep.Warnings, out.Warnings...)
	return out, nil
}

// Revert 删除收敛文件并把备份里的原始值写回内核。
func Revert(dataDir string) (TuneReport, error) {
	rep := Detect(dataDir)
	if !rep.Supported {
		return rep, fmt.Errorf("当前系统不支持内核参数调优（仅 Linux 支持）")
	}
	if !rep.CanApply {
		return rep, fmt.Errorf("无权修改 %s，请用 root 运行", rep.ConfPath)
	}
	if err := tuneRemoveConf(); err != nil {
		return rep, fmt.Errorf("删除 %s 失败：%w", rep.ConfPath, err)
	}

	orig, err := loadSysctlBackup(dataDir)
	if err != nil {
		rep.Warnings = append(rep.Warnings, "未找到原始值备份，只删除了配置文件（内核运行值会在重启后自然恢复）")
		out := Detect(dataDir)
		out.Warnings = append(rep.Warnings, out.Warnings...)
		return out, nil
	}

	var failed []string
	for k, v := range orig {
		if strings.TrimSpace(v) == "" {
			continue
		}
		if err := tuneSetValue(k, v); err != nil {
			failed = append(failed, k)
		}
	}
	if len(failed) > 0 {
		rep.Warnings = append(rep.Warnings, "以下参数还原失败："+strings.Join(failed, "、"))
	}
	// 备份已被消费，删掉以便下次应用重新记录当时的原始值。
	_ = os.Remove(filepath.Join(dataDir, tuneBackupFile))

	out := Detect(dataDir)
	out.Warnings = append(rep.Warnings, "已回滚到调优前的值（缓冲类参数需重启内核进程后完全恢复）。")
	out.Warnings = append(out.Warnings, out.Warnings...)
	return out, nil
}

func backupSysctlOnce(dataDir string, items []TuneItem) error {
	if dataDir == "" {
		return fmt.Errorf("数据目录为空")
	}
	path := filepath.Join(dataDir, tuneBackupFile)
	if _, err := os.Stat(path); err == nil {
		return nil // 保留最早的一份备份
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	orig := map[string]string{}
	for _, it := range items {
		orig[it.Key] = it.Current
	}
	b, err := json.MarshalIndent(orig, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func loadSysctlBackup(dataDir string) (map[string]string, error) {
	if dataDir == "" {
		return nil, fmt.Errorf("数据目录为空")
	}
	b, err := os.ReadFile(filepath.Join(dataDir, tuneBackupFile))
	if err != nil {
		return nil, err
	}
	orig := map[string]string{}
	if err := json.Unmarshal(b, &orig); err != nil {
		return nil, err
	}
	return orig, nil
}
