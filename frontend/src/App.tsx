import {startTransition, useDeferredValue, useEffect, useMemo, useState} from 'react';
import './App.css';
import {
  CheckUpdates,
  GetBootstrapState,
  OpenDataDir,
  RestartCore,
  SaveNetworkSettings,
  StartCore,
  StopCore,
  UpdateApp,
  UpdateCore,
  UpdatePanel,
} from '../wailsjs/go/backend/App';
import {backend} from '../wailsjs/go/models';
import {EventsOff, EventsOn} from '../wailsjs/runtime/runtime';
import {AppHeader} from './components/AppHeader';
import {PanelView} from './components/PanelView';
import {SystemView} from './components/SystemView';
import {appendLog, createEmptyState} from './utils/appState';

type ViewMode = 'panel' | 'system';

/**
 * 根组件负责驱动托管界面。
 */
function App() {
  const [view, setView] = useState<ViewMode>('panel');
  const [state, setState] = useState<backend.BootstrapState>(createEmptyState());
  const [busyAction, setBusyAction] = useState('');
  const [notice, setNotice] = useState('');
  const [networkSettings, setNetworkSettings] = useState<backend.NetworkSettings>(new backend.NetworkSettings({
    githubProxyEnabled: false,
    githubProxyURL: '',
  }));
  const [logFilter, setLogFilter] = useState('');
  const [panelFrameURL, setPanelFrameURL] = useState('');
  const deferredLogFilter = useDeferredValue(logFilter);

  /**
   * 拉取后端聚合状态。
   */
  const refreshState = async () => {
    const next = await GetBootstrapState();
    startTransition(() => {
      const resolved = new backend.BootstrapState(next);
      setState(resolved);
      setNetworkSettings(new backend.NetworkSettings(resolved.networkSettings));
      if (resolved.panelURL) {
        setPanelFrameURL((current: string) => current === resolved.panelURL ? current : resolved.panelURL);
      }
    });
  };

  /**
   * 执行带状态刷新的后端动作。
   */
  const runAction = async (label: string, action: () => Promise<backend.BootstrapState>) => {
    setBusyAction(label);
    setNotice('');
    try {
      const next = await action();
      startTransition(() => {
        const resolved = new backend.BootstrapState(next);
        setState(resolved);
        setNetworkSettings(new backend.NetworkSettings(resolved.networkSettings));
      });
    } catch (error: unknown) {
      const message = error instanceof Error ? error.message : String(error);
      setNotice(message);
      await refreshState();
    } finally {
      setBusyAction('');
    }
  };

  /**
   * 打开数据目录。
   */
  const openDataDir = () => {
    void OpenDataDir().catch((error: unknown) => {
      setNotice(error instanceof Error ? error.message : String(error));
    });
  };

  useEffect(() => {
    let mounted = true;

    /**
     * 初始化状态与事件监听。
     */
    const init = async () => {
      try {
        const next = await GetBootstrapState();
        if (!mounted) {
          return;
        }
        startTransition(() => {
          const resolved = new backend.BootstrapState(next);
          setState(resolved);
          setNetworkSettings(new backend.NetworkSettings(resolved.networkSettings));
          if (resolved.panelURL) {
            setPanelFrameURL(resolved.panelURL);
          }
        });
      } catch (error: unknown) {
        if (mounted) {
          setNotice(error instanceof Error ? error.message : String(error));
        }
      }
    };

    init();

    EventsOn('bootstrap:progress', () => {
      void refreshState();
    });
    EventsOn('core:status', () => {
      void refreshState();
    });
    EventsOn('network:status', () => {
      void refreshState();
    });
    EventsOn('core:log', (payload: any) => {
      const entry = new backend.LogEntry(payload);
      startTransition(() => {
        setState((current) => new backend.BootstrapState({
          ...current,
          recentLogs: appendLog(current.recentLogs ?? [], entry),
        }));
      });
    });

    const timer = window.setInterval(() => {
      void refreshState();
    }, 4000);

    return () => {
      mounted = false;
      window.clearInterval(timer);
      EventsOff('bootstrap:progress');
      EventsOff('core:status');
      EventsOff('core:log');
      EventsOff('network:status');
    };
  }, []);

  const filteredLogs = useMemo(() => {
    const keyword = deferredLogFilter.trim().toLowerCase();
    const items = [...(state.recentLogs ?? [])].reverse();
    if (!keyword) {
      return items;
    }
    return items.filter((item) =>
      `${item.source} ${item.message}`.toLowerCase().includes(keyword)
    );
  }, [deferredLogFilter, state.recentLogs]);

  const coreNeedsUpdate = Boolean(state.coreVersion && state.coreLatestVersion && state.coreVersion !== state.coreLatestVersion);
  const panelNeedsUpdate = Boolean(state.panelVersion && state.panelLatestVersion && state.panelVersion !== state.panelLatestVersion);
  const appNeedsUpdate = Boolean(state.appNeedsUpdate);

  return (
    <div className="shell">
      <AppHeader
        view={view}
        state={state}
        busyAction={busyAction}
        appNeedsUpdate={appNeedsUpdate}
        coreNeedsUpdate={coreNeedsUpdate}
        onViewChange={setView}
        onUpdateApp={() => void runAction('app', UpdateApp)}
        onUpdateCore={() => void runAction('core', UpdateCore)}
        onOpenDataDir={openDataDir}
      />

      <main className={view === 'panel' ? 'content content-panel' : 'content content-system'}>
        {notice || state.lastError ? (
          <div className="notice error">{notice || state.lastError}</div>
        ) : null}

        <PanelView
          visible={view === 'panel'}
          state={state}
          panelFrameURL={panelFrameURL}
        />

        <SystemView
          visible={view === 'system'}
          state={state}
          busyAction={busyAction}
          networkSettings={networkSettings}
          logFilter={logFilter}
          filteredLogs={filteredLogs}
          appNeedsUpdate={appNeedsUpdate}
          coreNeedsUpdate={coreNeedsUpdate}
          panelNeedsUpdate={panelNeedsUpdate}
          onLogFilterChange={setLogFilter}
          onNetworkProxyEnabledChange={(enabled) => setNetworkSettings(new backend.NetworkSettings({
            ...networkSettings,
            githubProxyEnabled: enabled,
          }))}
          onNetworkProxyURLChange={(value) => setNetworkSettings(new backend.NetworkSettings({
            ...networkSettings,
            githubProxyURL: value,
          }))}
          onStartCore={() => void runAction('start', StartCore)}
          onStopCore={() => void runAction('stop', StopCore)}
          onRestartCore={() => void runAction('restart', RestartCore)}
          onUpdateApp={() => void runAction('app', UpdateApp)}
          onCheckUpdates={() => void runAction('check', CheckUpdates)}
          onUpdatePanel={() => void runAction('panel', UpdatePanel)}
          onUpdateCore={() => void runAction('core', UpdateCore)}
          onSaveNetworkSettings={() => void runAction('network', () => SaveNetworkSettings(networkSettings))}
        />
      </main>
    </div>
  );
}

export default App;
