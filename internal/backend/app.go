package backend

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	defaultPort         = 8317
	stateBootstrapIdle  = "idle"
	stateBootstrapRun   = "running"
	stateBootstrapReady = "ready"
	stateBootstrapError = "error"
)

// App 负责聚合 easy-cpa 的全部运行状态。
type App struct {
	ctx         context.Context
	cancel      context.CancelFunc
	paths       ManagedPaths
	settings    *SettingsStore
	proxy       *ProxyManager
	release     *ReleaseManager
	config      *ConfigManager
	logs        *LogBuffer
	panel       *PanelManager
	runtime     *CoreRuntime
	mu          sync.RWMutex
	state       BootstrapState
	bootOnce    sync.Once
	bootRunning bool
}

// NewApp 创建应用主实例。
func NewApp() *App {
	paths, err := ResolveManagedPaths()
	if err != nil {
		panic(err)
	}

	settings := NewSettingsStore(paths.SettingsPath)
	logs := NewLogBuffer(400)
	proxy := NewProxyManager(settings, logs)
	release := NewReleaseManager(proxy)
	config := NewConfigManager(paths, settings)
	panel := NewPanelManager(paths, proxy, logs)

	app := &App{
		paths:    paths,
		settings: settings,
		proxy:    proxy,
		release:  release,
		config:   config,
		logs:     logs,
		panel:    panel,
		state: BootstrapState{
			BootstrapPhase:     stateBootstrapIdle,
			BootstrapStep:      "等待启动",
			BootstrapDetail:    "应用尚未开始初始化。",
			GithubProxyMode:    proxy.CurrentMode(),
			GithubNetworkLabel: proxy.CurrentLabel(),
			BootstrapHistory:   []BootstrapProgress{},
			RecentLogs:         []LogEntry{},
			Port:               defaultPort,
			DataDir:            paths.RootDir,
			NetworkSettings:    settings.NetworkSettings(),
		},
	}
	app.runtime = NewCoreRuntime(paths, config, settings, logs, app.emitCoreLog, app.emitCoreStatus)
	return app
}

// Startup 在 Wails 启动后初始化后台服务。
func (a *App) Startup(ctx context.Context) {
	a.ctx, a.cancel = context.WithCancel(ctx)
	a.bootOnce.Do(func() {
		go a.bootstrap()
	})
}

// Shutdown 在应用退出时停止后台进程和服务。
func (a *App) Shutdown(ctx context.Context) {
	if a.cancel != nil {
		a.cancel()
	}
	_ = a.runtime.Stop()
	a.panel.Stop()
}

// BeforeClose 在关闭窗口前提醒核心也会一起停止。
func (a *App) BeforeClose(ctx context.Context) (prevent bool) {
	// 核心未运行时直接允许关闭，避免无意义打断。
	if !a.runtime.IsRunning() {
		return false
	}
	result, err := wruntime.MessageDialog(ctx, wruntime.MessageDialogOptions{
		Type:          wruntime.QuestionDialog,
		Title:         "确认关闭 Easy CPA",
		Message:       "关闭应用后，当前托管的 CPA 核心也会停止运行。确定继续关闭吗？",
		DefaultButton: "No",
		CancelButton:  "No",
	})
	// 弹窗失败时默认阻止关闭，避免用户在无感知时误停核心。
	if err != nil {
		entry := a.logs.Append("system", fmt.Sprintf("关闭确认弹窗失败: %v", err))
		if entry.Message != "" {
			a.emitCoreLog(entry)
		}
		return true
	}
	return !isCloseConfirmed(result)
}

// isCloseConfirmed 统一判断关闭确认弹窗的肯定结果。
func isCloseConfirmed(result string) bool {
	switch strings.ToLower(strings.TrimSpace(result)) {
	case "yes", "ok", "关闭应用", "确认", "是":
		return true
	default:
		return false
	}
}

// GetBootstrapState 返回当前聚合状态。
func (a *App) GetBootstrapState() BootstrapState {
	a.refreshState(false)
	return a.snapshotState()
}

// StartCore 手动启动托管核心。
func (a *App) StartCore() (BootstrapState, error) {
	if err := a.ensureConfigAndRuntime(false); err != nil {
		a.setLastError(err)
		return a.snapshotState(), err
	}
	a.refreshState(false)
	return a.snapshotState(), nil
}

// StopCore 手动停止托管核心。
func (a *App) StopCore() (BootstrapState, error) {
	if err := a.runtime.Stop(); err != nil {
		a.setLastError(err)
		return a.snapshotState(), err
	}
	a.refreshState(false)
	return a.snapshotState(), nil
}

