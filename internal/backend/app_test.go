package backend

import (
	"path/filepath"
	"testing"
)

// TestIsCloseConfirmed 验证关闭确认按钮识别。
func TestIsCloseConfirmed(t *testing.T) {
	if !isCloseConfirmed(closeButtonConfirm) {
		t.Fatalf("应识别关闭应用按钮")
	}
	if !isCloseConfirmed(closeButtonSilence) {
		t.Fatalf("应识别关闭并不再询问按钮")
	}
	if isCloseConfirmed(closeButtonCancel) {
		t.Fatalf("不应把取消识别为确认关闭")
	}
}

// TestShouldDisableCloseConfirm 验证仅“不再询问”会关闭提示。
func TestShouldDisableCloseConfirm(t *testing.T) {
	if !shouldDisableCloseConfirm(closeButtonSilence) {
		t.Fatalf("应识别关闭并不再询问")
	}
	if shouldDisableCloseConfirm(closeButtonConfirm) {
		t.Fatalf("普通关闭不应关闭提示")
	}
}

// TestSettingsStoreCloseConfirmEnabled 验证关闭确认设置可持久化。
func TestSettingsStoreCloseConfirmEnabled(t *testing.T) {
	store := NewSettingsStore(filepath.Join(t.TempDir(), "settings.json"))
	if err := store.Load(); err != nil {
		t.Fatalf("加载设置失败: %v", err)
	}
	if !store.CloseConfirmEnabled() {
		t.Fatalf("默认应开启关闭确认")
	}
	if err := store.SaveCloseConfirmEnabled(false); err != nil {
		t.Fatalf("保存关闭确认设置失败: %v", err)
	}

	reloaded := NewSettingsStore(store.path)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("重新加载设置失败: %v", err)
	}
	if reloaded.CloseConfirmEnabled() {
		t.Fatalf("应持久化关闭确认已关闭")
	}
}
