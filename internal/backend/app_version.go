package backend

import "strings"

// AppVersion 表示当前 easy-cpa 应用版本，可在构建时通过 ldflags 注入。
var AppVersion = "dev"

// CurrentAppVersion 返回当前应用版本。
func CurrentAppVersion() string {
	version := strings.TrimSpace(AppVersion)
	if version == "" {
		return "dev"
	}
	return version
}
