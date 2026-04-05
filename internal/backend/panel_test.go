package backend

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBuildPanelBootstrapHTML 验证包装页会写入自动登录信息。
func TestBuildPanelBootstrapHTML(t *testing.T) {
	html := BuildPanelBootstrapHTML("http://127.0.0.1:8317", "secret-key")
	if !strings.Contains(html, `const apiBase = "http://127.0.0.1:8317";`) {
		t.Fatalf("apiBase not found in bootstrap html")
	}
	if !strings.Contains(html, `const managementKey = "secret-key";`) {
		t.Fatalf("managementKey not found in bootstrap html")
	}
	if !strings.Contains(html, `localStorage.setItem("apiBase", apiBase)`) {
		t.Fatalf("apiBase bootstrap write not found in bootstrap html")
	}
	if !strings.Contains(html, `localStorage.setItem("managementKey", managementKey)`) {
		t.Fatalf("managementKey bootstrap write not found in bootstrap html")
	}
	if !strings.Contains(html, `fetch("/v0/management/config"`) {
		t.Fatalf("health probe not found in bootstrap html")
	}
	if !strings.Contains(html, `const renderWaitingDetail = (status) => {`) {
		t.Fatalf("waiting detail helper not found in bootstrap html")
	}
	if !strings.Contains(html, `if (status >= 500) {`) {
		t.Fatalf("5xx waiting fallback not found in bootstrap html")
	}
	if strings.Contains(html, `正在等待管理接口响应（" + response.status + "）。"`) {
		t.Fatalf("raw status prompt should not be shown in bootstrap html")
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
	if !strings.Contains(html, `.theme-menu + button {`) {
		t.Fatalf("managed header button override not found")
	}
	if !strings.Contains(html, "</style></head>") {
		t.Fatalf("dark theme should be injected before head end")
	}
}

// TestPanelManagerStartProxiesAPI 验证管理页服务会把 API 请求反代到核心。
func TestPanelManagerStartProxiesAPI(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v0/management/config" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("X-Cpa-Version", "6.9.15")
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	paths := ManagedPaths{
		PanelDir:      t.TempDir(),
		TmpDir:        t.TempDir(),
		PanelHTMLPath: t.TempDir() + "/management.html",
	}
	manager := NewPanelManager(paths, NewProxyManager(NewSettingsStore(t.TempDir()+"/settings.json"), NewLogBuffer(10)), NewLogBuffer(10))
	if err := manager.Start(upstream.URL, "secret-key"); err != nil {
		t.Fatalf("start panel manager failed: %v", err)
	}
	defer manager.Stop()

	baseURL := strings.Split(manager.URL(), "/?v=")[0]
	response, err := http.Get(baseURL + "/v0/management/config")
	if err != nil {
		t.Fatalf("proxy request failed: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read proxy response failed: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Fatalf("unexpected proxy body: %s", string(body))
	}
	if response.Header.Get("X-Cpa-Version") != "6.9.15" {
		t.Fatalf("expected version header to pass through, got %q", response.Header.Get("X-Cpa-Version"))
	}
}