// RestartCore 手动重启托管核心。
func (a *App) RestartCore() (BootstrapState, error) {
	if err := a.runtime.Restart(); err != nil {
		a.setLastError(err)
		return a.snapshotState(), err
	}
	a.refreshState(false)
	return a.snapshotState(), nil
}

// CheckUpdates 主动刷新发布信息。
func (a *App) CheckUpdates() (BootstrapState, error) {
	if err := a.refreshLatestReleases(); err != nil {
		a.setLastError(err)
		return a.snapshotState(), err
	}
	a.refreshState(false)
	return a.snapshotState(), nil
}

// UpdatePanel 手动更新官方管理页。
func (a *App) UpdatePanel() (BootstrapState, error) {
	meta, err := a.release.FetchLatestPanelRelease(a.ctx)
	if err != nil {
		a.setLastError(err)
		return a.snapshotState(), err
	}
	if err := a.panel.Install(a.ctx, meta); err != nil {
		a.setLastError(err)
		return a.snapshotState(), err
	}
	a.refreshState(false)
	return a.snapshotState(), nil
}

// UpdateCore 手动更新核心并在必要时重启。
func (a *App) UpdateCore() (BootstrapState, error) {
	meta, err := a.release.FetchLatestCoreRelease(a.ctx)
	if err != nil {
		a.setLastError(err)
		return a.snapshotState(), err
	}

	wasRunning := a.runtime.IsRunning()
	if wasRunning {
		if err := a.runtime.Stop(); err != nil {
			a.setLastError(err)
			return a.snapshotState(), err
		}
	}

	if err := a.release.InstallCoreRelease(a.ctx, meta, a.paths); err != nil {
		a.setLastError(err)
		return a.snapshotState(), err
	}

	if wasRunning {
		if err := a.ensureConfigAndRuntime(false); err != nil {
			a.setLastError(err)
			return a.snapshotState(), err
		}
	}

	a.refreshState(false)
	return a.snapshotState(), nil
}

// GetPanelURL 返回内嵌管理页入口地址。
func (a *App) GetPanelURL() string {
	if err := a.ensurePanelServer(); err != nil {
		a.setLastError(err)
		return ""
	}
	a.refreshState(false)
	return a.snapshotState().PanelURL
}

// GetRecentLogs 返回最近采集到的日志。
func (a *App) GetRecentLogs() []LogEntry {
	return a.logs.List()
}

// SaveNetworkSettings 保存 GitHub 网络设置。
func (a *App) SaveNetworkSettings(settings NetworkSettings) (BootstrapState, error) {
	if err := a.settings.SaveNetworkSettings(settings); err != nil {
		a.setLastError(err)
		return a.snapshotState(), err
	}
	a.proxy.Reset()
	a.emitNetworkStatus()
	a.refreshState(false)
	return a.snapshotState(), nil
}

// OpenDataDir 在系统文件管理器中打开托管目录。
func (a *App) OpenDataDir() error {
	path := a.paths.RootDir
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer.exe", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		a.setLastError(err)
		return fmt.Errorf("打开数据目录失败: %w", err)
	}
	return nil
}

// bootstrap 执行首次托管引导。
func (a *App) bootstrap() {
	a.mu.Lock()
	if a.bootRunning {
		a.mu.Unlock()
		return
	}
	a.bootRunning = true
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.bootRunning = false
		a.mu.Unlock()
	}()

	if err := a.settings.Load(); err != nil {
		a.setLastError(err)
		return
	}
	if _, err := a.settings.EnsureManagementKey(); err != nil {
		a.setLastError(err)
		return
	}
	if err := EnsureManagedDirectories(a.paths); err != nil {
		a.setLastError(err)
		return
	}
	if err := a.ensurePanelServer(); err != nil {
		a.setLastError(err)
		return
	}

	a.emitBootstrapProgress("读取设置", "正在加载本地设置与托管目录。")
	if err := a.refreshLatestReleases(); err != nil {
		// 这里只在本地完全没有资源时中断启动。
		if !a.release.HasAnyLocalAsset(a.paths) {
			a.setLastError(err)
			return
		}
		a.setSoftError(err)
	}

	a.emitBootstrapProgress("检查管理界面", "正在确认官方 management.html 是否已缓存。")
	if err := a.ensurePanelAsset(); err != nil {
		a.setLastError(err)
		return
	}

	a.emitBootstrapProgress("检查核心", "正在确认 CPA 核心二进制是否存在。")
	if err := a.ensureCoreBinary(); err != nil {
		a.setLastError(err)
		return
	}

	a.emitBootstrapProgress("写入配置", "正在补齐本地托管配置与管理密钥。")
	if _, err := a.config.EnsureConfig(); err != nil {
		a.setLastError(err)
		return
	}

	a.emitBootstrapProgress("启动核心", "正在拉起托管的 CLIProxyAPI 进程。")
	if err := a.ensureConfigAndRuntime(true); err != nil {
		a.setLastError(err)
		return
	}

	a.emitBootstrapProgress("准备完成", "核心与管理页均已就绪。")
	a.refreshState(true)
}

