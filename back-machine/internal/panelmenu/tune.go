package panelmenu

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strings"

	"horseui/back-machine/internal/sysinfo"
)

// requireRoot 修改系统参数需要 root；Windows 无 euid 概念，交给具体功能判断是否支持。
func requireRoot() bool {
	if runtime.GOOS == "windows" {
		return true
	}
	if os.Geteuid() != 0 {
		fmt.Println("  提示: 该操作需要 root 权限（请用 sudo horseui 重新运行）")
		return false
	}
	return true
}

// tuneMenu 系统调优（弱网优化）：检测 → 一键应用 / 回滚。
func tuneMenu(opts Options, rd *bufio.Reader) {
	if !requireRoot() {
		return
	}

	printTune(sysinfo.Detect(opts.DataDir))
	fmt.Println()
	fmt.Println("   1. 应用推荐参数（写入 /etc/sysctl.d/99-horseui.conf，可回滚）")
	fmt.Println("   2. 回滚（删除配置并还原调优前的原始值）")
	fmt.Println("   0. 返回")
	fmt.Print("  请输入: ")

	line, err := rd.ReadString('\n')
	if err != nil {
		return
	}
	switch strings.TrimSpace(line) {
	case "1":
		rep, err := sysinfo.Apply(opts.DataDir)
		if err != nil {
			fmt.Println("  应用失败: " + err.Error())
			return
		}
		fmt.Println()
		fmt.Println("  已应用。")
		printTune(rep)
		fmt.Println("  提示: 缓冲类参数只对新建 socket 生效，重启一次内核进程才会完全生效。")
	case "2":
		rep, err := sysinfo.Revert(opts.DataDir)
		if err != nil {
			fmt.Println("  回滚失败: " + err.Error())
			return
		}
		fmt.Println()
		fmt.Println("  已回滚。")
		printTune(rep)
	case "0", "":
		return
	default:
		fmt.Println("  已取消")
	}
}

// printTune 打印调优体检结果。
func printTune(rep sysinfo.TuneReport) {
	if !rep.Supported {
		fmt.Println("  当前系统不支持内核参数调优（仅 Linux 支持）")
		return
	}
	fmt.Printf("  内存 %dMB    bbr=%v    容器=%v    已应用=%v\n", rep.MemMB, rep.BBRAvailable, rep.InContainer, rep.Applied)
	if rep.ConfPath != "" {
		fmt.Printf("  配置文件: %s\n", rep.ConfPath)
	}
	for _, it := range rep.Items {
		mark := "OK"
		if !it.OK {
			mark = "!!"
		}
		tail := ""
		if !it.Safe {
			tail = "  [仅建议]"
		}
		fmt.Printf("  %-2s %-38s %s -> %s%s\n", mark, it.Key, it.Current, it.Suggest, tail)
	}
	for _, w := range rep.Warnings {
		fmt.Println("   ! " + w)
	}
}
