//go:build !windows

package backend

import "os/exec"

// attachManagedProcessGuard 在非 Windows 平台保持空实现。
func attachManagedProcessGuard(cmd *exec.Cmd) (managedProcessGuard, error) {
	_ = cmd
	return nil, nil
}
