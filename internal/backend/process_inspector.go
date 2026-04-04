package backend

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ListeningProcess 描述某个监听端口的进程信息。
type ListeningProcess struct {
	PID            int
	ExecutablePath string
	CommandLine    string
	StartedAt      time.Time
}

// IsManagedProcess 判断是否为 easy-cpa 托管的核心进程。
func (p ListeningProcess) IsManagedProcess(managedBinaryPath, configPath string) bool {
	if p.PID == 0 {
		return false
	}
	normalizedBinary := normalizeComparablePath(managedBinaryPath)
	if normalizedBinary == "" {
		return false
	}

	// 进程路径可读时，直接按核心二进制路径匹配。
	if normalizeComparablePath(p.ExecutablePath) == normalizedBinary {
		return true
	}

	// 命令行为空时，无法进一步判断是否为托管实例。
	if strings.TrimSpace(p.CommandLine) == "" {
		return false
	}

	// 命令行至少要命中托管核心路径，避免误杀其他进程。
	if !containsComparablePath(p.CommandLine, normalizedBinary) {
		return false
	}

	// 配置路径为空时，仅凭二进制路径即可视为托管实例。
	if strings.TrimSpace(configPath) == "" {
		return true
	}
	return containsComparablePath(p.CommandLine, configPath)
}

// FindListeningProcess 查找占用指定 TCP 端口的监听进程。
func FindListeningProcess(port int) (ListeningProcess, error) {
	switch runtime.GOOS {
	case "windows":
		return findListeningProcessWindows(port)
	case "darwin", "linux":
		return findListeningProcessUnix(port)
	default:
		return ListeningProcess{}, nil
	}
}

// terminatePID 结束指定 PID 的进程。
func terminatePID(pid int) error {
	if pid <= 0 {
		return nil
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

// waitPortReleased 等待指定端口释放。
func waitPortReleased(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			_ = listener.Close()
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("端口 %d 仍被占用", port)
}

// findListeningProcessWindows 通过 netstat 与 CIM 查询监听进程。
func findListeningProcessWindows(port int) (ListeningProcess, error) {
	output, err := exec.Command("netstat", "-ano", "-p", "tcp").Output()
	if err != nil {
		return ListeningProcess{}, fmt.Errorf("读取端口占用失败: %w", err)
	}
	pid, err := parseWindowsNetstatPID(string(output), port)
	if err != nil || pid == 0 {
		return ListeningProcess{}, err
	}
	return readWindowsProcess(pid)
}

// findListeningProcessUnix 通过 lsof 查询监听进程。
func findListeningProcessUnix(port int) (ListeningProcess, error) {
	output, err := exec.Command("lsof", "-nP", fmt.Sprintf("-iTCP:%d", port), "-sTCP:LISTEN", "-Fpctn").Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) == 0 {
			return ListeningProcess{}, nil
		}
		return ListeningProcess{}, fmt.Errorf("读取端口占用失败: %w", err)
	}
	process := parseUnixLsof(output)
	if process.PID == 0 {
		return process, nil
	}
	commandLine, executablePath := readUnixProcess(process.PID)
	if commandLine != "" {
		process.CommandLine = commandLine
	}
	if executablePath != "" {
		process.ExecutablePath = executablePath
	}
	process.StartedAt = readUnixProcessStartTime(process.PID)
	return process, nil
}

// parseWindowsNetstatPID 解析 netstat 输出中的监听 PID。
func parseWindowsNetstatPID(output string, port int) (int, error) {
	target := fmt.Sprintf(":%d", port)
	lines := strings.Split(output, "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 || !strings.EqualFold(fields[0], "TCP") {
			continue
		}
		if !strings.HasSuffix(fields[1], target) {
			continue
		}
		if !strings.EqualFold(fields[3], "LISTENING") {
			continue
		}
		pid, err := strconv.Atoi(fields[4])
		if err != nil {
			return 0, fmt.Errorf("解析监听 PID 失败: %w", err)
		}
		return pid, nil
	}
	return 0, nil
}

