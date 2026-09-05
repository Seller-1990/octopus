package server

import (
	"fmt"
	"net/http"
	"time"

	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/relay/bodycache"
	_ "github.com/bestruirui/octopus/internal/server/handlers"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/safe"
	"github.com/bestruirui/octopus/static"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

var httpSrv http.Server

func Start() error {
	r, err := newEngine()
	if err != nil {
		return err
	}

	// 启动时清理 Images 请求体临时文件（失败仅告警，不阻断启动）
	tmpDir := bodycache.TmpDirFromEnv()
	olderThan := bodycache.TmpCleanupOlderThanFromEnv()
	if err := bodycache.CleanupOldTmpFiles(tmpDir, bodycache.TmpFilePrefix, olderThan); err != nil {
		log.Warnf("cleanup images tmp files failed: dir=%s prefix=%s olderThan=%s err=%v", tmpDir, bodycache.TmpFilePrefix, olderThan, err)
	}

	// 路由注册失败必须阻断启动：带残缺路由表的 engine 一旦对外服务即违反启动成功契约。
	if err := router.RegisterAll(r); err != nil {
		return fmt.Errorf("register routes: %w", err)
	}

	httpSrv.Addr = fmt.Sprintf("%s:%d", conf.AppConfig.Server.Host, conf.AppConfig.Server.Port)
	httpSrv.Handler = r
	safe.Go("http-listen", func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Errorf("http server listen and serve error: %v", err)
		}
	})
	return nil
}

func newEngine() (*gin.Engine, error) {
	if conf.IsDebug() {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	if err := r.SetTrustedProxies(conf.AppConfig.Server.TrustedProxies); err != nil {
		return nil, fmt.Errorf("invalid server.trusted_proxies: %w", err)
	}
	if len(conf.AppConfig.Server.TrustedProxies) == 0 {
		// 部署形态不能从代码推断：直连是默认，但反代部署漏配时所有用户共享代理 IP 的登录预算，提前给出可检索的告警。
		log.Warnf("server.trusted_proxies is empty: forwarded X-Forwarded-For/X-Real-IP headers are ignored; " +
			"if Octopus runs behind a reverse proxy, configure its real addresses/CIDRs or all clients behind it will share the proxy IP's login budget")
	}
	r.Use(gin.CustomRecovery(func(c *gin.Context, recovered interface{}) {
		log.Errorf("http panic recovered: %v", recovered)
		resp.Error(c, http.StatusInternalServerError, resp.ErrInternalServer)
		c.Abort()
	}))

	r.Use(gzip.Gzip(gzip.DefaultCompression,
		gzip.WithExcludedPaths([]string{"/v1/"}),
		// SSE 端点必须排除：gzip 缓冲会破坏事件流的逐条 Flush 语义。
		gzip.WithExcludedPathsRegexs([]string{`/api/v1/log/.*/stream`}),
	))

	r.Use(middleware.Logger(middleware.LoggerConfig{
		Enabled:       conf.AppConfig.Log.Access.Enabled || conf.IsDebug(),
		SlowThreshold: time.Duration(conf.AppConfig.Log.Access.SlowThresholdMS) * time.Millisecond,
	}))
	r.Use(middleware.Cors())
	r.Use(middleware.StaticEmbed("/", static.StaticFS))

	return r, nil
}

func Close() error {
	return httpSrv.Close()
}
