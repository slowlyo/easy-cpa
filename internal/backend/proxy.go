package backend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	apiRequestTimeout             = 20 * time.Second
	apiResponseHeaderTimeout      = 12 * time.Second
	downloadResponseHeaderTimeout = 20 * time.Second
	downloadRetryLimit            = 2
)

type proxyCandidate struct {
	mode  string
	proxy string
}

type requestProfile struct {
	totalTimeout          time.Duration
	responseHeaderTimeout time.Duration
}

var (
	apiRequestProfile = requestProfile{
		totalTimeout:          apiRequestTimeout,
		responseHeaderTimeout: apiResponseHeaderTimeout,
	}
	downloadRequestProfile = requestProfile{
		responseHeaderTimeout: downloadResponseHeaderTimeout,
	}
)

// ProxyManager 负责 GitHub 请求代理选择与回退。
type ProxyManager struct {
	settings *SettingsStore
	logs     *LogBuffer
	mu       sync.RWMutex
	current  string
	clients  map[string]*http.Client
}

// NewProxyManager 创建代理管理器。
func NewProxyManager(settings *SettingsStore, logs *LogBuffer) *ProxyManager {
	return &ProxyManager{
		settings: settings,
		logs:     logs,
		current:  "direct",
		clients:  make(map[string]*http.Client),
	}
}

// Reset 清除当前成功通道缓存。
func (p *ProxyManager) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current = "direct"
	p.closeIdleClientsLocked()
}

// CurrentMode 返回最近一次成功的代理模式。
func (p *ProxyManager) CurrentMode() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.current
}

// CurrentLabel 返回当前网络模式的用户文案。
func (p *ProxyManager) CurrentLabel() string {
	switch p.CurrentMode() {
	case "custom":
		return "自定义代理"
	case "fallback-7890":
		return "自动代理"
	case "fallback-7897":
		return "自动代理"
	default:
		return "自动直连"
	}
}

