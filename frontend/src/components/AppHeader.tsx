import {backend} from '../../wailsjs/go/models';

type ViewMode = 'panel' | 'system';

interface AppHeaderProps {
  view: ViewMode;
  state: backend.BootstrapState;
  busyAction: string;
  coreNeedsUpdate: boolean;
  onViewChange: (view: ViewMode) => void;
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
  coreNeedsUpdate,
  onViewChange,
  onUpdateCore,
  onOpenDataDir,
}: AppHeaderProps) {
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
          <div className="brand-mark">EC</div>
          <div className="brand-copy">
            <strong>Easy CPA</strong>
            <span>托管控制台</span>
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

      <div className="header-meta">
        <div className="meta-pill">
          <span>状态</span>
          <strong>{state.bootstrapPhase || '-'}</strong>
        </div>
        <div className="meta-pill">
          <span>网络</span>
          <strong>{state.githubNetworkLabel || '自动检测'}</strong>
        </div>
        <div className="meta-pill version-pill">
          <span>核心</span>
          <div className="meta-pill-body">
            <strong>{coreVersion}</strong>
            <small className={versionStatusClass}>{coreHint}</small>
          </div>
        </div>
      </div>

      <div className="menu-actions">
        {coreNeedsUpdate ? (
          <button
            className="menu-button primary"
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
    </header>
  );
}
