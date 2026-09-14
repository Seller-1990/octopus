# Octopus v1.8.5 Release Notes

> English first, 中文见下方. — v1.8.4 → v1.8.5.

## ✨ Highlights

- **Three adversarial review rounds closed end-to-end**: every finding across the 2026-09-13 morning/evening audits and the 2026-09-13/14 third round is fixed, explicitly skipped (intranet-only posture), or refuted with evidence — all tracked in the new single-source ledger `docs/BACKLOG.md` with globally unique IDs (F-number reuse across reports is over).
- **Routing hot path de-coupled from history**: the 24h attempt-facts aggregation moved behind a 30s in-process snapshot (it previously ran synchronously on every proxied request, making request latency a function of uptime — 30 rps ≈ 0.8–2.6s per request after a day of traffic), and per-candidate price quotes are batch-prefetched once per request instead of 2 queries per candidate.
- **Quota writes go write-behind**: per-request `UPDATE quota_used` no longer serializes against log/stats flushes, VACUUM and backup imports on SQLite's single writer — in-memory accumulation + 5-minute flush + shutdown/import-time flush; the limiter reads the in-memory value so enforcement is unchanged. Log/usage backpressure flushes get bounded retries.
- **Streaming hangs are bounded**: a new `stream_inactivity_timeout` setting (default 300s) aborts mid-stream stalls (upstream 200 + SSE headers then silence), heartbeats no longer keep dead upstreams alive, auto-provisioned groups get a 120s first-token default, and outbound bodies are token-counted lazily (only when the upstream reports no usage).

## 🐛 Fixes

- **Circuit breaker**: Open-state in-flight failures no longer extend the cooldown (recovery probes can no longer be postponed forever); consecutive 429/503 soft failures now trip the breaker (previously a rate-limited channel was retried head-of-line on every request under Failover).
- **WebSocket subsystem**: processor exit cancels reads before returning pooled connections (double-reader window closed); reader fields are atomic and connections touched by an interrupted read are dropped instead of recycled; same-channel retries stop on first-token timeout (twin of the HTTP path); conversation-state pruning is throttled.
- **Data correctness**: backup import flushes in-memory key billing before cache rebuild (key cost ledger no longer evaporates); site balance keeps the previous value when the balance API fails transiently (a 0 no longer routes around the account); aggregate identity rows refresh denormalized names on rename; pricing refresh validates the success envelope before clearing last_error; route-detection probes stop after the first authenticated hit (was up to 7 duplicate site-level fetches per token).
- **Startup & lifecycle**: `runOnStart` tasks fire after their phase offset (no more t=0 thundering herd); SIGHUP no longer kills the service; SQLite DSN gains `_txlock=immediate` (BUSY_SNAPSHOT write-congestion hardening); self-update verifies the unpacked binary, rolls back `.backup` and exits for the supervisor on restart failure.
- **Web**: mutations no longer fail silently (error toasts on delete-channel / account-toggle); `request()` gains a 30s timeout and normalizes network errors through the i18n error channel (no more raw "Failed to fetch" toasts); 10 phantom `setQueryData` writes to an unobserved query key removed; site check-in history polling 30s → 5min.
- **Migration dialects**: ALTER TABLE DDL is dialect-aware — MySQL VARCHAR for TEXT-with-DEFAULT (ERROR 1101) and PostgreSQL TIMESTAMP instead of DATETIME (migration 004 previously failed PG startup).
- **Self-update**: unzip verifies the unpacked binary (exists, non-empty, +x) before declaring success.

## 🔧 Maintenance

- Dedup: TLS fingerprint whitelist (single authority in model), `crudError` (3 identical wrappers), channel post-process goroutine, model-name splitting (`xstrings.SplitTrimCompactUnique`), Retry-After parsing (`utils/httputil`), settings URL validation, `StreamWriter` alias (twin interface removed), `relay.UpstreamReader` deleted, `parseIDParam` promoted (24 copies), balance formatting (single authority in `lib/utils`).
- `site-channel/index.tsx` split from 3191 → 2187 lines (HistorySummary / TableView / UnifiedCompletionDialog extracted); `site-channel-model` jump dead code removed (-97 lines).
- Multi-DB migrations 003/004/006/007 fixed; legacy 2026-09-05 audit findings merged into the ledger (4 closed, 19 recorded with verification status).

## 🔧 Upgrade Notes

- **Database migration 2026091401 runs on first start**: rows matching the F01 pollution signature (`manual_override=true AND group_multiplier_known=true AND group_multiplier=0`) are reset to `known=false` so the 0→1 normalization applies. This is a data (not schema) change and is idempotent; intentional "explicit free" rows set via the raw API with `known=true` will be reset and must be re-affirmed (the UI never sends that flag).
- **New setting** `stream_inactivity_timeout` (default 300, 0 disables) — slow-but-alive models are unaffected; only fully silent upstreams are aborted.
- Rollback to v1.8.4 is schema-safe. Take your usual backup anyway.
- Quality gates: full `go test`/`go vet`, `-race` on hot paths, `tsc`/ESLint/Next production build green.

---

## 中文更新说明

> 自 v1.8.4 起。English above.

### ✨ 重点

- **三轮对抗性审查全量闭环**：09-13 早/晚两轮与第三轮的全部发现已修复、按内网自用立场显式跳过、或带证据否决，统一收进唯一缺陷台账 `docs/BACKLOG.md`（全局唯一 ID，跨报告 F 编号复用成为历史）。
- **选路热路径与历史流量解耦**：24h 表现聚合改为 30s 进程内快照（此前每个代理请求同步全表聚合，延迟随上线时间线性恶化），候选取价批量预取（此前每候选 2 次 DB 往返）。
- **配额写改 write-behind**：成功请求不再同步 UPDATE 配额（SQLite 单写者下与日志/统计/VACUUM/导入互相排队），内存累加 + 5 分钟落库；限额判定读缓存值不受影响。
- **流式挂死有界**：新设置 `stream_inactivity_timeout`（默认 300 秒）判停流中途停摆；自动建组首 token 默认 120 秒；出站体 token 计数惰性化。

### 🐛 修复

- 熔断：Open 态在途失败不再顺延冷却；429/503 软失败连续达阈值也会熔断；WS 四处修复（回池窗口/字段竞争/首token重试/清理节流）。
- 数据：备份导入前 flush key 计费账本；余额接口瞬时失败保留上一份；聚合报表改名后不再挂旧名；错误信封不再误清 last_error；路由探测首个命中即短路。
- 启动与生命周期：启动任务按相位错峰；SIGHUP 不再误杀；SQLite `_txlock=immediate`；自更新失败回滚并交进程管理器。
- 前端：删除渠道/账号启停失败弹 toast；请求 30s 超时 + 网络错误走统一翻译；10 处幻影 setQueryData 清理；签到历史轮询降为 5 分钟。

### 🔧 升级须知

- **首次启动执行数据迁移 2026091401**：重置 F01 污染签名（手动设价倍率=0 且误标已知）的存量行，幂等可重跑；通过裸 API 显式声明的「免费」倍率会被一并重置，需重新确认。
- **新设置** `stream_inactivity_timeout`（默认 300，0 禁用）。
- 回滚到 v1.8.4 无 schema 风险，照常备份即可。
