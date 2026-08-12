package visionbridge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// ImageRef 描述请求中一个 image_url 内容块的位置与身份。
type ImageRef struct {
	MessageIndex int
	PartIndex    int
	URL          string // 原始引用（data URI 或 http(s) URL）
	IsDataURI    bool
	Bytes        int    // 估算字节数（data URI 解码后大小；URL 计引用长度）
	Identity     string // 缓存身份：data URI 取 payload 的 sha256，URL 取 URL 本身
}

// Discover 扫描请求全部消息（含 tool 角色）收集 image_url 块，逐个做安全校验，
// 并施加张数与总字节上限。任一图片非法即整体报错（fail-closed，不做部分替换）。
func Discover(req *model.InternalLLMRequest, cfg Config) ([]ImageRef, error) {
	if req == nil {
		return nil, nil
	}
	var refs []ImageRef
	totalBytes := 0
	for i := range req.Messages {
		parts := req.Messages[i].Content.MultipleContent
		for j := range parts {
			if parts[j].Type != "image_url" || parts[j].ImageURL == nil || parts[j].ImageURL.URL == "" {
				continue
			}
			raw := parts[j].ImageURL.URL
			size, err := ValidateImageReference(raw, cfg.MaxImageReferenceBytes)
			if err != nil {
				return nil, fmt.Errorf("image %d (message %d): %w", len(refs)+1, i, err)
			}
			refs = append(refs, ImageRef{
				MessageIndex: i,
				PartIndex:    j,
				URL:          raw,
				IsDataURI:    strings.HasPrefix(raw, "data:"),
				Bytes:        size,
				Identity:     imageIdentity(raw),
			})
			totalBytes += size
		}
	}
	if len(refs) == 0 {
		return nil, nil
	}
	if cfg.MaxImagesPerRequest > 0 && len(refs) > cfg.MaxImagesPerRequest {
		return nil, fmt.Errorf("too many images: %d > %d", len(refs), cfg.MaxImagesPerRequest)
	}
	if cfg.MaxRequestBytes > 0 && totalBytes > cfg.MaxRequestBytes {
		return nil, fmt.Errorf("images exceed total size limit: ~%d > %d bytes", totalBytes, cfg.MaxRequestBytes)
	}
	return refs, nil
}

func imageIdentity(raw string) string {
	if _, payload, ok := strings.Cut(raw, ","); ok && strings.HasPrefix(raw, "data:") {
		sum := sha256.Sum256([]byte(payload))
		return "sha256:" + hex.EncodeToString(sum[:])
	}
	return raw
}
