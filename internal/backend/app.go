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
	latestCheckInterval = 30 * time.Minute
	stateBootstrapIdle  = "idle"
	stateBootstrapRun   = "running"
	stateBootstrapReady = "ready"
	stateBootstrapError = "error"
	closeButtonConfirm  = "关闭应用"
	closeButtonSilence  = "关闭并不再询问"
	closeButtonCancel   = "取消"
)

// App 负责聚合 easy-cpa 的全部运行状态。
type App struct {
	ctx              context.Context
	cancel           context.CancelFunc
	paths            ManagedPaths
	settings         *SettingsStore
	proxy            *ProxyManager
	release          *ReleaseManager
	config           *ConfigManager
	logs             *LogBuffer
	panel            *PanelManager
	runtime          *CoreRuntime
	mu               sync.RWMutex
	releaseRefreshMu sync.Mutex
	state            BootstrapState
	bootOnce         sync.Once
	bootRunning      bool
	closingForUpdate bool
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
			BootstrapPhase:      stateBootstrapIdle,
			BootstrapStep:       "等待启动",
			BootstrapDetail:     "应用尚未开始初始化。",
			AppVersion:          CurrentAppVersion(),
			GithubProxyMode:     proxy.CurrentMode(),
			GithubNetworkLabel:  proxy.CurrentLabel(),
			BootstrapHistory:    []BootstrapProgress{},
			RecentLogs:          []LogEntry{},
			Port:                defaultPort,
			DataDir:             paths.RootDir,
			NetworkSettings:     settings.NetworkSettings(),
			CloseConfirmEnabled: settings.CloseConfirmEnabled(),
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
		go a.watchLatestReleases()
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
	a.mu.RLock()
	closingForUpdate := a.closingForUpdate
	a.mu.RUnlock()
	// 更新流程主动退出时直接放行，避免再次打断。
	if closingForUpdate {
		return false
	}
	// 核心未运行时直接允许关闭，避免无意义打断。
	if !a.runtime.IsRunning() {
		return false
	}
	// 用户关闭了确认提示时直接放行，同时维持核心随应用退出。
	if !a.settings.CloseConfirmEnabled() {
		return false
	}
	result, err := wruntime.MessageDialog(ctx, wruntime.MessageDialogOptions{
		Type:          wruntime.QuestionDialog,
		Title:         "确认关闭 Easy CPA",
		Message:       "关闭应用后，当前托管的 CPA 核心也会停止运行。确定继续关闭吗？",
		Buttons:       []string{closeButtonConfirm, closeButtonSilence, closeButtonCancel},
		DefaultButton: closeButtonCancel,
		CancelButton:  closeButtonCancel,
	})
	// 弹窗失败时默认阻止关闭，避免用户在无感知时误停核心。
	if err != nil {
		entry := a.logs.Append("system", fmt.Sprintf("关闭确认弹窗失败: %v", err))
		if entry.Message != "" {
			a.emitCoreLog(entry)
		}
		return true
	}
	// 选择“不再询问”时立即持久化，后续关闭窗口不再弹窗。
	if shouldDisableCloseConfirm(result) {
		if err := a.settings.SaveCloseConfirmEnabled(false); err != nil {
			entry := a.logs.Append("system", fmt.Sprintf("保存关闭确认设置失败: %v", err))
			if entry.Message != "" {
				a.emitCoreLog(entry)
			}
		}
		a.refreshState(false)
	}
	return !isCloseConfirmed(result)
}

// isCloseConfirmed 统一判断关闭确认弹窗的肯定结果。
func isCloseConfirmed(result string) bool {
	switch strings.ToLower(strings.TrimSpace(result)) {
	case "yes", "ok", "关闭应用", "确认", "是", "关闭并不再询问":
		return true
	default:
		return false
	}
}

