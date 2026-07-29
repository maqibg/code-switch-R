//go:build !windows

package infra

import "os/exec"

// HideWindowCmd 在非 Windows 平台创建 exec.Cmd（无隐藏窗口逻辑）
func HideWindowCmd(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}
