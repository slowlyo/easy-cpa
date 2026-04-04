package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	appLatestReleaseAPI   = "https://api.github.com/repos/slowlyo/easy-cpa/releases/latest"
	coreLatestReleaseAPI  = "https://api.github.com/repos/router-for-me/CLIProxyAPI/releases/latest"
	panelLatestReleaseAPI = "https://api.github.com/repos/router-for-me/Cli-Proxy-API-Management-Center/releases/latest"
)

type githubReleaseResponse struct {
	TagName     string               `json:"tag_name"`
	PublishedAt time.Time            `json:"published_at"`
	Assets      []githubReleaseAsset `json:"assets"`
}

type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
}

// ReleaseManager 负责解析 GitHub 发布与下载安装。
type ReleaseManager struct {
	proxy       *ProxyManager
	mu          sync.RWMutex
	latestApp   ReleaseMeta
	latestCore  ReleaseMeta
	latestPanel ReleaseMeta
	lastError   string
}

// NewReleaseManager 创建发布管理器。
func NewReleaseManager(proxy *ProxyManager) *ReleaseManager {
	return &ReleaseManager{proxy: proxy}
}

// LatestApp 返回缓存的应用发布信息。
func (r *ReleaseManager) LatestApp() ReleaseMeta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.latestApp
}

// LatestCore 返回缓存的核心发布信息。
func (r *ReleaseManager) LatestCore() ReleaseMeta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.latestCore
}

// LatestPanel 返回缓存的管理页发布信息。
func (r *ReleaseManager) LatestPanel() ReleaseMeta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.latestPanel
}

// LastError 返回最近一次发布请求错误。
func (r *ReleaseManager) LastError() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastError
}

// HasAnyLocalAsset 判断本地是否已有可用资源。
func (r *ReleaseManager) HasAnyLocalAsset(paths ManagedPaths) bool {
	return FileExists(paths.CoreBinaryPath) || FileExists(paths.PanelHTMLPath)
}

// FetchLatestAppRelease 获取最新应用发布。
func (r *ReleaseManager) FetchLatestAppRelease(ctx context.Context) (ReleaseMeta, error) {
	meta, err := r.fetchLatestRelease(ctx, appLatestReleaseAPI, pickAppAsset)
	if err != nil {
		r.setLastError(err)
		return ReleaseMeta{}, err
	}
	r.mu.Lock()
	r.latestApp = meta
	r.lastError = ""
	r.mu.Unlock()
	return meta, nil
}

// FetchLatestCoreRelease 获取最新核心发布。
func (r *ReleaseManager) FetchLatestCoreRelease(ctx context.Context) (ReleaseMeta, error) {
	meta, err := r.fetchLatestRelease(ctx, coreLatestReleaseAPI, pickCoreAsset)
	if err != nil {
		r.setLastError(err)
		return ReleaseMeta{}, err
	}
	r.mu.Lock()
	r.latestCore = meta
	r.lastError = ""
	r.mu.Unlock()
	return meta, nil
}

// FetchLatestPanelRelease 获取最新管理页发布。
func (r *ReleaseManager) FetchLatestPanelRelease(ctx context.Context) (ReleaseMeta, error) {
	meta, err := r.fetchLatestRelease(ctx, panelLatestReleaseAPI, pickPanelAsset)
	if err != nil {
		r.setLastError(err)
		return ReleaseMeta{}, err
	}
	r.mu.Lock()
	r.latestPanel = meta
	r.lastError = ""
	r.mu.Unlock()
	return meta, nil
}

// InstallCoreRelease 下载并安装核心。
func (r *ReleaseManager) InstallCoreRelease(ctx context.Context, meta ReleaseMeta, paths ManagedPaths) error {
	if meta.DownloadURL == "" {
		return errors.New("核心下载地址为空")
	}
	if err := os.MkdirAll(paths.TmpDir, 0o755); err != nil {
		return fmt.Errorf("创建临时目录失败: %w", err)
	}

	tempPath := filepath.Join(paths.TmpDir, meta.AssetName)
	if err := downloadFile(ctx, r.proxy, meta.DownloadURL, tempPath); err != nil {
		return err
	}
	defer os.Remove(tempPath)

	if err := verifySHA256(tempPath, meta.SHA256); err != nil {
		return err
	}
	if err := extractCoreAsset(tempPath, paths.CoreDir); err != nil {
		return err
	}
	return WriteReleaseMetaFile(paths.CoreMetaPath, meta)
}

type releaseAssetPicker func(assets []githubReleaseAsset) (githubReleaseAsset, bool)

// fetchLatestRelease 请求发布接口并匹配资产。
func (r *ReleaseManager) fetchLatestRelease(ctx context.Context, endpoint string, picker releaseAssetPicker) (ReleaseMeta, error) {
	headers := map[string]string{
		"Accept":               "application/vnd.github+json",
		"User-Agent":           "easy-cpa",
		"X-GitHub-Api-Version": "2022-11-28",
	}
	var payload githubReleaseResponse
	if err := fetchJSON(ctx, r.proxy, endpoint, headers, &payload); err != nil {
		return ReleaseMeta{}, err
	}

	asset, ok := picker(payload.Assets)
	if !ok {
		return ReleaseMeta{}, errors.New("未找到匹配当前平台的发布资产")
	}

	return ReleaseMeta{
		Tag:         payload.TagName,
		PublishedAt: payload.PublishedAt,
		AssetName:   asset.Name,
		DownloadURL: asset.BrowserDownloadURL,
		SHA256:      strings.TrimPrefix(asset.Digest, "sha256:"),
	}, nil
}

