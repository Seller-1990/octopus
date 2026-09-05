# Octopus v1.8.2 Release Notes

> English first, 中文见下方. — v1.8.1 → v1.8.2.

## ⚠️ Breaking / Action Required

- **Credentials are now masked in list responses**: API keys and site-account secrets (username / password / access_token / api_key / refresh_token / tokens) are returned masked from `GET /apikey/list` and the site list endpoints. Copying or exporting now goes through an explicit per-secret **reveal** action. Anything that scraped plaintext from list responses (personal scripts, automations, or a browser tab left open from the previous version) will silently receive masked values — refresh the page after upgrading and re-check any integration that copies keys from the UI. Editing is safe: masked values are detected and never written back to storage.
- **Check the running version after upgrading (Docker)**: the container entrypoint prefers an `octopus-updated` binary left in the data volume over the image binary. If you have ever used the in-app self-update, a stale `octopus-updated` will silently shadow the new image on both upgrade *and* rollback. Remove `/app/data/octopus-updated` when upgrading and verify the version on the Settings → info panel.

## ✨ Highlights

- **Log page converges to two tabs, with in-flight requests visible**: the standalone "Live" tab is gone. Its unique capabilities — seeing requests while they are still running (spinner + per-second duration), watching the channel-retry stream, and stopping the current attempt — now live at the top of the log detail list as an inline "running" section. Completed requests hand off seamlessly into the history list below; a 60 s TTL guarantees eviction even under fixed historical windows, active filters, or after clearing logs.
- **Real-time log streams are now multi-subscriber**: the overview and per-request detail SSE streams previously allowed a single viewer (a second connection kicked the first, and a slow consumer with a full buffer cut the stream). Every connection now owns its subscription; two admin windows work side by side, and a slow tab only drops itself, not others. Reconnects back off exponentially (1 s → 30 s).
- **SQLite file high-water mark reclamation**: a daily task runs `VACUUM` when freelist pages exceed 25% of a database larger than 64 MB — SQLite deletes rows but never shrinks the file, so long-running instances keep growing even with log retention working. The VACUUM connection forces `temp_store=FILE` (overriding the in-memory temp store that would otherwise OOM on multi-GB databases), and a `wal_checkpoint(TRUNCATE)` follows so the `-wal` file doesn't keep the space.

## 🐛 Fixes

- **Relay stability batch**: token-version revocation replaces JWT-key rotation on password change (decoupling it from the AEAD data key); circuit-breaker accounting for compact mode keys off the upstream model name; weighted sampling upgraded to Efraimidis–Spirakis (previously biased); `APIKeyDelete` fixes an error-order bug and defers stats cleanup until after the delete commits; route-candidate health and post-delete writes now refresh the catalog cache.
- **Log/usage pipeline**: stream tokens are consumed atomically (single use, 60 s TTL) so a token leaked into proxy logs can't be replayed; usage aggregation is phase-offset from stats saving (ends the recurring `busy_timeout` failures) with bounded retries; verification sessions/tasks get a 7-day physical retention sweep; handler errors converge to a generic `InternalError` instead of echoing `err.Error()`.
- **SSE correctness**: the gzip exclusion regex actually matches the real SSE routes now — in v1.8.1 both live streams were silently gzip-buffered, breaking per-event flush semantics; the dead `GET /api/v1/log/stream` endpoint (zero frontend callers) was removed; the frontend's local `/log/list` snapshot (racy against the SSE snapshot, could blank running cards mid-request) was dropped in favor of the server's connection snapshot.
- **Build/release**: release builds now hard-fail on a dirty worktree (prevents WIP code leaking into published images); dependency installation in `build.sh` is locked.

## 🚀 UX Improvements

- Retention hint on the log detail page: picking a range older than the configured retention shows "logs are only guaranteed for the last N days" instead of silently empty results.
- Indeterminate outcomes no longer masquerade as "failed" in live views (previously a request could flip from failed to unknown when it landed in the DB).
- Log detail is leaner on the hot path (fewer per-request queries; dead code removed).

## 🔧 Upgrade Notes

- The first automatic `VACUUM` runs ~24 h after upgrade (phase-offset daily task). If your production DB is already multi-GB, expect a minutes-long exclusive write lock and ~2–3× disk space transiently; consider running one `VACUUM` manually off-peak first, and make sure the host disk has that headroom. On failure it rolls back atomically and retries the next day.
- Rollback to v1.8.1 is schema-safe (no new migrations in this release); hard-refresh the browser once afterwards.
- If live logs appear frozen after upgrading, refresh the page once — pre-upgrade tabs hold a consumed stream token; the new frontend re-tokens on every (re)connect.

