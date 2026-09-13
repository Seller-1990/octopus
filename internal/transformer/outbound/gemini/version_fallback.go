package gemini

import (
	"net/url"
	"strings"
)

// AppendVersionFallback returns baseURL with a `/v1beta` path suffix when the
// path lacks a Gemini API version segment. This is the G-H5 fallback rule
// (see messages.go) exported for non-chat callers — model listing in
// internal/helper/fetch.go builds `base + /models` on its own and must land
// on the same versioned path as chat, otherwise bare-hostname channels chat
// fine but 404 on model listing.
func AppendVersionFallback(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSuffix(baseURL, "/"))
	if err != nil {
		return baseURL, err
	}
	if pathHasGeminiVersion(parsed.Path) {
		return parsed.String(), nil
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1beta"
	return parsed.String(), nil
}
