# Octopus v1.8.6 Release Notes

> English first, 中文见下方. — v1.8.5 → v1.8.6.

## ✨ Highlights

- **Verification bridge retry storm root-caused and fixed**: the Cloudflare misjudgment that turned any HTML response behind a CF proxy into "needs human verification" is fixed (200 responses now require challenge evidence; non-200 requires an HTML shape), and the ensure-verify-retry loop gained two breakers (browser retries never re-create sessions; a 30-minute cooldown per account+operation after a failed retry). The 2026-09-21 storm (1,344 failed retries in one day) is structurally impossible now.
- **Price comparison view**: the model page gains a third tab comparing the same model across sites/accounts/groups — raw price and effective USD price (group multiplier × exchange rate) side by side, cheapest row highlighted, "multiplier unknown" flagged, and a ≥2× spread warning to catch misconfigured accounts. Reuses the authoritative routing price conversion (no second arithmetic).
- **Task result notifications**: a new in-app notification center (top-bar bell, last 100 events, unread watermark, optional browser notifications) plus a generic Webhook sink (5s timeout, no retry, event-type filter) so check-in/sync failures and verification retries push themselves instead of waiting to be discovered. Backed by an in-memory ring — notifications are not data, nothing enters the database.

## 🐛 Fixes

- **Relay stats truthfulness**: streams that end on EOF without the protocol terminal frame (`[DONE]` / `message_stop` / `response.completed`) are now recorded as **failed** (upstream truncation) instead of success — success rates no longer inflated; such attempts no longer bind sticky sessions. Read/write failures flush already-received usage through the metrics finalizer (Anthropic `message_start` input tokens no longer evaporate). Upstream SSE comment keep-alives no longer count as first token (TTFT is measured to the first real content; heartbeat-only streams are treated as empty and fail over).
- **Web**: notification bell guards — "mark all read" can no longer lower the read watermark when the list is empty; browser notifications use a persisted notified-watermark so reconnect snapshot replays don't spam system notifications; permission-prompt rejections are handled.
- **Price compare**: site/account display-name lookups propagate errors instead of silently returning blank names.

## 🔧 Maintenance

- Migration 029 adds a `LOWER(model_name, observed_at)` expression index for the compare panel (MySQL falls back to a plain column index; documented trade-off).
- New settings `notify_webhook_url` / `notify_webhook_events` / `notify_webhook_enabled` (URL is excluded from backup exports).
- Delivery process: every PR this cycle ran the deterministic gate (gofmt/vet/golangci-lint-incremental/tests) plus the local LLM reviewer (ocr), with findings adopted or explicitly dispositioned.

## 🔧 Upgrade Notes

- **Database migration 2026092501 runs on first start**: creates the compare-panel index (online, no data change).
- **New settings**: Webhook notifications are disabled by default — configure them under Settings → 任务结果通知.
- Rollback to v1.8.5 is schema-safe (the new index is additive). Take your usual backup anyway.
- Quality gates: full `go test`/`go vet`, incremental golangci-lint, `tsc`/ESLint/production build green; notify package covered by `-race` concurrency tests.

---

## 中文更新说明

> 自 v1.8.5 起。English above.

## ✨ 亮点

- **验证桥重试风暴根治**：修复「Cloudflare 误判」（任何经过 CF 代理的 HTML 响应都被当成需要人机验证）——200 响应必须命中挑战证据、非 200 需 HTML 形态；"验证-失败-再验证"循环加了两道熔断（浏览器重试不再重建会话、同账号同操作失败后 30 分钟冷却）。9/21 单日 1344 条失败的风暴在结构上不可能复现。
- **价格对比视图**：模型页新增第三个 tab，同一模型跨站点/账号/分组横向对比——原价与到手价（分组倍率×汇率）并排、最便宜行高亮、倍率未知打标、价差 ≥2 倍警示（提示检查倍率错配）。折算复用路由打分同一权威口径，数字不会两说。
- **任务结果通知**：新增站内通知中心（顶栏铃铛、最近 100 条、已读水位、可选浏览器通知）+ 通用 Webhook（5s 超时不重试、事件类型过滤）——签到/同步失败与验证重试结果主动推送，不用再盯面板。通知存内存 ring，不入库。

## 🐛 修复

- **relay 统计可信度**：流以 EOF 结束但未见协议终态帧（`[DONE]`/`message_stop`/`response.completed`）现在记为**失败**（上游截断），成功率不再虚高、不再绑定会话保持；读/写失败会把已收到的用量补进账（Anthropic `message_start` 的 input tokens 不再蒸发）；上游 SSE 注释心跳不再被当作首 token（TTFT 量到首个真实内容，纯心跳流按空流处理可 failover）。
- **Web**：通知铃铛加固——"全部已读"在列表为空时不再把水位降回 0；浏览器通知改持久化已通知水位，重连快照重放不再轰炸；权限申请被拒有兜底。
- **价格对比**：站点/账号名查询失败改为报错，不再静默返回空名字。

## 🔧 维护

- 迁移 029 为对比面板加 `LOWER(model_name, observed_at)` 表达式索引（MySQL 退化为普通列索引，取舍已注释）。
- 新设置 `notify_webhook_url` / `notify_webhook_events` / `notify_webhook_enabled`（URL 已加入备份脱敏清单）。
- 本周期每个 PR 均跑确定性门禁（gofmt/vet/增量 golangci-lint/全量测试）+ 本地 LLM 评审（ocr），发现逐条采纳或明示处置。

## 🔧 升级注意

- **首次启动运行数据库迁移 2026092501**：创建对比面板索引（在线操作，无数据变更）。
- **新设置**：Webhook 通知默认关闭——在 设置 → 任务结果通知 中配置。
- 回滚到 v1.8.5 对 schema 安全（新索引是增量）。照常备份即可。
- 质量门禁：全量 `go test`/`go vet`、增量 golangci-lint、`tsc`/ESLint/生产构建全绿；notify 包有 `-race` 并发单测。
