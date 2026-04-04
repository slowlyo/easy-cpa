package backend

import (
	"strings"
	"testing"
)

// TestBuildPanelBootstrapHTML 验证包装页会写入自动登录信息。
func TestBuildPanelBootstrapHTML(t *testing.T) {
	html := BuildPanelBootstrapHTML("http://127.0.0.1:8317", "secret-key")
	if !strings.Contains(html, `localStorage.setItem("apiBase", "http://127.0.0.1:8317")`) {
		t.Fatalf("apiBase not found in bootstrap html")
	}
	if !strings.Contains(html, `localStorage.setItem("managementKey", "secret-key")`) {
		t.Fatalf("managementKey not found in bootstrap html")
	}
	if !strings.Contains(html, `location.replace("/management.html")`) {
		t.Fatalf("redirect not found in bootstrap html")
	}
}

// TestInjectManagedPanelTheme 验证管理页会注入暗色变量覆盖。
func TestInjectManagedPanelTheme(t *testing.T) {
	raw := "<html><head><title>test</title></head><body></body></html>"
	html := InjectManagedPanelTheme(raw)
	if !strings.Contains(html, `id="easy-cpa-dark-theme"`) {
		t.Fatalf("dark theme injection not found")
	}
	if !strings.Contains(html, `--bg-primary: #12100d;`) {
		t.Fatalf("dark theme variables not found")
	}
	if !strings.Contains(html, "</style></head>") {
		t.Fatalf("dark theme should be injected before head end")
	}
}