---

## 中文更新说明

> 自 v1.8.1 起。English above.

## ⚠️ 破坏性变更 / 升级必读

- **列表接口凭证改为掩码返回**：API Key 与站点账号的敏感字段（用户名/密码/access_token/api_key/refresh_token/tokens）在列表接口中只返回掩码值，复制/导出改走逐条 **reveal** 显式明文。任何依赖列表接口拿明文的脚本/自动化会静默拿到掩码值——升级后请刷新页面，并自查相关集成。编辑路径安全：掩码值会被识别、绝不回写存储。
- **升级后请核对运行版本（Docker）**：容器入口脚本优先执行数据卷里残留的 `octopus-updated` 二进制而非镜像内程序。如果曾用过应用内自更新，旧二进制会同时劫持「升级」和「回滚」。升级时删除 `/app/data/octopus-updated`，并在设置页信息面板核对版本号。

## ✨ 亮点

- **日志页收敛为两个 tab，进行中请求可见**：独立「实时」tab 撤销，其独有能力——运行中请求可见（转圈 + 秒级耗时跳动）、查看渠道重试过程、停止当前尝试——迁入日志明细列表顶部，以「进行中」区块内联呈现；完成的请求无缝落入下方历史列表。fixed 历史窗口、业务筛选、清空日志等场景由 60 秒 TTL 兜底逐出，不会永久滞留。
- **实时日志流改多订阅者**：概览流与请求详情流此前为单观众架构（第二条连接踢掉第一条、慢消费者缓冲写满即断流）。现在每条连接持有独立订阅——两个管理窗口并排工作互不干扰，慢 tab 只断自己。重连改为指数退避（1 秒起步、30 秒封顶）。
- **SQLite 文件高水位回收**：每日任务在「库 > 64MB 且 freelist 占比 > 25%」时执行 `VACUUM`——SQLite 删除只挂 freelist 不缩文件，长期运行的实例即使日志保留期正常工作也会持续膨胀。VACUUM 连接强制 `temp_store=FILE`（避免临时库进内存导致几 GB 库 OOM），结束后补 `wal_checkpoint(TRUNCATE)` 防止 `-wal` 文件抵消回收成果。

## 🐛 修复

- **中继稳定性批次**：改密改为 token 版本撤销（与 AEAD 数据密钥解耦）；compact 模式熔断记账改按上游模型名；加权采样升级为 Efraimidis–Spirakis 算法（旧实现有偏）；`APIKeyDelete` 修复错误顺序并把统计清理后置到删除提交之后；路由候选健康度与删除后回写 catalog 缓存。
- **日志/统计链路**：日志流 token 改一次性原子消费（60 秒 TTL），泄露到代理日志的 token 无法重放；usage 聚合与 stats 保存错峰（终结反复的 `busy_timeout` 失败）并加有界重试；验证会话/任务增加 7 天物理保留期清理；handler 错误统一收敛为 `InternalError`，不再直出 `err.Error()`。
- **SSE 正确性**：gzip 排除正则实际命中真实 SSE 路由——v1.8.1 的两条实时流一直在被 gzip 缓冲破坏逐条 Flush 语义；删除前端零调用的 `GET /api/v1/log/stream` 死端点；移除前端本地 `/log/list` 快照（与 SSE 快照竞态，可能把 running 卡片整批抹掉），统一以服务端建连快照为准。
- **构建发布**：发布构建强制干净树检查（防止 WIP 代码混入发布镜像）；`build.sh` 依赖安装锁定。

## 🚀 体验优化

- 日志明细页超期提示：选择超出保留期的范围时显示「明细仅保证保留最近 N 天」，不再静默返回空结果。
- 「结果不确定」终态不再被实时视图误显示为「失败」（此前落库后状态会从失败翻转）。
- 日志明细热路径减查询、清理死代码。

## 🔧 升级注意

- 首次自动 `VACUUM` 在升级后约 24 小时（错相的每日任务）。若生产库已达 GB 级，预计会有分钟级独占写锁与约 2–3 倍库大小的瞬时磁盘占用；建议先在低峰手动跑一次 `VACUUM` 实测耗时，并确认宿主机磁盘余量。失败会原子回滚、次日重试，无损。
- 回滚到 v1.8.1 无 schema 风险（本版本无新增迁移）；回滚后浏览器强制刷新一次即可。
- 升级后若实时日志「不动了」，刷新一次页面即可——旧标签页持有已消费的 stream token；新前端每次（重）连都会重新取 token。
