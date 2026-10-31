package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"horseui/back-machine/internal/api"
	"horseui/back-machine/internal/panelmenu"
	"horseui/back-machine/internal/sysinfo"
)

// version 面板版本号，构建时可通过 -ldflags "-X main.version=..." 覆盖。
var version = "1.0.0-skyhorse-rc1"

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// fatalf 打印错误并退出；Windows 下双击运行时停留，避免控制台窗口一闪而过。
func fatalf(format string, args ...any) {
	fmt.Printf("\n"+format+"\n", args...)
	if runtime.GOOS == "windows" {
		fmt.Print("\n按回车键退出 ...")
		_, _ = fmt.Scanln()
	}
	os.Exit(1)
}

func main() {
	dataDir := flag.String("data", envOr("HORSEUI_DATA_DIR", "data"), "运行数据目录")
	listen := flag.String("listen", envOr("HORSEUI_LISTEN", "0.0.0.0:8080"), "面板监听地址")
	open := flag.Bool("open", runtime.GOOS == "windows", "启动后自动打开浏览器")
	menu := flag.Bool("menu", false, "显示管理菜单")
	run := flag.Bool("run", false, "直接启动面板（不显示菜单）")
	tune := flag.String("tune", "", "系统调优：detect / apply / revert（供 install.sh 复用）")
	flag.Parse()

	// -tune 必须在 useMenu 之前处理：install.sh 是非交互调用，不能掉进菜单。
	if *tune != "" {
		runTune(*tune, *dataDir)
		return
	}

	if useMenu(*menu, *run) {
		panelmenu.Run(panelmenu.Options{DataDir: *dataDir, Listen: *listen, Version: version})
		return
	}

	app, err := api.New(api.Options{
		DataDir: *dataDir,
		Listen:  *listen,
		Version: version,
		Open:    *open,
	})
	if err != nil {
		fatalf("[horseui] 启动失败: %v", err)
	}
	fmt.Printf("[horseui] 数据目录: %s\n", *dataDir)
	if err := app.Run(); err != nil {
		fatalf("[horseui] 服务异常退出: %v", err)
	}
}

// runTune 非交互执行系统调优，供 install.sh / 脚本复用同一份逻辑。
// 结果直接打到 stdout，不进入菜单、不等待输入。
func runTune(action, dataDir string) {
	switch action {
	case "detect", "apply", "revert":
	default:
		fmt.Printf("[horseui] -tune 只支持 detect / apply / revert，收到：%s\n", action)
		os.Exit(2)
	}

	var (
		rep sysinfo.TuneReport
		err error
	)
	switch action {
	case "detect":
		rep = sysinfo.Detect(dataDir)
	default:
		if action == "apply" {
			rep, err = sysinfo.Apply(dataDir)
		} else {
			rep, err = sysinfo.Revert(dataDir)
		}
		if err != nil {
			fmt.Printf("[horseui] 系统调优失败：%v\n", err)
			os.Exit(1)
		}
	}
	printTuneReport(rep)
}

func printTuneReport(rep sysinfo.TuneReport) {
	if !rep.Supported {
		fmt.Println("[horseui] 当前系统不支持内核参数调优，跳过")
		return
	}
	fmt.Printf("[horseui] 系统调优：内存 %dMB，bbr=%v，容器=%v，已应用=%v\n",
		rep.MemMB, rep.BBRAvailable, rep.InContainer, rep.Applied)
	pending := 0
	for _, it := range rep.Items {
		mark := "已达标"
		if !it.OK {
			mark = "待优化"
			pending++
		}
		hint := ""
		if !it.Safe {
			hint = "（仅建议，不自动应用）"
		}
		fmt.Printf("  [%s] %-40s 当前=%s  建议=%s%s\n", mark, it.Key, it.Current, it.Suggest, hint)
	}
	for _, w := range rep.Warnings {
		fmt.Println("  ! " + w)
	}
	if pending > 0 {
		fmt.Printf("[horseui] 还有 %d 项未达标\n", pending)
	} else {
		fmt.Println("[horseui] 参数均已达标")
	}
}

// useMenu 决定是否进入交互式管理菜单。
// 显式传了「运行类」参数（如 systemd 的 -listen）一律直接启动，保证服务化部署不受影响；
// 只传 -data 之类配置参数时仍显示菜单。
func useMenu(forceMenu, forceRun bool) bool {
	if forceRun {
		return false
	}
	if forceMenu {
		return true
	}
	runFlag := false
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "listen", "open":
			runFlag = true
		}
	})
	if runFlag {
		return false
	}
	if runtime.GOOS == "windows" {
		return true // 双击或在终端运行 → 显示菜单
	}
	return panelmenu.IsTerminal(os.Stdin) // Linux 等：交互终端才显示菜单
}
