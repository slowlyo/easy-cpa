//go:build !windows

package backend

import "os/exec"

// prepareManagedCommand 在非 Windows 平台保持默认启动行为。
func prepareManagedCommand(cmd *exec.Cmd) {
	_ = cmd
}
