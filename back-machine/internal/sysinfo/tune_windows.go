//go:build windows

package sysinfo

import "fmt"

// Windows 下不提供系统调优：Detect 会优雅降级（Supported=false，不报错不阻塞），
// 所有写操作直接返回错误，避免误以为已经生效。

func tuneSupported() bool { return false }

func tuneConfPath() string { return "" }

func tuneReadSysctl(string) (string, bool) { return "", false }

func tuneBBRAvailable() bool { return false }

func tuneInContainer() bool { return false }

func tuneCanWrite() bool { return false }

func tuneWriteConf(string) error { return errTuneUnsupported() }

func tuneRemoveConf() error { return errTuneUnsupported() }

func tuneSetValue(string, string) error { return errTuneUnsupported() }

func tuneMemMB() int64 { return 0 }

func errTuneUnsupported() error {
	return fmt.Errorf("Windows 不支持内核参数调优，请在 Linux 服务器上操作")
}