// readWindowsProcess 读取 Windows 进程的路径与命令行。
func readWindowsProcess(pid int) (ListeningProcess, error) {
	script := fmt.Sprintf(`$p = Get-CimInstance Win32_Process -Filter "ProcessId = %d"; if ($p) { [PSCustomObject]@{ ExecutablePath = $p.ExecutablePath; CommandLine = $p.CommandLine; StartedAt = ([System.Management.ManagementDateTimeConverter]::ToDateTime($p.CreationDate)).ToString('o') } | ConvertTo-Json -Compress }`, pid)
	output, err := exec.Command("powershell", "-NoProfile", "-Command", script).Output()
	if err != nil {
		return ListeningProcess{PID: pid}, nil
	}
	process := ListeningProcess{PID: pid}
	if err := parseWindowsProcessJSON(output, &process); err != nil {
		return process, nil
	}
	return process, nil
}

// parseWindowsProcessJSON 解析 PowerShell 返回的进程 JSON。
func parseWindowsProcessJSON(output []byte, process *ListeningProcess) error {
	var payload struct {
		ExecutablePath string `json:"ExecutablePath"`
		CommandLine    string `json:"CommandLine"`
		StartedAt      string `json:"StartedAt"`
	}
	if err := json.Unmarshal(bytesTrimSpace(output), &payload); err != nil {
		return err
	}
	process.ExecutablePath = strings.TrimSpace(payload.ExecutablePath)
	process.CommandLine = strings.TrimSpace(payload.CommandLine)
	if startedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(payload.StartedAt)); err == nil {
		process.StartedAt = startedAt
	}
	return nil
}

// bytesTrimSpace 兼容 JSON 输入的空白清理。
func bytesTrimSpace(input []byte) []byte {
	return []byte(strings.TrimSpace(string(input)))
}

// normalizeComparablePath 统一路径格式，便于跨平台比较。
func normalizeComparablePath(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, `"'`)
	if raw == "" {
		return ""
	}
	return strings.ToLower(filepath.ToSlash(filepath.Clean(strings.ReplaceAll(raw, `\`, `/`))))
}

// containsComparablePath 判断命令行中是否包含指定路径。
func containsComparablePath(commandLine, path string) bool {
	normalizedPath := normalizeComparablePath(path)
	if normalizedPath == "" {
		return false
	}
	normalizedLine := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(commandLine), `\`, `/`))
	return strings.Contains(normalizedLine, normalizedPath)
}

// parseUnixLsof 解析 lsof 输出。
func parseUnixLsof(output []byte) ListeningProcess {
	process := ListeningProcess{}
	for _, raw := range strings.Split(string(output), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			process.PID, _ = strconv.Atoi(strings.TrimSpace(line[1:]))
		case 'c':
			process.CommandLine = strings.TrimSpace(line[1:])
		case 'n':
			if strings.Contains(line, "(LISTEN)") {
				process.ExecutablePath = strings.TrimSpace(process.CommandLine)
			}
		}
	}
	return process
}

// readUnixProcess 读取类 Unix 平台的进程命令行。
func readUnixProcess(pid int) (string, string) {
	output, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	if err != nil {
		return "", ""
	}
	commandLine := strings.TrimSpace(string(output))
	if commandLine == "" {
		return "", ""
	}
	fields := strings.Fields(commandLine)
	if len(fields) == 0 {
		return commandLine, ""
	}
	return commandLine, fields[0]
}

// readUnixProcessStartTime 读取类 Unix 平台的进程启动时间。
func readUnixProcessStartTime(pid int) time.Time {
	output, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "lstart=").Output()
	if err != nil {
		return time.Time{}
	}
	startedAt, err := time.Parse("Mon Jan 2 15:04:05 2006", strings.TrimSpace(string(output)))
	if err != nil {
		return time.Time{}
	}
	return startedAt
}
