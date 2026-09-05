package conf

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/spf13/viper"
)

func TestLoadTrustedProxies(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		config string
		env    string
		want   []string
	}{
		{name: "safe default", config: `{}`},
		{name: "file array", config: `{"server":{"trusted_proxies":["127.0.0.1","::1"]}}`, want: []string{"127.0.0.1", "::1"}},
		{name: "environment only", config: `{}`, env: "127.0.0.1,10.0.0.0/8,::1", want: []string{"127.0.0.1", "10.0.0.0/8", "::1"}},
		{name: "environment overrides file", config: `{"server":{"trusted_proxies":["10.0.0.0/8"]}}`, env: "127.0.0.1", want: []string{"127.0.0.1"}},
		{name: "empty environment retains file", config: `{"server":{"trusted_proxies":["::1"]}}`, want: []string{"::1"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			previous := AppConfig
			viper.Reset()
			t.Cleanup(func() {
				viper.Reset()
				AppConfig = previous
			})
			t.Setenv("OCTOPUS_BOOTSTRAP_PASSWORD", "")
			t.Setenv("OCTOPUS_SERVER_TRUSTED_PROXIES", scenario.env)
			configPath := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(configPath, []byte(scenario.config), 0600); err != nil {
				t.Fatal(err)
			}
			if err := Load(configPath); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(AppConfig.Server.TrustedProxies, scenario.want) {
				t.Fatalf("trusted proxies = %q, want %q", AppConfig.Server.TrustedProxies, scenario.want)
			}
		})
	}
}