// shouldDisableCloseConfirm 判断本次关闭是否要求后续不再询问。
func shouldDisableCloseConfirm(result string) bool {
	return strings.TrimSpace(result) == closeButtonSilence
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

// watchLatestReleases 定时静默刷新应用、核心与管理页的最新发布信息。
func (a *App) watchLatestReleases() {
	ticker := time.NewTicker(latestCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			// 引导仍在执行时跳过本轮，避免后台检查打断启动态展示。
			if a.isBootRunning() {
				continue
			}
			if err := a.refreshLatestReleasesQuietly(); err != nil {
				a.setSoftError(err)
			}
		}
	}
}

// UpdateApp 下载最新应用并在退出后自动替换重启。
func (a *App) UpdateApp() (BootstrapState, error) {
	if a.ctx == nil {
		err := errors.New("应用上下文未初始化")
		a.setLastError(err)
		return a.snapshotState(), err
	}
	meta, err := a.release.FetchLatestAppRelease(a.ctx)
	if err != nil {
		a.setLastError(err)
		return a.snapshotState(), err
	}
	if meta.Tag == "" {
		err := errors.New("未获取到应用发布信息")
		a.setLastError(err)
		return a.snapshotState(), err
	}
	currentVersion := CurrentAppVersion()
	if currentVersion != "dev" && CompareReleaseTags(currentVersion, meta.Tag) >= 0 {
		a.finishUpdateProgress("app", "无需更新应用", fmt.Sprintf("当前已是最新版本 %s。", currentVersion))
		a.clearUpdateProgress()
		a.refreshState(false)
		return a.snapshotState(), nil
	}
	a.emitUpdateProgress("app", "准备应用更新", fmt.Sprintf("正在下载 Easy CPA %s 并准备重启更新。", meta.Tag), 0, 0)
	if err := a.prepareSelfUpdate(a.ctx, meta); err != nil {
		a.setLastError(err)
		return a.snapshotState(), err
	}
	a.mu.Lock()
	a.closingForUpdate = true
	a.state.LastError = ""
	a.state.BootstrapPhase = stateBootstrapRun
	a.state.BootstrapStep = "应用更新中"
	a.state.BootstrapDetail = fmt.Sprintf("已下载 %s，正在退出并自动重启完成更新。", meta.Tag)
	a.state.BootstrapUpdatedAt = time.Now()
	completed := a.state.UpdateProgress
	completed.Active = false
	completed.Target = "app"
	completed.Stage = "应用更新中"
	completed.Detail = a.state.BootstrapDetail
	completed.Indeterminate = false
	if completed.TotalBytes > 0 {
		completed.DownloadedBytes = completed.TotalBytes
	}
	completed.Percent = 1
	a.state.UpdateProgress = completed
	a.appendBootstrapHistoryLocked("应用更新中", a.state.BootstrapDetail)
	a.mu.Unlock()
	go func() {
		time.Sleep(300 * time.Millisecond)
		if a.ctx != nil {
			wruntime.Quit(a.ctx)
		}
	}()
	return a.snapshotState(), nil
}

