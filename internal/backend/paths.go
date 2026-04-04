package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ResolveManagedPaths 解析 easy-cpa 托管目录。
func ResolveManagedPaths() (ManagedPaths, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return ManagedPaths{}, fmt.Errorf("获取用户配置目录失败: %w", err)
	}
	root = filepath.Join(root, "easy-cpa")
	coreBinary := "cli-proxy-api"
	if runtime.GOOS == "windows" {
		coreBinary = "cli-proxy-api.exe"
	}
	return ManagedPaths{
		RootDir:        root,
		CoreDir:        filepath.Join(root, "core"),
		PanelDir:       filepath.Join(root, "panel"),
		LogsDir:        filepath.Join(root, "logs"),
		TmpDir:         filepath.Join(root, "tmp"),
		SettingsPath:   filepath.Join(root, "settings.json"),
		ConfigPath:     filepath.Join(root, "config.yaml"),
		CoreMetaPath:   filepath.Join(root, "core", "release.json"),
		PanelMetaPath:  filepath.Join(root, "panel", "release.json"),
		CoreBinaryPath: filepath.Join(root, "core", coreBinary),
		PanelHTMLPath:  filepath.Join(root, "panel", "management.html"),
	}, nil
}

// EnsureManagedDirectories 创建托管所需目录。
func EnsureManagedDirectories(paths ManagedPaths) error {
	dirs := []string{paths.RootDir, paths.CoreDir, paths.PanelDir, paths.LogsDir, paths.TmpDir}
	for _, dir := range dirs {
		// 这里只创建缺失目录，不碰已有内容。
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建目录失败 %s: %w", dir, err)
		}
	}
	return nil
}

// FileExists 判断文件是否存在。
func FileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
