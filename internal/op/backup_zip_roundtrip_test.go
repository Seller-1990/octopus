package op

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

// F01 回归：导出与导入的容量契约必须一致。导出既要能产出可恢复的归档
// （4 MiB 邻界记录完整往返），也要在产出不可恢复归档前明确失败。

func boundaryRelayLog(id int64, bodyLength int) model.RelayLog {
	return model.RelayLog{
		ID:               id,
		Time:             id,
		RequestModelName: "boundary-model",
		ChannelId:        1,
		Outcome:          model.RequestOutcomeSuccess,
		RequestContent:   strings.Repeat("x", bodyLength),
	}
}

// relayLogBodyLengthForRecordSize 计算让单条记录序列化后接近目标字节数的正文
// 长度（预留 margin 字节余量，容忍 DB 读取后的编码差异；精确的边界判定由
// 守卫单元测试覆盖）。
func relayLogBodyLengthForRecordSize(t *testing.T, target int, margin int) int {
	t.Helper()
	if target > maxBackupZipRecordBytes {
		t.Fatalf("target %d exceeds the per-record limit", target)
	}
	probe, err := json.Marshal(boundaryRelayLog(1, 0))
	if err != nil {
		t.Fatalf("marshal probe relay log: %v", err)
	}
	// 探针正文为空字符串（""），替换为 N 个单字节字符后记录长度恰好 +N。
	length := target - margin - len(probe)
	if length <= 0 {
		t.Fatalf("record overhead %d already exceeds target %d", len(probe), target)
	}
	return length
}

func TestDBExportZipRelayLogRecordBoundaryRoundTrips(t *testing.T) {
	ctx := setupBackupTestDB(t)
	conn := dbpkg.GetDB().WithContext(ctx)
	// 导入端校验 relay_log 的渠道必须有父行，导出端同样带出该渠道。
	channel := model.Channel{Name: "boundary-channel", Enabled: true}
	mustCreateBackupRow(t, conn, &channel)
	bodyLength := relayLogBodyLengthForRecordSize(t, maxBackupZipRecordBytes, 64)
	log := boundaryRelayLog(1, bodyLength)
	log.ChannelId = channel.ID
	mustCreateBackupRow(t, conn, &log)

	var buffer bytesBuffer
	if err := DBExportZip(ctx, &buffer, true, true); err != nil {
		t.Fatalf("DBExportZip failed at the record boundary: %v", err)
	}

	_ = dbpkg.Close()
	targetPath := t.TempDir() + "/zip-boundary-restore.db"
	if err := dbpkg.InitDB("sqlite", targetPath, false); err != nil {
		t.Fatalf("InitDB target: %v", err)
	}
	payload := buffer.Bytes()
	if _, err := DBImportZip(ctx, bytes.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatalf("DBImportZip failed at the record boundary: %v", err)
	}
	var restored model.RelayLog
	if err := dbpkg.GetDB().WithContext(ctx).First(&restored, log.ID).Error; err != nil {
		t.Fatalf("query restored relay log: %v", err)
	}
	if len(restored.RequestContent) != bodyLength {
		t.Fatalf("restored request content length = %d, want %d", len(restored.RequestContent), bodyLength)
	}
}

func TestDBExportZipFailsOnUnrecoverableRecord(t *testing.T) {
	ctx := setupBackupTestDB(t)
	conn := dbpkg.GetDB().WithContext(ctx)

	t.Run("preflight rejects oversized raw content", func(t *testing.T) {
		// 正文原始字节就已超过导入上限：预检必须在写出任何字节前失败，
		// handler 才能返回干净的错误响应而不是截断的归档。
		log := model.RelayLog{
			ID:               1,
			Time:             1,
			RequestModelName: "boundary-model",
			ChannelId:        1,
			Outcome:          model.RequestOutcomeSuccess,
			RequestContent:   strings.Repeat("x", maxBackupZipRecordBytes+1),
		}
		mustCreateBackupRow(t, conn, &log)

		var buffer bytesBuffer
		err := DBExportZip(ctx, &buffer, true, true)
		if err == nil {
			t.Fatal("DBExportZip succeeded despite an unrecoverable record")
		}
		if !strings.Contains(err.Error(), "import limit") {
			t.Fatalf("export error should name the import limit, got: %v", err)
		}
		if buffer.Len() != 0 {
			// 失败发生在任何条目写出之前；缓冲区里只允许剩空归档的 22 字节 EOCD。
			if reader, zerr := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len())); zerr != nil || len(reader.File) != 0 {
				t.Fatalf("export wrote %d bytes with %v before failing", buffer.Len(), zerr)
			}
		}
	})

	t.Run("guard rejects oversized encoded record", func(t *testing.T) {
		// 正文原始字节在上限之内、编码后的记录（含其余字段与转义）超限：
		// 预检不触发，由写路径守卫在流中途拦截并中止导出。
		bodyLength := relayLogBodyLengthForRecordSize(t, maxBackupZipRecordBytes, 64) + 65
		log := boundaryRelayLog(2, bodyLength)
		mustCreateBackupRow(t, conn, &log)

		var buffer bytesBuffer
		err := DBExportZip(ctx, &buffer, true, true)
		if err == nil {
			t.Fatal("DBExportZip succeeded despite an unrecoverable record")
		}
		if !strings.Contains(err.Error(), "import limit") {
			t.Fatalf("export error should name the import limit, got: %v", err)
		}
	})

	var logsCount int64
	if err := conn.Model(&model.RelayLog{}).Count(&logsCount).Error; err != nil || logsCount != 2 {
		t.Fatalf("source relay_logs must stay untouched: count=%d err=%v", logsCount, err)
	}
	_ = dbpkg.Close()
}

