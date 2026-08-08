package client

import (
	"fmt"
	"io"
	"net/http"
	"sort"

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
		return profiles.Firefox_148
	default:
		return profiles.Chrome_146
	}
}

// chromeHeaderOrder defines the canonical header ordering that Chrome uses.
// Headers not in this list are appended alphabetically after the known ones.
var chromeHeaderOrder = []string{
	"Host",
	"User-Agent",
	"Accept",
	"Accept-Language",
	"Accept-Encoding",
	"Content-Type",
	"Content-Length",
	"Authorization",
	"Cookie",
}

// NewFingerprintedClient returns the raw tls-client HttpClient with browser TLS fingerprint.
// Use this with DoFingerprintedRequest to preserve header ordering.
func NewFingerprintedClient(fingerprint string, proxyURL string) (tls_client.HttpClient, error) {
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

	return client, nil
}

// DoFingerprintedRequest executes an HTTP request using the raw tls-client, preserving
// header ordering in a Chrome-like sequence to avoid Cloudflare bot detection.
// The headers map is applied with deterministic browser-like ordering.
func DoFingerprintedRequest(client tls_client.HttpClient, method, url string, body io.Reader, headers map[string]string) (*http.Response, error) {
	fReq, err := fhttp.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	// Build header with explicit ordering
	fReq.Header = make(fhttp.Header)
	orderSlice := buildHeaderOrder(headers)

	for _, key := range orderSlice {
		if val, ok := headers[key]; ok {
			fReq.Header.Set(key, val)
		}
	}
	fReq.Header[fhttp.HeaderOrderKey] = orderSlice

	// Set PseudoHeaderOrder for HTTP/2 (Chrome-like)
	fReq.Header[fhttp.PHeaderOrderKey] = []string{
		":method",
		":authority",
		":scheme",
		":path",
	}

	fResp, err := client.Do(fReq)
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

// buildHeaderOrder returns header keys ordered in Chrome-like sequence.
// Known headers appear in chromeHeaderOrder; unknown headers are appended alphabetically.
func buildHeaderOrder(headers map[string]string) []string {
	var ordered []string
	seen := make(map[string]bool, len(headers))

	// Add known headers in Chrome order (only if present)
	for _, key := range chromeHeaderOrder {
		if _, ok := headers[key]; ok {
			ordered = append(ordered, key)
			seen[key] = true
		}
	}

	// Collect remaining headers and sort alphabetically for determinism
	var remaining []string
	for key := range headers {
		if !seen[key] {
			remaining = append(remaining, key)
		}
	}
	sort.Strings(remaining)
	ordered = append(ordered, remaining...)

	return ordered
}

// GetHTTPClientFingerprinted returns an http.Client with browser TLS fingerprint.
// Note: This adapter loses header ordering during the net/http → fhttp conversion.
// For Cloudflare-protected endpoints, use NewFingerprintedClient + DoFingerprintedRequest instead.
func GetHTTPClientFingerprinted(fingerprint string, proxyURL string) (*http.Client, error) {
	client, err := NewFingerprintedClient(fingerprint, proxyURL)
	if err != nil {
		return nil, err
	}

	return &http.Client{
		Transport: &fingerprintedTransport{client: client},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

// fingerprintedTransport adapts tls_client.HttpClient to http.RoundTripper.
// This loses header ordering — kept for backward compatibility only.
type fingerprintedTransport struct {
	client tls_client.HttpClient
}

func (t *fingerprintedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var bodyReader io.Reader
	if req.Body != nil {
		bodyReader = req.Body
	}

	fReq, err := fhttp.NewRequest(req.Method, req.URL.String(), bodyReader)
	if err != nil {
		return nil, err
	}

	fReq.Header = make(fhttp.Header)
	for key, values := range req.Header {
		for _, value := range values {
			fReq.Header.Add(key, value)
		}
	}

	if req.Host != "" {
		fReq.Host = req.Host
	}
	if req.ContentLength > 0 {
		fReq.ContentLength = req.ContentLength
	}

	fResp, err := t.client.Do(fReq)
	if err != nil {
		return nil, err
	}

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
