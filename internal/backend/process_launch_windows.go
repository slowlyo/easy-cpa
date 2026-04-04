//go:build windows

package backend

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// prepareManagedCommand 配置 Windows 子进程为无窗口启动。
func prepareManagedCommand(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}
