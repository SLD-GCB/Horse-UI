package api

import (
	"context"
	"time"

	"horseui/back-machine/internal/model"
	"horseui/back-machine/internal/svc"
	"horseui/core/stats"
)

const (
	statsPollInterval  = 3 * time.Second // 连接采样间隔
	statsFlushInterval = 5 * time.Second // 落库间隔
	certRenewEvery     = 12 * time.Hour
	certRenewBefore    = 30 * 24 * time.Hour
)

// startTasks 启动后台任务。
func (a *App) startTasks() {
	go a.enforceLoop()
	go a.statsLoop()
	go a.certRenewLoop()
	go a.maintenanceLoop()
}

// maintenanceLoop 定期刷新管理员认证缓存并清理限流状态。
func (a *App) maintenanceLoop() {
	authTicker := time.NewTicker(time.Minute)
	purgeTicker := time.NewTicker(5 * time.Minute)
	defer authTicker.Stop()
	defer purgeTicker.Stop()
	for {
		select {
		case <-authTicker.C:
			a.refreshAdminState()
		case <-purgeTicker.C:
			a.loginLimit.purge()
			a.loginFails.purge()
			a.subLimit.purge()
		}
	}
}

// ---------------- 配额与到期 ----------------

// enforceLoop 每分钟检查一次用户是否过期/超配额。
func (a *App) enforceLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		a.enforceOnce()
		<-ticker.C
	}
}

// enforceOnce 自动禁用已过期或超额的用户，并在有变更时重载内核。
func (a *App) enforceOnce() {
	var clients []model.Client
	if err := a.db.Where("enable = ?", true).Find(&clients).Error; err != nil {
		return
	}
	changed := false
	for _, cl := range clients {
		if cl.Expired() || cl.OverQuota() {
			if err := a.db.Model(&model.Client{}).Where("id = ?", cl.ID).
				Update("enable", false).Error; err == nil {
				changed = true
			}
		}
	}
	if changed {
		_ = a.core.Apply()
	}
}

// ---------------- 流量统计 ----------------

// statsLoop 周期性采样流量，并按固定时间间隔把累计增量落库。
// 采样与落库分开计时，避免流量稀疏时数据长期滞留在内存。
func (a *App) statsLoop() {
	time.Sleep(10 * time.Second)
	pollTicker := time.NewTicker(statsPollInterval)
	flushTicker := time.NewTicker(statsFlushInterval)
	defer pollTicker.Stop()
	defer flushTicker.Stop()

	for {
		select {
		case <-pollTicker.C:
			a.collectOnce()
		case <-flushTicker.C:
			a.flushOnce()
		}
	}
}

// flushOnce 交换缓冲区并把增量写入数据库。
func (a *App) flushOnce() {
	a.statsMu.Lock()
	users := a.pending
	inbounds := a.pendingInbound
	a.pending = make(map[string]stats.Delta)
	a.pendingInbound = make(map[string]stats.Delta)
	a.statsMu.Unlock()

	if len(users) == 0 && len(inbounds) == 0 {
		return
	}
	a.flushTraffic(users, inbounds)
}

func (a *App) collectOnce() {
	mode := a.settings.GetDefault(model.SettingStatsMode, "clash")
	if mode == "off" {
		return
	}
	if !a.core.Proc.Running() {
		// 内核未运行，重置采样状态，避免重启后重复计数。
		a.statsMu.Lock()
		a.clashSeen.Reset()
		a.pending = make(map[string]stats.Delta)
		a.pendingInbound = make(map[string]stats.Delta)
		a.statsMu.Unlock()
		return
	}
	if mode == "v2ray" {
		a.collectV2Ray()
		return
	}
	a.collectClash()
}

// collectClash 基于 Clash API 的活动连接做增量累计。
// 官方 sing-box 发行版默认包含 Clash API，但其连接信息不含用户名，
// 因此只能统计到入站粒度；用户用量由入站流量按绑定关系分摊得到。
func (a *App) collectClash() {
	conns, err := a.clashClient().Connections(context.Background())
	if err != nil {
		return
	}
	sample := a.clashSeen.Observe(conns)
	if len(sample.Users) == 0 && len(sample.Inbounds) == 0 {
		return
	}

	a.statsMu.Lock()
	for tag, d := range sample.Inbounds {
		p := a.pendingInbound[tag]
		p.Up += d.Up
		p.Down += d.Down
		a.pendingInbound[tag] = p
	}
	for user, d := range sample.Users {
		p := a.pending[user]
		p.Up += d.Up
		p.Down += d.Down
		a.pending[user] = p
	}
	a.statsMu.Unlock()
}

