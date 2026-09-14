package middleware

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// isHashedAsset checks if a path points to a content-hashed static asset
// that can be safely cached with immutable headers (e.g. _next/static/chunks/abc123.js).
func isHashedAsset(path string) bool {
	return strings.Contains(path, "/_next/static/") ||
		strings.Contains(path, "/static/chunks/") ||
		strings.Contains(path, "/static/css/") ||
		strings.Contains(path, "/static/media/")
}

func StaticEmbed(urlPrefix string, embedFS fs.FS) gin.HandlerFunc {
	fs := http.FS(embedFS)
	return static(urlPrefix, fs)
}

func static(urlPrefix string, fileSystem http.FileSystem) gin.HandlerFunc {
	fileserver := http.FileServer(fileSystem)
	if urlPrefix != "" {
		fileserver = http.StripPrefix(urlPrefix, fileserver)
	}
	return func(c *gin.Context) {
		// /api 管理面与 /v1 代理面都不是静态资源：跳过内嵌 FS 的 Open 探测，
		// 否则代理热路径每请求多一次注定失败的 embed.FS 打开（F15）。
		if strings.HasPrefix(c.Request.URL.Path, "/api") || strings.HasPrefix(c.Request.URL.Path, "/v1") {
			c.Next()
			return
		}
		if _, err := fileSystem.Open(c.Request.URL.Path); err == nil {
			if isHashedAsset(c.Request.URL.Path) {
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				c.Header("Cache-Control", "no-cache")
			}
			fileserver.ServeHTTP(c.Writer, c.Request)
			c.Abort()
		}
	}
}