// ensureConfigAndRuntime 确保配置可用并拉起核心。
func (a *App) ensureConfigAndRuntime(waitHealth bool) error {
	a.emitBootstrapProgress("校验配置", "正在读取当前托管配置。")
	configState, err := a.config.EnsureConfig()
	if err != nil {
		return err
	}
	a.emitBootstrapProgress("同步管理页入口", "正在更新内嵌管理页的连接参数。")
	if err := a.ensurePanelServer(); err != nil {
		return err
	}
	a.emitBootstrapProgress("启动核心进程", "正在启动 CLIProxyAPI 子进程。")
	if err := a.runtime.Start(a.ctx, configState); err != nil {
		return err
	}
	if waitHealth {
		a.emitBootstrapProgress("等待管理接口", "正在等待 /v0/management/config 返回健康响应。")
		if err := a.runtime.WaitHealthy(25 * time.Second); err != nil {
			return err
		}
		a.emitBootstrapProgress("管理接口已连接", "已确认管理 API 可用，正在进入管理页。")
	}
	return nil
}

// ensurePanelServer 确保本地静态服务已启动。
func (a *App) ensurePanelServer() error {
	apiBase, managementKey := a.currentPanelBootstrap()
	return a.panel.Start(apiBase, managementKey)
}

// ensurePanelAsset 确保本地管理页为最新版本。
func (a *App) ensurePanelAsset() error {
	latest := a.release.LatestPanel()
	local := ReadReleaseMetaFile(a.paths.PanelMetaPath)
	if latest.Tag == "" {
		if local.Tag != "" || FileExists(a.paths.PanelHTMLPath) {
			return nil
		}
		return errors.New("无法获取管理页发布信息")
	}
	if local.Tag == latest.Tag && FileExists(a.paths.PanelHTMLPath) {
		return nil
	}
	return a.panel.Install(a.ctx, latest)
}

// ensureCoreBinary 确保核心二进制存在。
func (a *App) ensureCoreBinary() error {
	if FileExists(a.paths.CoreBinaryPath) {
		return nil
	}
	latest := a.release.LatestCore()
	if latest.Tag == "" {
		return errors.New("无法获取核心发布信息")
	}
	return a.release.InstallCoreRelease(a.ctx, latest, a.paths)
}

// refreshLatestReleases 刷新核心和面板发布信息。
func (a *App) refreshLatestReleases() error {
	if a.ctx == nil {
		return errors.New("应用上下文未初始化")
	}

	var errs []error
	a.emitBootstrapProgress("检查核心版本", "正在读取 CLIProxyAPI latest release。")
	if _, err := a.release.FetchLatestCoreRelease(a.ctx); err != nil {
		errs = append(errs, fmt.Errorf("获取核心发布失败: %w", err))
	}
	a.emitBootstrapProgress("检查管理页版本", "正在读取管理界面 latest release。")
	if _, err := a.release.FetchLatestPanelRelease(a.ctx); err != nil {
		errs = append(errs, fmt.Errorf("获取管理页发布失败: %w", err))
	}
	a.emitNetworkStatus()
	a.refreshState(false)

	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// refreshState 从各模块回填聚合状态。
func (a *App) refreshState(markReady bool) {
	localCore := ReadReleaseMetaFile(a.paths.CoreMetaPath)
	localPanel := ReadReleaseMetaFile(a.paths.PanelMetaPath)
	configState, _ := a.config.LoadConfigState()
	runtimeState := a.runtime.State()

	a.mu.Lock()
	defer a.mu.Unlock()

	if markReady {
		a.state.BootstrapPhase = stateBootstrapReady
	} else if a.state.BootstrapPhase == "" || a.state.BootstrapPhase == stateBootstrapIdle {
		a.state.BootstrapPhase = stateBootstrapRun
	}

	a.state.CoreInstalled = FileExists(a.paths.CoreBinaryPath)
	a.state.CoreRunning = runtimeState.Running
	a.state.CoreVersion = localCore.Tag
	a.state.CoreLatestVersion = a.release.LatestCore().Tag
	a.state.PanelInstalled = FileExists(a.paths.PanelHTMLPath)
	a.state.PanelVersion = localPanel.Tag
	a.state.PanelLatestVersion = a.release.LatestPanel().Tag
	a.state.PanelURL = a.panel.URL()
	a.state.ManagementAPIHealthy = runtimeState.ManagementHealthy
	a.state.GithubProxyMode = a.proxy.CurrentMode()
	a.state.GithubNetworkLabel = a.proxy.CurrentLabel()
	a.state.NetworkSettings = a.settings.NetworkSettings()
	lastError := runtimeState.LastError
	if lastError == "" {
		lastError = a.state.LastError
	}
	a.state.LastError = lastError
	if a.state.LastError == "" {
		a.state.LastError = a.release.LastError()
	}
	a.state.RecentLogs = a.logs.List()
	a.state.Port = configState.Port
	a.state.Host = configState.Host
	a.state.DataDir = a.paths.RootDir
	a.state.Process = runtimeState
}

// snapshotState 返回一份线程安全的状态副本。
func (a *App) snapshotState() BootstrapState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	state := a.state
	state.RecentLogs = append([]LogEntry(nil), state.RecentLogs...)
	return state
}

