import {backend} from '../../wailsjs/go/models';

const LOG_LIMIT = 400;

/**
 * 创建前端初始状态。
 */
export function createEmptyState(): backend.BootstrapState {
  return new backend.BootstrapState({
    bootstrapPhase: 'idle',
    bootstrapStep: '等待启动',
    bootstrapDetail: '应用尚未开始初始化。',
    bootstrapUpdatedAt: '',
    bootstrapHistory: [],
    coreInstalled: false,
    coreRunning: false,
    coreVersion: '',
    coreLatestVersion: '',
    panelInstalled: false,
    panelVersion: '',
    panelLatestVersion: '',
    panelURL: '',
    managementAPIHealthy: false,
    githubProxyMode: 'direct',
    githubNetworkLabel: '自动检测',
    lastError: '',
    recentLogs: [],
    process: {
      running: false,
      pid: 0,
      startedAt: '',
      exitedAt: '',
      exitCode: 0,
      lastError: '',
      managementHealthy: false,
    },
    networkSettings: {
      githubProxyEnabled: false,
      githubProxyURL: '',
    },
    port: 8317,
    host: '127.0.0.1',
    dataDir: '',
  });
}

/**
 * 格式化时间字段。
 */
export function formatTime(value: any): string {
  if (!value) {
    return '-';
  }
  if (typeof value === 'string' && value.startsWith('0001-01-01')) {
    return '-';
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime()) || date.getFullYear() <= 1) {
    return '-';
  }
  return date.toLocaleString('zh-CN', {hour12: false});
}

/**
 * 按进程状态格式化最近启动时间。
 */
export function formatStartedAt(process?: backend.CoreProcessState): string {
  if (!process?.running) {
    return process?.startedAt ? formatTime(process.startedAt) : '-';
  }
  return process.startedAt ? formatTime(process.startedAt) : '当前会话未记录';
}

/**
 * 按进程状态格式化最近退出时间。
 */
export function formatExitedAt(process?: backend.CoreProcessState): string {
  if (process?.running) {
    return process.exitedAt ? formatTime(process.exitedAt) : '-';
  }
  return process?.exitedAt ? formatTime(process.exitedAt) : '-';
}

/**
 * 归并日志并裁剪长度。
 */
export function appendLog(items: backend.LogEntry[], entry: backend.LogEntry): backend.LogEntry[] {
  const next = [...items, new backend.LogEntry(entry)];
  if (next.length <= LOG_LIMIT) {
    return next;
  }
  return next.slice(next.length - LOG_LIMIT);
}