// collectV2Ray 基于 V2Ray Stats gRPC 接口统计（需自编译带 with_v2ray_api 的内核）。
// 该方式能拿到内核累计的精确用户流量，无需按入站分摊。
func (a *App) collectV2Ray() {
	ctx := context.Background()

	if users, err := a.v2rayClient().QueryUserTraffic(ctx); err == nil && len(users) > 0 {
		a.applyUserTraffic(users)
	}
	if inbounds, err := a.v2rayClient().QueryInboundTraffic(ctx); err == nil && len(inbounds) > 0 {
		a.applyInboundTrafficRaw(inbounds)
	}
}

// applyInboundTrafficRaw 用内核上报的累计值计算入站增量（不做用户分摊）。
func (a *App) applyInboundTrafficRaw(traffic map[string]stats.UserTraffic) {
	var inbounds []model.Inbound
	if err := a.db.Find(&inbounds).Error; err != nil {
		return
	}
	for i := range inbounds {
		ib := &inbounds[i]
		t, ok := traffic[ib.Tag]
		if !ok {
			continue
		}
		dUp, dDown := t.Up-ib.RawUp, t.Down-ib.RawDown
		if dUp < 0 {
			dUp = t.Up
		}
		if dDown < 0 {
			dDown = t.Down
		}
		if dUp == 0 && dDown == 0 {
			continue
		}
		ib.RawUp, ib.RawDown = t.Up, t.Down
		ib.UsedUp += dUp
		ib.UsedDown += dDown
		a.db.Model(&model.Inbound{}).Where("id = ?", ib.ID).Updates(map[string]any{
			"used_up":   ib.UsedUp,
			"used_down": ib.UsedDown,
			"raw_up":    ib.RawUp,
			"raw_down":  ib.RawDown,
		})
	}
}

// flushTraffic 把累计增量写入数据库。
// users 为内核直接上报的精确用户增量（部分内核支持），此时不再按入站分摊，避免重复计数。
func (a *App) flushTraffic(users, inbounds map[string]stats.Delta) {
	split := len(users) == 0
	if len(inbounds) > 0 {
		a.applyInboundTraffic(inbounds, split)
	}
	if len(users) > 0 {
		a.applyUserTrafficByDelta(users)
	}
}

// applyInboundTraffic 累计入站流量；splitToClients 为 true 时按绑定关系均摊给用户。
func (a *App) applyInboundTraffic(batch map[string]stats.Delta, splitToClients bool) {
	tags := make([]string, 0, len(batch))
	for t := range batch {
		tags = append(tags, t)
	}

	var inbounds []model.Inbound
	if err := a.db.Where("tag IN ?", tags).Find(&inbounds).Error; err != nil || len(inbounds) == 0 {
		return
	}

	var bindMap map[uint][]uint
	if splitToClients {
		m, err := svc.BoundClients(a.db)
		if err != nil {
			return
		}
		bindMap = m
	}

	needApply := false
	for i := range inbounds {
		ib := &inbounds[i]
		d, ok := batch[ib.Tag]
		if !ok || (d.Up == 0 && d.Down == 0) {
			continue
		}

		ib.UsedUp += d.Up
		ib.UsedDown += d.Down
		a.db.Model(&model.Inbound{}).Where("id = ?", ib.ID).Updates(map[string]any{
			"used_up":   ib.UsedUp,
			"used_down": ib.UsedDown,
		})

		if splitToClients && a.splitToClients(bindMap[ib.ID], d) {
			needApply = true
		}
	}
	if needApply {
		_ = a.core.Apply()
	}
}

// splitToClients 把某个入站的流量增量按绑定人数均摊给各用户。
// 只绑定一个用户时即为精确值。
func (a *App) splitToClients(clientIDs []uint, d stats.Delta) (needApply bool) {
	if len(clientIDs) == 0 {
		return false
	}

	var clients []model.Client
	if err := a.db.Where("id IN ?", clientIDs).Find(&clients).Error; err != nil || len(clients) == 0 {
		return false
	}

	n := int64(len(clients))
	shareUp, shareDown := d.Up/n, d.Down/n
	remUp, remDown := d.Up-shareUp*n, d.Down-shareDown*n

	for i := range clients {
		cl := &clients[i]
		up, down := shareUp, shareDown
		if i == 0 {
			up += remUp
			down += remDown
		}
		if up == 0 && down == 0 {
			continue
		}
		cl.UsedUp += up
		cl.UsedDown += down

		updates := map[string]any{"used_up": cl.UsedUp, "used_down": cl.UsedDown}
		if cl.OverQuota() && cl.Enable {
			updates["enable"] = false
			needApply = true
		}
		a.db.Model(&model.Client{}).Where("id = ?", cl.ID).Updates(updates)
		a.db.Create(&model.ClientStat{ClientID: cl.ID, Up: up, Down: down})
	}
	return needApply
}

