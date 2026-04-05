package backend

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"
)

// TestCoreRuntimeIsManagedConfigSnapshot 验证 auth-dir 可识别托管实例。
func TestCoreRuntimeIsManagedConfigSnapshot(t *testing.T) {
	runtime := &CoreRuntime{
		paths: ManagedPaths{
			RootDir: `C:\Users\test\AppData\Roaming\easy-cpa`,
		},
	}
	if !runtime.isManagedConfigSnapshot(ManagementConfigSnapshot{
		AuthDir: `C:\Users\test\AppData\Roaming\easy-cpa\auth`,
	}) {
		t.Fatalf("应通过 auth-dir 识别为托管实例")
	}
}

// TestCoreRuntimeIsManagedConfigSnapshotRejectsExternal 验证外部 auth-dir 不会被误判。
func TestCoreRuntimeIsManagedConfigSnapshotRejectsExternal(t *testing.T) {
	runtime := &CoreRuntime{
		paths: ManagedPaths{
			RootDir: `C:\Users\test\AppData\Roaming\easy-cpa`,
		},
	}
	if runtime.isManagedConfigSnapshot(ManagementConfigSnapshot{
		AuthDir: `C:\Tools\other-app\auth`,
	}) {
		t.Fatalf("不应把外部 auth-dir 识别为托管实例")
	}
}

// TestFetchManagementConfig 验证能从管理接口读取 auth-dir。
func TestFetchManagementConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer secret-key" {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		_, _ = writer.Write([]byte(`{"auth-dir":"C:\\Users\\test\\AppData\\Roaming\\easy-cpa\\auth"}`))
	}))
	defer server.Close()

	host, port := splitTestHostPort(t, server.URL)
	runtime := &CoreRuntime{}
	snapshot, err := runtime.fetchManagementConfig(ConfigState{
		Host:          host,
		Port:          port,
		ManagementKey: "secret-key",
	})
	if err != nil {
		t.Fatalf("读取管理配置失败: %v", err)
	}
	if snapshot.AuthDir != `C:\Users\test\AppData\Roaming\easy-cpa\auth` {
		t.Fatalf("auth-dir 不正确: %+v", snapshot)
	}
}

// TestAttachExistingProcessLocked 验证运行时可直接接管遗留核心。
func TestAttachExistingProcessLocked(t *testing.T) {
	runtime := &CoreRuntime{}
	configState := ConfigState{Host: "127.0.0.1", Port: 8317, ManagementKey: "secret-key"}
	process := ListeningProcess{
		PID:       40940,
		StartedAt: nowForTest(),
	}

	runtime.attachExistingProcessLocked(context.Background(), process, configState)
	state := runtime.State()
	if !state.Running || state.PID != 40940 || !state.ManagementHealthy {
		t.Fatalf("接管状态不正确: %+v", state)
	}
	if !runtime.attached || runtime.managedPID != 40940 || runtime.cancelMonitor == nil {
		t.Fatalf("接管内部状态不正确: attached=%v pid=%d cancel=%v", runtime.attached, runtime.managedPID, runtime.cancelMonitor != nil)
	}
	runtime.cancelMonitor()
}

// nowForTest 返回一个稳定的测试时间。
func nowForTest() time.Time {
	return time.Date(2026, 4, 5, 12, 15, 18, 0, time.Local)
}

// splitTestHostPort 解析 httptest 地址里的主机和端口。
func splitTestHostPort(t *testing.T, rawURL string) (string, int) {
	t.Helper()

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("解析测试地址失败: %v", err)
	}
	host, portText, err := net.SplitHostPort(parsedURL.Host)
	if err != nil {
		t.Fatalf("拆分测试地址失败: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("解析测试端口失败: %v", err)
	}
	return host, port
}