// setLastError 保存发布层错误。
func (r *ReleaseManager) setLastError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastError = err.Error()
}

// fetchJSON 获取 JSON 接口并解析。
func fetchJSON(ctx context.Context, proxy *ProxyManager, requestURL string, headers map[string]string, target any) error {
	req, err := httpNewRequest(ctx, requestURL, headers)
	if err != nil {
		return err
	}
	resp, _, err := proxy.Do(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return describeGitHubFailure(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}
	return nil
}

// downloadFile 下载文件到本地。
func downloadFile(ctx context.Context, proxy *ProxyManager, requestURL, targetPath string) error {
	file, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("创建下载文件失败: %w", err)
	}
	defer file.Close()

	headers := map[string]string{
		"Accept":               "application/octet-stream",
		"User-Agent":           "easy-cpa",
		"X-GitHub-Api-Version": "2022-11-28",
	}
	if _, err := proxy.Download(ctx, requestURL, headers, file); err != nil {
		return fmt.Errorf("下载发布资产失败: %w", err)
	}
	return nil
}

// verifySHA256 校验下载文件哈希。
func verifySHA256(path, expected string) error {
	if strings.TrimSpace(expected) == "" {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("打开校验文件失败: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("计算哈希失败: %w", err)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("哈希校验失败: expected=%s actual=%s", expected, actual)
	}
	return nil
}

// pickCoreAsset 选择当前平台核心资产。
func pickCoreAsset(assets []githubReleaseAsset) (githubReleaseAsset, bool) {
	suffix, err := expectedCoreAssetSuffix()
	if err != nil {
		return githubReleaseAsset{}, false
	}
	for _, asset := range assets {
		if strings.HasSuffix(asset.Name, suffix) {
			return asset, true
		}
	}
	return githubReleaseAsset{}, false
}

// pickAppAsset 选择当前平台应用资产。
func pickAppAsset(assets []githubReleaseAsset) (githubReleaseAsset, bool) {
	suffix, err := expectedAppAssetSuffix()
	if err != nil {
		return githubReleaseAsset{}, false
	}
	for _, asset := range assets {
		if strings.HasSuffix(asset.Name, suffix) {
			return asset, true
		}
	}
	return githubReleaseAsset{}, false
}

// pickPanelAsset 选择管理页资产。
func pickPanelAsset(assets []githubReleaseAsset) (githubReleaseAsset, bool) {
	for _, asset := range assets {
		if asset.Name == "management.html" {
			return asset, true
		}
	}
	return githubReleaseAsset{}, false
}

// expectedCoreAssetSuffix 计算当前平台资产后缀。
func expectedCoreAssetSuffix() (string, error) {
	return expectedCoreAssetSuffixFor(runtime.GOOS, runtime.GOARCH)
}

// expectedAppAssetSuffix 计算当前平台应用资产后缀。
func expectedAppAssetSuffix() (string, error) {
	return expectedAppAssetSuffixFor(runtime.GOOS)
}

// expectedAppAssetSuffixFor 根据平台计算应用资产后缀。
func expectedAppAssetSuffixFor(goos string) (string, error) {
	switch goos {
	case "windows":
		return "-windows.zip", nil
	case "linux":
		return "-linux.tar.gz", nil
	case "darwin":
		return "-macos.zip", nil
	default:
		return "", fmt.Errorf("不支持当前平台: %s", goos)
	}
}

// expectedCoreAssetSuffixFor 根据平台计算资产后缀。
func expectedCoreAssetSuffixFor(goos, goarch string) (string, error) {
	switch goos {
	case "windows":
		switch goarch {
		case "amd64":
			return "_windows_amd64.zip", nil
		case "arm64":
			return "_windows_arm64.zip", nil
		}
	case "linux":
		switch goarch {
		case "amd64":
			return "_linux_amd64.tar.gz", nil
		case "arm64":
			return "_linux_arm64.tar.gz", nil
		}
	case "darwin":
		switch goarch {
		case "amd64":
			return "_darwin_amd64.tar.gz", nil
		case "arm64":
			return "_darwin_arm64.tar.gz", nil
		}
	}
	return "", fmt.Errorf("不支持当前平台: %s/%s", goos, goarch)
}

// WriteReleaseMetaFile 保存发布元信息。
func WriteReleaseMetaFile(path string, meta ReleaseMeta) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

// ReadReleaseMetaFile 读取发布元信息。
func ReadReleaseMetaFile(path string) ReleaseMeta {
	if !FileExists(path) {
		return ReleaseMeta{}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ReleaseMeta{}
	}
	var meta ReleaseMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return ReleaseMeta{}
	}
	return meta
}
