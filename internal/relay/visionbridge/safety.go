// Package visionbridge 把发往纯文本通道模型的图片内容替换为 VLM 生成的文本描述。
//
// 背景（docs/reviews/vision-bridge-step0a-report.md）：纯文本上游收到 image_url 时
// 100% 静默降质（HTTP 200 + 空 choices / 挂起 / 拒答），不会返回干净的 400，
// 因此触发与成败判定都不依赖上游状态码，而是按模型能力位前置判断。
package visionbridge

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ValidateImageReference 校验单个图片引用并返回估算字节数。
// 只接受 data:image/*;base64 与 http(s) URL；拒绝 file:// 等其他 scheme、
// 内网/loopback/link-local 地址（防 SSRF——URL 由 VLM 侧抓取，此处做纵深防御）。
func ValidateImageReference(ref string, maxBytes int) (int, error) {
	if strings.HasPrefix(ref, "data:") {
		return validateDataURI(ref, maxBytes)
	}
	return validateHTTPURL(ref)
}

func validateDataURI(ref string, maxBytes int) (int, error) {
	meta, payload, ok := strings.Cut(ref, ",")
	if !ok {
		return 0, fmt.Errorf("malformed data URI")
	}
	meta = strings.TrimPrefix(meta, "data:")
	mediaType, encoding, _ := strings.Cut(meta, ";")
	if !strings.EqualFold(encoding, "base64") {
		return 0, fmt.Errorf("data URI must be base64-encoded")
	}
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	subtype, found := strings.CutPrefix(mediaType, "image/")
	if !found || subtype == "" || strings.ContainsAny(subtype, "*;") {
		return 0, fmt.Errorf("unsupported media type %q (require concrete image/*)", mediaType)
	}
	// 只按长度估算并抽样解码头部校验合法性，避免为超限图分配整块内存
	estimated := base64.StdEncoding.DecodedLen(len(payload))
	if maxBytes > 0 && estimated > maxBytes {
		return estimated, fmt.Errorf("image exceeds size limit: ~%d > %d bytes", estimated, maxBytes)
	}
	sample := payload
	if len(sample) > 4096 {
		sample = sample[:4096]
	}
	sample = sample[:len(sample)-len(sample)%4]
	if _, err := base64.StdEncoding.DecodeString(sample); err != nil {
		return 0, fmt.Errorf("invalid base64 payload: %w", err)
	}
	return estimated, nil
}

func validateHTTPURL(ref string) (int, error) {
	u, err := url.Parse(ref)
	if err != nil {
		return 0, fmt.Errorf("invalid image URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return 0, fmt.Errorf("unsupported URL scheme %q", u.Scheme)
	}
	if u.User != nil {
		return 0, fmt.Errorf("image URL must not carry userinfo")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return 0, fmt.Errorf("image URL missing host")
	}
	if isForbiddenHost(host) {
		return 0, fmt.Errorf("image URL host %q not allowed", host)
	}
	return len(ref), nil
}

func isForbiddenHost(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}
