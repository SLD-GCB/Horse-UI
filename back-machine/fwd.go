package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"horseui/back-machine/internal/firewall"
	"horseui/back-machine/internal/store"
)

// runFwd 独立数据面：只读数据库并维护 nftables，不启动 HTTP 面板。
func runFwd(dataDir, listen string) {
	db, err := store.Open(dataDir)
	if err != nil {
		fatalf("[horseui-fwd] 打开数据库失败: %v", err)
	}
	if err := store.Migrate(db); err != nil {
		fatalf("[horseui-fwd] 迁移数据库失败: %v", err)
	}
	svc := firewall.New(db, dataDir)
	if !svc.Supported() {
		fatalf("[horseui-fwd] 当前系统不支持 nftables")
	}
	if port := listenPortOf(listen); port > 0 {
		_ = svc.EnsurePort(port, "tcp", "面板端口（fwd 启动保护）")
	}
	_ = svc.EnsurePort(22, "tcp", "SSH（fwd 启动保护）")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	svc.Start(ctx)
	last := svc.Revision()
	protected := map[int]bool{22: true}
	if port := listenPortOf(listen); port > 0 {
		protected[port] = true
	}
	svc.StartGuard(ctx, protected)
	fmt.Printf("[horseui-fwd] 已启动，数据目录: %s，监听保护: %s\n", dataDir, listen)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			fmt.Println("[horseui-fwd] 退出")
			return
		case <-ticker.C:
			// 其他防火墙工具可能删表；发现表丢失就重建。
			if svc.Enabled() && !svc.TablePresent() {
				if err := svc.Apply(); err != nil {
					fmt.Printf("[horseui-fwd] 规则表丢失，重建失败: %v\n", err)
				} else {
					fmt.Println("[horseui-fwd] 规则表丢失，已重建")
				}
				last = svc.Revision()
				continue
			}
			rev := svc.Revision()
			if rev == last {
				continue
			}
			if err := svc.Apply(); err != nil {
				fmt.Printf("[horseui-fwd] 应用失败: %v\n", err)
				continue
			}
			last = rev
			fmt.Printf("[horseui-fwd] 已应用新规则 %s\n", rev)
		}
	}
}

func listenPortOf(addr string) int {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	value, err := strconv.Atoi(port)
	if err != nil || value <= 0 || value > 65535 {
		return 0
	}
	return value
}
