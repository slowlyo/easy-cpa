package backend

// managedProcessGuard 负责绑定宿主与托管核心的生命周期。
type managedProcessGuard interface {
	// Close 释放守护资源。
	Close() error
}
