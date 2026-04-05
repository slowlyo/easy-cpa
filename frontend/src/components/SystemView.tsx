import {backend} from '../../wailsjs/go/models';
import {formatExitedAt, formatStartedAt, formatTime} from '../utils/appState';

interface SystemViewProps {
  visible: boolean;
  state: backend.BootstrapState;
  busyAction: string;
  networkSettings: backend.NetworkSettings;
  logFilter: string;
  filteredLogs: backend.LogEntry[];
  appNeedsUpdate: boolean;
  coreNeedsUpdate: boolean;
  panelNeedsUpdate: boolean;
  onLogFilterChange: (value: string) => void;
  onNetworkProxyEnabledChange: (enabled: boolean) => void;
  onNetworkProxyURLChange: (value: string) => void;
  onStartCore: () => void;
  onStopCore: () => void;
  onRestartCore: () => void;
  onUpdateApp: () => void;
  onCheckUpdates: () => void;
  onUpdatePanel: () => void;
  onUpdateCore: () => void;
  onSaveNetworkSettings: () => void;
  onCloseConfirmEnabledChange: (enabled: boolean) => void;
}

const UPDATE_TARGET_LABELS: Record<string, string> = {
  app: '应用更新',
  core: '核心更新',
  panel: '管理页更新',
};

/**
 * formatBytes 将字节数转换成易读文案。
 */
function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) {
    return '-';
  }
  const units = ['B', 'KB', 'MB', 'GB'];
  let size = value;
  let unitIndex = 0;
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex += 1;
  }
  return `${size >= 10 || unitIndex === 0 ? size.toFixed(0) : size.toFixed(1)} ${units[unitIndex]}`;
}

/**
 * resolveUpdateTargetLabel 返回当前更新任务的标题。
 */
function resolveUpdateTargetLabel(target: string): string {
  return UPDATE_TARGET_LABELS[target] || '更新中心';
}

/**
 * SystemView 渲染系统页信息与操作面板。
 */