// Do 对同一个请求执行代理回退。
func (p *ProxyManager) Do(ctx context.Context, req *http.Request) (*http.Response, string, error) {
	candidates := p.candidates()
	var errs []error

	for _, candidate := range candidates {
		resp, err := p.doWithCandidate(ctx, req, candidate, apiRequestProfile)
		if err == nil {
			p.mu.Lock()
			p.current = candidate.mode
			p.mu.Unlock()
			return resp, candidate.mode, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", candidate.mode, err))
	}

	if len(errs) == 0 {
		return nil, "", errors.New("没有可用的 GitHub 代理候选")
	}
	return nil, "", errors.Join(errs...)
}

// Download 执行下载并写入目标 Writer。
func (p *ProxyManager) Download(ctx context.Context, requestURL string, headers map[string]string, writer io.Writer, progress func(DownloadProgress)) (string, error) {
	candidates := p.candidates()
	var errs []error

	for _, candidate := range candidates {
		if err := p.downloadWithCandidate(ctx, candidate, requestURL, headers, writer, progress); err == nil {
			p.mu.Lock()
			p.current = candidate.mode
			p.mu.Unlock()
			return candidate.mode, nil
		} else {
			errs = append(errs, fmt.Errorf("%s: %w", candidate.mode, err))
		}
	}

	if len(errs) == 0 {
		return "", errors.New("没有可用的 GitHub 下载通道")
	}
	return "", errors.Join(errs...)
}

// copyDownloadWithProgress 在复制响应体时按节流频率回传字节进度。
func copyDownloadWithProgress(writer io.Writer, reader io.Reader, totalBytes int64, progress func(DownloadProgress)) error {
	if totalBytes < 0 {
		totalBytes = 0
	}
	if progress != nil {
		progress(DownloadProgress{DownloadedBytes: 0, TotalBytes: totalBytes})
	}

	buffer := make([]byte, 32*1024)
	var downloadedBytes int64
	var lastReportedBytes int64
	lastReportedAt := time.Now()

	for {
		readBytes, readErr := reader.Read(buffer)
		if readBytes > 0 {
			writtenBytes, writeErr := writer.Write(buffer[:readBytes])
			if writtenBytes > 0 {
				downloadedBytes += int64(writtenBytes)
				// 下载过程只在时间片、百分比变化或结束时广播，避免事件过于频繁。
				if progress != nil && shouldReportDownloadProgress(downloadedBytes, totalBytes, lastReportedBytes, lastReportedAt) {
					progress(DownloadProgress{DownloadedBytes: downloadedBytes, TotalBytes: totalBytes})
					lastReportedBytes = downloadedBytes
					lastReportedAt = time.Now()
				}
			}
			if writeErr != nil {
				return writeErr
			}
			if writtenBytes != readBytes {
				return io.ErrShortWrite
			}
		}
		// 读到 EOF 时补发一次终态，确保前端看到 100%。
		if errors.Is(readErr, io.EOF) {
			if progress != nil && downloadedBytes != lastReportedBytes {
				progress(DownloadProgress{DownloadedBytes: downloadedBytes, TotalBytes: totalBytes})
			}
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

// shouldReportDownloadProgress 判断当前字节进度是否值得推送一次事件。
func shouldReportDownloadProgress(downloadedBytes, totalBytes, lastReportedBytes int64, lastReportedAt time.Time) bool {
	if totalBytes > 0 && downloadedBytes >= totalBytes {
		return true
	}
	if time.Since(lastReportedAt) >= 160*time.Millisecond {
		return true
	}
	if totalBytes > 0 && lastReportedBytes < totalBytes {
		return float64(downloadedBytes-lastReportedBytes)/float64(totalBytes) >= 0.01
	}
	return false
}

// candidates 计算代理尝试顺序。
func (p *ProxyManager) candidates() []proxyCandidate {
	settings := p.settings.NetworkSettings()
	list := make([]proxyCandidate, 0, 4)

	// 用户配置了自定义代理时优先尝试它，但仍保留自动回退，避免单点代理抖动直接导致更新失败。
	if settings.GithubProxyEnabled && strings.TrimSpace(settings.GithubProxyURL) != "" {
		list = append(list, proxyCandidate{mode: "custom", proxy: strings.TrimSpace(settings.GithubProxyURL)})
	}

	autoCandidates := []proxyCandidate{
		{mode: "direct"},
		{mode: "fallback-7890", proxy: "http://127.0.0.1:7890"},
		{mode: "fallback-7897", proxy: "http://127.0.0.1:7897"},
	}
	if current := p.CurrentMode(); current != "" && current != "custom" {
		autoCandidates = prioritizeCandidate(autoCandidates, current)
	}
	list = append(list, autoCandidates...)
	return list
}

// doWithCandidate 使用单一候选通道发起请求。
func (p *ProxyManager) doWithCandidate(ctx context.Context, req *http.Request, candidate proxyCandidate, profile requestProfile) (*http.Response, error) {
	client, err := p.clientForCandidate(candidate, profile)
	if err != nil {
		return nil, err
	}
	cloned := req.Clone(ctx)
	resp, err := client.Do(cloned)
	if err != nil {
		return nil, err
	}
	if shouldRetryCandidate(resp.StatusCode) {
		err = describeGitHubFailure(resp)
		resp.Body.Close()
		return nil, err
	}
	if resp.StatusCode >= 500 {
		defer resp.Body.Close()
		return nil, fmt.Errorf("远端状态异常: %s", resp.Status)
	}
	return resp, nil
}

// clientForCandidate 返回可复用的 HTTP 客户端，减少代理场景下的重复握手开销。
func (p *ProxyManager) clientForCandidate(candidate proxyCandidate, profile requestProfile) (*http.Client, error) {
	key := clientCacheKey(candidate, profile)

	p.mu.RLock()
	client := p.clients[key]
	p.mu.RUnlock()
	if client != nil {
		return client, nil
	}

	transport, err := p.buildTransport(candidate, profile)
	if err != nil {
		return nil, err
	}
	client = &http.Client{Transport: transport}
	// 下载场景不设置总超时，只约束建连和首包，避免大文件读 body 时被客户端硬切断。
	if profile.totalTimeout > 0 {
		client.Timeout = profile.totalTimeout
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	// 并发场景下以首个成功创建的客户端为准，避免重复缓存同一配置。
	if cached := p.clients[key]; cached != nil {
		transport.CloseIdleConnections()
		return cached, nil
	}
	p.clients[key] = client
	return client, nil
}

// buildTransport 创建带代理策略的传输层配置。
func (p *ProxyManager) buildTransport(candidate proxyCandidate, profile requestProfile) (*http.Transport, error) {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 15 * time.Second,
		}).DialContext,
		ResponseHeaderTimeout: profile.responseHeaderTimeout,
		TLSHandshakeTimeout:   8 * time.Second,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          16,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       90 * time.Second,
	}

	// 自定义代理需要显式写入到 Transport，避免被环境变量覆盖。
	if candidate.proxy != "" {
		proxyURL, err := url.Parse(candidate.proxy)
		if err != nil {
			return nil, fmt.Errorf("代理地址无效: %w", err)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	// 直连模式强制关闭代理继承，确保不会意外走系统代理。
	if candidate.mode == "direct" {
		transport.Proxy = nil
	}

	return transport, nil
}

// downloadWithCandidate 使用单一通道执行下载，并对瞬时错误做有限重试。
func (p *ProxyManager) downloadWithCandidate(ctx context.Context, candidate proxyCandidate, requestURL string, headers map[string]string, writer io.Writer, progress func(DownloadProgress)) error {
	var lastErr error

	for attempt := 1; attempt <= downloadRetryLimit; attempt++ {
		if err := resetDownloadWriter(writer); err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return err
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}

		resp, err := p.doWithCandidate(ctx, req, candidate, downloadRequestProfile)
		if err != nil {
			lastErr = err
		} else {
			func() {
				defer resp.Body.Close()
				// 非 2xx 直接按 GitHub 错误处理，避免把 HTML 错页写入目标文件。
				if resp.StatusCode < 200 || resp.StatusCode >= 300 {
					err = describeGitHubFailure(resp)
					return
				}
				err = copyDownloadWithProgress(writer, resp.Body, resp.ContentLength, progress)
			}()
			if err == nil {
				return nil
			}
			lastErr = err
		}

		// 上下文已结束时直接停止，避免退出中的应用被无意义重试拖住。
		if ctx.Err() != nil {
			return lastErr
		}
		// 只有明显的瞬时下载错误才重试一次，其余错误直接切下一个通道。
		if !shouldRetryDownloadAttempt(lastErr) || attempt == downloadRetryLimit {
			return lastErr
		}
		time.Sleep(time.Duration(attempt) * 300 * time.Millisecond)
	}

	return lastErr
}

// shouldRetryCandidate 判断当前响应是否应切换到下一个通道重试。
func shouldRetryCandidate(statusCode int) bool {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusProxyAuthRequired:
		return true
	default:
		return false
	}
}

// shouldRetryDownloadAttempt 判断同一下载通道内是否值得再试一次。
func shouldRetryDownloadAttempt(err error) bool {
	if err == nil {
		return false
	}
	// 用户主动取消时不再重试，避免把退出操作变成等待。
	if errors.Is(err, context.Canceled) {
		return false
	}
	// 读流超时、连接抖动和意外截断通常是瞬时故障，重试一次成功率更高。
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// clientCacheKey 生成客户端缓存键，确保不同代理和超时配置互不污染。
func clientCacheKey(candidate proxyCandidate, profile requestProfile) string {
	return fmt.Sprintf("%s|%s|%d|%d", candidate.mode, candidate.proxy, profile.totalTimeout, profile.responseHeaderTimeout)
}

// closeIdleClientsLocked 关闭并清空当前缓存的空闲连接。
func (p *ProxyManager) closeIdleClientsLocked() {
	for key, client := range p.clients {
		if client == nil {
			continue
		}
		if transport, ok := client.Transport.(*http.Transport); ok {
			transport.CloseIdleConnections()
		}
		delete(p.clients, key)
	}
}

// describeGitHubFailure 生成更明确的 GitHub 请求错误。
func describeGitHubFailure(resp *http.Response) error {
	if resp == nil {
		return errors.New("请求失败")
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	message := strings.TrimSpace(string(body))
	remaining := strings.TrimSpace(resp.Header.Get("X-RateLimit-Remaining"))
	resetAt := strings.TrimSpace(resp.Header.Get("X-RateLimit-Reset"))

	switch resp.StatusCode {
	case http.StatusForbidden, http.StatusTooManyRequests:
		if remaining == "0" {
			if resetText := formatRateLimitReset(resetAt); resetText != "" {
				return fmt.Errorf("GitHub API 触发限流: %s，可在 %s 后重试", resp.Status, resetText)
			}
			return fmt.Errorf("GitHub API 触发限流: %s", resp.Status)
		}
		if message != "" {
			return fmt.Errorf("请求失败: %s %s", resp.Status, compactHTTPError(message))
		}
		return fmt.Errorf("请求失败: %s", resp.Status)
	default:
		if message != "" {
			return fmt.Errorf("请求失败: %s %s", resp.Status, compactHTTPError(message))
		}
		return fmt.Errorf("请求失败: %s", resp.Status)
	}
}

// compactHTTPError 压缩远端错误文案。
func compactHTTPError(message string) string {
	message = strings.Join(strings.Fields(message), " ")
	if len(message) > 180 {
		return message[:180] + "..."
	}
	return message
}

// formatRateLimitReset 格式化 GitHub 限流重置时间。
func formatRateLimitReset(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return ""
	}
	return time.Unix(seconds, 0).Local().Format("2006-01-02 15:04:05")
}

// resetDownloadWriter 在重试前清空目标写入器。
func resetDownloadWriter(writer io.Writer) error {
	truncater, canTruncate := writer.(interface{ Truncate(int64) error })
	seeker, canSeek := writer.(io.Seeker)
	if !canTruncate || !canSeek {
		return nil
	}
	if err := truncater.Truncate(0); err != nil {
		return fmt.Errorf("重置下载文件失败: %w", err)
	}
	if _, err := seeker.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("重置下载游标失败: %w", err)
	}
	return nil
}

// prioritizeCandidate 将最近成功的候选前置。
func prioritizeCandidate(candidates []proxyCandidate, mode string) []proxyCandidate {
	index := -1
	for i, candidate := range candidates {
		if candidate.mode == mode {
			index = i
			break
		}
	}
	if index <= 0 {
		return candidates
	}
	ordered := make([]proxyCandidate, 0, len(candidates))
	ordered = append(ordered, candidates[index])
	ordered = append(ordered, candidates[:index]...)
	ordered = append(ordered, candidates[index+1:]...)
	return ordered
}
