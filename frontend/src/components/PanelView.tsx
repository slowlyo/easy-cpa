import {backend} from '../../wailsjs/go/models';
import {formatTime} from '../utils/appState';

interface PanelViewProps {
  visible: boolean;
  state: backend.BootstrapState;
  panelFrameURL: string;
}

/**
 * PanelView 渲染官方管理页与引导占位态。
 */
export function PanelView({visible, state, panelFrameURL}: PanelViewProps) {
  const panelReady = Boolean(state.panelInstalled && panelFrameURL);
  const updateProgress = state.updateProgress;
  const bootstrapHistory = [...(state.bootstrapHistory ?? [])].reverse();
  const panelChecks = [
    {label: '管理页资源', ready: Boolean(state.panelInstalled), detail: state.panelInstalled ? '已缓存' : '未缓存'},
    {label: '本地入口', ready: Boolean(panelFrameURL), detail: panelFrameURL || '尚未生成'},
    {label: '核心进程', ready: Boolean(state.coreRunning), detail: state.coreRunning ? `PID ${state.process?.pid || '-'}` : '未运行'},
    {label: '管理接口', ready: Boolean(state.managementAPIHealthy), detail: state.managementAPIHealthy ? '已连通' : (state.process?.lastError || state.lastError || '等待探测中')},
  ];
  const blockingDetail = panelChecks.find((item) => !item.ready)?.detail || '无';
  const coreInstallPending = !state.coreInstalled;
  const progressPercent = Math.round((updateProgress?.percent || 0) * 100);
  const showCoreDownloadProgress = updateProgress?.target === 'core' && Boolean(updateProgress?.stage || updateProgress?.active);
  const progressLabel = showCoreDownloadProgress
    ? (updateProgress?.indeterminate ? '下载中' : `${progressPercent}%`)
    : '准备中';
  const progressStage = updateProgress?.stage || state.bootstrapStep || '等待启动';
  const progressDetail = updateProgress?.detail || state.bootstrapDetail || '正在等待新的引导信息。';
  const leadTitle = coreInstallPending ? '首次准备核心中' : '管理页准备中';
  const leadDescription = coreInstallPending
    ? '检测到本机尚未缓存核心，正在下载并初始化，请保持网络通畅。'
    : '等待本地入口与核心管理接口就绪后，将自动进入官方管理界面。';

  return (
    <section className={visible ? 'panel-view active' : 'panel-view hidden'}>
      {panelReady ? (
        <>
          <iframe
            title="CPA 管理页"
            className="panel-frame"
            src={panelFrameURL}
          />
          {!state.managementAPIHealthy ? (
            <div className="panel-overlay">
              <div className="panel-overlay-card">
                <strong>{state.bootstrapStep || '管理页准备中'}</strong>
                <span>{state.bootstrapDetail || '正在等待管理接口恢复。'}</span>
              </div>
            </div>
          ) : null}
        </>
      ) : (
        <div className="empty-state">
          <div className="bootstrap-status">
            <div className="bootstrap-hero" aria-live="polite">
              <div className="bootstrap-hero-visual" aria-hidden="true">
                <div className="bootstrap-spinner">
                  <span className="bootstrap-spinner-ring bootstrap-spinner-ring-outer" />
                  <span className="bootstrap-spinner-ring bootstrap-spinner-ring-inner" />
                  <span className="bootstrap-spinner-core" />
                </div>
              </div>
              <div className="bootstrap-hero-copy">
                <span className="eyebrow">准备中</span>
                <h2>{leadTitle}</h2>
                <p>{leadDescription}</p>
                <div className="bootstrap-progress-panel">
                  <div className="bootstrap-progress-head">
                    <strong>{progressStage}</strong>
                    <span>{progressLabel}</span>
                  </div>
                  <div className="progress-track" aria-hidden="true">
                    <div
                      className={showCoreDownloadProgress && !updateProgress?.indeterminate ? 'progress-bar' : 'progress-bar indeterminate'}
                      style={showCoreDownloadProgress && !updateProgress?.indeterminate ? {width: `${Math.max(progressPercent, progressPercent > 0 ? 8 : 0)}%`} : undefined}
                    />
                  </div>
                  <div className="bootstrap-progress-detail">{progressDetail}</div>
                </div>
              </div>
            </div>
            <div className="bootstrap-current">
              <div>
                <span className="eyebrow">当前步骤</span>
                <strong>{state.bootstrapStep || '等待启动'}</strong>
              </div>
              <span className="bootstrap-time">{formatTime(state.bootstrapUpdatedAt)}</span>
            </div>
            <div className="bootstrap-detail">
              {state.bootstrapDetail || '正在等待新的引导信息。'}
            </div>
            <div className="bootstrap-flags">
              <span>核心：{state.coreRunning ? '运行中' : (state.coreInstalled ? '未就绪' : '未安装')}</span>
              <span>管理接口：{state.managementAPIHealthy ? '健康' : '未连通'}</span>
              <span>网络：{state.githubNetworkLabel || '自动检测'}</span>
            </div>
            <div className="meta-list compact">
              {panelChecks.map((item) => (
                <div key={item.label}>
                  <span>{item.label}</span>
                  <strong>{item.ready ? '已就绪' : '未就绪'}</strong>
                </div>
              ))}
            </div>
            <div className="bootstrap-detail">
              当前阻塞：{blockingDetail}
            </div>
            <div className="bootstrap-history">
              {bootstrapHistory.length > 0 ? bootstrapHistory.map((item, index) => (
                <div className="bootstrap-history-item" key={`${item.stage}-${item.timestamp}-${index}`}>
                  <span className="bootstrap-history-stage">{item.stage}</span>
                  <span className="bootstrap-history-detail">{item.detail}</span>
                  <span className="bootstrap-history-time">{formatTime(item.timestamp)}</span>
                </div>
              )) : (
                <div className="bootstrap-history-empty">暂无引导记录</div>
              )}
            </div>
          </div>
        </div>
      )}
    </section>
  );
}
