package helper

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/bestruirui/octopus/internal/client"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
	tls_client "github.com/bogdanfinn/tls-client"
)

func ChannelHttpClient(channel *model.Channel) (*http.Client, error) {
	return ChannelHTTPClientWithContext(context.Background(), channel)
}

func ChannelHTTPClientWithContext(ctx context.Context, channel *model.Channel) (*http.Client, error) {
	if channel == nil {
		return nil, errors.New("channel is nil")
	}
	switch channel.ProxyMode {
	case "", model.ProxyUsageModeDirect:
		return client.GetHTTPClientSystemProxy(false)
	case model.ProxyUsageModeSystem:
		return client.GetHTTPClientSystemProxy(true)
	case model.ProxyUsageModePool:
		if channel.ProxyConfigID == nil || *channel.ProxyConfigID <= 0 {
			return nil, fmt.Errorf("proxy config id is required when proxy mode is pool")
		}
		proxyURL, err := op.ProxyURLForConfig(*channel.ProxyConfigID, ctx)
		if err != nil {
			return nil, err
		}
		return client.GetHTTPClientCustomProxy(proxyURL)
	default:
		return nil, fmt.Errorf("unsupported proxy mode: %s", channel.ProxyMode)
	}
}

// ChannelProxyURL resolves the effective proxy URL for a channel.
// Empty string means direct connection (no proxy).
func ChannelProxyURL(ctx context.Context, channel *model.Channel) (string, error) {
	if channel == nil {
		return "", errors.New("channel is nil")
	}
	switch channel.ProxyMode {
	case "", model.ProxyUsageModeDirect:
		return "", nil
	case model.ProxyUsageModeSystem:
		proxyURL := client.ResolveSystemProxyURL()
		if proxyURL == "" {
			return "", fmt.Errorf("proxy url is empty")
		}
		return proxyURL, nil
	case model.ProxyUsageModePool:
		if channel.ProxyConfigID == nil || *channel.ProxyConfigID <= 0 {
			return "", fmt.Errorf("proxy config id is required when proxy mode is pool")
		}
		return op.ProxyURLForConfig(*channel.ProxyConfigID, ctx)
	default:
		return "", fmt.Errorf("unsupported proxy mode: %s", channel.ProxyMode)
	}
}

type fingerprintedClientKey struct {
	fingerprint string
	proxyURL    string
}

var fingerprintedClients sync.Map // fingerprintedClientKey -> tls_client.HttpClient

// ChannelFingerprintedClient returns a cached TLS-fingerprinted client for
// channels that opt into browser TLS fingerprint spoofing.
func ChannelFingerprintedClient(ctx context.Context, channel *model.Channel) (tls_client.HttpClient, error) {
	if channel == nil {
		return nil, errors.New("channel is nil")
	}
	if channel.TLSFingerprint == "" {
		return nil, errors.New("tls fingerprint is required")
	}
	proxyURL, err := ChannelProxyURL(ctx, channel)
	if err != nil {
		return nil, err
	}

	key := fingerprintedClientKey{fingerprint: channel.TLSFingerprint, proxyURL: proxyURL}
	if cached, ok := fingerprintedClients.Load(key); ok {
		return cached.(tls_client.HttpClient), nil
	}

	fpClient, err := client.NewFingerprintedClient(channel.TLSFingerprint, proxyURL)
	if err != nil {
		return nil, err
	}
	actual, _ := fingerprintedClients.LoadOrStore(key, fpClient)
	return actual.(tls_client.HttpClient), nil
}

func ChannelBaseUrlDelayUpdate(channel *model.Channel, ctx context.Context) {
	if channel == nil {
		return
	}
	newBaseUrls := make([]model.BaseUrl, 0, len(channel.BaseUrls))
	for _, baseUrl := range channel.BaseUrls {
		if baseUrl.URL == "" {
			continue
		}
		var httpClient *http.Client
		var err error
		if channel.TLSFingerprint != "" {
			proxyURL, proxyErr := ChannelProxyURL(ctx, channel)
			if proxyErr != nil {
				log.Warnf("failed to resolve channel proxy (channel=%d): %v", channel.ID, proxyErr)
				continue
			}
			httpClient, err = client.GetHTTPClientFingerprinted(channel.TLSFingerprint, proxyURL)
		} else {
			httpClient, err = ChannelHTTPClientWithContext(ctx, channel)
		}
		if err != nil {
			log.Warnf("failed to get http client (channel=%d): %v", channel.ID, err)
			continue
		}
		delay, err := GetUrlDelay(httpClient, baseUrl.URL, ctx)
		if err != nil {
			log.Warnf("failed to get url delay (channel=%d): %v", channel.ID, err)
			continue
		}
		newBaseUrls = append(newBaseUrls, model.BaseUrl{
			URL:   baseUrl.URL,
			Delay: delay,
		})
	}
	if len(newBaseUrls) > 0 {
		op.ChannelBaseUrlUpdate(channel.ID, newBaseUrls)
	}
}

func ChannelAutoGroup(channel *model.Channel, ctx context.Context) {
	op.ChannelAutoGroup(channel, ctx)
}
