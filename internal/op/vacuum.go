package op

import (
	"context"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/utils/log"
)

// vacuumFreeListRatioThreshold 触发 VACUUM 的 freelist 页占比下限：
// 低于该比例时回收收益不足以抵消 VACUUM 的独占锁开销。
const vacuumFreeListRatioThreshold = 0.25

// vacuumMinDBBytes 库体积低于该值时不回收：小库回收收益有限，
// 且避免新装实例在启动初期做无谓的 VACUUM。
const vacuumMinDBBytes = 64 << 20

// DBVacuumIfNeeded 检查 SQLite 文件高水位并在超过阈值时执行 VACUUM。
//
// SQLite 的 DELETE 只把页挂到 freelist、不缩小文件——即使日志保留期清理
// 正常工作，文件也会维持在历史峰值不回落。本函数按「freelist 占比 > 25%
// 且库 > 64MB」判定是否值得回收。MySQL/Postgres 直接跳过。
//
// 实现约束：
//   - 全部操作绑定在同一物理连接上：temp_store 是 per-connection 的 pragma，
//     DSN 里的 temp_store(MEMORY) 会让 VACUUM 的临时库整个建在内存，
//     几 GB 库会 RSS 峰值逼近库大小直至 OOM kill（失败原子回滚后每日任务
//     循环重演）；VACUUM 前在同一连接改为 temp_store=FILE 落盘。
//   - PRAGMA 读取也走同一连接，避免连接池多连接读出失真的快照比例。
//   - VACUUM 全程持独占写锁（WAL 单写者），期间所有写路径 SQLITE_BUSY，
//     由 busy_timeout 兜底后返回失败；日志/统计写入失败由调用方日志可见。
//     触发窗口为分钟级，属接受的运维代价（增量模式列为后续改进）。
//   - 磁盘余量不预检：空间不足时 VACUUM 原子回滚、原库完好，仅记错误日志。
func DBVacuumIfNeeded(ctx context.Context) (bool, error) {
	if !db.IsSQLite() {
		return false, nil
	}

	sqlDB, err := db.GetDB().DB()
	if err != nil {
		return false, err
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()

	var pageCount, freeListCount, pageSize int64
	if err := conn.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount); err != nil {
		return false, err
	}
	if err := conn.QueryRowContext(ctx, "PRAGMA freelist_count").Scan(&freeListCount); err != nil {
		return false, err
	}
	if err := conn.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return false, err
	}
	if pageCount <= 0 || pageSize <= 0 {
		return false, nil
	}

	dbBytes := pageCount * pageSize
	freeBytes := freeListCount * pageSize
	ratio := float64(freeListCount) / float64(pageCount)
	if dbBytes < vacuumMinDBBytes || ratio <= vacuumFreeListRatioThreshold {
		log.Debugf("db vacuum skipped: size=%dMB freelist=%.1f%%", dbBytes>>20, ratio*100)
		return false, nil
	}

	log.Infof("db vacuum starting: size=%dMB free_pages=%d (%.1f%%), reclaimable=%dMB",
		dbBytes>>20, freeListCount, ratio*100, freeBytes>>20)
	if _, err := conn.ExecContext(ctx, "PRAGMA temp_store=FILE"); err != nil {
		return false, err
	}
	if _, err := conn.ExecContext(ctx, "VACUUM"); err != nil {
		return false, err
	}
	// WAL 文件不会自动收缩：VACUUM 的回写先经 -wal 落盘，不做 TRUNCATE
	// checkpoint 的话文件回收会被 WAL 高水位抵消大半。
	if _, err := conn.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return false, err
	}
	log.Infof("db vacuum finished")
	return true, nil
}
