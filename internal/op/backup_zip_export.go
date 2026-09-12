package op

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"gorm.io/gorm"
)

// 导出端容量守卫：与导入端（backup_zip_import.go）的容量限制一一对应。
// 导出的备份必须能被自身导入器接受——导出成功而导入被拒等于备份不可恢复，
// 因此导入端的每一条容量限制都在导出端同步强制执行，超限立即失败。

type backupZipExportGuard struct {
	// zip.Writer 在条目内部压缩：条目侧写入的是未压缩字节，压缩后的字节
	// 只能在 sink 侧精确计数，两处计数互不替代。
	sink              io.Writer
	totalCompressed   uint64
	totalUncompressed uint64
	entryName         string
	entryBytes        uint64
	records           int
	encodeBuf         bytes.Buffer
	encoder           *json.Encoder
}

type backupZipSinkCounter struct {
	guard *backupZipExportGuard
}

func (counter *backupZipSinkCounter) Write(p []byte) (int, error) {
	if uint64(len(p)) > maxBackupZipCompressedBytes-counter.guard.totalCompressed {
		return 0, fmt.Errorf(
			"zip export aborted: compressed backup would exceed the %d-byte import limit",
			maxBackupZipCompressedBytes,
		)
	}
	n, err := counter.guard.sink.Write(p)
	counter.guard.totalCompressed += uint64(n)
	return n, err
}

type backupZipEntryCounter struct {
	guard *backupZipExportGuard
	entry io.Writer
}

func (counter *backupZipEntryCounter) Write(p []byte) (int, error) {
	if uint64(len(p)) > maxBackupZipEntryBytes-counter.guard.entryBytes {
		return 0, fmt.Errorf(
			"zip export aborted: entry %q would exceed the %d-byte import limit",
			counter.guard.entryName, maxBackupZipEntryBytes,
		)
	}
	if uint64(len(p)) > maxBackupZipUncompressedBytes-counter.guard.totalUncompressed {
		return 0, fmt.Errorf(
			"zip export aborted: backup would exceed the %d-byte uncompressed import limit",
			maxBackupZipUncompressedBytes,
		)
	}
	n, err := counter.entry.Write(p)
	counter.guard.entryBytes += uint64(n)
	counter.guard.totalUncompressed += uint64(n)
	return n, err
}

// backupZipRecordTokenCheckMinBytes：token 计数的字节阈值。JSON 里 `{`/`}`/
// `[`/`]`/`0` 都是 1 字节 token，理论上每 token 可低至 1 字节，因此阈值取
// 导入端 token 上限（100k）本身：低于该字节数的记录不可能超过导入上限，
// 跳过昂贵的逐 token 计数；超过才校验。
const backupZipRecordTokenCheckMinBytes = 100_000

func newBackupZipExportGuard(sink io.Writer) *backupZipExportGuard {
	guard := &backupZipExportGuard{sink: sink}
	guard.encoder = json.NewEncoder(&guard.encodeBuf)
	return guard
}

// checkBackupRecordTokenLimit 与导入端 maxBackupZipRecordTokens 对齐：
// token 密集型记录（深层结构/超大数组）导出成功而导入被拒，同样违反
// 「导出必可恢复」不变式。
func checkBackupRecordTokenLimit(record []byte, name string) error {
	decoder := json.NewDecoder(bytes.NewReader(record))
	for tokens := 1; ; tokens++ {
		if tokens > maxBackupZipRecordTokens {
			return fmt.Errorf(
				"zip export aborted: record in %q has more than %d JSON tokens, exceeding the %d-token import limit; the exported backup would be unrecoverable",
				name, tokens, maxBackupZipRecordTokens,
			)
		}
		if _, err := decoder.Token(); errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return fmt.Errorf("zip encode %s: %w", name, err)
		}
	}
}

func (guard *backupZipExportGuard) createEntry(zw *zip.Writer, name string) (io.Writer, error) {
	entry, err := zw.Create(name)
	if err != nil {
		return nil, fmt.Errorf("zip create %s: %w", name, err)
	}
	guard.entryName = name
	guard.entryBytes = 0
	return &backupZipEntryCounter{guard: guard, entry: entry}, nil
}

