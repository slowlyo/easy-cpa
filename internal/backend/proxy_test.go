package backend

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestProxyCandidatesWithoutCustom 验证默认回退顺序。
func TestProxyCandidatesWithoutCustom(t *testing.T) {
	store := NewSettingsStore(t.TempDir() + "/settings.json")
	manager := NewProxyManager(store, NewLogBuffer(10))
	list := manager.candidates()
	if len(list) != 3 {
		t.Fatalf("unexpected candidate count: %d", len(list))
	}
	if list[0].mode != "direct" || list[1].mode != "fallback-7890" || list[2].mode != "fallback-7897" {
		t.Fatalf("unexpected candidate order: %+v", list)
	}
}

// TestProxyCandidatesWithCustom 验证自定义代理优先级。
func TestProxyCandidatesWithCustom(t *testing.T) {
	store := NewSettingsStore(t.TempDir() + "/settings.json")
	if err := store.SaveNetworkSettings(NetworkSettings{
		GithubProxyEnabled: true,
		GithubProxyURL:     "http://127.0.0.1:9999",
	}); err != nil {
		t.Fatalf("save settings failed: %v", err)
	}
	manager := NewProxyManager(store, NewLogBuffer(10))
	list := manager.candidates()
	if len(list) != 1 {
		t.Fatalf("unexpected candidate count: %d", len(list))
	}
	if list[0].mode != "custom" {
		t.Fatalf("unexpected custom candidate order: %+v", list)
	}
}

// TestAutoProxyCandidatesPreferLastSuccess 验证自动模式会优先复用上次成功结果。
func TestAutoProxyCandidatesPreferLastSuccess(t *testing.T) {
	manager := NewProxyManager(NewSettingsStore(t.TempDir()+"/settings.json"), NewLogBuffer(10))
	manager.current = "fallback-7890"
	list := manager.candidates()
	if len(list) != 3 {
		t.Fatalf("unexpected candidate count: %d", len(list))
	}
	if list[0].mode != "fallback-7890" {
		t.Fatalf("expected fallback-7890 to be prioritized, got %+v", list)
	}
}

// TestDownloadFallsBackAfterBodyReadFailure 验证读响应体失败也会自动切代理。
func TestDownloadFallsBackAfterBodyReadFailure(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("healthy-payload"))
	}))
	defer target.Close()

	store := NewSettingsStore(filepath.Join(t.TempDir(), "settings.json"))
	manager := NewProxyManager(store, NewLogBuffer(10))
	manager.current = "fallback-7890"

	listener, err := net.Listen("tcp", "127.0.0.1:7890")
	if err != nil {
		t.Skipf("port 7890 unavailable: %v", err)
	}
	flakyProxy := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		hijacker, ok := writer.(http.Hijacker)
		if !ok {
			t.Fatalf("writer does not support hijack")
		}
		conn, buffer, err := hijacker.Hijack()
		if err != nil {
			t.Fatalf("hijack failed: %v", err)
		}
		defer conn.Close()
		_, _ = buffer.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 64\r\n\r\nbroken")
		_ = buffer.Flush()
	})}
	defer func() {
		_ = flakyProxy.Close()
	}()
	go func() {
		_ = flakyProxy.Serve(listener)
	}()

	outputPath := filepath.Join(t.TempDir(), "asset.bin")
	file, err := os.Create(outputPath)
	if err != nil {
		t.Fatalf("create output file failed: %v", err)
	}
	defer file.Close()

	mode, err := manager.Download(t.Context(), target.URL, map[string]string{"User-Agent": "easy-cpa-test"}, file)
	if err != nil {
		t.Fatalf("download should fallback successfully, got error: %v", err)
	}
	if mode != "direct" {
		t.Fatalf("expected fallback to direct, got %s", mode)
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("seek output failed: %v", err)
	}
	content, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("read output failed: %v", err)
	}
	if string(content) != "healthy-payload" {
		t.Fatalf("unexpected output content: %s", string(content))
	}
}

// TestDoFallsBackAfterForbidden 验证 403 会继续尝试下一个自动通道。
func TestDoFallsBackAfterForbidden(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer target.Close()

	store := NewSettingsStore(filepath.Join(t.TempDir(), "settings.json"))
	manager := NewProxyManager(store, NewLogBuffer(10))
	manager.current = "fallback-7890"

	listener, err := net.Listen("tcp", "127.0.0.1:7890")
	if err != nil {
		t.Skipf("port 7890 unavailable: %v", err)
	}
	blockedProxy := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-RateLimit-Remaining", "0")
		writer.Header().Set("X-RateLimit-Reset", "4102444800")
		http.Error(writer, `{"message":"API rate limit exceeded"}`, http.StatusForbidden)
	})}
	defer func() {
		_ = blockedProxy.Close()
	}()
	go func() {
		_ = blockedProxy.Serve(listener)
	}()

	req, err := httpNewRequest(t.Context(), target.URL, map[string]string{"User-Agent": "easy-cpa-test"})
	if err != nil {
		t.Fatalf("create request failed: %v", err)
	}
	resp, mode, err := manager.Do(t.Context(), req)
	if err != nil {
		t.Fatalf("request should fallback successfully, got error: %v", err)
	}
	defer resp.Body.Close()
	if mode != "direct" {
		t.Fatalf("expected fallback to direct, got %s", mode)
	}
}
