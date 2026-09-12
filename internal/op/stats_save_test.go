package op

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

// F03 回归：统计保存的「取快照→落库」必须整体串行。若只锁单次快照读取，
// 先取旧快照的保存入口可以在新快照落库之后才写入，覆盖式更新会让数据库
// 统计回退（内存领先、数据库倒退，重启后旧值固化）。
//
// 测试用 GORM 更新回调构造确定性屏障：保存 A 挂在 stats_total 的 UPDATE
// 上，期间内存累计推进到 v2、保存 B 启动。加锁后 B 必须等 A 完成才开始
// 自己的保存，最终数据库值 = v2；若去掉 statsSaveMu，B 会在 A 挂起期间
// 完成，A 恢复后把旧快照 v1 覆盖回去，断言失败。
func TestStatsSaveDBSerializesSnapshotAndPersist(t *testing.T) {
	ctx := setupBackupTestDB(t)
	conn := dbpkg.GetDB().WithContext(ctx)

	if err := conn.Create(&model.StatsTotal{ID: 1}).Error; err != nil {
		t.Fatalf("seed stats_total: %v", err)
	}
	statsTotalCacheLock.Lock()
	statsTotalCache = model.StatsTotal{ID: 1}
	statsTotalCacheLock.Unlock()
	// 包级缓存跨测试共享：按生产形态初始化日缓存（带日期，走 UPDATE/INSERT
	// 回退路径而不是空主键 INSERT），避免空 Date 撞 UNIQUE 约束。
	statsDailyCacheLock.Lock()
	statsDailyCache = model.StatsDaily{Date: time.Now().Format("20060102")}
	statsDailyCacheLock.Unlock()

	gate := make(chan struct{})
	var reachedGate atomic.Bool
	callbackName := "test:stats-save-gate"
	if err := conn.Callback().Update().After("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*model.StatsTotal); !ok {
			return
		}
		// 只拦截第一次（保存 A 的旧快照），保存 B 的更新直接放行。
		if !reachedGate.CompareAndSwap(false, true) {
			return
		}
		<-gate
	}); err != nil {
		t.Fatalf("register gate callback: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Callback().Update().Remove(callbackName)
	})

	v1 := model.StatsMetrics{RequestSuccess: 1}
	v2 := model.StatsMetrics{RequestSuccess: 3}
	StatsTotalUpdate(v1)

	saveA := make(chan error, 1)
	go func() { saveA <- StatsSaveDB(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for !reachedGate.Load() {
		if time.Now().After(deadline) {
			t.Fatal("save A never reached the gate")
		}
		time.Sleep(time.Millisecond)
	}

	// A 的快照已取（值 v1），现在把内存推进到 v2。
	StatsTotalUpdate(v2)

	saveBDone := make(chan error, 1)
	go func() {
		saveBDone <- StatsSaveDB(ctx)
	}()

	// A 仍挂在屏障上：串行化生效时 B 不可能完成自己的保存。
	time.Sleep(100 * time.Millisecond)
	select {
	case err := <-saveBDone:
		t.Fatalf("save B completed while save A still held the snapshot sequence: %v", err)
	default:
	}

	close(gate)
	if err := <-saveA; err != nil {
		t.Fatalf("save A: %v", err)
	}
	select {
	case err := <-saveBDone:
		if err != nil {
			t.Fatalf("save B: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("save B never completed")
	}

	var row model.StatsTotal
	if err := conn.First(&row, 1).Error; err != nil {
		t.Fatalf("load stats_total: %v", err)
	}
	// 核心不变量：数据库最终值等于内存累计值（保存 B 的较新快照）。
	// 保存 A 的旧快照（仅 v1）若后落库覆盖，这里会看到回退。
	want := StatsTotalGet().StatsMetrics
	if row.StatsMetrics != want {
		t.Fatalf("stats_total in DB = %+v, want the newer snapshot %+v (older snapshot must not overwrite it)", row.StatsMetrics, want)
	}
}

// 停机协议回归（对抗审查 R4）：统计保存锁的等待必须响应 ctx 取消。
// 停机时 SaveCache 只有 10s 预算——若锁等待不可取消，预算被周期保存
// 吞掉后三路 flush 全败，还会阻塞排在其后的 relay-log flush hook。
func TestStatsSaveDBLockWaitRespectsContext(t *testing.T) {
	ctx := setupBackupTestDB(t)
	statsSaveCh <- struct{}{} // 人为占住保存锁
	t.Cleanup(func() { <-statsSaveCh })

	done := make(chan error, 1)
	go func() {
		saveCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
		done <- StatsSaveDB(saveCtx)
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("StatsSaveDB error = %v, want deadline exceeded while the lock is held", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("StatsSaveDB blocked past its ctx deadline waiting for the save lock")
	}
}

// 日切覆盖快照回归：翻转缓存之前，昨日累计值必须先单行落库（幂等 upsert），
// 关闭「翻转后进程崩溃丢昨日尾窗」的窗口；热路径不再等待保存锁。
func TestStatsDailyUpdatePersistsRolloverSnapshot(t *testing.T) {
	ctx := setupBackupTestDB(t)
	pendingDailyOverridesLock.Lock()
	pendingDailyOverrides = nil
	pendingDailyOverridesLock.Unlock()
	t.Cleanup(func() {
		pendingDailyOverridesLock.Lock()
		pendingDailyOverrides = nil
		pendingDailyOverridesLock.Unlock()
	})

	statsDailyCacheLock.Lock()
	statsDailyCache = model.StatsDaily{Date: "20000101", StatsMetrics: model.StatsMetrics{RequestSuccess: 5}}
	statsDailyCacheLock.Unlock()

	if err := StatsDailyUpdate(ctx, model.StatsMetrics{RequestSuccess: 1}); err != nil {
		t.Fatalf("StatsDailyUpdate: %v", err)
	}

	var rolloverRow model.StatsDaily
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("date = ?", "20000101").First(&rolloverRow).Error; err != nil {
		t.Fatalf("rollover snapshot must be persisted before the cache flip: %v", err)
	}
	if rolloverRow.StatsMetrics != (model.StatsMetrics{RequestSuccess: 5}) {
		t.Fatalf("rollover snapshot content mismatch: %+v", rolloverRow.StatsMetrics)
	}
	if got := StatsTodayGet().Date; got != time.Now().Format("20060102") {
		t.Fatalf("daily cache did not roll over: %q", got)
	}
}

// hourly 过滤必须使用快照时刻的日期（调用方显式传入），不能在落库时重取
// 时钟——跨零点后旧日期的桶曾被整批过滤，尾窗增量永久丢失（对抗审查 R4）。
func TestPersistStatsSnapshotsUsesSnapshotDate(t *testing.T) {
	ctx := setupBackupTestDB(t)
	if err := dbpkg.GetDB().WithContext(ctx).Create(&model.StatsTotal{ID: 1}).Error; err != nil {
		t.Fatalf("seed stats_total: %v", err)
	}
	hourly := [24]model.StatsHourly{}
	hourly[23] = model.StatsHourly{Hour: 23, Date: "19990101", StatsMetrics: model.StatsMetrics{RequestSuccess: 7}}

	// 显式传入快照日期 19990101：真实时钟是 2026 年，旧实现（落库时取时钟）
	// 会把该桶过滤掉；新实现按传入日期持久化。
	if err := persistStatsSnapshots(ctx, "19990101", model.StatsTotal{ID: 1}, model.StatsDaily{Date: "19990101"}, hourly, nil, nil); err != nil {
		t.Fatalf("persist with snapshot date: %v", err)
	}
	var row model.StatsHourly
	if err := dbpkg.GetDB().WithContext(ctx).First(&row, 23).Error; err != nil {
		t.Fatalf("load hourly row: %v", err)
	}
	if row.StatsMetrics != (model.StatsMetrics{RequestSuccess: 7}) {
		t.Fatalf("hourly tail window was dropped: %+v", row.StatsMetrics)
	}
}
