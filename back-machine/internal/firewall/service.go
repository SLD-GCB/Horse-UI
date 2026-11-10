package firewall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"horseui/back-machine/internal/model"
)

const tableName = "horseui_fw"

// Status 防火墙状态。
type Status struct {
	Supported     bool                 `json:"supported"`
	Applied       bool                 `json:"applied"`
	Enabled       bool                 `json:"enabled"`
	AutoImport    bool                 `json:"auto_import"`
	AutoProtect   bool                 `json:"auto_protect"`
	DefaultPolicy string               `json:"default_policy"`
	Error         string               `json:"error"`
	Ports         []model.FirewallPort `json:"ports"`
	Bans          []model.FirewallBan  `json:"bans"`
}

// Service 玄武数据面。P0 先做端口集合与黑名单，动态判定后续接入。
type Service struct {
	db      *gorm.DB
	dataDir string

	mu      sync.Mutex
	applied bool
	lastErr string
}

// New 创建防火墙服务。
func New(db *gorm.DB, dataDir string) *Service {
	return &Service{db: db, dataDir: dataDir}
}

// Supported 当前平台是否支持 nftables。
func (s *Service) Supported() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if _, err := exec.LookPath("nft"); err != nil {
		return false
	}
	// 有 nft 还不够：容器/LXC 可能没有 NET_ADMIN，读取规则表会失败。
	return exec.Command("nft", "list", "tables").Run() == nil
}

// Start 启动时导入存量端口并应用规则。
func (s *Service) Start(ctx context.Context) {
	settings := s.settings()
	if settings.AutoImport && s.Supported() {
		if _, err := s.Discover(); err != nil {
			s.logf("discover failed, skip apply: %v", err)
			return
		}
	}
	// 先同步已有节点端口；即使防火墙暂未启用，也把放行规则准备好。
	if _, err := s.SyncInbounds(); err != nil {
		s.logf("sync inbounds failed: %v", err)
	}
	if !settings.Enabled || !s.Supported() {
		return
	}
	ports, err := s.Ports()
	if err != nil || len(ports) == 0 {
		s.logf("no service ports, skip apply")
		return
	}
	if err := s.Apply(); err != nil {
		s.logf("start apply failed: %v", err)
	}
}

// Logs 返回 fwd 日志尾部，最多 1000 行。
func (s *Service) Logs(limit int) ([]string, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	path := filepath.Join(s.dataDir, "logs", "fwd.log")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	if len(lines) == 1 && lines[0] == "" {
		return []string{}, nil
	}
	return lines, nil
}

// TablePresent 检查独立表是否还在，供 fwd 看门狗使用。
func (s *Service) TablePresent() bool {
	if !s.Supported() {
		return false
	}
	return s.runNft("list", "table", "inet", tableName) == nil
}

// Enabled 当前是否启用。
func (s *Service) Enabled() bool {
	return s.settings().Enabled
}

// Status 返回当前状态。
func (s *Service) Status() Status {
	settings := s.settings()
	st := Status{
		Supported:     s.Supported(),
		Applied:       s.applied,
		Enabled:       settings.Enabled,
		AutoImport:    settings.AutoImport,
		AutoProtect:   settings.AutoProtect,
		DefaultPolicy: "store-open-new-deny",
		Error:         s.lastErr,
	}
	if !st.Supported && runtime.GOOS == "linux" {
		if _, err := exec.LookPath("nft"); err == nil {
			st.Error = "nftables 已安装，但当前进程没有 NET_ADMIN 权限（容器/LXC 常见）"
		}
	}
	_ = s.db.Order("port ASC, protocol ASC").Find(&st.Ports).Error
	_ = s.db.Order("id DESC").Find(&st.Bans).Error
	return st
}

// Ports 端口规则。
func (s *Service) Ports() ([]model.FirewallPort, error) {
	var rows []model.FirewallPort
	err := s.db.Order("port ASC, protocol ASC").Find(&rows).Error
	return rows, err
}

// Bans 黑名单。
func (s *Service) Bans() ([]model.FirewallBan, error) {
	var rows []model.FirewallBan
	err := s.db.Order("id DESC").Find(&rows).Error
	return rows, err
}

