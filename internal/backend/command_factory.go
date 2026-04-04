package backend

import (
	"context"
	"os/exec"
)

// newBackgroundCommand 创建后台命令并应用平台相关的启动参数。
func newBackgroundCommand(name string, arg ...string) *exec.Cmd {
	cmd := exec.Command(name, arg...)
	prepareManagedCommand(cmd)
	return cmd
}

// newBackgroundCommandContext 创建带上下文的后台命令并应用平台相关的启动参数。
func newBackgroundCommandContext(ctx context.Context, name string, arg ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, arg...)
	prepareManagedCommand(cmd)
	return cmd
}
