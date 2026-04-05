import {startTransition, useDeferredValue, useEffect, useMemo, useRef, useState} from 'react';
import './App.css';
import {
  CheckUpdates,
  GetBootstrapState,
  OpenDataDir,
  RestartCore,
  SaveCloseConfirmEnabled,
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
 * buildNetworkSettings 统一构造网络设置模型。
 */
function buildNetworkSettings(source?: Partial<backend.NetworkSettings> | null): backend.NetworkSettings {
  return new backend.NetworkSettings({
    githubProxyEnabled: Boolean(source?.githubProxyEnabled),
    githubProxyURL: source?.githubProxyURL ?? '',
  });
}

/**
 * sameNetworkSettings 判断两份网络设置是否一致。
 */
function sameNetworkSettings(left?: Partial<backend.NetworkSettings> | null, right?: Partial<backend.NetworkSettings> | null): boolean {
  return Boolean(left?.githubProxyEnabled) === Boolean(right?.githubProxyEnabled)
    && (left?.githubProxyURL ?? '') === (right?.githubProxyURL ?? '');
}

/**
 * 根组件负责驱动托管界面。
 */
function App() {
  const [view, setView] = useState<ViewMode>('panel');
  const [state, setState] = useState<backend.BootstrapState>(createEmptyState());
  const [busyAction, setBusyAction] = useState('');
  const [notice, setNotice] = useState('');
  const [networkSettings, setNetworkSettings] = useState<backend.NetworkSettings>(buildNetworkSettings());
  const [networkSettingsDirty, setNetworkSettingsDirty] = useState(false);
  const [logFilter, setLogFilter] = useState('');
  const [panelFrameURL, setPanelFrameURL] = useState('');
  const deferredLogFilter = useDeferredValue(logFilter);
  const networkSettingsRef = useRef<backend.NetworkSettings>(buildNetworkSettings());
  const syncedNetworkSettingsRef = useRef<backend.NetworkSettings>(buildNetworkSettings());
  const networkSettingsDirtyRef = useRef(false);

  /**
   * applyBootstrapState 同步后端状态，并保护未保存的代理草稿。
   */
  const applyBootstrapState = (next: backend.BootstrapState, forceSyncNetwork = false) => {
    const resolved = new backend.BootstrapState(next);
    const syncedNetworkSettings = buildNetworkSettings(resolved.networkSettings);
    syncedNetworkSettingsRef.current = syncedNetworkSettings;

    const shouldSyncNetwork = forceSyncNetwork
      || !networkSettingsDirtyRef.current
      || sameNetworkSettings(networkSettingsRef.current, syncedNetworkSettings);

    // 后端设置与草稿一致时直接收敛，避免保存成功后界面残留脏状态。
    if (shouldSyncNetwork) {
      networkSettingsRef.current = syncedNetworkSettings;
      networkSettingsDirtyRef.current = false;
    }

    startTransition(() => {
      setState(resolved);
      // 草稿存在未保存改动时只更新后端状态，避免定时刷新覆盖用户输入。
      if (shouldSyncNetwork) {
        setNetworkSettings(syncedNetworkSettings);
        setNetworkSettingsDirty(false);
      }
      if (resolved.panelURL) {
        setPanelFrameURL((current: string) => current === resolved.panelURL ? current : resolved.panelURL);
      }
    });
  };

  /**
   * updateDraftNetworkSettings 更新本地网络设置草稿。
   */
  const updateDraftNetworkSettings = (patch: Partial<backend.NetworkSettings>) => {
    const next = buildNetworkSettings({
      ...networkSettingsRef.current,
      ...patch,
    });
    const dirty = !sameNetworkSettings(next, syncedNetworkSettingsRef.current);
    networkSettingsRef.current = next;
    networkSettingsDirtyRef.current = dirty;

    startTransition(() => {
      setNetworkSettings(next);
      setNetworkSettingsDirty(dirty);
    });
  };

  /**
   * 拉取后端聚合状态。
   */
  const refreshState = async () => {
    const next = await GetBootstrapState();
    applyBootstrapState(next);
  };

  /**
   * 执行带状态刷新的后端动作。
   */
  const runAction = async (label: string, action: () => Promise<backend.BootstrapState>) => {
    setBusyAction(label);
    setNotice('');
    try {
      const next = await action();
      applyBootstrapState(next, label === 'network');
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
        applyBootstrapState(next, true);
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
  const globalNotice = notice;
  const shouldShowGlobalNotice = Boolean(globalNotice) && view === 'system';

  return (
    <div className="shell">
      <AppHeader
        view={view}
        state={state}
        busyAction={busyAction}
        appNeedsUpdate={appNeedsUpdate}
        coreNeedsUpdate={coreNeedsUpdate}
        onViewChange={setView}
        onCheckUpdates={() => void runAction('check', CheckUpdates)}
        onUpdateApp={() => void runAction('app', UpdateApp)}
        onUpdateCore={() => void runAction('core', UpdateCore)}
        onOpenDataDir={openDataDir}
      />

      <main className={view === 'panel' ? 'content content-panel' : 'content content-system'}>
        {shouldShowGlobalNotice ? (
          <div className="notice error">{globalNotice}</div>
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
          networkSettingsDirty={networkSettingsDirty}
          onLogFilterChange={setLogFilter}
          onNetworkProxyEnabledChange={(enabled) => updateDraftNetworkSettings({
            githubProxyEnabled: enabled,
          })}
          onNetworkProxyURLChange={(value) => updateDraftNetworkSettings({
            githubProxyURL: value,
          })}
          onStartCore={() => void runAction('start', StartCore)}
          onStopCore={() => void runAction('stop', StopCore)}
          onRestartCore={() => void runAction('restart', RestartCore)}
          onUpdateApp={() => void runAction('app', UpdateApp)}
          onCheckUpdates={() => void runAction('check', CheckUpdates)}
          onUpdatePanel={() => void runAction('panel', UpdatePanel)}
          onUpdateCore={() => void runAction('core', UpdateCore)}
          onSaveNetworkSettings={() => void runAction('network', () => SaveNetworkSettings(networkSettingsRef.current))}
          onCloseConfirmEnabledChange={(enabled) => void runAction('close-confirm', () => SaveCloseConfirmEnabled(enabled))}
        />
      </main>
    </div>
  );
}

export default App;
