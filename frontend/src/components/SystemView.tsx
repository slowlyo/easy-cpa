import {backend} from '../../wailsjs/go/models';
import {formatExitedAt, formatStartedAt, formatTime} from '../utils/appState';

interface SystemViewProps {
  visible: boolean;
  state: backend.BootstrapState;
  busyAction: string;
  networkSettings: backend.NetworkSettings;
  logFilter: string;
  filteredLogs: backend.LogEntry[];
  coreNeedsUpdate: boolean;
  panelNeedsUpdate: boolean;
  onLogFilterChange: (value: string) => void;
  onNetworkProxyEnabledChange: (enabled: boolean) => void;
  onNetworkProxyURLChange: (value: string) => void;
  onStartCore: () => void;
  onStopCore: () => void;
  onRestartCore: () => void;
  onCheckUpdates: () => void;
  onUpdatePanel: () => void;
  onUpdateCore: () => void;
  onSaveNetworkSettings: () => void;
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
  coreNeedsUpdate,
  panelNeedsUpdate,
  onLogFilterChange,
  onNetworkProxyEnabledChange,
  onNetworkProxyURLChange,
  onStartCore,
  onStopCore,
  onRestartCore,
  onCheckUpdates,
  onUpdatePanel,
  onUpdateCore,
  onSaveNetworkSettings,
}: SystemViewProps) {
  return (
    <section className={visible ? 'system-view active' : 'system-view hidden'}>
      <div className="system-layout">
        <section className="system-hero">
          <article className="hero-card hero-card-primary">
            <span className="eyebrow">运行状态</span>
            <strong>{state.coreRunning ? '核心运行中' : (state.coreInstalled ? '核心未运行' : '核心未安装')}</strong>
            <p>{state.process?.lastError || state.bootstrapDetail || '当前托管实例状态正常。'}</p>
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
            <div className="button-row">
              <button disabled={busyAction !== '' || state.coreRunning} onClick={onStartCore}>启动核心</button>
              <button disabled={busyAction !== '' || !state.coreRunning} onClick={onStopCore}>停止核心</button>
              <button disabled={busyAction !== '' || !state.coreInstalled} onClick={onRestartCore}>重启核心</button>
            </div>
            <div className="button-row">
              <button disabled={busyAction !== ''} onClick={onCheckUpdates}>检查更新</button>
              <button disabled={busyAction !== '' || !panelNeedsUpdate} onClick={onUpdatePanel}>更新管理页</button>
              <button disabled={busyAction !== '' || !coreNeedsUpdate} onClick={onUpdateCore}>更新核心</button>
            </div>
            <div className="meta-list">
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
            <div className="meta-list">
              <div><span>下载网络</span><strong>{state.githubNetworkLabel || '自动检测'}</strong></div>
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
