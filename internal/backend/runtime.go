package backend

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CoreRuntime 负责 CPA 子进程生命周期和健康探测。
type CoreRuntime struct {
	paths         ManagedPaths
	config        *ConfigManager
	settings      *SettingsStore
	logs          *LogBuffer
	emitLog       func(LogEntry)
	emitStatus    func(CoreProcessState)
	mu            sync.RWMutex
	cmd           *exec.Cmd
	state         CoreProcessState
	cancelMonitor context.CancelFunc
	logCursor     int64
	logLineCount  int
	managedPID    int
	attached      bool
}

// ManagementConfigSnapshot 描述管理接口暴露出的关键配置。
type ManagementConfigSnapshot struct {
	AuthDir string `json:"auth-dir"`
}

// NewCoreRuntime 创建运行时管理器。
func NewCoreRuntime(paths ManagedPaths, config *ConfigManager, settings *SettingsStore, logs *LogBuffer, emitLog func(LogEntry), emitStatus func(CoreProcessState)) *CoreRuntime {
	return &CoreRuntime{
		paths:      paths,
		config:     config,
		settings:   settings,
		logs:       logs,
		emitLog:    emitLog,
		emitStatus: emitStatus,
	}
}

// Start 启动核心进程。
func (r *CoreRuntime) Start(ctx context.Context, configState ConfigState) error {
	r.mu.Lock()
	var bootLogs []string
	if r.cmd != nil && r.state.Running {
		r.mu.Unlock()
		return nil
	}
	var err error
	bootLogs, err = r.reconcileExistingProcessLocked(ctx, configState)
	if err != nil {
		r.mu.Unlock()
		return err
	}
	if r.state.Running {
		// 接管已存在的托管核心后，不再重复拉起新进程。
		state := r.state
		r.mu.Unlock()
		r.emitSystemLogs(bootLogs)
		r.emitStatus(state)
		return nil
	}

	cmd := newBackgroundCommandContext(ctx, r.paths.CoreBinaryPath, "-config", configState.ConfigPath)
	cmd.Env = append(cmd.Environ(), "MANAGEMENT_PASSWORD="+r.settings.ManagementKey())
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		r.mu.Unlock()
		return fmt.Errorf("创建 stdout 管道失败: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		r.mu.Unlock()
		return fmt.Errorf("创建 stderr 管道失败: %w", err)
	}
	if err := cmd.Start(); err != nil {
		r.mu.Unlock()
		return fmt.Errorf("启动核心失败: %w", err)
	}

	r.cmd = cmd
	r.managedPID = cmd.Process.Pid
	r.attached = false
	r.state = CoreProcessState{
		Running:           true,
		PID:               cmd.Process.Pid,
		StartedAt:         time.Now(),
		ExitedAt:          time.Time{},
		ExitCode:          0,
		LastError:         "",
		ManagementHealthy: false,
	}
	monitorCtx, cancel := context.WithCancel(ctx)
	r.cancelMonitor = cancel
	go r.pipeLogs("stdout", stdout)
	go r.pipeLogs("stderr", stderr)
	go r.waitProcess(cmd)
	go r.monitor(monitorCtx, configState)
	state := r.state
	r.mu.Unlock()
	r.emitSystemLogs(bootLogs)
	r.emitStatus(state)
	return nil
}

// Stop 优雅停止核心进程。
func (r *CoreRuntime) Stop() error {
	r.mu.Lock()
	cmd := r.cmd
	cancel := r.cancelMonitor
	pid := r.managedPID
	attached := r.attached
	r.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	} else if attached && pid > 0 {
		process, err := os.FindProcess(pid)
		if err != nil {
			return fmt.Errorf("查找核心进程失败: %w", err)
		}
		_ = process.Kill()
	}
	r.mu.Lock()
	r.cmd = nil
	r.managedPID = 0
	r.attached = false
	r.state.Running = false
	r.state.ManagementHealthy = false
	r.state.ExitedAt = time.Now()
	state := r.state
	r.mu.Unlock()
	r.emitStatus(state)
	return nil
}

// Restart 重启核心进程。
func (r *CoreRuntime) Restart() error {
	configState, err := r.config.LoadConfigState()
	if err != nil {
		return err
	}
	if err := r.Stop(); err != nil {
		return err
	}
	return r.Start(context.Background(), configState)
}

// IsRunning 判断核心是否运行中。
func (r *CoreRuntime) IsRunning() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.state.Running
}

