package op

import (
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
