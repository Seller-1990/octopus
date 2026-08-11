package update

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/client"
	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/utils/log"
	"golang.org/x/net/proxy"
)

type LatestInfo struct {
	TagName     string `json:"tag_name"`
	PublishedAt string `json:"published_at"`
	Body        string `json:"body"`
	Message     string `json:"message"`
	ReleaseURL  string `json:"release_url"`
	// Container 标记当前运行在容器中，前端据此禁用自动更新入口。
	Container bool `json:"container"`
	// AutoUpdate 标记当前发行形态是否支持安全的原地二进制更新。
	AutoUpdate bool `json:"auto_update"`
}

var github_pat = os.Getenv(strings.ToUpper(conf.APP_NAME) + "_GITHUB_PAT")

func latestReleaseAPIURL() string {
	return "https://api.github.com/repos/" + conf.GitHubRepository + "/releases/latest"
}

func releaseDownloadURL(filename string) string {
	return strings.TrimRight(conf.Repo, "/") + "/releases/latest/download/" + filename
}

// isReleaseDownloadURL 判断是否为 release 二进制下载 URL（github.com 而非 api.github.com）。
// api.github.com 的 releases/latest 是版本检查（保留 HTTP/2）；github.com/releases/.../download
// 是大文件下载（用 HTTP/1.1 规避 HTTP/2 代理流中断）。
func isReleaseDownloadURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return strings.Contains(rawURL, "/releases/") && !strings.Contains(rawURL, "api.github.com")
	}
	return strings.EqualFold(parsed.Host, "github.com") && strings.Contains(parsed.Path, "/releases/")
}

// doRequestWithFallback performs an HTTP GET request, first without proxy, then with proxy if failed.
func doRequestWithFallback(url string) ([]byte, error) {
	data, err := doRequest(url, false)
	if err == nil {
		return data, nil
	}
	log.Warnf("direct request failed, trying with proxy: %v", err)
	return doRequest(url, true)
}

// newDownloadClient 构建下载专用 HTTP client（禁用 HTTP/2）。
// 修复（2026-08-11）：大文件下载（60MB release 二进制）经 HTTP/2 代理传输时
// 流易中断（`PROTOCOL_ERROR` / SSL_ERROR_SYSCALL），一键更新反复失败。
// HTTP/1.1 对长连接/代理更稳，规避 HTTP/2 多路复用的流中断问题。
// 注意：仅用于 releases 下载（github.com），版本检查（api.github.com）保留 HTTP/2——
// api.github.com 对 Go HTTP/1.1 稳定 EOF（实测），不得一刀切。
func newDownloadClient(useProxy bool) (*http.Client, error) {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default transport is not *http.Transport")
	}
	cloned := transport.Clone()
	cloned.ForceAttemptHTTP2 = false // 关键：禁用 HTTP/2 协商
	cloned.TLSNextProto = make(map[string]func(string, *tls.Conn) http.RoundTripper)
	if useProxy {
		proxyURL := client.ResolveSystemProxyURL()
		if proxyURL != "" {
			u, err := url.Parse(proxyURL)
			if err != nil {
				return nil, fmt.Errorf("invalid proxy url: %w", err)
			}
			switch u.Scheme {
			case "http", "https":
				cloned.Proxy = http.ProxyURL(u)
			case "socks", "socks5":
				d, err := proxy.FromURL(u, proxy.Direct)
				if err != nil {
					return nil, fmt.Errorf("invalid socks proxy: %w", err)
				}
				cloned.Proxy = nil
				cloned.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
					return d.Dial(network, addr)
				}
			default:
				return nil, fmt.Errorf("unsupported proxy scheme: %s", u.Scheme)
			}
		}
	} else {
		cloned.Proxy = nil
	}
	return &http.Client{Transport: cloned}, nil
}

func doRequest(url string, useProxy bool) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// 按主机选择协议：github.com（release 下载）用 HTTP/1.1（规避 HTTP/2 代理流中断）；
	// api.github.com（版本检查）保留 HTTP/2（HTTP/1.1 对 Go EOF，实测）。
	var hc *http.Client
	var err error
	if isReleaseDownloadURL(url) {
		hc, err = newDownloadClient(useProxy)
	} else {
		hc, err = client.GetHTTPClientSystemProxy(useProxy)
	}
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		log.Debugf("new request failed: %v", err)
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Octopus-update/1.0)")

	if github_pat != "" {
		req.Header.Set("Authorization", "Bearer "+github_pat)
	}

	resp, err := hc.Do(req)
	if err != nil {
		log.Debugf("request failed: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Debugf("HTTP %d from %s: %s", resp.StatusCode, url, string(body[:min(len(body), 200)]))
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Debugf("read body failed: %v", err)
		return nil, err
	}
	return data, nil
}

func GetLatestInfo() (*LatestInfo, error) {
	body, err := doRequestWithFallback(latestReleaseAPIURL())
	if err != nil {
		return nil, err
	}

	var latestInfo LatestInfo
	if err := json.Unmarshal(body, &latestInfo); err != nil {
		log.Debugf("unmarshal body failed: %v", err)
		return nil, err
	}
	if latestInfo.Message != "" {
		return nil, fmt.Errorf("failed to get latest info: %s", latestInfo.Message)
	}
	latestInfo.Container = InContainer()
	latestInfo.AutoUpdate = AutoUpdateSupported()
	latestInfo.ReleaseURL = strings.TrimRight(conf.Repo, "/") + "/releases/latest"
	return &latestInfo, nil
}

func unzip(data []byte, dest string) error {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		log.Debugf("new zip reader failed: %v", err)
		return err
	}

	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)

		if !isPathInDest(fpath, dest) {
			log.Debugf("invalid file path: %s", fpath)
			return fmt.Errorf("invalid file path: %s", fpath)
		}

		info := f.FileInfo()
		if info.IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}

		if err := extractFile(f, fpath); err != nil {
			return err
		}
	}
	return nil
}

func extractFile(f *zip.File, fpath string) error {
	if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
		log.Debugf("mkdir all failed: %v", err)
		return err
	}

	outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode().Perm())
	if err != nil {
		if err = os.Remove(fpath); err != nil {
			log.Debugf("remove file failed: %v", err)
			return err
		}
		outFile, err = os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			log.Debugf("open file failed: %v", err)
			return err
		}
	}
	defer outFile.Close()

	rc, err := f.Open()
	if err != nil {
		log.Debugf("open file failed: %v", err)
		return err
	}
	defer rc.Close()

	if _, err = io.Copy(outFile, rc); err != nil {
		log.Debugf("copy failed: %v", err)
		return err
	}
	return nil
}

func isPathInDest(fpath, dest string) bool {
	rel, err := filepath.Rel(dest, fpath)
	if err != nil {
		return false
	}
	return filepath.IsLocal(rel)
}
