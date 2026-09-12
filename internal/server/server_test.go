package server

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"

	"github.com/bestruirui/octopus/internal/conf"
	"github.com/gin-gonic/gin"
)

func TestStartRejectsOccupiedAddress(t *testing.T) {
	previous := conf.AppConfig
	t.Cleanup(func() { conf.AppConfig = previous })
	t.Setenv("OCTOPUS_IMAGES_BODY_TMP_DIR", t.TempDir())
	conf.AppConfig.Server.TrustedProxies = nil
	for _, scenario := range []struct {
		name    string
		network string
		host    string
	}{
		{name: "IPv4", network: "tcp4", host: "127.0.0.1"},
		{name: "IPv6", network: "tcp6", host: "::1"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			listener, err := net.Listen(scenario.network, net.JoinHostPort(scenario.host, "0"))
			if err != nil {
				if scenario.network == "tcp6" && (errors.Is(err, syscall.EAFNOSUPPORT) || errors.Is(err, syscall.EADDRNOTAVAIL)) {
					t.Skipf("IPv6 loopback unavailable: %v", err)
				}
				t.Fatal(err)
			}
			t.Cleanup(func() { listener.Close() })
			conf.AppConfig.Server.Host = scenario.host
			conf.AppConfig.Server.Port = listener.Addr().(*net.TCPAddr).Port
			err = Start()
			if err == nil {
				Close()
				t.Fatal("Start succeeded despite an occupied listen address")
			}
			if !errors.Is(err, syscall.EADDRINUSE) {
				t.Fatalf("Start error = %v, want address already in use", err)
			}
		})
	}
}

func TestEngineClientIPTrust(t *testing.T) {
	previous := conf.AppConfig
	t.Cleanup(func() { conf.AppConfig = previous })
	for _, scenario := range []struct {
		name    string
		proxies []string
		peer    string
		xff     string
		realIP  string
		want    string
	}{
		{name: "direct ignores all forwarded headers", peer: "192.0.2.10:1234", xff: "198.51.100.5", realIP: "198.51.100.6", want: "192.0.2.10"},
		{name: "empty list ignores real IP", proxies: []string{}, peer: "192.0.2.10:1234", realIP: "198.51.100.6", want: "192.0.2.10"},
		{name: "direct IPv6", peer: "[2001:db8::10]:1234", xff: "2001:db8::20", want: "2001:db8::10"},
		{name: "trusted IPv4 address", proxies: []string{"127.0.0.1"}, peer: "127.0.0.1:1234", xff: "198.51.100.5", want: "198.51.100.5"},
		{name: "trusted IPv6 address", proxies: []string{"::1"}, peer: "[::1]:1234", realIP: "2001:db8::20", want: "2001:db8::20"},
		{name: "untrusted peer", proxies: []string{"127.0.0.1"}, peer: "192.0.2.10:1234", xff: "198.51.100.5", want: "192.0.2.10"},
		{name: "trusted chain stops at client", proxies: []string{"10.0.0.0/8", "127.0.0.1"}, peer: "127.0.0.1:1234", xff: "203.0.113.9, 198.51.100.5, 10.0.0.2", want: "198.51.100.5"},
		{name: "trusted IPv6 CIDR", proxies: []string{"2001:db8:1::/48"}, peer: "[2001:db8:1::1]:1234", xff: "2001:db8:2::1", want: "2001:db8:2::1"},
		{name: "invalid header ignored", proxies: []string{"127.0.0.1"}, peer: "127.0.0.1:1234", xff: "not-an-ip", want: "127.0.0.1"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			conf.AppConfig.Server.TrustedProxies = scenario.proxies
			engine, err := newEngine()
			if err != nil {
				t.Fatal(err)
			}
			engine.GET("/api/v1/proxy-trust-test", func(context *gin.Context) {
				context.String(http.StatusOK, context.ClientIP())
			})
			request := httptest.NewRequest(http.MethodGet, "/api/v1/proxy-trust-test", nil)
			request.RemoteAddr = scenario.peer
			request.Header.Set("X-Forwarded-For", scenario.xff)
			request.Header.Set("X-Real-IP", scenario.realIP)
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK || recorder.Body.String() != scenario.want {
				t.Fatalf("status = %d, client IP = %q, want %q", recorder.Code, recorder.Body.String(), scenario.want)
			}
		})
	}
}

func TestEngineRejectsInvalidTrustedProxies(t *testing.T) {
	previous := conf.AppConfig
	t.Cleanup(func() { conf.AppConfig = previous })
	for _, proxies := range [][]string{
		{"invalid"}, {"10.0.0.0/33"}, {"2001:db8::/129"}, {"127.0.0.1", "invalid"}, {"127.0.0.1", ""}, {" 127.0.0.1"},
	} {
		t.Run(strings.Join(proxies, ","), func(t *testing.T) {
			conf.AppConfig.Server.TrustedProxies = proxies
			engine, err := newEngine()
			if err == nil || engine != nil || !strings.Contains(err.Error(), "server.trusted_proxies") {
				t.Fatalf("invalid proxy configuration must reject the engine, got engine=%v error=%v", engine != nil, err)
			}
		})
	}
}