// WaitHealthy 等待管理接口可用。
func (r *CoreRuntime) WaitHealthy(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		state := r.State()
		if state.ManagementHealthy {
			return nil
		}
		if !state.Running {
			if state.LastError != "" {
				return fmt.Errorf("核心未运行: %s", state.LastError)
			}
			return fmt.Errorf("核心未运行")
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("等待管理接口超时")
}

// State 返回状态副本。
func (r *CoreRuntime) State() CoreProcessState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.state
}

// pipeLogs 采集 stdout/stderr 日志。
func (r *CoreRuntime) pipeLogs(source string, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		entry := r.logs.Append(source, scanner.Text())
		if entry.Message == "" {
			continue
		}
		r.emitLog(entry)
	}
}

// waitProcess 等待子进程退出。
func (r *CoreRuntime) waitProcess(cmd *exec.Cmd) {
	err := cmd.Wait()
	r.mu.Lock()

	if r.cmd == cmd {
		r.cmd = nil
	}
	if r.managedPID == cmd.Process.Pid {
		r.managedPID = 0
		r.attached = false
	}
	r.state.Running = false
	r.state.ManagementHealthy = false
	r.state.ExitedAt = time.Now()
	if err != nil {
		r.state.LastError = err.Error()
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		r.state.ExitCode = exitErr.ExitCode()
	}
	state := r.state
	r.mu.Unlock()
	r.emitStatus(state)
}

// monitor 周期性检查管理接口和文件日志。
func (r *CoreRuntime) monitor(ctx context.Context, configState ConfigState) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			healthy, err := r.checkManagementHealth(configState)
			r.mu.Lock()
			r.state.ManagementHealthy = healthy
			if err != nil {
				r.state.LastError = err.Error()
			} else {
				r.state.LastError = ""
			}
			state := r.state
			r.mu.Unlock()
			r.emitStatus(state)
			if healthy {
				r.pullManagementLogs(configState)
			}
		}
	}
}

// checkManagementHealth 通过配置接口验证管理 API。
func (r *CoreRuntime) checkManagementHealth(configState ConfigState) (bool, error) {
	_, err := r.fetchManagementConfig(configState)
	if err != nil {
		return false, err
	}
	return true, nil
}

// fetchManagementConfig 读取管理接口配置快照。
func (r *CoreRuntime) fetchManagementConfig(configState ConfigState) (ManagementConfigSnapshot, error) {
	url := fmt.Sprintf("http://%s:%d/v0/management/config", hostOrLocal(configState.Host), configState.Port)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return ManagementConfigSnapshot{}, err
	}
	req.Header.Set("Authorization", "Bearer "+configState.ManagementKey)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ManagementConfigSnapshot{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		message := strings.TrimSpace(string(payload))
		if message != "" {
			return ManagementConfigSnapshot{}, fmt.Errorf("管理接口异常: %s %s", resp.Status, message)
		}
		return ManagementConfigSnapshot{}, fmt.Errorf("管理接口异常: %s", resp.Status)
	}
	var snapshot ManagementConfigSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		return ManagementConfigSnapshot{}, fmt.Errorf("解析管理配置失败: %w", err)
	}
	return snapshot, nil
}

// isManagedConfigSnapshot 判断配置快照是否属于 easy-cpa 托管实例。
func (r *CoreRuntime) isManagedConfigSnapshot(snapshot ManagementConfigSnapshot) bool {
	managedAuthDir := normalizeComparablePath(filepath.Join(r.paths.RootDir, "auth"))
	if managedAuthDir == "" {
		return false
	}
	configuredAuthDir := normalizeComparablePath(snapshot.AuthDir)
	if configuredAuthDir == "" {
		return false
	}
	// auth-dir 命中 easy-cpa 托管目录时，即可认定为本应用遗留实例。
	return configuredAuthDir == managedAuthDir || hasComparablePathPrefix(configuredAuthDir, managedAuthDir)
}

