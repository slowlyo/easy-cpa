package backend

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type selfUpdatePlan struct {
	WorkDir         string
	SourcePath      string
	TargetPath      string
	RestartPath     string
	RestartMode     string
	ReplaceAsBundle bool
}

// prepareSelfUpdate 下载并准备自身更新资产。
func (a *App) prepareSelfUpdate(ctx context.Context, meta ReleaseMeta) error {
	workDir := filepath.Join(a.paths.TmpDir, "self-update", time.Now().Format("20060102150405"))
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return fmt.Errorf("创建应用更新目录失败: %w", err)
	}

	archivePath := filepath.Join(workDir, meta.AssetName)
	if err := downloadFile(ctx, a.proxy, meta.DownloadURL, archivePath); err != nil {
		return err
	}
	if err := verifySHA256(archivePath, meta.SHA256); err != nil {
		return err
	}

	extractedDir := filepath.Join(workDir, "extracted")
	if err := os.MkdirAll(extractedDir, 0o755); err != nil {
		return fmt.Errorf("创建更新解压目录失败: %w", err)
	}
	if err := extractArchive(archivePath, extractedDir); err != nil {
		return err
	}

	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("读取当前应用路径失败: %w", err)
	}
	plan, err := buildSelfUpdatePlan(executablePath, extractedDir, workDir)
	if err != nil {
		return err
	}
	return launchSelfUpdate(plan)
}

// buildSelfUpdatePlan 组装当前平台的更新替换方案。
func buildSelfUpdatePlan(executablePath, extractedDir, workDir string) (selfUpdatePlan, error) {
	switch runtime.GOOS {
	case "windows":
		sourcePath := filepath.Join(extractedDir, "easy-cpa.exe")
		if !FileExists(sourcePath) {
			return selfUpdatePlan{}, fmt.Errorf("未找到应用更新文件: %s", sourcePath)
		}
		return selfUpdatePlan{
			WorkDir:         workDir,
			SourcePath:      sourcePath,
			TargetPath:      executablePath,
			RestartPath:     executablePath,
			RestartMode:     "binary",
			ReplaceAsBundle: false,
		}, nil
	case "linux":
		sourcePath := filepath.Join(extractedDir, "easy-cpa")
		if !FileExists(sourcePath) {
			return selfUpdatePlan{}, fmt.Errorf("未找到应用更新文件: %s", sourcePath)
		}
		return selfUpdatePlan{
			WorkDir:         workDir,
			SourcePath:      sourcePath,
			TargetPath:      executablePath,
			RestartPath:     executablePath,
			RestartMode:     "binary",
			ReplaceAsBundle: false,
		}, nil
	case "darwin":
		sourceAppPath, err := findAppBundle(extractedDir)
		if err != nil {
			return selfUpdatePlan{}, err
		}
		targetAppPath, err := currentAppBundlePath(executablePath)
		if err != nil {
			return selfUpdatePlan{}, err
		}
		return selfUpdatePlan{
			WorkDir:         workDir,
			SourcePath:      sourceAppPath,
			TargetPath:      targetAppPath,
			RestartPath:     targetAppPath,
			RestartMode:     "app",
			ReplaceAsBundle: true,
		}, nil
	default:
		return selfUpdatePlan{}, fmt.Errorf("当前平台暂不支持应用热更新: %s", runtime.GOOS)
	}
}

// findAppBundle 从解压目录中查找 .app 包。
func findAppBundle(root string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", fmt.Errorf("读取应用更新目录失败: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".app") {
			return filepath.Join(root, entry.Name()), nil
		}
	}
	return "", fmt.Errorf("未找到 macOS 应用包: %s", root)
}

// currentAppBundlePath 解析当前运行中的 .app 根目录。
func currentAppBundlePath(executablePath string) (string, error) {
	current := filepath.Clean(executablePath)
	for {
		if strings.HasSuffix(strings.ToLower(current), ".app") {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("当前应用不在 .app 包中: %s", executablePath)
		}
		current = parent
	}
}
