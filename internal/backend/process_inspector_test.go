package backend

import "testing"

// TestParseWindowsNetstatPID 验证 Windows 监听端口解析。
func TestParseWindowsNetstatPID(t *testing.T) {
	output := `
  Proto  Local Address          Foreign Address        State           PID
  TCP    127.0.0.1:8317         0.0.0.0:0              LISTENING       40680
  TCP    127.0.0.1:9000         0.0.0.0:0              LISTENING       99999
`
	pid, err := parseWindowsNetstatPID(output, 8317)
	if err != nil {
		t.Fatalf("解析监听 PID 失败: %v", err)
	}
	if pid != 40680 {
		t.Fatalf("监听 PID 不正确: got=%d want=%d", pid, 40680)
	}
}

// TestListeningProcessIsManagedProcessByExecutable 验证托管核心路径匹配。
func TestListeningProcessIsManagedProcessByExecutable(t *testing.T) {
	process := ListeningProcess{PID: 1, ExecutablePath: `C:\Users\test\AppData\Roaming\easy-cpa\core\cli-proxy-api.exe`}
	if !process.IsManagedProcess(`c:\users\test\appdata\roaming\easy-cpa\core\cli-proxy-api.exe`, `C:\Users\test\AppData\Roaming\easy-cpa\config.yaml`) {
		t.Fatalf("应识别为托管核心")
	}
}

// TestListeningProcessIsManagedProcessByCommandLine 验证仅凭命令行也能识别托管核心。
func TestListeningProcessIsManagedProcessByCommandLine(t *testing.T) {
	process := ListeningProcess{
		PID:         28568,
		CommandLine: `"C:\Users\18217\AppData\Roaming\easy-cpa\core\cli-proxy-api.exe" -config "C:\Users\18217\AppData\Roaming\easy-cpa\config.yaml"`,
	}
	if !process.IsManagedProcess(`C:\Users\18217\AppData\Roaming\easy-cpa\core\cli-proxy-api.exe`, `C:\Users\18217\AppData\Roaming\easy-cpa\config.yaml`) {
		t.Fatalf("应通过命令行识别为托管核心: %+v", process)
	}
}

// TestParseWindowsProcessJSON 验证 Windows 进程 JSON 解析。
func TestParseWindowsProcessJSON(t *testing.T) {
	raw := []byte(`{"ExecutablePath":"C:\\Users\\18217\\AppData\\Roaming\\easy-cpa\\core\\cli-proxy-api.exe","CommandLine":"C:\\Users\\18217\\AppData\\Roaming\\easy-cpa\\core\\cli-proxy-api.exe -config C:\\Users\\18217\\AppData\\Roaming\\easy-cpa\\config.yaml","StartedAt":"2026-04-04T22:10:00+08:00"}`)
	process := ListeningProcess{PID: 28568}
	if err := parseWindowsProcessJSON(raw, &process); err != nil {
		t.Fatalf("解析进程 JSON 失败: %v", err)
	}
	if !process.IsManagedProcess(`C:\Users\18217\AppData\Roaming\easy-cpa\core\cli-proxy-api.exe`, `C:\Users\18217\AppData\Roaming\easy-cpa\config.yaml`) {
		t.Fatalf("应识别为托管核心: %+v", process)
	}
	if process.StartedAt.IsZero() {
		t.Fatalf("应解析到启动时间: %+v", process)
	}
}
