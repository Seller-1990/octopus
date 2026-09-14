package client

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"golang.org/x/net/proxy"
)

var (
	systemDirectClient *http.Client
	systemProxyClient  *http.Client
	systemProxyURL     string
	clientLock         sync.RWMutex
)

// GetHTTPClientSystemProxy returns a cached http.Client.
// - useProxy=false: bypass proxy
// - useProxy=true: use proxy settings from system/app settings (setting key: proxy_url)
func GetHTTPClientSystemProxy(useProxy bool) (*http.Client, error) {
	if useProxy {
		currentProxyURL, err := op.SettingGetString(model.SettingKeyProxyURL)
		if err != nil {
			return nil, err
		}
		if currentProxyURL == "" {
			// Fallback to environment variable if app setting is not configured
			if envProxy := os.Getenv("HTTPS_PROXY"); envProxy != "" {
				currentProxyURL = envProxy
			} else if envProxy = os.Getenv("HTTP_PROXY"); envProxy != "" {
				currentProxyURL = envProxy
			} else {
				return nil, fmt.Errorf("proxy url is empty")
			}
		}

		clientLock.RLock()
		if systemProxyClient != nil && systemProxyURL == currentProxyURL {
			clientLock.RUnlock()
			return systemProxyClient, nil
		}
		clientLock.RUnlock()

		clientLock.Lock()
		defer clientLock.Unlock()

		// Re-check after acquiring write lock.
		if systemProxyClient != nil && systemProxyURL == currentProxyURL {
			return systemProxyClient, nil
		}

		client, err := newHTTPClientCustomProxy(currentProxyURL)
		if err != nil {
			return nil, err
		}
		systemProxyClient = client
		systemProxyURL = currentProxyURL
		return systemProxyClient, nil
	}

	clientLock.RLock()
	if !useProxy && systemDirectClient != nil {
		clientLock.RUnlock()
		return systemDirectClient, nil
	}
	clientLock.RUnlock()

	clientLock.Lock()
	defer clientLock.Unlock()

	if systemDirectClient != nil {
		return systemDirectClient, nil
	}
	client, err := newHTTPClientNoProxy()
	if err != nil {
		return nil, err
	}
	systemDirectClient = client
	return systemDirectClient, nil
}

// customProxyClients 按完整 proxyURL（含凭据）缓存自定义代理客户端。
// 此前每次调用都 Transport.Clone 新建 client——Go 的 Transport.Clone 不复制
// 连接池，克隆体空池启动，连接完全无法复用：每个请求都完整 TCP+TLS 握手，
// 高 QPS 下客户端端口 TIME_WAIT 堆积直至 EADDRNOTAVAIL。键为 URL 天然兼容
// 池轮换与凭据变更（URL 变了就是新条目）。
var customProxyClients sync.Map // key: proxyURL string -> *http.Client

// GetHTTPClientCustomProxy returns a cached http.Client for the given proxy URL.
// proxyURL supports: http, https, socks, socks5
func GetHTTPClientCustomProxy(proxyURL string) (*http.Client, error) {
	if proxyURL == "" {
		return nil, fmt.Errorf("proxy url is empty")
	}
	if cached, ok := customProxyClients.Load(proxyURL); ok {
		return cached.(*http.Client), nil
	}
	client, err := newHTTPClientCustomProxy(proxyURL)
	if err != nil {
		return nil, err
	}
	actual, loaded := customProxyClients.LoadOrStore(proxyURL, client)
	if loaded {
		// 竞态落败方的 transport 丢弃前归还其空闲连接
		client.CloseIdleConnections()
	}
	return actual.(*http.Client), nil
}

// ResolveSystemProxyURL returns the effective system proxy URL from app settings or env.
// Returns empty string if no proxy is configured.
func ResolveSystemProxyURL() string {
	proxyURL, _ := op.SettingGetString(model.SettingKeyProxyURL)
	if proxyURL != "" {
		return proxyURL
	}
	if envProxy := os.Getenv("HTTPS_PROXY"); envProxy != "" {
		return envProxy
	}
	if envProxy := os.Getenv("HTTP_PROXY"); envProxy != "" {
		return envProxy
	}
	return ""
}

func clonedDefaultTransport() (*http.Transport, error) {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default transport is not *http.Transport")
	}
	cloned := transport.Clone()
	// 被墙/不可达站点会卡在 TCP 连接或 TLS 握手阶段（Go 默认无连接超时，
	// TLS 握手 10s），导致 Failover 前等待过长。这里应用可配置的短超时，
	// 让故障快速降级到下一个候选。
	cloned.DialContext = (&net.Dialer{
		Timeout:   conf.ClientDialTimeout(),
		KeepAlive: 30 * time.Second,
	}).DialContext
	cloned.TLSHandshakeTimeout = conf.ClientTLSHandshakeTimeout()
	return cloned, nil
}

func newHTTPClientNoProxy() (*http.Client, error) {
	cloned, err := clonedDefaultTransport()
	if err != nil {
		return nil, err
	}
	cloned.Proxy = nil
	return &http.Client{Transport: cloned}, nil
}

func newHTTPClientCustomProxy(proxyURLStr string) (*http.Client, error) {
	cloned, err := clonedDefaultTransport()
	if err != nil {
		return nil, err
	}

	proxyURL, err := url.Parse(proxyURLStr)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy url: %w", err)
	}

	switch proxyURL.Scheme {
	case "http", "https":
		cloned.Proxy = http.ProxyURL(proxyURL)
	case "socks", "socks5":
		// 前置拨号器带拨号超时（与直连路径一致），且用 ContextDialer 传播
		// ctx——旧实现 proxy.FromURL(_, proxy.Direct) + Dial(network, addr)
		// 无超时也不响应取消，代理被墙时拨号挂到系统级超时（分钟级），
		// Failover 被长时间阻塞。
		socksDialer, err := proxy.FromURL(proxyURL, &net.Dialer{
			Timeout:   conf.ClientDialTimeout(),
			KeepAlive: 30 * time.Second,
		})
		if err != nil {
			return nil, fmt.Errorf("invalid socks proxy: %w", err)
		}
		cloned.Proxy = nil
		if ctxDialer, ok := socksDialer.(proxy.ContextDialer); ok {
			cloned.DialContext = ctxDialer.DialContext
		} else {
			cloned.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				return socksDialer.Dial(network, addr)
			}
		}
	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s", proxyURL.Scheme)
	}

	return &http.Client{Transport: cloned}, nil
}
