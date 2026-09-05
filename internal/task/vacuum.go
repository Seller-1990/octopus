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
func DBVacuumTask() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
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
