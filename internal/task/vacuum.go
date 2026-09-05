package task

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
)

// DBVacuumTask 每日检查一次 SQLite 文件高水位，超阈值时执行 VACUUM。
// VACUUM 持独占写锁，通过 RegisterWithPhase 与其他周期写者错相；
// 任务框架的 running 原子标志天然防止同任务重入。
// 超时给足 30 分钟：modernc 纯 Go 驱动比 C SQLite 慢数倍，几 GB 库在
// 低配 VPS 上可能超过 10 分钟，超限只会原子回滚零进展并次日重试。
func DBVacuumTask() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	ran, err := op.DBVacuumIfNeeded(ctx)
	if err != nil {
		log.Warnf("db vacuum task failed: %v", err)
		return
	}
	if ran {
		log.Infof("db vacuum task reclaimed sqlite freelist pages")
	}
}
