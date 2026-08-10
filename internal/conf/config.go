package conf

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/spf13/viper"
)

type Server struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

type Log struct {
	Level           string `mapstructure:"level"`
	Format          string `mapstructure:"format"`
	Caller          bool   `mapstructure:"caller"`
	StacktraceLevel string `mapstructure:"stacktrace_level"`
	Access          struct {
		Enabled         bool `mapstructure:"enabled"`
		SlowThresholdMS int  `mapstructure:"slow_threshold_ms"`
	} `mapstructure:"access"`
	Relay struct {
		Summary bool `mapstructure:"summary"`
	} `mapstructure:"relay"`
}

type Database struct {
	Type string `mapstructure:"type"`
	Path string `mapstructure:"path"`
}

type Client struct {
	// DialTimeoutSeconds 上游 TCP 连接（含 DNS）超时，默认 5。
	DialTimeoutSeconds int `mapstructure:"dial_timeout_seconds"`
	// TLSHandshakeTimeoutSeconds 上游 TLS 握手超时，默认 5。
	TLSHandshakeTimeoutSeconds int `mapstructure:"tls_handshake_timeout_seconds"`
}

type Startup struct {
	CacheInitTimeoutSeconds int `mapstructure:"cache_init_timeout_seconds"`
}

// Bootstrap 首次启动初始化配置（F01：取消固定 admin/admin）。
type Bootstrap struct {
	// Password 空库首启的管理员初始密码（环境变量 OCTOPUS_BOOTSTRAP_PASSWORD）。
	// 未设置且库中无用户时拒绝启动，杜绝固定默认凭据。
	Password string `mapstructure:"password"`
}

type Config struct {
	Server    Server    `mapstructure:"server"`
	Log       Log       `mapstructure:"log"`
	Database  Database  `mapstructure:"database"`
	Client    Client    `mapstructure:"client"`
	Startup   Startup   `mapstructure:"startup"`
	Bootstrap Bootstrap `mapstructure:"bootstrap"`
}

var AppConfig Config

func CacheInitTimeout() time.Duration {
	seconds := AppConfig.Startup.CacheInitTimeoutSeconds
	if seconds <= 0 {
		seconds = 120
	}
	return time.Duration(seconds) * time.Second
}

func ClientDialTimeout() time.Duration {
	seconds := AppConfig.Client.DialTimeoutSeconds
	if seconds <= 0 {
		seconds = 5
	}
	return time.Duration(seconds) * time.Second
}

func ClientTLSHandshakeTimeout() time.Duration {
	seconds := AppConfig.Client.TLSHandshakeTimeoutSeconds
	if seconds <= 0 {
		seconds = 5
	}
	return time.Duration(seconds) * time.Second
}

// scrubBootstrapPassword 从已落盘的默认配置中移除 bootstrap.password 键
// （F01：密码只应存在于环境变量/受保护密钥，绝不随 data 目录/备份明文留存）。
// 缺失文件/解析失败静默跳过（写盘泄漏防御性清理，不阻断启动）。
func scrubBootstrapPassword(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return
	}
	if bootstrap, ok := cfg["bootstrap"].(map[string]any); ok {
		delete(bootstrap, "password")
		cfg["bootstrap"] = bootstrap
	}
	cleaned, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, cleaned, 0644)
}

func Load(path string) error {
	if path != "" {
		viper.SetConfigFile(path)
	} else {
		viper.SetConfigName("config")
		viper.SetConfigType("json")
		viper.AddConfigPath("data")
	}

	viper.AutomaticEnv()
	viper.SetEnvPrefix(APP_NAME)
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	setDefaults()

	if err := viper.ReadInConfig(); err == nil {
		log.Infof("Using config file: %s", viper.ConfigFileUsed())
	} else {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			log.Infof("Config file not found, creating default config")
			if err := os.MkdirAll("data", 0755); err != nil {
				log.Errorf("Failed to create data directory: %v", err)
			}
			if err := viper.SafeWriteConfigAs("data/config.json"); err != nil {
				log.Errorf("Failed to create default config: %v", err)
			} else {
				// F01 修复：SafeWriteConfigAs 序列化 AllSettings，会把 env 解析出的
				// bootstrap.password 明文写进默认配置并随数据卷/备份留存——写盘后剥离该键。
				// 运行进程仍按 env 解析（viper env 优先级高于 config 文件），下次启动
				// 文件无该键 → 回落默认空值 → 继续从 env 取，无功能影响。
				scrubBootstrapPassword("data/config.json")
			}
		} else {
			return fmt.Errorf("error reading config file: %w", err)
		}
	}

	if err := viper.Unmarshal(&AppConfig); err != nil {
		return fmt.Errorf("unable to decode config into struct: %w", err)
	}
	return nil
}

func setDefaults() {
	// F01 修复：默认监听 127.0.0.1（裸机安全默认）——公网/容器部署由配置显式开启。
	// 注意：docker 场景容器内必须 0.0.0.0（绑定回环会使 compose 端口映射失效），
	// 已在 docker-compose.yml 显式设置；裸机 `server.host` 缺省即本机回环。
	viper.SetDefault("server.host", "127.0.0.1")
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("database.type", "sqlite")
	viper.SetDefault("database.path", "data/data.db")
	viper.SetDefault("log.level", "info")
	viper.SetDefault("log.format", "console")
	viper.SetDefault("log.caller", false)
	viper.SetDefault("log.stacktrace_level", "error")
	viper.SetDefault("log.access.enabled", false)
	viper.SetDefault("log.access.slow_threshold_ms", 3000)
	viper.SetDefault("log.relay.summary", true)
	viper.SetDefault("startup.cache_init_timeout_seconds", 120)
	viper.SetDefault("client.dial_timeout_seconds", 5)
	viper.SetDefault("client.tls_handshake_timeout_seconds", 5)
	viper.SetDefault("bootstrap.password", "")
}