// applyUserTraffic 使用 V2Ray 上报的累计值计算增量并写入用户用量。
func (a *App) applyUserTraffic(traffic map[string]stats.UserTraffic) {
	var clients []model.Client
	if err := a.db.Find(&clients).Error; err != nil {
		return
	}

	needApply := false
	for i := range clients {
		cl := &clients[i]
		t, ok := traffic[cl.Name]
		if !ok {
			continue
		}
		// 内核重启后计数器归零，此时把当前值整体视为新增流量。
		dUp, dDown := t.Up-cl.RawUp, t.Down-cl.RawDown
		if dUp < 0 {
			dUp = t.Up
		}
		if dDown < 0 {
			dDown = t.Down
		}
		if dUp == 0 && dDown == 0 {
			continue
		}
		cl.UsedUp += dUp
		cl.UsedDown += dDown

		updates := map[string]any{
			"used_up":   cl.UsedUp,
			"used_down": cl.UsedDown,
			"raw_up":    t.Up,
			"raw_down":  t.Down,
		}
		if cl.OverQuota() && cl.Enable {
			updates["enable"] = false
			needApply = true
		}
		a.db.Model(&model.Client{}).Where("id = ?", cl.ID).Updates(updates)
		a.db.Create(&model.ClientStat{ClientID: cl.ID, Up: dUp, Down: dDown})
	}
	if needApply {
		_ = a.core.Apply()
	}
}

// applyUserTrafficByDelta 直接把用户增量写入用量（Clash 方式下用于分摊结果以外的补充）。
func (a *App) applyUserTrafficByDelta(batch map[string]stats.Delta) {
	names := make([]string, 0, len(batch))
	for n := range batch {
		names = append(names, n)
	}
	var clients []model.Client
	if err := a.db.Where("name IN ?", names).Find(&clients).Error; err != nil {
		return
	}

	needApply := false
	for i := range clients {
		cl := &clients[i]
		d, ok := batch[cl.Name]
		if !ok || (d.Up == 0 && d.Down == 0) {
			continue
		}
		cl.UsedUp += d.Up
		cl.UsedDown += d.Down

		updates := map[string]any{"used_up": cl.UsedUp, "used_down": cl.UsedDown}
		if cl.OverQuota() && cl.Enable {
			updates["enable"] = false
			needApply = true
		}
		a.db.Model(&model.Client{}).Where("id = ?", cl.ID).Updates(updates)
		a.db.Create(&model.ClientStat{ClientID: cl.ID, Up: d.Up, Down: d.Down})
	}
	if needApply {
		_ = a.core.Apply()
	}
}

// clashClient 返回 Clash API 客户端，配置变化时自动重建。
func (a *App) clashClient() *stats.ClashClient {
	addr := a.settings.GetDefault(model.SettingClashListen, "127.0.0.1:9090")
	secret := a.settings.Get(model.SettingClashSecret)

	a.statsMu.Lock()
	defer a.statsMu.Unlock()
	if a.clashCli == nil || a.clashAddr != addr || a.clashSecret != secret {
		a.clashCli = stats.NewClashClient(addr, secret)
		a.clashAddr = addr
		a.clashSecret = secret
	}
	return a.clashCli
}

// v2rayClient 返回 V2Ray 统计客户端，地址变化时自动重建。
func (a *App) v2rayClient() *stats.Client {
	addr := a.settings.GetDefault(model.SettingV2RayListen, "127.0.0.1:9091")

	a.statsMu.Lock()
	defer a.statsMu.Unlock()
	if a.statsCli == nil || a.statsAddr != addr {
		if a.statsCli != nil {
			_ = a.statsCli.Close()
		}
		a.statsCli = stats.NewClient(addr)
		a.statsAddr = addr
	}
	return a.statsCli
}

// ---------------- 证书自动续签 ----------------

// certRenewLoop 每 12 小时检查一次证书，距到期不足 30 天则自动续签。
func (a *App) certRenewLoop() {
	time.Sleep(time.Minute)
	ticker := time.NewTicker(certRenewEvery)
	defer ticker.Stop()
	for {
		a.renewCertsOnce()
		<-ticker.C
	}
}

func (a *App) renewCertsOnce() {
	var certs []model.Certificate
	if err := a.db.Where("auto_renew = ?", true).Find(&certs).Error; err != nil {
		return
	}
	renewed := false
	for i := range certs {
		ct := &certs[i]
		if ct.NotAfter == nil || time.Until(*ct.NotAfter) > certRenewBefore {
			continue
		}
		if err := a.obtainCert(ct); err != nil {
			continue
		}
		renewed = true
	}
	if renewed && a.core.Proc.Running() {
		_ = a.core.RestartCore()
	}
}