// writeRecord 编码单条记录（JSON 数组元素或 NDJSON 行，含分隔换行），
// 在写入前执行导入端的单记录与总记录数限制，超限即中止导出。
func (guard *backupZipExportGuard) writeRecord(entry io.Writer, name string, record any) error {
	guard.encodeBuf.Reset()
	if err := guard.encoder.Encode(record); err != nil {
		return fmt.Errorf("zip encode %s: %w", name, err)
	}
	line := guard.encodeBuf.Bytes()
	recordBytes := len(line) - 1
	if recordBytes > maxBackupZipRecordBytes {
		return fmt.Errorf(
			"zip export aborted: record in %q is %d bytes, exceeding the %d-byte import limit; the exported backup would be unrecoverable",
			name, recordBytes, maxBackupZipRecordBytes,
		)
	}
	if recordBytes > backupZipRecordTokenCheckMinBytes {
		if err := checkBackupRecordTokenLimit(line, name); err != nil {
			return err
		}
	}
	guard.records++
	if guard.records > maxBackupZipRecords {
		return fmt.Errorf(
			"zip export aborted: more than %d records would exceed the import limit",
			maxBackupZipRecords,
		)
	}
	if _, err := entry.Write(line); err != nil {
		return fmt.Errorf("zip write %s: %w", name, err)
	}
	return nil
}

func (guard *backupZipExportGuard) writeManifest(zw *zip.Writer, manifest map[string]any) error {
	entry, err := guard.createEntry(zw, "manifest.json")
	if err != nil {
		return err
	}
	if err := guard.writeRecord(entry, "manifest.json", manifest); err != nil {
		return err
	}
	if guard.entryBytes > maxBackupZipManifestBytes {
		return fmt.Errorf(
			"zip export aborted: manifest.json exceeds the %d-byte import limit",
			maxBackupZipManifestBytes,
		)
	}
	return nil
}

// writeZipExportArray 以流式写出 JSON 数组：每个元素单独编码并计入记录预算，
// 避免"整表一个 JSON 文档"绕过导入端按元素计的限制。
func writeZipExportArray[T any](
	guard *backupZipExportGuard,
	zw *zip.Writer,
	name string,
	rows []T,
) error {
	entry, err := guard.createEntry(zw, name)
	if err != nil {
		return err
	}
	if _, err := entry.Write([]byte{'['}); err != nil {
		return fmt.Errorf("zip write %s: %w", name, err)
	}
	for i := range rows {
		if i > 0 {
			if _, err := entry.Write([]byte{','}); err != nil {
				return fmt.Errorf("zip write %s: %w", name, err)
			}
		}
		if err := guard.writeRecord(entry, name, &rows[i]); err != nil {
			return err
		}
	}
	if _, err := entry.Write([]byte{']', '\n'}); err != nil {
		return fmt.Errorf("zip write %s: %w", name, err)
	}
	return nil
}

// preflightBackupZipRecordLimit 在写出任何字节前检查 relay_logs：它是唯一
// 携带不限长请求/响应正文的表。数据库侧取原始字节长度做下界（JSON 转义与
// 其余字段只会更长），超限直接失败——此时响应尚未产出，handler 能返回干净
// 的错误，而不是先流式写出一段必然无法导入的归档。预检查询失败不阻断导出：
// 写路径守卫（writeRecord）仍是最终强制执行点。
func preflightBackupZipRecordLimit(ctx context.Context, conn *gorm.DB) error {
	byteLen := func(column string) string {
		switch conn.Dialector.Name() {
		case "mysql":
			// MySQL 的 LENGTH 返回字节数。
			return fmt.Sprintf("LENGTH(%s)", column)
		case "postgres":
			return fmt.Sprintf("OCTET_LENGTH(%s)", column)
		default:
			// SQLite 的 LENGTH 对 TEXT 按字符计数，需转 BLOB 取字节。
			return fmt.Sprintf("LENGTH(CAST(%s AS BLOB))", column)
		}
	}
	// COALESCE 逐列包裹：任一列 NULL 会使整行表达式为 NULL，被 MAX 聚合
	// 跳过——该行的真实体积将不参与预检（历史迁移行可能为 NULL）。
	expr := fmt.Sprintf(
		"%s+%s+%s",
		fmt.Sprintf("COALESCE(%s,0)", byteLen("request_content")),
		fmt.Sprintf("COALESCE(%s,0)", byteLen("response_content")),
		fmt.Sprintf("COALESCE(%s,0)", byteLen("error")),
	)
	var maxBytes int64
	if err := conn.WithContext(ctx).Raw(
		"SELECT COALESCE(MAX(" + expr + "),0) FROM relay_logs",
	).Scan(&maxBytes).Error; err != nil {
		return fmt.Errorf("zip preflight relay_logs: %w", err)
	}
	if maxBytes > maxBackupZipRecordBytes {
		return fmt.Errorf(
			"zip export aborted: relay_logs contains a record of at least %d raw bytes, exceeding the %d-byte import limit; export without logs or adjust the record limit on both sides",
			maxBytes, maxBackupZipRecordBytes,
		)
	}
	return nil
}
