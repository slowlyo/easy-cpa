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
  const bootstrapHistory = [...(state.bootstrapHistory ?? [])].reverse();
  const panelChecks = [
    {label: '管理页资源', ready: Boolean(state.panelInstalled), detail: state.panelInstalled ? '已缓存' : '未缓存'},
    {label: '本地入口', ready: Boolean(panelFrameURL), detail: panelFrameURL || '尚未生成'},
    {label: '核心进程', ready: Boolean(state.coreRunning), detail: state.coreRunning ? `PID ${state.process?.pid || '-'}` : '未运行'},
    {label: '管理接口', ready: Boolean(state.managementAPIHealthy), detail: state.managementAPIHealthy ? '已连通' : (state.process?.lastError || state.lastError || '等待探测中')},
  ];

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
          <h2>管理页准备中</h2>
          <p>等待本地入口与核心管理接口就绪后，将自动进入官方管理界面。</p>
          <div className="bootstrap-status">
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
              当前阻塞：{panelChecks.find((item) => !item.ready)?.detail || '无'}
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
