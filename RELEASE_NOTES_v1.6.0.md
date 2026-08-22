# Octopus v1.6.0 Release Notes

> English first, 中文见下方. — v1.5.1 → v1.6.0.

## ✨ Features

- **Per-key forensics in request logs**: each retry attempt now records which channel key served it. The log timeline shows the key remark, the actual multiplier applied to that attempt, and the failure reason; attempts that failed slowly (>10s) are highlighted so bad keys can be spotted and disabled quickly.
- **Redesigned capability icons**: model capability badges (multimodal / reasoning / voice / image / video) now use a consistent line-icon set instead of emoji.
- **Better capability matching**: model names with release suffixes (`-20241022`, `-latest`, …) now resolve against the models.dev index after suffix stripping; suffix heuristics additionally cover `glm-4v`, `internvl`/`vl` family, `llava`, and `-think` variants.
- **Streamlined group card detection area**: the duplicate manual-probe button (identical to the badge's built-in Run) was removed, "Run Full" moved into the health detail dialog, and the batch tools probe got a distinct icon from the per-row single test.

## 🐛 Fixes

Full code-review round (2026-08-22, every finding re-verified before fixing):

- **Group health checks can no longer get stuck forever**: a crash or one failed DB write used to leave the snapshot in `running` permanently (every later check returned 409 until manual DB surgery). Running snapshots now get a deferred terminal state and 10-minute staleness threshold.
- **Image-heavy requests no longer stall before forwarding**: the full-body BPE token estimate (quadratic on base64 runs, codec rebuilt per call) is replaced by a singleton encoder plus byte-length estimation for payloads over 64 KB.
- **WebSocket pool double-leasing fixed**: a connection could be handed to two requests simultaneously during the preflight ping window, interleaving two upstream sessions.
- **Channel-key cost rollback fixed**: long streaming requests used to write back a minutes-old snapshot of `TotalCost`, erasing concurrent requests' accumulated spend; runtime updates now apply increments under a per-channel lock.
- **Quota no longer leaks on client disconnect**: the quota update now uses `context.WithoutCancel` (matching the log path), so a client closing right after the response still gets billed correctly.
- **"Today" stats no longer show yesterday across midnight**; stats caches (channel/model/apikey) got per-entity locking against lost updates.
- **Frontend**: API-key dashboard keeps cached data (with a loading state) instead of flipping to a full error screen when the backend briefly restarts; live log list is capped at 500 entries; persisted log date range is no longer reset on every mount; unreachable SSE branch in `useLogs` removed.
- **Hot-update integrity**: when `sha256sums.txt` cannot be fetched, the updater now refuses to install instead of silently skipping checksum verification.
- **Data race in early-heartbeat tests fixed** (plus the same theoretical race guarded in `FlushOrError`).

Post-review stabilization round:

- **CI frontend gate fixed**: two long-standing `set-state-in-effect` lint errors (the reason every push since 08-21 failed CI) plus an `exhaustive-deps`/memoization warning — all resolved with proper React patterns; `pnpm lint` is now clean.
- **Terminal-failure error details preserved**: when the upstream stream ends with `response.failed` / `error` events, the attempt log and live log now carry the upstream error message (previously only the event name was recorded, losing the actual failure cause).
- **Failover order made deterministic**: same-priority members used to be reordered non-deterministically by an unstable sort; priority ordering is now stable with a deterministic DB load order (`priority ASC, id ASC`), locked by a regression test.

## 🚀 Performance

- Quota accounting: per-key locks instead of one global mutex, and the read-back SELECT after every successful request is gone.
- Large request bodies are pre-truncated before base64 redaction in log persistence (an order of magnitude less allocation on 15 MB image requests).

## 🔧 Refactoring

- **Anthropic streaming deduplicated (−711 lines)**: the legacy chunk-style stream transformers were replaced with 5-line delegations to the single event-based state machine (same pattern as the OpenAI adapters), so streaming logic now lives in exactly one place.
- **Dead `StatsModel` accounting layer removed (−68 lines)**: it had no writers and no readers.
- **~980 lines of dead code removed** across backend and frontend (legacy model-price page, unused hooks, write-only caches, dead wrappers).

**Full Changelog**: https://github.com/Seller-1990/octopus/compare/v1.5.1...v1.6.0

---

# 🐙 Octopus v1.6.0 发布说明（中文）

> 自 v1.5.1 起。English above.

## ✨ 新功能

- **请求日志的 key 级取证**：每次重试尝试现在记录实际使用的渠道 key。日志时间线展示 key 备注、该次调用的实际倍率与失败原因；失败且耗时超过 10 秒的尝试会标红加 ⚠，便于快速识别并禁用劣化 key。
- **能力图标重制**：模型能力徽标（多模态/推理/语音/生图/视频）从 emoji 更换为风格统一的线性图标集。
- **能力匹配增强**：带发布后缀的模型名（`-20241022`、`-latest` 等）在精确匹配失败后会剥后缀重查 models.dev 索引；后缀推断补充覆盖 `glm-4v`、`internvl`/`vl` 系列、`llava` 与 `-think` 变体。
- **分组卡片检测区精简**：删除与徽标内置 Run 完全重复的手动探测按钮；「Run Full」移入健康详情对话框；批量 tools 检测换用与行内单测不同的图标。

## 🐛 修复

完整代码审查轮次（2026-08-22，每项均经复核确认后修复）：

- **分组健康检查不再可能永久卡死**：此前进程崩溃或一次数据库写失败会让快照永远停留 running（后续检查全部 409，只能手改数据库恢复）。现在中断路径兜底写终态，并对遗留 running 记录设 10 分钟过期阈值。
- **含图请求不再在转发前卡顿**：全量 BPE 分词估算（对 base64 长跑接近平方级、且每次调用重建编解码器）改为全局单例 + 超过 64KB 的负载按字节长度估算。
- **修复 WS 连接池「双租用」**：preflight Ping 窗口内一条上游连接可能被同时分给两个请求，导致两个会话内容交错。
- **修复渠道 key 成本回滚**：长流式请求结束时会把数分钟前的 `TotalCost` 快照整体写回，抹掉并发请求的累计成本；运行时更新改为 per-channel 锁内增量。
- **客户端断连不再漏扣配额**：配额落账改用 `context.WithoutCancel`（与日志路径一致），客户端收完响应立即断开时费用仍正确入账。
- **「今日统计」跨日零点后不再显示昨日全天**；stats 三处缓存（channel/model/apikey）补 per-entity 锁，消除并发覆盖。
- **前端**：后端短暂重启时 API Key 面板保留缓存数据并显示加载态，不再整页翻车；实时日志列表加上 500 条上限；持久化的日志日期范围不再每次挂载被重置；删除不可达的 `useLogs` SSE 分支。
- **热更新完整性**：sha256sums.txt 获取失败时拒绝安装，不再静默跳过校验替换二进制。
- **修复 early-heartbeat 测试的数据竞争**（并对 `FlushOrError` 的同类理论竞争加防护）。

审查后的稳定化修复：

- **CI frontend 门禁修复**：两个自 08-21 起导致每次 push 失败的 `set-state-in-effect` lint error 与 `exhaustive-deps`/记忆化告警全部以正确的 React 模式解决，`pnpm lint` 归零。
- **终态失败错误详情保留**：上游流以 `response.failed` / `error` 事件终止时，尝试日志与实时日志现在携带上游错误信息（此前只记事件名，真实失败原因丢失）。
- **failover 顺序确定化**：同优先级成员此前被不稳定排序随机重排；现改为稳定排序并配合确定的加载序（`priority ASC, id ASC`），回归测试锁定顺序契约。

## 🚀 性能

- 配额记账：per-key 锁替代全局互斥，删除每个成功请求后的回读 SELECT。
- 日志持久化对超大请求体先预截断再做 base64 消隐（15MB 含图请求收尾分配降一个数量级）。

## 🔧 重构

- **Anthropic 流式去重（-711 行）**：旧块式流转换器替换为对唯一事件状态机的 5 行委托（与 OpenAI 适配器范式一致），流式逻辑从此只在一处维护。
- **删除死的 `StatsModel` 记账层（-68 行）**：它既无写入方也无读取方。
- **清除约 980 行死代码**（后端+前端：旧模型价格页、无用 hooks、只写不读缓存、死包装函数）。

**完整变更**: https://github.com/Seller-1990/octopus/compare/v1.5.1...v1.6.0
