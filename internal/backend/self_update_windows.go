//go:build windows

package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// launchSelfUpdate 在 Windows 上启动隐藏更新脚本。
func launchSelfUpdate(plan selfUpdatePlan) error {
	scriptPath := filepath.Join(plan.WorkDir, "apply-update.ps1")
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$pidToWait = %d
$source = %s
$target = %s
$restart = %s
$cleanup = %s
while (Get-Process -Id $pidToWait -ErrorAction SilentlyContinue) {
  Start-Sleep -Milliseconds 500
}
Start-Sleep -Milliseconds 300
New-Item -ItemType Directory -Force -Path ([System.IO.Path]::GetDirectoryName($target)) | Out-Null
Copy-Item -LiteralPath $source -Destination $target -Force
Start-Process -FilePath $restart | Out-Null
Remove-Item -LiteralPath $cleanup -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath $PSCommandPath -Force -ErrorAction SilentlyContinue
`, os.Getpid(), quotePowerShell(plan.SourcePath), quotePowerShell(plan.TargetPath), quotePowerShell(plan.RestartPath), quotePowerShell(plan.WorkDir))
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		return fmt.Errorf("写入应用更新脚本失败: %w", err)
	}
	cmd := newBackgroundCommand("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-File", scriptPath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动应用更新脚本失败: %w", err)
	}
	return nil
}

// quotePowerShell 转义 PowerShell 字符串字面量。
func quotePowerShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
