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

type proxyCandidate struct {
	mode  string
	proxy string
}

// ProxyManager 负责 GitHub 请求代理选择与回退。
type ProxyManager struct {
	settings *SettingsStore
	logs     *LogBuffer
	mu       sync.RWMutex
	current  string
}

// NewProxyManager 创建代理管理器。
func NewProxyManager(settings *SettingsStore, logs *LogBuffer) *ProxyManager {
	return &ProxyManager{
		settings: settings,
		logs:     logs,
		current:  "direct",
	}
}

// Reset 清除当前成功通道缓存。
func (p *ProxyManager) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current = "direct"
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
		resp, err := p.doWithCandidate(ctx, req, candidate)
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
func (p *ProxyManager) Download(ctx context.Context, requestURL string, headers map[string]string, writer io.Writer) (string, error) {
	candidates := p.candidates()
	var errs []error

	for _, candidate := range candidates {
		if err := resetDownloadWriter(writer); err != nil {
			return "", err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return "", err
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}

		resp, err := p.doWithCandidate(ctx, req, candidate)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", candidate.mode, err))
			continue
		}

		func() {
			defer resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				err = describeGitHubFailure(resp)
				return
			}
			_, err = io.Copy(writer, resp.Body)
		}()

		if err == nil {
			p.mu.Lock()
			p.current = candidate.mode
			p.mu.Unlock()
			return candidate.mode, nil
		}

		errs = append(errs, fmt.Errorf("%s: %w", candidate.mode, err))
	}

	if len(errs) == 0 {
		return "", errors.New("没有可用的 GitHub 下载通道")
	}
	return "", errors.Join(errs...)
}

// candidates 计算代理尝试顺序。
func (p *ProxyManager) candidates() []proxyCandidate {
	settings := p.settings.NetworkSettings()
	list := make([]proxyCandidate, 0, 4)

	if settings.GithubProxyEnabled && strings.TrimSpace(settings.GithubProxyURL) != "" {
		return []proxyCandidate{{mode: "custom", proxy: strings.TrimSpace(settings.GithubProxyURL)}}
	}

	list = []proxyCandidate{
		{mode: "direct"},
		{mode: "fallback-7890", proxy: "http://127.0.0.1:7890"},
		{mode: "fallback-7897", proxy: "http://127.0.0.1:7897"},
	}
	if current := p.CurrentMode(); current != "" && current != "custom" {
		list = prioritizeCandidate(list, current)
	}
	return list
}

// doWithCandidate 使用单一候选通道发起请求。
func (p *ProxyManager) doWithCandidate(ctx context.Context, req *http.Request, candidate proxyCandidate) (*http.Response, error) {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 15 * time.Second,
		}).DialContext,
		ResponseHeaderTimeout: 12 * time.Second,
		TLSHandshakeTimeout:   8 * time.Second,
		ForceAttemptHTTP2:     true,
	}

	if candidate.proxy != "" {
		proxyURL, err := url.Parse(candidate.proxy)
		if err != nil {
			return nil, fmt.Errorf("代理地址无效: %w", err)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	if candidate.mode == "direct" {
		transport.Proxy = nil
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   20 * time.Second,
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

// shouldRetryCandidate 判断当前响应是否应切换到下一个通道重试。
func shouldRetryCandidate(statusCode int) bool {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusProxyAuthRequired:
		return true
	default:
		return false
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