// Discover 扫描当前监听端口，把未登记端口作为允许规则导入。
func (s *Service) Discover() (int, error) {
	ports, err := listeningPorts()
	if err != nil {
		return 0, err
	}
	added := 0
	for _, item := range ports {
		var count int64
		s.db.Model(&model.FirewallPort{}).
			Where("port = ? AND protocol = ?", item.Port, item.Protocol).
			Count(&count)
		if count > 0 {
			continue
		}
		now := time.Now().Unix()
		row := model.FirewallPort{
			Port:      item.Port,
			Protocol:  item.Protocol,
			Action:    "allow",
			Source:    "any",
			Comment:   "启动时自动导入",
			Auto:      true,
			Enabled:   true,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := s.db.Create(&row).Error; err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}

// EnsurePort 只补一条允许规则，不立即应用；用于面板启动前保证管理端口不被默认拒绝。
func (s *Service) EnsurePort(port int, protocol, comment string) error {
	if err := validatePort(port, protocol); err != nil {
		return err
	}
	var count int64
	s.db.Model(&model.FirewallPort{}).
		Where("port = ? AND protocol = ?", port, protocol).
		Count(&count)
	if count > 0 {
		return nil
	}
	now := time.Now().Unix()
	return s.db.Create(&model.FirewallPort{
		Port:      port,
		Protocol:  protocol,
		Action:    "allow",
		Source:    "any",
		Comment:   comment,
		Auto:      true,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}).Error
}

// Revision 返回数据库状态版本，供独立 fwd 轮询。
func (s *Service) Revision() string {
	var ports int64
	var portAt int64
	var bans int64
	var banAt int64
	var settingsAt int64
	_ = s.db.Model(&model.FirewallPort{}).Count(&ports).Error
	_ = s.db.Model(&model.FirewallPort{}).Select("COALESCE(MAX(updated_at),0)").Scan(&portAt).Error
	_ = s.db.Model(&model.FirewallBan{}).Count(&bans).Error
	_ = s.db.Model(&model.FirewallBan{}).Select("COALESCE(MAX(created_at),0)").Scan(&banAt).Error
	_ = s.db.Model(&model.FirewallSettings{}).Select("COALESCE(MAX(updated_at),0)").Scan(&settingsAt).Error
	return fmt.Sprintf("%d/%d/%d/%d/%d", ports, portAt, bans, banAt, settingsAt)
}

// SetPortProtect 设置端口只观察、不自动关闭。
func (s *Service) SetPortProtect(port int, protocol string, protect bool) error {
	if err := validatePort(port, protocol); err != nil {
		return err
	}
	var row model.FirewallPort
	if err := s.db.Where("port = ? AND protocol = ?", port, protocol).First(&row).Error; err != nil {
		return err
	}
	row.Protect = protect
	row.UpdatedAt = time.Now().Unix()
	return s.db.Save(&row).Error
}

// OpenPort 允许端口。
func (s *Service) OpenPort(port int, protocol, comment string) error {
	if err := validatePort(port, protocol); err != nil {
		return err
	}
	return s.upsertPort(port, protocol, "allow", comment)
}

// ClosePort 关闭端口。
func (s *Service) ClosePort(port int, protocol, comment string) error {
	if err := validatePort(port, protocol); err != nil {
		return err
	}
	return s.upsertPort(port, protocol, "close", comment)
}

// DeletePort 删除规则。
func (s *Service) DeletePort(id uint) error {
	if err := s.db.Delete(&model.FirewallPort{}, id).Error; err != nil {
		return err
	}
	return s.Apply()
}

// Ban 加入黑名单。
func (s *Service) Ban(ip, reason, source string, permanent bool) error {
	ip = strings.TrimSpace(ip)
	if net.ParseIP(ip) == nil {
		return errors.New("IP 格式不正确")
	}
	if source == "" {
		source = "manual"
	}
	var count int64
	s.db.Model(&model.FirewallBan{}).Where("ip = ?", ip).Count(&count)
	if count == 0 {
		row := model.FirewallBan{
			IP:        ip,
			Reason:    reason,
			Source:    source,
			Permanent: permanent,
			CreatedAt: time.Now().Unix(),
		}
		if err := s.db.Create(&row).Error; err != nil {
			return err
		}
	}
	s.db.Model(&model.FirewallAttackSource{}).Where("ip = ?", ip).Update("banned", true)
	return s.Apply()
}

// Unban 解封 IP。
func (s *Service) Unban(ip string) error {
	if err := s.db.Where("ip = ?", strings.TrimSpace(ip)).
		Delete(&model.FirewallBan{}).Error; err != nil {
		return err
	}
	s.db.Model(&model.FirewallAttackSource{}).Where("ip = ?", ip).Update("banned", false)
	return s.Apply()
}

// AttackSources 返回异常来源，按最近出现排序。
func (s *Service) AttackSources(limit int) ([]model.FirewallAttackSource, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var rows []model.FirewallAttackSource
	err := s.db.Order("last_seen DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

// AttackLogs 返回攻击事件，按时间倒序。
func (s *Service) AttackLogs(limit int) ([]model.FirewallAttackLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var rows []model.FirewallAttackLog
	err := s.db.Order("created_at DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

func marshalSources(sources map[string]int) string {
	type item struct {
		IP    string `json:"ip"`
		Count int    `json:"count"`
	}
	items := make([]item, 0, len(sources))
	for ip, count := range sources {
		items = append(items, item{IP: ip, Count: count})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Count > items[j].Count })
	if len(items) > 20 {
		items = items[:20]
	}
	data, _ := json.Marshal(items)
	return string(data)
}

// recordAttack 记录异常来源和事件，供面板人工确认。
func (s *Service) recordAttack(protocol string, port, current int, limit, baseline float64, action string, sources map[string]int) {
	now := time.Now().Unix()
	s.db.Create(&model.FirewallAttackLog{
		Port: port, Protocol: protocol, Current: current,
		Limit: limit, Baseline: baseline, Action: action,
		Sources: marshalSources(sources), CreatedAt: now,
	})
	for ip, count := range sources {
		var row model.FirewallAttackSource
		err := s.db.Where("ip = ? AND port = ? AND protocol = ?", ip, port, protocol).
			First(&row).Error
		if err != nil {
			s.db.Create(&model.FirewallAttackSource{
				IP: ip, Port: port, Protocol: protocol, Count: count,
				FirstSeen: now, LastSeen: now,
			})
			continue
		}
		row.Count = count
		row.LastSeen = now
		s.db.Save(&row)
	}
}

// SetEnabled 开关防火墙。
func (s *Service) SetEnabled(enabled bool) error {
	settings := s.settings()
	settings.Enabled = enabled
	settings.UpdatedAt = time.Now().Unix()
	if err := s.db.Save(&settings).Error; err != nil {
		return err
	}
	if !enabled {
		return s.clear()
	}
	if _, err := s.SyncInbounds(); err != nil {
		return err
	}
	return s.Apply()
}

// SetAutoProtect 开关自动关端口。
func (s *Service) SetAutoProtect(enabled bool) error {
	settings := s.settings()
	settings.AutoProtect = enabled
	settings.UpdatedAt = time.Now().Unix()
	return s.db.Save(&settings).Error
}

// Apply 把数据库状态同步到 nftables。
func (s *Service) Apply() error {
	if !s.Supported() {
		return errors.New("当前系统不支持 nftables")
	}
	settings := s.settings()
	if !settings.Enabled {
		return s.clear()
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureTable(); err != nil {
		s.lastErr = err.Error()
		s.logf("ensure table: %v", err)
		return err
	}
	if err := s.syncChain(); err != nil {
		s.lastErr = err.Error()
		s.logf("sync chain: %v", err)
		return err
	}
	rows, err := s.Ports()
	if err != nil {
		s.lastErr = err.Error()
		return err
	}
	bans, err := s.Bans()
	if err != nil {
		s.lastErr = err.Error()
		return err
	}

	var allowTCP, allowUDP, closeTCP, closeUDP []string
	for _, row := range rows {
		if !row.Enabled {
			continue
		}
		value := strconv.Itoa(row.Port)
		switch row.Protocol {
		case "udp":
			if row.Action == "close" {
				closeUDP = append(closeUDP, value)
			} else {
				allowUDP = append(allowUDP, value)
			}
		default:
			if row.Action == "close" {
				closeTCP = append(closeTCP, value)
			} else {
				allowTCP = append(allowTCP, value)
			}
		}
	}

	var deny4, deny6 []string
	for _, row := range bans {
		if strings.Contains(row.IP, ":") {
			deny6 = append(deny6, row.IP)
		} else {
			deny4 = append(deny4, row.IP)
		}
	}

	// 没有任何存量放行端口时拒绝应用，避免把自己锁在门外。
	if len(allowTCP)+len(allowUDP) == 0 {
		s.lastErr = "没有存量放行端口，拒绝应用"
		s.logf("refuse apply: no service ports")
		return errors.New(s.lastErr)
	}

	var b strings.Builder
	for _, set := range []string{"service_tcp", "service_udp", "closed_tcp", "closed_udp", "deny_v4", "deny_v6"} {
		fmt.Fprintf(&b, "flush set inet %s %s\n", tableName, set)
	}
	appendElements(&b, "service_tcp", allowTCP)
	appendElements(&b, "service_udp", allowUDP)
	appendElements(&b, "closed_tcp", closeTCP)
	appendElements(&b, "closed_udp", closeUDP)
	appendElements(&b, "deny_v4", deny4)
	appendElements(&b, "deny_v6", deny6)

	if err := s.runScript(b.String()); err != nil {
		s.lastErr = err.Error()
		s.logf("apply: %v", err)
		return err
	}
	s.applied = true
	s.lastErr = ""
	s.logf("applied: allow_tcp=%d allow_udp=%d close_tcp=%d close_udp=%d bans=%d",
		len(allowTCP), len(allowUDP), len(closeTCP), len(closeUDP), len(bans))
	return nil
}

// Rollback 关闭玄武规则，恢复系统原有策略。
func (s *Service) Rollback() error {
	return s.clear()
}

func (s *Service) clear() error {
	if !s.Supported() {
		return nil
	}
	err := s.runNft("delete", "table", "inet", tableName)
	if err != nil && !strings.Contains(err.Error(), "No such file") {
		return err
	}
	s.applied = false
	s.lastErr = ""
	s.logf("rolled back")
	return nil
}

func (s *Service) ensureTable() error {
	if err := s.runNft("list", "table", "inet", tableName); err == nil {
		return s.ensureSchema()
	}
	script := fmt.Sprintf(`
table inet %s {
    set allow_v4     { type ipv4_addr; flags interval; }
    set allow_v6     { type ipv6_addr; flags interval; }
    set shortban_v4  { type ipv4_addr; flags timeout; timeout 10m; }
    set shortban_v6  { type ipv6_addr; flags timeout; timeout 10m; }
    set service_tcp { type inet_service; flags interval; }
    set service_udp { type inet_service; flags interval; }
    set closed_tcp  { type inet_service; flags interval; }
    set closed_udp  { type inet_service; flags interval; }
    set deny_v4     { type ipv4_addr; flags interval; }
    set deny_v6     { type ipv6_addr; flags interval; }

    chain input {
        type filter hook input priority -10; policy drop;
        ct state invalid drop
        iifname "lo" accept
        ct state established,related accept
        ip  saddr @deny_v4 drop
        ip6 saddr @deny_v6 drop
        tcp dport @closed_tcp drop
        udp dport @closed_udp drop
        tcp dport @service_tcp accept
        udp dport @service_udp accept
    }
}
`, tableName)
	return s.runScript(script)
}

// ensureSchema 兼容旧表：补齐后来新增的 set / chain。
func (s *Service) ensureSchema() error {
	sets := []struct {
		name string
		body string
	}{
		{"allow_v4", "{ type ipv4_addr; flags interval; }"},
		{"allow_v6", "{ type ipv6_addr; flags interval; }"},
		{"shortban_v4", "{ type ipv4_addr; flags timeout; timeout 10m; }"},
		{"shortban_v6", "{ type ipv6_addr; flags timeout; timeout 10m; }"},
		{"service_tcp", "{ type inet_service; flags interval; }"},
		{"service_udp", "{ type inet_service; flags interval; }"},
		{"closed_tcp", "{ type inet_service; flags interval; }"},
		{"closed_udp", "{ type inet_service; flags interval; }"},
		{"deny_v4", "{ type ipv4_addr; flags interval; }"},
		{"deny_v6", "{ type ipv6_addr; flags interval; }"},
	}
	for _, spec := range sets {
		if err := s.runNft("list", "set", "inet", tableName, spec.name); err == nil {
			continue
		}
		if err := s.runScript(fmt.Sprintf("add set inet %s %s %s\n", tableName, spec.name, spec.body)); err != nil {
			return err
		}
	}
	if err := s.runNft("list", "chain", "inet", tableName, "input"); err != nil {
		if err := s.runScript(fmt.Sprintf(
			"add chain inet %s input { type filter hook input priority -10; policy drop; }\n",
			tableName,
		)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) syncChain() error {
	script := fmt.Sprintf(`
flush chain inet %[1]s input
add rule inet %[1]s input ct state invalid drop
add rule inet %[1]s input iifname "lo" accept
add rule inet %[1]s input ip  saddr @allow_v4 accept
add rule inet %[1]s input ip6 saddr @allow_v6 accept
add rule inet %[1]s input ip  saddr @shortban_v4 drop
add rule inet %[1]s input ip6 saddr @shortban_v6 drop
add rule inet %[1]s input ip  saddr @deny_v4 drop
add rule inet %[1]s input ip6 saddr @deny_v6 drop
add rule inet %[1]s input tcp dport @closed_tcp drop
add rule inet %[1]s input udp dport @closed_udp drop
add rule inet %[1]s input ct state established,related accept
add rule inet %[1]s input icmp type echo-request limit rate 5/second accept
add rule inet %[1]s input icmpv6 type echo-request limit rate 5/second accept
add rule inet %[1]s input tcp dport @service_tcp accept
add rule inet %[1]s input udp dport @service_udp accept
`, tableName)
	return s.runScript(script)
}

func (s *Service) upsertPort(port int, protocol, action, comment string) error {
	now := time.Now().Unix()
	var row model.FirewallPort
	err := s.db.Where("port = ? AND protocol = ?", port, protocol).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row = model.FirewallPort{
			Port:      port,
			Protocol:  protocol,
			Action:    action,
			Source:    "any",
			Comment:   comment,
			Enabled:   true,
			CreatedAt: now,
		}
		if err := s.db.Create(&row).Error; err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		row.Action = action
		row.Comment = comment
		row.Enabled = true
		row.UpdatedAt = now
		if err := s.db.Save(&row).Error; err != nil {
			return err
		}
	}
	return s.Apply()
}

func (s *Service) settings() model.FirewallSettings {
	var rows []model.FirewallSettings
	_ = s.db.Where("id = ?", 1).Limit(1).Find(&rows).Error
	if len(rows) == 0 {
		row := model.FirewallSettings{ID: 1, Enabled: false, AutoImport: true, AutoProtect: false}
		_ = s.db.Create(&row).Error
		return row
	}
	return rows[0]
}

func (s *Service) runScript(script string) error {
	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (s *Service) runNft(args ...string) error {
	cmd := exec.Command("nft", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (s *Service) logf(format string, args ...any) {
	dir := filepath.Join(s.dataDir, "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	file, err := os.OpenFile(filepath.Join(dir, "fwd.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = fmt.Fprintf(file, "%s %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, args...))
}

type listeningPort struct {
	Port     int
	Protocol string
}

func listeningPorts() ([]listeningPort, error) {
	cmd := exec.Command("ss", "-lntuH")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var result []listeningPort
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		proto := fields[0]
		if proto != "tcp" && proto != "udp" {
			continue
		}
		local := fields[4]
		idx := strings.LastIndex(local, ":")
		if idx < 0 {
			continue
		}
		port, err := strconv.Atoi(local[idx+1:])
		if err != nil || port <= 0 {
			continue
		}
		key := fmt.Sprintf("%s/%d", proto, port)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, listeningPort{Port: port, Protocol: proto})
	}
	return result, nil
}

func validatePort(port int, protocol string) error {
	if port <= 0 || port > 65535 {
		return errors.New("端口必须在 1–65535")
	}
	if protocol != "tcp" && protocol != "udp" {
		return errors.New("协议只能是 tcp 或 udp")
	}
	return nil
}

func appendElements(b *strings.Builder, set string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(b, "add element inet %s %s { %s }\n", tableName, set, strings.Join(values, ", "))
}
