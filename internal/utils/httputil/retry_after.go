// Package httputil 承载 HTTP 头解析类的跨模块共享逻辑。
package httputil

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ParseRetryAfter 解析 Retry-After 头（秒数或 HTTP 日期两种格式），非正值归
// 零，超过 max 封顶。relay（退避重试）与 sitesync（同步退避）此前各有一份
// 逐字等价的实现（C250913-08），连 60 秒封顶值都相同。
func ParseRetryAfter(header string, now time.Time, max time.Duration) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}
	if secs, err := strconv.Atoi(header); err == nil {
		return boundRetryAfter(time.Duration(secs)*time.Second, max)
	}
	retryAt, err := http.ParseTime(header)
	if err != nil {
		return 0
	}
	return boundRetryAfter(retryAt.Sub(now), max)
}

// BoundRetryAfter 对任意来源的延迟值做统一钳制：非正值归零，超上限封顶。
// sitesync 的 retryAfterMillis 参数路径没有头部字符串可解析，直接复用钳制。
func BoundRetryAfter(delay, max time.Duration) time.Duration {
	return boundRetryAfter(delay, max)
}

func boundRetryAfter(delay, max time.Duration) time.Duration {
	if delay <= 0 {
		return 0
	}
	if delay > max {
		return max
	}
	return delay
}