// pullManagementLogs 增量拉取管理日志。
func (r *CoreRuntime) pullManagementLogs(configState ConfigState) {
	url := fmt.Sprintf("http://%s:%d/v0/management/logs?after=%d", hostOrLocal(configState.Host), configState.Port, r.logCursor)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+configState.ManagementKey)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return
	}

	var payload struct {
		Lines           []string `json:"lines"`
		LatestTimestamp int64    `json:"latest-timestamp"`
		LineCount       int      `json:"line-count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return
	}
	if payload.LineCount == r.logLineCount && len(payload.Lines) == 0 {
		return
	}
	r.logLineCount = payload.LineCount
	if payload.LatestTimestamp > 0 {
		r.logCursor = payload.LatestTimestamp
	}
	for _, line := range payload.Lines {
		entry := r.logs.Append("management", line)
		if entry.Message == "" {
			continue
		}
		r.emitLog(entry)
	}
}

// hostOrLocal 统一把空 host 转为本地回环。
func hostOrLocal(host string) string {
	host = strings.TrimSpace(host)
	if host == "" || host == "0.0.0.0" || host == "::" {
		return "127.0.0.1"
	}
	return host
}

// reconcileExistingProcessLocked 处理已有监听进程与遗留托管实例。
func (r *CoreRuntime) reconcileExistingProcessLocked(ctx context.Context, configState ConfigState) ([]string, error) {
	process, err := FindListeningProcess(configState.Port)
	if err != nil {
		return nil, err
	}
	if process.PID == 0 {
		return nil, nil
	}

	var logs []string
	matchedByProcess := process.IsManagedProcess(r.paths.CoreBinaryPath, configState.ConfigPath)
	if !matchedByProcess && normalizeComparablePath(process.ExecutablePath) == normalizeComparablePath(r.paths.CoreBinaryPath) {
		matchedByProcess = true
	}
	snapshot, snapshotErr := r.fetchManagementConfig(configState)
	matchedByConfig := snapshotErr == nil && r.isManagedConfigSnapshot(snapshot)

	// Windows 权限受限时可能读不到路径/命令行，此时回退到管理接口配置指纹判断。
	if !matchedByProcess && matchedByConfig {
		logs = append(logs, fmt.Sprintf("检测到占用端口 %d 的进程 PID=%d 未暴露路径，但其 auth-dir=%s 命中 easy-cpa 托管目录。", configState.Port, process.PID, snapshot.AuthDir))
	}

	// 能确认是 easy-cpa 托管实例且管理接口可用时，优先直接接管，避免 dev 重载反复清理。
	if matchedByProcess || matchedByConfig {
		if snapshotErr == nil {
			r.attachExistingProcessLocked(ctx, process, configState)
			logs = append(logs, fmt.Sprintf("检测到 Easy CPA 遗留核心占用端口 %d，已直接接管进程 PID=%d。", configState.Port, process.PID))
			return logs, nil
		}

		// 已确认是托管实例但接口异常时，再执行强制清理并重启。
		logs = append(logs, fmt.Sprintf("检测到 Easy CPA 遗留核心占用端口 %d，但管理接口异常，正在清理进程 PID=%d。", configState.Port, process.PID))
		if err := terminatePID(process.PID); err != nil {
			return logs, fmt.Errorf("终止遗留核心失败: %w", err)
		}
		if err := waitPortReleased(configState.Port, 5*time.Second); err != nil {
			return logs, fmt.Errorf("等待遗留核心释放端口失败: %w", err)
		}
		logs = append(logs, fmt.Sprintf("Easy CPA 遗留核心已清理，端口 %d 已释放。", configState.Port))
		return logs, nil
	}

	if snapshotErr != nil {
		return logs, fmt.Errorf("端口 %d 已被外部进程占用: PID=%d 路径=%s，且管理接口未通过托管密钥校验: %v", configState.Port, process.PID, process.ExecutablePath, snapshotErr)
	}
	// 即使能通过当前密钥访问，只要 auth-dir 不属于托管目录，也不允许 easy-cpa 清理。
	return logs, fmt.Errorf("端口 %d 已被外部进程占用: PID=%d 路径=%s auth-dir=%s", configState.Port, process.PID, process.ExecutablePath, snapshot.AuthDir)
}

// attachExistingProcessLocked 接管已存在的托管核心进程。
func (r *CoreRuntime) attachExistingProcessLocked(ctx context.Context, process ListeningProcess, configState ConfigState) {
	if r.cancelMonitor != nil {
		r.cancelMonitor()
	}
	monitorCtx, cancel := context.WithCancel(ctx)
	r.cancelMonitor = cancel
	r.cmd = nil
	r.managedPID = process.PID
	r.attached = true
	r.state = CoreProcessState{
		Running:           true,
		PID:               process.PID,
		StartedAt:         process.StartedAt,
		ExitedAt:          time.Time{},
		ExitCode:          0,
		LastError:         "",
		ManagementHealthy: true,
	}
	go r.monitor(monitorCtx, configState)
}

// emitSystemLogs 在锁外补发运行时系统日志。
func (r *CoreRuntime) emitSystemLogs(messages []string) {
	for _, message := range messages {
		if strings.TrimSpace(message) == "" {
			continue
		}
		entry := r.logs.Append("system", message)
		if entry.Message != "" {
			r.emitLog(entry)
		}
	}
}
