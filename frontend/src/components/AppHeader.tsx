import {backend} from '../../wailsjs/go/models';

type ViewMode = 'panel' | 'system';

interface AppHeaderProps {
  view: ViewMode;
  state: backend.BootstrapState;
  busyAction: string;
  appNeedsUpdate: boolean;
  coreNeedsUpdate: boolean;
  onViewChange: (view: ViewMode) => void;
  onCheckUpdates: () => void;
  onUpdateApp: () => void;
  onUpdateCore: () => void;
  onOpenDataDir: () => void;
}

/**
 * AppHeader 渲染顶部导航与全局操作。
 */
export function AppHeader({
  view,
  state,
  busyAction,
  appNeedsUpdate,
  coreNeedsUpdate,
  onViewChange,
  onCheckUpdates,
  onUpdateApp,
  onUpdateCore,
  onOpenDataDir,
}: AppHeaderProps) {
  const appVersion = state.appVersion || '开发版';
  let appStatusClass = 'version-status pending';
  let appHint = '未检测';
  if (appNeedsUpdate) {
    appStatusClass = 'version-status update';
    appHint = `可更新到 ${state.appLatestVersion || '最新版本'}`;
  } else if (state.appVersion) {
    appStatusClass = 'version-status latest';
    appHint = state.appVersion === 'dev' ? '开发版' : '已是最新版本';
  }
  const coreVersion = state.coreVersion || '未安装';
  let versionStatusClass = 'version-status pending';
  let coreHint = '等待安装';
  if (coreNeedsUpdate) {
    versionStatusClass = 'version-status update';
    coreHint = `可更新到 ${state.coreLatestVersion || '最新版本'}`;
  } else if (state.coreInstalled) {
    versionStatusClass = 'version-status latest';
    coreHint = '已是最新版本';
  }

  return (
    <header className="app-header">
      <div className="menu-primary">
        <div className="brand-block">
          <div className="brand-copy">
            <strong>Easy CPA</strong>
          </div>
        </div>
        <nav className="nav top-nav">
          <button
            className={view === 'panel' ? 'nav-item active' : 'nav-item'}
            onClick={() => onViewChange('panel')}
          >
            管理页
          </button>
          <button
            className={view === 'system' ? 'nav-item active' : 'nav-item'}
            onClick={() => onViewChange('system')}
          >
            系统页
          </button>
        </nav>
      </div>

      <div className="header-side">
        <div className="header-meta">
          <div className="meta-pill">
            <span>状态</span>
            <strong>{state.bootstrapPhase || '-'}</strong>
          </div>
          <div className="meta-pill">
            <span>网络</span>
            <strong>{state.githubNetworkLabel || '自动检测'}</strong>
          </div>
          <button
            type="button"
            className="meta-pill version-pill version-pill-button"
            disabled={busyAction !== ''}
            onClick={onCheckUpdates}
            title="点击手动查询是否有新版本"
            aria-label="点击手动查询应用是否有新版本"
          >
            <span>应用</span>
            <div className="meta-pill-body">
              <strong>{appVersion}</strong>
              <small className={appStatusClass}>{appHint}</small>
            </div>
          </button>
          <button
            type="button"
            className="meta-pill version-pill version-pill-button"
            disabled={busyAction !== ''}
            onClick={onCheckUpdates}
            title="点击手动查询是否有新版本"
            aria-label="点击手动查询核心是否有新版本"
          >
            <span>核心</span>
            <div className="meta-pill-body">
              <strong>{coreVersion}</strong>
              <small className={versionStatusClass}>{coreHint}</small>
            </div>
          </button>
        </div>

        <div className="menu-actions">
          {appNeedsUpdate ? (
            <button
              className="menu-button primary"
              disabled={busyAction !== ''}
              onClick={onUpdateApp}
            >
              更新应用
            </button>
          ) : null}
          {coreNeedsUpdate ? (
            <button
              className="menu-button secondary"
              disabled={busyAction !== ''}
              onClick={onUpdateCore}
            >
              更新核心
            </button>
          ) : null}
          <button
            className="menu-button secondary icon-button"
            disabled={busyAction !== ''}
            onClick={onOpenDataDir}
            title="打开数据目录"
            aria-label="打开数据目录"
          >
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path d="M3 7.5A1.5 1.5 0 0 1 4.5 6H10l2 2h7.5A1.5 1.5 0 0 1 21 9.5v8A1.5 1.5 0 0 1 19.5 19h-15A1.5 1.5 0 0 1 3 17.5z" />
              <path d="M3 10h18" />
            </svg>
          </button>
        </div>
      </div>
    </header>
  );
}
