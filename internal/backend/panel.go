package backend

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// PanelManager 负责管理官方管理页缓存与本地托管。
type PanelManager struct {
	paths    ManagedPaths
	proxy    *ProxyManager
	logs     *LogBuffer
	mu       sync.RWMutex
	server   *http.Server
	listener net.Listener
	url      string
	handler  http.HandlerFunc
	revision int
	lastKey  string
}

// NewPanelManager 创建管理页管理器。
func NewPanelManager(paths ManagedPaths, proxy *ProxyManager, logs *LogBuffer) *PanelManager {
	return &PanelManager{paths: paths, proxy: proxy, logs: logs}
}

// Start 启动本地静态服务。
func (m *PanelManager) Start(apiBase, managementKey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	signature := fmt.Sprintf("%s|%s", apiBase, managementKey)
	if m.lastKey != signature {
		m.revision++
		m.lastKey = signature
	}

	m.handler = func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			html := BuildPanelBootstrapHTML(apiBase, managementKey)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(html))
			return
		}
		if r.URL.Path == "/management.html" && FileExists(m.paths.PanelHTMLPath) {
			html, err := BuildManagedPanelHTML(m.paths.PanelHTMLPath)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(html))
			return
		}
		http.NotFound(w, r)
	}

	if m.server != nil {
		return nil
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("启动面板服务失败: %w", err)
	}

	m.listener = listener
	m.url = fmt.Sprintf("http://%s", listener.Addr().String())
	m.server = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.RLock()
		handler := m.handler
		m.mu.RUnlock()
		handler(w, r)
	})}

	go func() {
		if err := m.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			m.logs.Append("system", fmt.Sprintf("面板服务异常: %v", err))
		}
	}()
	return nil
}

// Stop 停止本地静态服务。
func (m *PanelManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.server == nil {
		return
	}
	_ = m.server.Shutdown(context.Background())
	m.server = nil
	m.listener = nil
}

// URL 返回包装页地址。
func (m *PanelManager) URL() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.url == "" {
		return ""
	}
	return fmt.Sprintf("%s/?v=%d", m.url, m.revision)
}

// Install 下载并更新管理页文件。
func (m *PanelManager) Install(ctx context.Context, meta ReleaseMeta, progress func(DownloadProgress)) error {
	if err := os.MkdirAll(m.paths.PanelDir, 0o755); err != nil {
		return fmt.Errorf("创建面板目录失败: %w", err)
	}
	if err := os.MkdirAll(m.paths.TmpDir, 0o755); err != nil {
		return fmt.Errorf("创建临时目录失败: %w", err)
	}
	tempPath := filepath.Join(m.paths.TmpDir, meta.AssetName)
	if err := downloadFile(ctx, m.proxy, meta.DownloadURL, tempPath, progress); err != nil {
		return err
	}
	defer os.Remove(tempPath)

	if err := verifySHA256(tempPath, meta.SHA256); err != nil {
		return err
	}
	raw, err := os.ReadFile(tempPath)
	if err != nil {
		return fmt.Errorf("读取管理页文件失败: %w", err)
	}
	if err := os.WriteFile(m.paths.PanelHTMLPath, raw, 0o644); err != nil {
		return fmt.Errorf("写入管理页失败: %w", err)
	}
	return WriteReleaseMetaFile(m.paths.PanelMetaPath, meta)
}

// BuildPanelBootstrapHTML 生成管理页包装页。
func BuildPanelBootstrapHTML(apiBase, managementKey string) string {
	return fmt.Sprintf(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Easy CPA Panel</title>
</head>
<body>
<script>
  localStorage.setItem("apiBase", %q);
  localStorage.setItem("managementKey", %q);
  localStorage.setItem("isLoggedIn", "true");
  location.replace("/management.html");
</script>
</body>
</html>`, apiBase, managementKey)
}

// BuildManagedPanelHTML 生成带暗色主题覆盖的管理页。
func BuildManagedPanelHTML(panelHTMLPath string) (string, error) {
	raw, err := os.ReadFile(panelHTMLPath)
	if err != nil {
		return "", fmt.Errorf("读取管理页文件失败: %w", err)
	}
	return InjectManagedPanelTheme(string(raw)), nil
}

// InjectManagedPanelTheme 给官方管理页注入暗色变量，并隐藏由 easy-cpa 接管的头部按钮。
func InjectManagedPanelTheme(html string) string {
	injection := `<style id="easy-cpa-dark-theme">
:root {
  color-scheme: dark;
  --bg-secondary: #181512;
  --bg-primary: #12100d;
  --bg-tertiary: #211d18;
  --bg-hover: #29241e;
  --bg-quinary: #16130f;
  --bg-error-light: rgba(209, 116, 99, .12);
  --floating-surface: #1d1915;
  --floating-border: #3a332c;
  --floating-shadow: 0 18px 42px rgba(0, 0, 0, .42);
  --text-primary: #f3ede3;
  --text-secondary: #c7bdae;
  --text-tertiary: #978d80;
  --text-quaternary: #71685d;
  --text-muted: var(--text-tertiary);
  --border-color: #2b2621;
  --border-secondary: var(--border-color);
  --border-primary: #3a332c;
  --border-hover: #4a4238;
  --primary-color: #c7a06b;
  --primary-hover: #b88f5a;
  --primary-active: #a77e48;
  --primary-contrast: #17130e;
  --success-color: #5fb07f;
  --quota-medium-color: #d1a53d;
  --warning-color: #d17463;
  --error-color: #d17463;
  --danger-color: var(--error-color);
  --info-color: var(--primary-color);
  --warning-bg: rgba(209, 116, 99, .16);
  --warning-border: rgba(209, 116, 99, .36);
  --warning-text: #efb2a5;
  --success-badge-bg: rgba(95, 176, 127, .16);
  --success-badge-text: #aee1c0;
  --success-badge-border: rgba(95, 176, 127, .35);
  --failure-badge-bg: rgba(209, 116, 99, .16);
  --failure-badge-text: #efb2a5;
  --failure-badge-border: rgba(209, 116, 99, .35);
  --count-badge-bg: rgba(199, 160, 107, .16);
  --count-badge-text: #ead9bf;
  --shadow: 0 1px 2px 0 rgb(0 0 0 / .28);
  --shadow-lg: 0 18px 32px -10px rgb(0 0 0 / .48);
  --accent-tertiary: var(--bg-tertiary);
  --glass-bg: rgba(24, 21, 18, .82);
  --glass-bg-secondary: rgba(31, 27, 22, .8);
}
html, body, #root {
  background: var(--bg-primary) !important;
  color: var(--text-primary) !important;
}
.theme-menu,
.theme-menu + button {
  display: none !important;
}
</style>`
	if strings.Contains(html, `id="easy-cpa-dark-theme"`) {
		return html
	}
	if strings.Contains(html, "</head>") {
		return strings.Replace(html, "</head>", injection+"</head>", 1)
	}
	return injection + html
}
