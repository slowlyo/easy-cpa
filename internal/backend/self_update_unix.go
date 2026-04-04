//go:build !windows

package backend

import (
	"fmt"
	"os"
	"path/filepath"
)

// launchSelfUpdate 在类 Unix 平台启动后台更新脚本。
func launchSelfUpdate(plan selfUpdatePlan) error {
	scriptPath := filepath.Join(plan.WorkDir, "apply-update.sh")
	restartCommand := fmt.Sprintf("%q >/dev/null 2>&1 &", plan.RestartPath)
	if plan.RestartMode == "app" {
		restartCommand = fmt.Sprintf("open -n %q >/dev/null 2>&1 &", plan.RestartPath)
	}
	copyCommand := fmt.Sprintf("cp %q %q", plan.SourcePath, plan.TargetPath)
	if plan.ReplaceAsBundle {
		copyCommand = fmt.Sprintf("cp -R %q %q", plan.SourcePath, plan.TargetPath)
	}
	script := fmt.Sprintf(`#!/bin/sh
PID_TO_WAIT=%d
while kill -0 "$PID_TO_WAIT" 2>/dev/null; do
  sleep 1
done
sleep 1
rm -rf %q
mkdir -p %q
%s
chmod +x %q 2>/dev/null || true
%s
rm -rf %q
rm -f "$0"
`, os.Getpid(), plan.TargetPath, filepath.Dir(plan.TargetPath), copyCommand, plan.TargetPath, restartCommand, plan.WorkDir)
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		return fmt.Errorf("写入应用更新脚本失败: %w", err)
	}
	cmd := newBackgroundCommand("sh", scriptPath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动应用更新脚本失败: %w", err)
	}
	return nil
}
