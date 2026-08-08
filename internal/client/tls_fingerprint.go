package client

import (
	"fmt"
	"net/http"

	fhttp "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

const (
	TLSFingerprintChrome  = "chrome"
	TLSFingerprintFirefox = "firefox"
)

func resolveTLSProfile(fingerprint string) profiles.ClientProfile {
	switch fingerprint {
	case TLSFingerprintFirefox:
		return profiles.Firefox_135
	default:
		return profiles.Chrome_131
	}
}

// GetHTTPClientFingerprinted returns an http.Client with browser TLS fingerprint.
// Composes with proxy: if proxyURL is non-empty, requests are tunneled through it.
func GetHTTPClientFingerprinted(fingerprint string, proxyURL string) (*http.Client, error) {
	if fingerprint == "" {
		return nil, fmt.Errorf("fingerprint is required")
	}

	options := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(30),
		tls_client.WithClientProfile(resolveTLSProfile(fingerprint)),
		tls_client.WithNotFollowRedirects(),
		tls_client.WithInsecureSkipVerify(),
	}

	if proxyURL != "" {
		options = append(options, tls_client.WithProxyUrl(proxyURL))
	}

	client, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(), options...)
	if err != nil {
		return nil, fmt.Errorf("create fingerprinted client: %w", err)
	}

	return &http.Client{
		Transport: &fingerprintedTransport{client: client},
	}, nil
}

// fingerprintedTransport adapts tls_client.HttpClient to http.RoundTripper
type fingerprintedTransport struct {
	client tls_client.HttpClient
}

func (t *fingerprintedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Convert net/http.Request -> fhttp.Request
	fReq := &fhttp.Request{
		Method: req.Method,
		URL:    req.URL,
		Body:   req.Body,
		Host:   req.Host,
	}
	if req.Proto != "" {
		fReq.Proto = req.Proto
		fReq.ProtoMajor = req.ProtoMajor
		fReq.ProtoMinor = req.ProtoMinor
	} else {
		fReq.Proto = "HTTP/1.1"
		fReq.ProtoMajor = 1
		fReq.ProtoMinor = 1
	}
	if req.ContentLength > 0 {
		fReq.ContentLength = req.ContentLength
	}

	// Copy headers
	fReq.Header = make(fhttp.Header)
	for key, values := range req.Header {
		for _, value := range values {
			fReq.Header.Add(key, value)
		}
	}

	// Execute
	fResp, err := t.client.Do(fReq)
	if err != nil {
		return nil, err
	}

	// Convert fhttp.Response -> net/http.Response
	resp := &http.Response{
		Status:        fResp.Status,
		StatusCode:    fResp.StatusCode,
		Proto:         fResp.Proto,
		ProtoMajor:    fResp.ProtoMajor,
		ProtoMinor:    fResp.ProtoMinor,
		ContentLength: fResp.ContentLength,
		Uncompressed:  fResp.Uncompressed,
		Body:          fResp.Body,
		Request:       req,
	}
	resp.Header = make(http.Header)
	for key, values := range fResp.Header {
		for _, value := range values {
			resp.Header.Add(key, value)
		}
	}
	if len(fResp.TransferEncoding) > 0 {
		resp.TransferEncoding = fResp.TransferEncoding
	}

	return resp, nil
}

var _ http.RoundTripper = (*fingerprintedTransport)(nil)
