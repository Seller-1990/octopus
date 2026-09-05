package client

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFingerprintedClientsRejectUntrustedTLS(t *testing.T) {
	var received atomic.Int32
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		received.Add(1)
		writer.WriteHeader(http.StatusOK)
	}))
	upstream.Config.ErrorLog = log.New(io.Discard, "", 0)
	upstream.StartTLS()
	defer upstream.Close()

	for _, fingerprint := range []string{TLSFingerprintChrome, TLSFingerprintFirefox} {
		t.Run(fingerprint, func(t *testing.T) {
			fingerprinted, err := NewFingerprintedClient(fingerprint, "")
			if err != nil {
				t.Fatal(err)
			}
			defer fingerprinted.CloseIdleConnections()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			response, err := DoFingerprintedRequestContext(ctx, fingerprinted, http.MethodGet, upstream.URL, nil, map[string]string{"Authorization": "Bearer test-only"})
			if response != nil {
				response.Body.Close()
			}
			var untrusted x509.UnknownAuthorityError
			if !errors.As(err, &untrusted) {
				t.Errorf("untrusted certificate must be rejected, got %v", err)
			}
			adapted, err := GetHTTPClientFingerprinted(fingerprint, "")
			if err != nil {
				t.Fatal(err)
			}
			response, err = adapted.Get(upstream.URL)
			if response != nil {
				response.Body.Close()
			}
			if !errors.As(err, &untrusted) {
				t.Errorf("adapted client must reject untrusted certificate, got %v", err)
			}
		})
	}
	if received.Load() != 0 {
		t.Fatalf("unverified server received %d HTTP requests", received.Load())
	}
}

func TestFingerprintedClientsVerifyTrustedTLS(t *testing.T) {
	if rootPath := os.Getenv("OCTOPUS_TLS_TEST_ROOT"); rootPath != "" {
		rootPEM, err := os.ReadFile(rootPath)
		if err != nil {
			t.Fatal(err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(rootPEM) {
			t.Fatal("invalid test CA")
		}
		x509.SetFallbackRoots(roots)
		upstreamURL := os.Getenv("OCTOPUS_TLS_TEST_URL")
		for _, fingerprint := range []string{TLSFingerprintChrome, TLSFingerprintFirefox} {
			fingerprinted, err := NewFingerprintedClient(fingerprint, "")
			if err != nil {
				t.Fatal(err)
			}
			defer fingerprinted.CloseIdleConnections()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			response, err := DoFingerprintedRequestContext(ctx, fingerprinted, http.MethodGet, upstreamURL, nil, nil)
			if err != nil {
				t.Fatalf("%s trusted certificate rejected: %v", fingerprint, err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("unexpected response: %d", response.StatusCode)
			}
			wrongHostURL := strings.Replace(upstreamURL, "127.0.0.1", "localhost", 1)
			response, err = DoFingerprintedRequestContext(ctx, fingerprinted, http.MethodGet, wrongHostURL, nil, nil)
			if response != nil {
				response.Body.Close()
			}
			var hostnameError x509.HostnameError
			if !errors.As(err, &hostnameError) {
				t.Errorf("%s hostname mismatch must be rejected, got %v", fingerprint, err)
			}
		}
		return
	}

	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	upstream.Config.ErrorLog = log.New(io.Discard, "", 0)
	upstream.StartTLS()
	defer upstream.Close()
	rootPath := filepath.Join(t.TempDir(), "root.pem")
	rootPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: upstream.Certificate().Raw})
	if err := os.WriteFile(rootPath, rootPEM, 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, executable, "-test.run=^TestFingerprintedClientsVerifyTrustedTLS$", "-test.timeout=12s")
	child.Env = append(os.Environ(), "GODEBUG=x509usefallbackroots=1", "OCTOPUS_TLS_TEST_ROOT="+rootPath, "OCTOPUS_TLS_TEST_URL="+upstream.URL)
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("isolated trusted-CA test failed: %v\n%s", err, output)
	}
}
