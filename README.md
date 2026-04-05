# Easy CPA

基于 Wails 的 CLIProxyAPI 桌面托管应用。

## 当前能力

- 启动后自动检查 GitHub latest release
- 首次缺失时自动下载 CPA 核心
- 独立缓存并托管官方 `management.html`
- 自动生成并补齐本地托管配置
- 自动启动/停止本地单实例 CPA
- 内嵌官方管理界面
- 系统页展示版本、状态、代理、日志、更新操作与下载进度
- 支持检测 easy-cpa 自身最新 release，并执行应用自更新
- GitHub 请求支持自定义代理与 `127.0.0.1:7890` / `127.0.0.1:7897` 回退

## 目录

运行时数据默认写入：

- Windows: `%AppData%/easy-cpa`
- macOS/Linux: `os.UserConfigDir()/easy-cpa`

主要内容：

- `core/`：CPA 核心二进制与版本元数据
- `panel/`：官方管理页缓存
- `logs/`：easy-cpa 自身日志目录预留
- `tmp/`：下载与解压临时目录
- `config.yaml`：托管 CPA 配置
- `settings.json`：GitHub 网络设置

## 开发

后端目录：

- `main.go`：Wails 应用入口
- `internal/backend/`：托管、下载、配置、面板、运行时等后端实现

前端依赖：

```bash
cd frontend
npm install
```

后端测试：

```bash
go test ./...
```

前端构建：

```bash
cd frontend
npm run build
```

桌面构建：

```bash
go run github.com/wailsapp/wails/v2/cmd/wails@v2.12.0 build
```

如果本机全局 `wails` 版本低于 `2.12.0`，请优先使用上面的 `go run` 方式。

## 发布

已配置 GitHub Actions 自动发布流程：

- 推送任意 tag 时触发，例如 `v1.0.0`
- 分别在 Windows、Linux、macOS 上构建 Wails 应用
- macOS 默认使用 GitHub 当前提供的 `macos-latest` runner
- Linux 默认按新发行版环境构建，并为 Wails 追加 `-tags webkit2_41`
- 发布构建会把 tag 注入应用版本号，供桌面端自身更新检测使用
- 自动创建 GitHub Release 并上传各平台压缩包
- 同时生成 `SHA256SUMS.txt` 校验文件

## 当前默认值

- 托管实例只管理 easy-cpa 自己下载的单实例 CPA
- 默认监听 `127.0.0.1:8317`
- 默认启用 `logging-to-file: true`
- 默认禁用 CPA 自带 control panel 路由，统一由 easy-cpa 托管官方管理页
- 关闭 easy-cpa 时会停止托管 CPA