// UpdatePanel 手动更新官方管理页。
func (a *App) UpdatePanel() (BootstrapState, error) {
	meta, err := a.release.FetchLatestPanelRelease(a.ctx)
	if err != nil {
		a.setLastError(err)
		return a.snapshotState(), err
	}
	local := ReadReleaseMetaFile(a.paths.PanelMetaPath)
	// 本地版本已匹配时直接返回，避免重复下载同一份管理页。
	if local.Tag == meta.Tag && FileExists(a.paths.PanelHTMLPath) {
		a.finishUpdateProgress("panel", "管理页已最新", fmt.Sprintf("当前已缓存管理页 %s。", meta.Tag))
		a.clearUpdateProgress()
		a.refreshState(false)
		return a.snapshotState(), nil
	}
	a.emitUpdateProgress("panel", "准备更新管理页", fmt.Sprintf("正在下载管理页 %s。", meta.Tag), 0, 0)
	if err := a.panel.Install(a.ctx, meta, func(progress DownloadProgress) {
		a.emitUpdateProgress("panel", "下载管理页", fmt.Sprintf("正在下载管理页 %s。", meta.Tag), progress.DownloadedBytes, progress.TotalBytes)
	}); err != nil {
		a.setLastError(err)
		return a.snapshotState(), err
	}
	a.finishUpdateProgress("panel", "管理页更新完成", fmt.Sprintf("管理页已更新到 %s。", meta.Tag))
	a.clearUpdateProgress()
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
	local := ReadReleaseMetaFile(a.paths.CoreMetaPath)
	// 本地核心已是目标版本时不再重复安装，避免无意义重启。
	if local.Tag == meta.Tag && FileExists(a.paths.CoreBinaryPath) {
		a.finishUpdateProgress("core", "核心已最新", fmt.Sprintf("当前核心已是 %s。", meta.Tag))
		a.clearUpdateProgress()
		a.refreshState(false)
		return a.snapshotState(), nil
	}
	a.emitUpdateProgress("core", "准备更新核心", fmt.Sprintf("正在准备更新 CLIProxyAPI %s。", meta.Tag), 0, 0)

	wasRunning := a.runtime.IsRunning()
	if wasRunning {
		a.emitUpdateProgress("core", "停止核心进程", "检测到核心正在运行，先停止旧进程。", 0, 0)
		if err := a.runtime.Stop(); err != nil {
			a.setLastError(err)
			return a.snapshotState(), err
		}
	}

	if err := a.release.InstallCoreRelease(a.ctx, meta, a.paths, func(progress DownloadProgress) {
		a.emitUpdateProgress("core", "下载核心更新", fmt.Sprintf("正在下载 CLIProxyAPI %s。", meta.Tag), progress.DownloadedBytes, progress.TotalBytes)
	}); err != nil {
		a.setLastError(err)
		return a.snapshotState(), err
	}

	if wasRunning {
		a.emitUpdateProgress("core", "重启核心进程", "新版本已安装，正在重新启动核心。", 0, 0)
		if err := a.ensureConfigAndRuntime(false); err != nil {
			a.setLastError(err)
			return a.snapshotState(), err
		}
	}

	a.finishUpdateProgress("core", "核心更新完成", fmt.Sprintf("核心已更新到 %s。", meta.Tag))
	a.clearUpdateProgress()
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

// SaveCloseConfirmEnabled 保存关闭窗口确认开关。
func (a *App) SaveCloseConfirmEnabled(enabled bool) (BootstrapState, error) {
	if err := a.settings.SaveCloseConfirmEnabled(enabled); err != nil {
		a.setLastError(err)
		return a.snapshotState(), err
	}
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
	return a.panel.Install(a.ctx, latest, nil)
}

// ensureCoreBinary 确保核心二进制存在。
func (a *App) ensureCoreBinary() error {
	// 已存在核心文件时直接复用，避免首次引导后重复下载。
	if FileExists(a.paths.CoreBinaryPath) {
		return nil
	}
	latest := a.release.LatestCore()
	if latest.Tag == "" {
		return errors.New("无法获取核心发布信息")
	}
	a.emitUpdateProgress("core", "准备安装核心", fmt.Sprintf("检测到本机尚未缓存 CLIProxyAPI，正在准备下载 %s。", latest.Tag), 0, 0)
	if err := a.release.InstallCoreRelease(a.ctx, latest, a.paths, func(progress DownloadProgress) {
		a.emitUpdateProgress("core", "下载核心", fmt.Sprintf("首次启动正在下载 CLIProxyAPI %s。", latest.Tag), progress.DownloadedBytes, progress.TotalBytes)
	}); err != nil {
		return err
	}
	a.finishUpdateProgress("core", "核心安装完成", fmt.Sprintf("CLIProxyAPI %s 已下载完成，正在继续初始化。", latest.Tag))
	return nil
}

// refreshLatestReleases 刷新应用、核心与管理页发布信息。
func (a *App) refreshLatestReleases() error {
	return a.refreshLatestReleasesWithMode(true)
}

// refreshLatestReleasesQuietly 静默刷新发布信息，供后台定时检查使用。
func (a *App) refreshLatestReleasesQuietly() error {
	return a.refreshLatestReleasesWithMode(false)
}

// refreshLatestReleasesWithMode 按指定模式刷新应用、核心与管理页发布信息。
func (a *App) refreshLatestReleasesWithMode(reportProgress bool) error {
	if a.ctx == nil {
		return errors.New("应用上下文未初始化")
	}

	a.releaseRefreshMu.Lock()
	defer a.releaseRefreshMu.Unlock()

	var errs []error
	// 只有显式检查或启动引导时才更新进度文案，后台轮询保持静默。
	if reportProgress {
		a.emitBootstrapProgress("检查核心版本", "正在读取 CLIProxyAPI latest release。")
	}
	if _, err := a.release.FetchLatestCoreRelease(a.ctx); err != nil {
		errs = append(errs, fmt.Errorf("获取核心发布失败: %w", err))
	}
	// 管理页版本与核心版本分开请求，便于前端分别提示更新。
	if reportProgress {
		a.emitBootstrapProgress("检查管理页版本", "正在读取管理界面 latest release。")
	}
	if _, err := a.release.FetchLatestPanelRelease(a.ctx); err != nil {
		errs = append(errs, fmt.Errorf("获取管理页发布失败: %w", err))
	}
	// 应用自身更新失败不阻断核心使用，只记录为软错误。
	if reportProgress {
		a.emitBootstrapProgress("检查应用版本", "正在读取 Easy CPA latest release。")
	}
	if _, err := a.release.FetchLatestAppRelease(a.ctx); err != nil {
		a.setSoftError(fmt.Errorf("获取应用发布失败: %w", err))
	}
	a.emitNetworkStatus()
	a.refreshState(false)

	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// isBootRunning 返回当前是否仍在执行启动引导。
func (a *App) isBootRunning() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.bootRunning
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
	a.state.AppVersion = CurrentAppVersion()
	a.state.AppLatestVersion = a.release.LatestApp().Tag
	a.state.AppNeedsUpdate = a.state.AppLatestVersion != "" && a.state.AppVersion != "dev" && CompareReleaseTags(a.state.AppVersion, a.state.AppLatestVersion) < 0
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
	a.state.CloseConfirmEnabled = a.settings.CloseConfirmEnabled()
	lastError := strings.TrimSpace(runtimeState.LastError)
	if lastError == "" {
		lastError = strings.TrimSpace(a.release.LastError())
	}
	// 核心已恢复健康或引导完成时，主动清空历史错误，避免前端长期显示陈旧异常。
	if markReady || runtimeState.ManagementHealthy {
		a.state.LastError = ""
	} else if lastError != "" {
		a.state.LastError = lastError
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
	// 更新过程报错时保留失败现场，便于前端展示终态。
	if a.state.UpdateProgress.Active {
		a.state.UpdateProgress.Active = false
		a.state.UpdateProgress.Stage = "更新失败"
		a.state.UpdateProgress.Detail = err.Error()
		a.state.UpdateProgress.Indeterminate = true
	}
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
	a.emitBootstrapEvent(stage, detail)
}

// appendBootstrapHistoryLocked 记录最近的引导步骤。
func (a *App) appendBootstrapHistoryLocked(stage, detail string) {
	now := time.Now()
	// 同阶段进度只更新最后一条，避免下载过程把历史刷满。
	if size := len(a.state.BootstrapHistory); size > 0 {
		last := &a.state.BootstrapHistory[size-1]
		if last.Stage == stage {
			last.Detail = detail
			last.Timestamp = now
			return
		}
	}
	entry := BootstrapProgress{
		Stage:     stage,
		Detail:    detail,
		Timestamp: now,
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
