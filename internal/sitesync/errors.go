package sitesync

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/apperror"
)

type CloudflareProtectionError struct {
	StatusCode int
	RetryAfter time.Duration
	Message    string
}

func (e *CloudflareProtectionError) Error() string {
	if e == nil {
		return ""
	}
	if e.RetryAfter > 0 {
		return fmt.Sprintf("http %d: %s，建议 %s 后重试", e.StatusCode, e.Message, e.RetryAfter)
	}
	return fmt.Sprintf("http %d: %s", e.StatusCode, e.Message)
}

func IsCloudflareProtectionError(err error) bool {
	var cfErr *CloudflareProtectionError
	return errors.As(err, &cfErr)
}

func CloudflareRetryAfter(err error) time.Duration {
	var cfErr *CloudflareProtectionError
	if errors.As(err, &cfErr) && cfErr != nil {
		return cfErr.RetryAfter
	}
	return 0
}

func newCloudflareProtectionError(statusCode int, header http.Header) *CloudflareProtectionError {
	return &CloudflareProtectionError{
		StatusCode: statusCode,
		RetryAfter: parseSiteRetryAfter(header.Get("Retry-After")),
		Message:    "站点触发 Cloudflare 保护，请稍后重试，或手动访问站点完成验证/联系站点管理员放行",
	}
}

func parseSiteRetryAfter(header string) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}
	if secs, err := strconv.Atoi(header); err == nil {
		return boundSiteRetryAfter(time.Duration(secs) * time.Second)
	}
	parsed, err := http.ParseTime(header)
	if err != nil {
		return 0
	}
	return boundSiteRetryAfter(time.Until(parsed))
}

func boundSiteRetryAfter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return 0
	}
	if delay > 60*time.Second {
		return 60 * time.Second
	}
	return delay
}

func siteErrorRetryAfter(err error) time.Duration {
	millis := anyToInt64(apperror.Params(err)["retryAfterMillis"])
	return boundSiteRetryAfter(time.Duration(millis) * time.Millisecond)
}
