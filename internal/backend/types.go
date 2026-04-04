package backend

import "time"

// BootstrapProgress 表示单条引导进度。
type BootstrapProgress struct {
	Stage     string    `json:"stage"`
	Detail    string    `json:"detail"`
	Timestamp time.Time `json:"timestamp"`
}

// BootstrapState 描述前端需要展示的聚合状态。
type BootstrapState struct {
	BootstrapPhase       string              `json:"bootstrapPhase"`
	BootstrapStep        string              `json:"bootstrapStep"`
	BootstrapDetail      string              `json:"bootstrapDetail"`
	BootstrapUpdatedAt   time.Time           `json:"bootstrapUpdatedAt"`
	BootstrapHistory     []BootstrapProgress `json:"bootstrapHistory"`
	CoreInstalled        bool                `json:"coreInstalled"`
	CoreRunning          bool                `json:"coreRunning"`
	CoreVersion          string              `json:"coreVersion"`
	CoreLatestVersion    string              `json:"coreLatestVersion"`
	PanelInstalled       bool                `json:"panelInstalled"`
	PanelVersion         string              `json:"panelVersion"`
	PanelLatestVersion   string              `json:"panelLatestVersion"`
	PanelURL             string              `json:"panelURL"`
	ManagementAPIHealthy bool                `json:"managementAPIHealthy"`
	GithubProxyMode      string              `json:"githubProxyMode"`
	GithubNetworkLabel   string              `json:"githubNetworkLabel"`
	LastError            string              `json:"lastError"`
	RecentLogs           []LogEntry          `json:"recentLogs"`
	Process              CoreProcessState    `json:"process"`
	NetworkSettings      NetworkSettings     `json:"networkSettings"`
	Port                 int                 `json:"port"`
	Host                 string              `json:"host"`
	DataDir              string              `json:"dataDir"`
}

// NetworkSettings 定义 GitHub 访问网络配置。
type NetworkSettings struct {
	GithubProxyEnabled bool   `json:"githubProxyEnabled"`
	GithubProxyURL     string `json:"githubProxyURL"`
}

// SettingsFile 描述本地设置文件结构。
type SettingsFile struct {
	ManagementKey string          `json:"managementKey"`
	Network       NetworkSettings `json:"network"`
}

// ReleaseMeta 统一描述发布信息。
type ReleaseMeta struct {
	Tag         string    `json:"tag"`
	PublishedAt time.Time `json:"publishedAt"`
	AssetName   string    `json:"assetName"`
	DownloadURL string    `json:"downloadURL"`
	SHA256      string    `json:"sha256"`
}

// LogEntry 表示单条聚合日志。
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`
	Message   string    `json:"message"`
}

// CoreProcessState 表示核心进程层状态。
type CoreProcessState struct {
	Running           bool      `json:"running"`
	PID               int       `json:"pid"`
	StartedAt         time.Time `json:"startedAt"`
	ExitedAt          time.Time `json:"exitedAt"`
	ExitCode          int       `json:"exitCode"`
	LastError         string    `json:"lastError"`
	ManagementHealthy bool      `json:"managementHealthy"`
}

// ManagedPaths 描述托管目录布局。
type ManagedPaths struct {
	RootDir        string
	CoreDir        string
	PanelDir       string
	LogsDir        string
	TmpDir         string
	SettingsPath   string
	ConfigPath     string
	CoreMetaPath   string
	PanelMetaPath  string
	CoreBinaryPath string
	PanelHTMLPath  string
}

// ConfigState 描述实际生效的核心配置。
type ConfigState struct {
	Host          string
	Port          int
	ManagementKey string
	ConfigPath    string
}
