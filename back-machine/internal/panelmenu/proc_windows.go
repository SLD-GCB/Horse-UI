//go:build windows

package panelmenu

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

// detachedProcess (0x00000008) 让子进程脱离当前控制台，父进程退出后继续运行。
const detachedProcess = 0x00000008

func detachAttrs() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess,
	}
}

// killTree 用 taskkill 终止整棵进程树（含 sing-box 子进程）。
func killTree(pid int) error {
	return exec.Command(systemTool("taskkill.exe"), "/F", "/T", "/PID", strconv.Itoa(pid)).Run()
}

// systemTool 解析系统工具路径：优先 PATH，失败则回退到 %SystemRoot%\System32。
// 某些机器的 PATH 被改坏（例如 %SystemRoot% 拼写错误）会导致 LookPath 失败。
func systemTool(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	if root := os.Getenv("SystemRoot"); root != "" {
		p := filepath.Join(root, "System32", name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return name
}

// Windows 无 systemd，相关概念返回空值。
func installDir() string  { return "" }
func wrapperPath() string { return "" }
func unitPath() string    { return "" }

func systemdAvailable() bool { return false }

func systemctlRun(args ...string) error {
	return fmt.Errorf("Windows 不支持 systemctl")
}

// platformUninstall Windows 下无法删除正在运行的程序自身，只做提示。
func platformUninstall() ([]string, error) {
	self, err := selfPath()
	if err == nil {
		return []string{"请手动删除程序文件：" + self}, nil
	}
	return nil, nil
}