export function SystemView({
  visible,
  state,
  busyAction,
  networkSettings,
  logFilter,
  filteredLogs,
  appNeedsUpdate,
  coreNeedsUpdate,
  panelNeedsUpdate,
  onLogFilterChange,
  onNetworkProxyEnabledChange,
  onNetworkProxyURLChange,
  onStartCore,
  onStopCore,
  onRestartCore,
  onUpdateApp,
  onCheckUpdates,
  onUpdatePanel,
  onUpdateCore,
  onSaveNetworkSettings,
  onCloseConfirmEnabledChange,
}: SystemViewProps) {
  const runtimeSummary = state.bootstrapDetail || (state.coreRunning
    ? '当前托管实例状态正常。'
    : (state.coreInstalled ? '当前核心已安装但未运行。' : '当前尚未安装核心。'));
  const updateProgress = state.updateProgress;
  const updateBusy = busyAction === 'app' || busyAction === 'core' || busyAction === 'panel';
  const updateFailed = updateProgress?.stage === '更新失败';
  const showUpdateCenter = updateBusy || Boolean(updateProgress?.active) || updateFailed;
  const updateTitle = resolveUpdateTargetLabel(updateProgress?.target || busyAction);
  const updatePercent = Math.round((updateProgress?.percent || 0) * 100);
  const progressLabel = updateFailed
    ? '失败'
    : updateProgress?.indeterminate
    ? (showUpdateCenter ? '处理中' : '待命')
    : `${updatePercent}%`;
  const bytesLabel = updateProgress?.totalBytes > 0
    ? `${formatBytes(updateProgress.downloadedBytes)} / ${formatBytes(updateProgress.totalBytes)}`
    : (updateProgress?.downloadedBytes > 0 ? formatBytes(updateProgress.downloadedBytes) : '等待下载');
  const updateDetail = showUpdateCenter
    ? (updateProgress?.detail || '正在准备更新任务。')
    : '检查更新后，可在这里看到下载速度、阶段状态和结果。';
  const updateStatusLabel = updateFailed ? '失败' : (updateProgress?.active || updateBusy ? '进行中' : '待命');
  const updateStatusClass = updateFailed ? 'failed' : (updateProgress?.active || updateBusy ? 'running' : 'idle');
  const appButtonLabel = busyAction === 'app' ? '更新应用中' : '更新应用';
  const panelButtonLabel = busyAction === 'panel' ? '更新管理页中' : '更新管理页';
  const coreButtonLabel = busyAction === 'core' ? '更新核心中' : '更新核心';
  const checkButtonLabel = busyAction === 'check' ? '检查中' : '检查更新';

  return (
    <section className={visible ? 'system-view active' : 'system-view hidden'}>
      <div className="system-layout">
        <section className="system-hero">
          <article className="hero-card hero-card-primary">
            <span className="eyebrow">运行状态</span>
            <strong>{state.coreRunning ? '核心运行中' : (state.coreInstalled ? '核心未运行' : '核心未安装')}</strong>
            <p>{runtimeSummary}</p>
          </article>
          <article className="hero-card">
            <span className="eyebrow">本地接口</span>
            <strong>{state.host || '127.0.0.1'}:{state.port || 8317}</strong>
            <p>{state.managementAPIHealthy ? '管理接口已连通。' : '管理接口尚未连通。'}</p>
          </article>
          <article className="hero-card">
            <span className="eyebrow">数据目录</span>
            <strong>{state.dataDir || '-'}</strong>
            <p>用于保存核心、面板、配置和日志。</p>
          </article>
        </section>

        <div className="card-grid">
          <article className="card stat-card">
            <span className="eyebrow">应用版本</span>
            <strong>{state.appVersion || '开发版'}</strong>
            <span>最新：{state.appLatestVersion || '未知'}</span>
          </article>
          <article className="card stat-card">
            <span className="eyebrow">核心状态</span>
            <strong>{state.coreRunning ? '运行中' : (state.coreInstalled ? '已停止' : '未安装')}</strong>
            <span>PID：{state.process?.pid || '-'}</span>
          </article>
          <article className="card stat-card">
            <span className="eyebrow">管理接口</span>
            <strong>{state.managementAPIHealthy ? '健康' : '未连通'}</strong>
            <span>{state.host || '127.0.0.1'}:{state.port || 8317}</span>
          </article>
          <article className="card stat-card">
            <span className="eyebrow">核心版本</span>
            <strong>{state.coreVersion || '未记录'}</strong>
            <span>最新：{state.coreLatestVersion || '未知'}</span>
          </article>
          <article className="card stat-card">
            <span className="eyebrow">管理页版本</span>
            <strong>{state.panelVersion || '未记录'}</strong>
            <span>最新：{state.panelLatestVersion || '未知'}</span>
          </article>
        </div>

        <div className="system-columns">
          <article className="card">
            <div className="card-header">
              <h2>运维操作</h2>
              <span>核心与管理页</span>
            </div>
            <div className="button-row action-grid">
              <button className="action-button action-button-neutral" disabled={busyAction !== '' || state.coreRunning} onClick={onStartCore}>
                <span>启动核心</span>
                <small>拉起当前托管实例</small>
              </button>
              <button className="action-button action-button-neutral" disabled={busyAction !== '' || !state.coreRunning} onClick={onStopCore}>
                <span>停止核心</span>
                <small>安全停止当前进程</small>
              </button>
              <button className="action-button action-button-neutral" disabled={busyAction !== '' || !state.coreInstalled} onClick={onRestartCore}>
                <span>重启核心</span>
                <small>重新加载本地配置</small>
              </button>
            </div>
            {showUpdateCenter ? (
              <div className="update-center">
                <div className="update-center-head">
                  <div>
                    <span className="eyebrow">更新中心</span>
                    <strong>{updateTitle}</strong>
                  </div>
                  <span className={`update-status-pill ${updateStatusClass}`}>{updateStatusLabel}</span>
                </div>
                <p>{updateDetail}</p>
                <div className="progress-track" aria-hidden="true">
                  <div
                    className={updateProgress?.indeterminate ? 'progress-bar indeterminate' : 'progress-bar'}
                    style={updateProgress?.indeterminate ? undefined : {width: `${Math.max(updatePercent, updatePercent > 0 ? 6 : 0)}%`}}
                  />
                </div>
                <div className="update-center-meta">
                  <span>{updateProgress?.stage || '等待操作'}</span>
                  <strong>{progressLabel}</strong>
                  <span>{bytesLabel}</span>
                </div>
              </div>
            ) : null}
            <div className="button-row action-grid update-action-grid">
              <button className="action-button action-button-primary" disabled={busyAction !== '' || !appNeedsUpdate} onClick={onUpdateApp}>
                <span>{appButtonLabel}</span>
                <small>{state.appLatestVersion || '无可用版本'}</small>
              </button>
              <button className="action-button action-button-secondary" disabled={busyAction !== '' || !coreNeedsUpdate} onClick={onUpdateCore}>
                <span>{coreButtonLabel}</span>
                <small>{state.coreLatestVersion || '无可用版本'}</small>
              </button>
              <button className="action-button action-button-secondary" disabled={busyAction !== '' || !panelNeedsUpdate} onClick={onUpdatePanel}>
                <span>{panelButtonLabel}</span>
                <small>{state.panelLatestVersion || '无可用版本'}</small>
              </button>
              <button className="action-button action-button-ghost" disabled={busyAction !== ''} onClick={onCheckUpdates}>
                <span>{checkButtonLabel}</span>
                <small>{state.githubNetworkLabel || '自动检测'}</small>
              </button>
            </div>
            <div className="meta-list">
              <div><span>应用更新提示</span><strong>{appNeedsUpdate ? '可更新' : (state.appVersion === 'dev' ? '开发版' : '已最新或未知')}</strong></div>
              <div><span>核心更新提示</span><strong>{coreNeedsUpdate ? '可更新' : '已最新或未知'}</strong></div>
              <div><span>面板更新提示</span><strong>{panelNeedsUpdate ? '可更新' : '已最新或未知'}</strong></div>
              <div><span>最近启动时间</span><strong>{formatStartedAt(state.process)}</strong></div>
              <div><span>最近退出时间</span><strong>{formatExitedAt(state.process)}</strong></div>
              <div><span>退出码</span><strong>{state.process?.exitCode ?? '-'}</strong></div>
              <div><span>最近错误</span><strong>{state.process?.lastError || state.lastError || '-'}</strong></div>
            </div>
          </article>

          <article className="card">
            <div className="card-header">
              <h2>GitHub 网络设置</h2>
              <span>只影响 easy-cpa 自身下载</span>
            </div>
            <label className="switch-row">
              <input
                type="checkbox"
                checked={networkSettings.githubProxyEnabled}
                onChange={(event) => onNetworkProxyEnabledChange(event.target.checked)}
              />
              <span>启用自定义代理</span>
            </label>
            <input
              className="text-input"
              placeholder="http://127.0.0.1:7890"
              value={networkSettings.githubProxyURL}
              onChange={(event) => onNetworkProxyURLChange(event.target.value)}
            />
            <div className="network-hint">
              未设置自定义代理时，程序会自动按直连、7890、7897 顺序检测可用网络。
            </div>
            <div className="button-row single">
              <button
                disabled={busyAction !== ''}
                onClick={onSaveNetworkSettings}
              >
                保存代理配置
              </button>
            </div>
            <label className="switch-row">
              <input
                type="checkbox"
                checked={state.closeConfirmEnabled}
                disabled={busyAction !== ''}
                onChange={(event) => onCloseConfirmEnabledChange(event.target.checked)}
              />
              <span>关闭窗口前弹出确认</span>
            </label>
            <div className="meta-list">
              <div><span>下载网络</span><strong>{state.githubNetworkLabel || '自动检测'}</strong></div>
              <div><span>关闭窗口确认</span><strong>{state.closeConfirmEnabled ? '已开启' : '已关闭'}</strong></div>
            </div>
          </article>
        </div>

        <article className="card log-card">
          <div className="card-header">
            <h2>日志输出</h2>
            <span>合并 stdout / stderr / management</span>
          </div>
          <div className="toolbar">
            <input
              className="text-input"
              placeholder="筛选日志关键字"
              value={logFilter}
              onChange={(event) => onLogFilterChange(event.target.value)}
            />
            <span className="toolbar-meta">共 {filteredLogs.length} 条</span>
          </div>
          <div className="log-list">
            {filteredLogs.length > 0 ? filteredLogs.map((item, index) => (
              <div className="log-item" key={`${item.timestamp}-${index}`}>
                <span className={`log-source ${item.source}`}>{item.source}</span>
                <span className="log-time">{formatTime(item.timestamp)}</span>
                <pre>{item.message}</pre>
              </div>
            )) : (
              <div className="empty-log">暂无日志</div>
            )}
          </div>
        </article>
      </div>
    </section>
  );
}
