package backend

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
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

	if m.server != nil {
		handler, err := m.buildHandler(apiBase, managementKey)
		if err != nil {
			return err
		}
		m.handler = handler
		return nil
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("启动面板服务失败: %w", err)
	}

	m.listener = listener
	m.url = fmt.Sprintf("http://%s", listener.Addr().String())
	handler, err := m.buildHandler(apiBase, managementKey)
	if err != nil {
		_ = listener.Close()
		m.listener = nil
		m.url = ""
		return err
	}
	m.handler = handler
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

// buildHandler 组装管理页包装页与 API 同源代理。
func (m *PanelManager) buildHandler(apiBase, managementKey string) (http.HandlerFunc, error) {
	proxy, err := m.buildAPIProxy(apiBase)
	if err != nil {
		return nil, err
	}
	panelBase := m.url
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/" || r.URL.Path == "/index.html":
			html := BuildPanelBootstrapHTML(panelBase, managementKey)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(html))
			return
		case r.URL.Path == "/management.html" && FileExists(m.paths.PanelHTMLPath):
			html, err := BuildManagedPanelHTML(m.paths.PanelHTMLPath)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(html))
			return
		case proxy != nil:
			proxy.ServeHTTP(w, r)
			return
		default:
			http.NotFound(w, r)
		}
	}, nil
}

// buildAPIProxy 创建指向核心管理接口的本地反向代理。
func (m *PanelManager) buildAPIProxy(apiBase string) (*httputil.ReverseProxy, error) {
	target, err := url.Parse(strings.TrimSpace(apiBase))
	if err != nil {
		return nil, fmt.Errorf("解析管理接口地址失败: %w", err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(req *http.Request) {
		director(req)
		req.Host = target.Host
	}
	// 代理异常时直接返回清晰错误，避免官方页面只看到空白失败。
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, proxyErr error) {
		http.Error(w, fmt.Sprintf("转发管理接口失败: %v", proxyErr), http.StatusBadGateway)
	}
	return proxy, nil
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
  <style>
    :root {
      color-scheme: dark;
      font-family: "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif;
      background: #12100d;
      color: #f3ede3;
    }
    body {
      margin: 0;
      min-height: 100vh;
      display: grid;
      place-items: center;
      background:
        radial-gradient(circle at top, rgba(199, 160, 107, .18), transparent 42%%),
        #12100d;
    }
    .boot-card {
      width: min(360px, calc(100vw - 32px));
      padding: 24px;
      border: 1px solid #3a332c;
      border-radius: 16px;
      background: rgba(29, 25, 21, .92);
      box-shadow: 0 18px 42px rgba(0, 0, 0, .32);
    }
    .boot-title {
      margin: 0 0 12px;
      font-size: 18px;
      font-weight: 700;
    }
    .boot-detail {
      margin: 0;
      color: #c7bdae;
      line-height: 1.6;
      font-size: 14px;
    }
  </style>
</head>
<body>
  <div class="boot-card">
    <h1 class="boot-title">管理页准备中</h1>
    <p class="boot-detail" id="boot-detail">正在等待管理接口就绪。</p>
  </div>
<script>
  const apiBase = %q;
  const managementKey = %q;
  const detailElement = document.getElementById("boot-detail");

  // 启动阶段的 5xx 多半只是核心尚未就绪，不直接展示原始状态码，避免误导用户。
  const renderWaitingDetail = (status) => {
    if (typeof status !== "number" || !Number.isFinite(status) || status <= 0) {
      detailElement.textContent = "正在等待核心进程启动并暴露管理接口。";
      return;
    }

    // 5xx 代表代理或核心短暂未就绪，统一收口为启动中提示。
    if (status >= 500) {
      detailElement.textContent = "正在等待核心进程启动并暴露管理接口。";
      return;
    }

    // 4xx 已有接口响应，通常是启动中的认证态切换，继续等待即可。
    if (status == 401 || status == 403) {
      detailElement.textContent = "管理接口已响应，正在同步认证信息。";
      return;
    }

    detailElement.textContent = "管理接口正在初始化，请稍候。";
  };

  // 只有在管理接口已健康时才跳转官方页，避免自动登录过早失败后停在认证页。
  const waitForManagementAPI = async () => {
    const headers = managementKey ? {"Authorization": "Bearer " + managementKey} : {};
    for (;;) {
      try {
        const response = await fetch("/v0/management/config", {
          method: "GET",
          headers,
          cache: "no-store",
        });
        if (response.ok) {
          localStorage.setItem("apiBase", apiBase);
          localStorage.setItem("managementKey", managementKey);
          localStorage.setItem("isLoggedIn", "true");
          location.replace("/management.html");
          return;
        }

        // 接口尚未健康时继续轮询，避免把官方页推进到登录页终态。
        renderWaitingDetail(response.status);
      } catch (_error) {
        // 核心尚未监听或代理未连通时，保持等待态即可。
        renderWaitingDetail(0);
      }

      await new Promise((resolve) => window.setTimeout(resolve, 500));
    }
  };

  void waitForManagementAPI();
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
