package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPatchExistingConfigOnlyFillsMissing 验证已有配置只补齐缺失字段。
func TestPatchExistingConfigOnlyFillsMissing(t *testing.T) {
	root := t.TempDir()
	paths := ManagedPaths{
		RootDir:      root,
		CoreDir:      filepath.Join(root, "core"),
		PanelDir:     filepath.Join(root, "panel"),
		LogsDir:      filepath.Join(root, "logs"),
		TmpDir:       filepath.Join(root, "tmp"),
		SettingsPath: filepath.Join(root, "settings.json"),
		ConfigPath:   filepath.Join(root, "config.yaml"),
	}
	settings := NewSettingsStore(filepath.Join(root, "settings.json"))
	if err := settings.Load(); err != nil {
		t.Fatalf("load settings failed: %v", err)
	}
	manager := NewConfigManager(paths, settings)

	raw := "host: 0.0.0.0\nport: 9317\nremote-management:\n  allow-remote: true\n"
	if err := os.WriteFile(paths.ConfigPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	state, err := manager.EnsureConfig()
	if err != nil {
		t.Fatalf("ensure config failed: %v", err)
	}
	if state.Host != "0.0.0.0" {
		t.Fatalf("host should keep existing value, got %s", state.Host)
	}
	if state.Port != 9317 {
		t.Fatalf("port should keep existing value, got %d", state.Port)
	}
	if state.ManagementKey == "" {
		t.Fatalf("management key should be generated")
	}

	content, err := os.ReadFile(paths.ConfigPath)
	if err != nil {
		t.Fatalf("read config failed: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "logging-to-file: true") {
		t.Fatalf("logging-to-file should be filled")
	}
	if !strings.Contains(text, "disable-control-panel: true") {
		t.Fatalf("disable-control-panel should be filled")
	}
}

// TestPatchExistingConfigSyncsManualPlainSecret 验证手动改明文密钥会同步回本地设置。
func TestPatchExistingConfigSyncsManualPlainSecret(t *testing.T) {
	root := t.TempDir()
	paths := ManagedPaths{
		RootDir:      root,
		CoreDir:      filepath.Join(root, "core"),
		PanelDir:     filepath.Join(root, "panel"),
		LogsDir:      filepath.Join(root, "logs"),
		TmpDir:       filepath.Join(root, "tmp"),
		SettingsPath: filepath.Join(root, "settings.json"),
		ConfigPath:   filepath.Join(root, "config.yaml"),
	}
	settings := NewSettingsStore(filepath.Join(root, "settings.json"))
	if err := settings.Load(); err != nil {
		t.Fatalf("load settings failed: %v", err)
	}
	if err := settings.SaveManagementKey("app-secret"); err != nil {
		t.Fatalf("save management key failed: %v", err)
	}
	manager := NewConfigManager(paths, settings)

	raw := "host: 127.0.0.1\nport: 9417\nremote-management:\n  secret-key: user-secret\n"
	if err := os.WriteFile(paths.ConfigPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	state, err := manager.EnsureConfig()
	if err != nil {
		t.Fatalf("ensure config failed: %v", err)
	}
	if state.Port != 9417 {
		t.Fatalf("port should keep existing value, got %d", state.Port)
	}
	if state.ManagementKey != "user-secret" {
		t.Fatalf("management key should follow config value, got %s", state.ManagementKey)
	}
	if settings.ManagementKey() != "user-secret" {
		t.Fatalf("settings should sync manual secret, got %s", settings.ManagementKey())
	}
}

// TestPatchExistingConfigKeepsSettingsForHashedSecret 验证哈希密钥不会覆盖本地明文设置。
func TestPatchExistingConfigKeepsSettingsForHashedSecret(t *testing.T) {
	root := t.TempDir()
	paths := ManagedPaths{
		RootDir:      root,
		CoreDir:      filepath.Join(root, "core"),
		PanelDir:     filepath.Join(root, "panel"),
		LogsDir:      filepath.Join(root, "logs"),
		TmpDir:       filepath.Join(root, "tmp"),
		SettingsPath: filepath.Join(root, "settings.json"),
		ConfigPath:   filepath.Join(root, "config.yaml"),
	}
	settings := NewSettingsStore(filepath.Join(root, "settings.json"))
	if err := settings.Load(); err != nil {
		t.Fatalf("load settings failed: %v", err)
	}
	if err := settings.SaveManagementKey("plain-secret"); err != nil {
		t.Fatalf("save management key failed: %v", err)
	}
	manager := NewConfigManager(paths, settings)

	raw := "host: 127.0.0.1\nport: 9517\nremote-management:\n  secret-key: $2a$10$wb/gLk9H9qbEtD3DdQMeCu5eHX8f8ozfnqRyR5Rv20tI0Ec6AM40m\n"
	if err := os.WriteFile(paths.ConfigPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	state, err := manager.EnsureConfig()
	if err != nil {
		t.Fatalf("ensure config failed: %v", err)
	}
	if state.ManagementKey != "plain-secret" {
		t.Fatalf("hashed secret should keep settings plain key, got %s", state.ManagementKey)
	}
	if settings.ManagementKey() != "plain-secret" {
		t.Fatalf("settings should not be replaced by hash, got %s", settings.ManagementKey())
	}
}
