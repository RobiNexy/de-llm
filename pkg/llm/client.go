// client.go 实现带重试与限流的 HTTP 客户端（设计文档 §7.11）。
package llm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter 解析 "60/minute" 形式的限流配置。
type RateLimiter struct {
	limiter *rate.Limiter
}

// NewRateLimiter 按 "N/unit" 解析；unit ∈ second|minute|hour。空配置不限流。
func NewRateLimiter(spec string) (*RateLimiter, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}
	var n float64
	var unit string
	if _, err := fmt.Sscanf(spec, "%f/%s", &n, &unit); err != nil {
		return nil, fmt.Errorf("rate_limit %q 解析失败（期望如 60/minute）", spec)
	}
	if n <= 0 {
		return nil, fmt.Errorf("rate_limit %q 必须为正数", spec)
	}
	var per time.Duration
	switch unit {
	case "second", "seconds", "s":
		per = time.Second
	case "minute", "minutes", "m":
		per = time.Minute
	case "hour", "hours", "h":
		per = time.Hour
	default:
		return nil, fmt.Errorf("rate_limit %q 的时间单位不支持（second/minute/hour）", spec)
	}
	interval := time.Duration(float64(per) / n)
	return &RateLimiter{limiter: rate.NewLimiter(rate.Every(interval), 1)}, nil
}

func (r *RateLimiter) Wait(ctx context.Context) error {
	if r == nil || r.limiter == nil {
		return nil
	}
	return r.limiter.Wait(ctx)
}

// DoWithRetry 发送 HTTP 请求：429/5xx 指数退避重试，尊重 Retry-After。
// 独立于具体 provider 协议，供 providers 包复用。
func DoWithRetry(ctx context.Context, client *http.Client, req *http.Request, body []byte, maxRetries int) (*http.Response, error) {
	var lastErr error
	var retryAfter string
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := backoffDelay(attempt, retryAfter)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		// 每次重试需重放 body（NewRequest 对 bytes.Reader 自动设置 GetBody）。
		if body != nil && req.GetBody != nil {
			b, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			req.Body = b
		}
		resp, err := client.Do(req)
		if err != nil {
			// 网络错误：可重试。
			lastErr = err
			retryAfter = ""
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			retryAfter = resp.Header.Get("Retry-After")
			io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			continue
		}
		retryAfter = ""
		return resp, nil
	}
	return nil, fmt.Errorf("重试 %d 次后仍失败: %w", maxRetries, lastErr)
}

// backoffDelay 计算指数退避：base * 2^(attempt-1)，尊重 Retry-After。
func backoffDelay(attempt int, retryAfter string) time.Duration {
	if retryAfter != "" {
		var secs int
		if _, err := fmt.Sscanf(retryAfter, "%d", &secs); err == nil && secs > 0 && secs < 300 {
			return time.Duration(secs) * time.Second
		}
	}
	base := 500 * time.Millisecond
	for i := 1; i < attempt && base < 8*time.Second; i++ {
		base *= 2
	}
	return base
}
