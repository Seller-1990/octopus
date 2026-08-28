# Octopus v1.7.1 Release Notes

> English first, 中文见下方. — v1.7.0 → v1.7.1.

## 🐛 Fixes

- **Complete vendor tags on the group page**: group member vendor detection now runs server-side with `modelvendor.Detect` and is exposed as a per-item `vendor` field. The group page falls back to a full local detection table aligned with the backend, and vendor filter chips now render even when only one vendor exists (e.g. a newly created `xiaomi` group shows its `Xiaomi` chip).
- **Log detail auto-refresh**: the log detail list now polls every 5 seconds automatically, so new logs appear without manual refresh.
- **Check-in status refresh after verification-bridge sync**: when a verification-bridge sync retry succeeds, Octopus now refreshes the account's check-in state right away (for accounts with auto check-in enabled). This clears stale check-in failures caused by account issues that the sync just repaired. Check-in refresh failures are appended to the retry message and never fail an already-successful sync retry.
- **TLS fingerprint selector crash**: the "none" TLS fingerprint option crashed the advanced settings accordion because the Radix Select item used an empty string value; it now maps empty string to `none` at the UI boundary.
- **Audit finding batch**: re-panic after transaction rollback to avoid silent success; deterministic Responses tool-call done event order; deep-copy `GroupList` items before writing policy fields; idempotent passthrough metrics collection; guard duplicate Anthropic error events; merge tool-call deltas by `(ID, Index)` instead of `Index` only; emit an empty Gemini args object on parse failure; keep analytics old data on background refetch failure; remove unused exported functions.

## 🚀 Performance

- **Non-streaming response timeout**: new `client.response_timeout_seconds` (default 120) for non-streaming upstream calls.
- **SQLite read pool**: `MaxOpenConns` raised to 4 with WAL `busy_timeout` rationale.
- **Header policy cache**: enabled header policies are cached for 3s and invalidated on upsert/delete.
- **Site channel binding cache**: binding lookups are cached with write invalidation hooks across `op` and `sitesync`.

## 🔧 Refactoring

- Extracted pure site formatting/status helpers into `site/format-utils.ts` and site-channel model utilities into `site-channel/model-utils.ts`.
- Split protocol helpers out of `relay.go` into `relay/protocol_helpers.go`.
- Updated the 2026-08-23 audit report with the second, third, and fourth fix batches.

---

## 中文更新说明

> 自 v1.7.0 起。English above.

### 🐛 修复

- **分组页厂商标签完整展示**：分组成员的厂商识别改为后端 `modelvendor.Detect` 注入每项 `vendor` 字段；分组页本地回退识别表与后端对齐，且单个厂商时也显示筛选标签。新建的 `xiaomi` 厂商分组顶部会正确出现 `Xiaomi` 标签。
- **日志明细自动刷新**：日志明细列表每 5 秒自动轮询刷新，无需再手动刷新即可看到新日志。
- **验证桥同步后刷新签到状态**：验证桥同步重试成功后，若账号开启自动签到，会立即补一次签到状态刷新，清除因账号问题导致的陈旧签到失败状态；签到刷新失败会附加到重试消息中，不会反过来让已成功的同步重试失败。
- **TLS 指纹选择器崩溃修复**：TLS 指纹为 "none" 时，Radix Select 空字符串 value 导致高级设置折叠面板崩溃，现已在 UI 边界将空字符串映射为 `none`。
- **审计问题批量修复**：事务回滚后重新 panic，避免静默成功；Responses tool-call done 事件顺序确定化；`GroupList` 深拷贝条目后再写策略字段；透传指标采集幂等化；Anthropic 重复 error 事件防护；tool-call 增量按 `(ID, Index)` 合并；Gemini 解析失败时输出空 args 对象；后台刷新失败时分析页保留旧数据；删除未使用的导出函数。

### 🚀 性能

- **非流式响应超时**：新增 `client.response_timeout_seconds`（默认 120）用于非流式上游调用。
- **SQLite 读池**：`MaxOpenConns` 提升至 4，并依据 WAL `busy_timeout` 说明调整。
- **Header Policy 缓存**：启用态的 Header Policy 缓存 3 秒，写入/删除时失效。
- **站点渠道绑定缓存**：绑定查询增加缓存，并在 `op` 与 `sitesync` 的写入路径统一失效。

### 🔧 重构

- 站点格式化/状态辅助函数抽到 `site/format-utils.ts`，站点渠道模型辅助函数抽到 `site-channel/model-utils.ts`。
- 协议辅助函数从 `relay.go` 拆到 `relay/protocol_helpers.go`。
- 2026-08-23 审计报告补充第二批、第三批、第四批修复记录。
