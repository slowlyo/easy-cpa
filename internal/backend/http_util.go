package backend

import (
	"context"
	"fmt"
	"net/http"
)

// httpNewRequest 构建带默认头的 HTTP 请求。
func httpNewRequest(ctx context.Context, requestURL string, headers map[string]string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	return req, nil
}