// setLastError 记录失败状态并广播。
func (a *App) setLastError(err error) {
	if err == nil {
		return
	}
	a.mu.Lock()
	a.state.BootstrapPhase = stateBootstrapError
	a.state.BootstrapStep = "引导失败"
	a.state.BootstrapDetail = err.Error()
	a.state.BootstrapUpdatedAt = time.Now()
	a.state.LastError = err.Error()
	a.appendBootstrapHistoryLocked("引导失败", err.Error())
	a.mu.Unlock()
	entry := a.logs.Append("system", err.Error())
	a.emitCoreLog(entry)
	a.emitBootstrapProgress("失败", err.Error())
}

// setSoftError 记录非致命错误。
func (a *App) setSoftError(err error) {
	if err == nil {
		return
	}
	entry := a.logs.Append("system", err.Error())
	a.emitCoreLog(entry)
	a.mu.Lock()
	a.state.LastError = err.Error()
	a.mu.Unlock()
}

// emitBootstrapProgress 向前端广播引导阶段。
func (a *App) emitBootstrapProgress(stage, detail string) {
	a.mu.Lock()
	now := time.Now()
	if stage == "失败" {
		a.state.BootstrapPhase = stateBootstrapError
	} else if stage == "准备完成" {
		a.state.BootstrapPhase = stateBootstrapReady
	} else {
		a.state.BootstrapPhase = stateBootstrapRun
	}
	a.state.BootstrapStep = stage
	a.state.BootstrapDetail = detail
	a.state.BootstrapUpdatedAt = now
	a.appendBootstrapHistoryLocked(stage, detail)
	a.mu.Unlock()
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "bootstrap:progress", map[string]string{"stage": stage, "detail": detail})
	}
}

// appendBootstrapHistoryLocked 记录最近的引导步骤。
func (a *App) appendBootstrapHistoryLocked(stage, detail string) {
	entry := BootstrapProgress{
		Stage:     stage,
		Detail:    detail,
		Timestamp: time.Now(),
	}
	a.state.BootstrapHistory = append(a.state.BootstrapHistory, entry)
	if len(a.state.BootstrapHistory) > 8 {
		a.state.BootstrapHistory = append([]BootstrapProgress(nil), a.state.BootstrapHistory[len(a.state.BootstrapHistory)-8:]...)
	}
}

// emitCoreStatus 向前端广播核心状态。
func (a *App) emitCoreStatus(state CoreProcessState) {
	a.refreshState(false)
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "core:status", state)
	}
}

// emitCoreLog 向前端广播日志。
func (a *App) emitCoreLog(entry LogEntry) {
	a.refreshState(false)
	if a.ctx != nil && entry.Message != "" {
		wruntime.EventsEmit(a.ctx, "core:log", entry)
	}
}

// emitNetworkStatus 向前端广播网络状态。
func (a *App) emitNetworkStatus() {
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "network:status", map[string]string{"mode": a.proxy.CurrentMode()})
	}
}

// currentPanelBootstrap 返回管理页包装页所需的连接信息。
func (a *App) currentPanelBootstrap() (string, string) {
	configState, err := a.config.LoadConfigState()
	if err != nil {
		return fmt.Sprintf("http://127.0.0.1:%d", defaultPort), a.settings.ManagementKey()
	}
	host := hostOrLocal(configState.Host)
	return fmt.Sprintf("http://%s:%d", host, configState.Port), a.settings.ManagementKey()
}