func TestBackupZipExportGuardEnforcesImportLimits(t *testing.T) {
	t.Run("entry limit", func(t *testing.T) {
		guard := newBackupZipExportGuard(io.Discard)
		entry, err := guard.createEntry(zip.NewWriter(io.Discard), "big.ndjson")
		if err != nil {
			t.Fatalf("createEntry: %v", err)
		}
		guard.entryBytes = maxBackupZipEntryBytes
		if _, err := entry.Write([]byte("x")); err == nil {
			t.Fatal("entry write over the limit should fail")
		}
	})

	t.Run("uncompressed total limit", func(t *testing.T) {
		guard := newBackupZipExportGuard(io.Discard)
		guard.totalUncompressed = maxBackupZipUncompressedBytes
		entry, err := guard.createEntry(zip.NewWriter(io.Discard), "overflow.ndjson")
		if err != nil {
			t.Fatalf("createEntry: %v", err)
		}
		if _, err := entry.Write([]byte("x")); err == nil {
			t.Fatal("write over the uncompressed total should fail")
		}
	})

	t.Run("compressed total limit", func(t *testing.T) {
		guard := newBackupZipExportGuard(io.Discard)
		guard.totalCompressed = maxBackupZipCompressedBytes
		counter := &backupZipSinkCounter{guard: guard}
		if _, err := counter.Write([]byte("x")); err == nil {
			t.Fatal("write over the compressed total should fail")
		}
	})

	t.Run("record count limit", func(t *testing.T) {
		guard := newBackupZipExportGuard(io.Discard)
		guard.records = maxBackupZipRecords
		if err := guard.writeRecord(io.Discard, "rows.ndjson", model.RelayLog{}); err == nil {
			t.Fatal("record over the count limit should fail")
		}
	})

	t.Run("record size limit", func(t *testing.T) {
		guard := newBackupZipExportGuard(io.Discard)
		big := model.RelayLog{RequestContent: strings.Repeat("x", maxBackupZipRecordBytes+1)}
		if err := guard.writeRecord(io.Discard, "rows.ndjson", big); err == nil {
			t.Fatal("record over the size limit should fail")
		}
	})

	t.Run("manifest limit", func(t *testing.T) {
		guard := newBackupZipExportGuard(io.Discard)
		big := map[string]any{"padding": strings.Repeat("x", maxBackupZipManifestBytes)}
		if err := guard.writeManifest(zip.NewWriter(io.Discard), big); err == nil {
			t.Fatal("manifest over the limit should fail")
		}
	})
}

// 空表导出的数组必须仍是导入器接受的合法空数组（"[]"）。
func TestDBExportZipEmptyTableArrayShape(t *testing.T) {
	var out bytes.Buffer
	guard := newBackupZipExportGuard(io.Discard)
	writer := zip.NewWriter(&out)
	if err := writeZipExportArray(guard, writer, "rows.json", []model.CurrencyRate{}); err != nil {
		t.Fatalf("write empty array: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	if len(reader.File) != 1 {
		t.Fatalf("zip entries = %d, want 1", len(reader.File))
	}
	rc, err := reader.File[0].Open()
	if err != nil {
		t.Fatalf("open entry: %v", err)
	}
	defer rc.Close()
	content, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read entry: %v", err)
	}
	if string(content) != "[]\n" {
		t.Fatalf("empty array shape = %q, want %q", content, "[]\n")
	}
}
