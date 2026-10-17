//go:build !windows

package panelmenu

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// Linux 侧的固定路径（与 service/install.sh 保持一致）。
const (
	linuxInstallDir = "/usr/local/horseui"
	linuxWrapper    = "/usr/local/bin/horseui"
	linuxUnitPath   = "/etc/systemd/system/horseui.service"
)

func detachAttrs() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// killTree 终止整个进程组（Setsid 后子进程自成一组），带走 sing-box 子进程。
func killTree(pid int) error {
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		return syscall.Kill(pid, syscall.SIGKILL)
	}
	return nil
}

func installDir() string  { return linuxInstallDir }
func wrapperPath() string { return linuxWrapper }
func unitPath() string    { return linuxUnitPath }

// systemdAvailable 判断是否已按系统服务方式安装（install.sh 装过）。
func systemdAvailable() bool {
	st, err := os.Stat(linuxUnitPath)
	return err == nil && !st.IsDir()
}

// systemctlRun 执行 systemctl 并返回输出。
func systemctlRun(args ...string) error {
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s 失败: %v %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// platformUninstall 卸载 systemd 服务与程序文件（不动数据目录）。
func platformUninstall() ([]string, error) {
	var done []string
	if systemdAvailable() {
		_ = systemctlRun("stop", "horseui")
		_ = systemctlRun("disable", "horseui")
		done = append(done, "已停止并禁用 systemd 服务")
		if err := os.Remove(linuxUnitPath); err == nil {
			done = append(done, "已删除 "+linuxUnitPath)
		}
		_ = systemctlRun("daemon-reload")
	}
	if err := os.Remove(linuxWrapper); err == nil {
		done = append(done, "已删除命令 "+linuxWrapper)
	}
	if err := os.RemoveAll(linuxInstallDir); err == nil {
		done = append(done, "已删除程序目录 "+linuxInstallDir)
	}
	return done, nil
}
