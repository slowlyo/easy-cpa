package backend

import (
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// DownloadProgress 表示单次下载的字节进度。
type DownloadProgress struct {
	DownloadedBytes int64
	TotalBytes      int64
}

// emitUpdateProgress 更新当前任务进度，并同步到引导状态。
func (a *App) emitUpdateProgress(target, stage, detail string, downloadedBytes, totalBytes int64) {
	a.mu.Lock()
	now := time.Now()
	a.state.BootstrapPhase = stateBootstrapRun
	a.state.BootstrapStep = stage
	a.state.BootstrapDetail = detail
	a.state.BootstrapUpdatedAt = now
	a.state.UpdateProgress = buildUpdateProgressState(target, stage, detail, downloadedBytes, totalBytes, true)
	a.appendBootstrapHistoryLocked(stage, detail)
	a.mu.Unlock()
	a.emitBootstrapEvent(stage, detail)
}

// finishUpdateProgress 标记当前更新任务已结束。
func (a *App) finishUpdateProgress(target, stage, detail string) {
	a.mu.Lock()
	now := time.Now()
	completed := a.state.UpdateProgress
	completed.Active = false
	completed.Target = target
	completed.Stage = stage
	completed.Detail = detail
	completed.Indeterminate = false
	if completed.TotalBytes > 0 {
		completed.DownloadedBytes = completed.TotalBytes
	}
	completed.Percent = 1
	a.state.BootstrapPhase = stateBootstrapRun
	a.state.BootstrapStep = stage
	a.state.BootstrapDetail = detail
	a.state.BootstrapUpdatedAt = now
	a.state.UpdateProgress = completed
	a.appendBootstrapHistoryLocked(stage, detail)
	a.mu.Unlock()
	a.emitBootstrapEvent(stage, detail)
}

// clearUpdateProgress 清空更新进度，避免成功态长期停留在界面上。
func (a *App) clearUpdateProgress() {
	a.mu.Lock()
	a.state.UpdateProgress = emptyUpdateProgressState()
	a.mu.Unlock()
}

// buildUpdateProgressState 组装前端需要的更新进度结构。
func buildUpdateProgressState(target, stage, detail string, downloadedBytes, totalBytes int64, active bool) UpdateProgressState {
	progress := UpdateProgressState{
		Active:          active,
		Target:          target,
		Stage:           stage,
		Detail:          detail,
		DownloadedBytes: downloadedBytes,
		TotalBytes:      totalBytes,
		Indeterminate:   totalBytes <= 0,
	}
	// 已知总大小时直接换算百分比，避免前端猜测。
	if totalBytes > 0 {
		progress.Percent = float64(downloadedBytes) / float64(totalBytes)
		if progress.Percent < 0 {
			progress.Percent = 0
		}
		if progress.Percent > 1 {
			progress.Percent = 1
		}
	}
	return progress
}

// emptyUpdateProgressState 返回空闲态更新进度。
func emptyUpdateProgressState() UpdateProgressState {
	return UpdateProgressState{
		Active:        false,
		Target:        "",
		Stage:         "",
		Detail:        "",
		Indeterminate: true,
	}
}

// emitBootstrapEvent 统一广播引导事件。
func (a *App) emitBootstrapEvent(stage, detail string) {
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "bootstrap:progress", map[string]string{"stage": stage, "detail": detail})
	}
}
