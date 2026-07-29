//go:build windows

package infra

import (
	"os/exec"
	"syscall"
)

// HideWindowCmd 在 Windows 上创建隐藏控制台窗口的 exec.Cmd
func HideWindowCmd(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}
