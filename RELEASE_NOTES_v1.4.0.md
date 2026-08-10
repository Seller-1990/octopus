# Octopus v1.4.0 Release Notes

> English first, 中文见下方. — v1.3.5 → v1.4.0, 12 commits.

## ✨ Features

- **Two-state multiplier cap policy**: the default multiplier cap now only blocks entries with a *confirmed* (known=true) group multiplier exceeding the cap; tentative/unknown multipliers pass through (previously a conservative default could over-block). Multiplier provenance (known/unknown) is tracked across all write paths and shown in the UI as a "tentative" badge.
- **Channel×model tools capability detection**:
  - Five-state discrimination matrix (accepted / executed / required-unsupported / unsupported / pending) with per-protocol tool-call detection (OpenAI chat, Anthropic, Gemini, OpenAI Responses).
  - Key-level **"tools-only" routing**: API keys can opt to only route through channels confirmed tools-capable; confirmed-unsupported channels are skipped.
  - Evidence-hierarchy writes (manual probe > T9 real failures ≥2 > weak 2xx), admin **force-unsupported / restore** endpoints, async **batch probe** with task polling, and editor-level probe with local result write-back.
- **Circuit breaker management UI**: new "Circuit" page listing breaker state (channel/key/model, state, failures, trip count, cooldown) with single-item / per-channel / reset-all actions; snapshot now lazily prunes idle entries and exports structured state.
- **models.dev vision capability prefill**: multimodal (image/video input) marking on canonical models from models.dev `modalities.input`, with model-name suffix fallback (`5v`/`vision`/`-vl`); shown as a read-only badge on model cards.
- **LLM price update timeout**: 30s fallback when the update context has no deadline, so a hung models.dev fetch no longer permanently stalls subsequent price updates.

## 🐛 Fixes

- **T9 feedback 5xx misjudgment**: real-request failure feedback now only judges on 4xx (5xx gateway faults and 429/408 throttling excluded), so a 502 wrapping an upstream error no longer flips a healthy channel to tools-unsupported.
- **NULL-source guard fix**: old databases whose `supports_tools_source` is NULL were silently excluded by SQL guards; now COALESCE-empty-safe across all write paths.
- **Evidence hierarchy corrections** (post-review R1–R11): executed (manual probe) is overridable by ≥2 real failures (only admin `manual-force` is permanent); preset rebuild inherits all tools metadata; structured JSON parsing replaces string-scanning for tool-call detection.
- **Bootstrap credential hardening** (F01): removed the fixed `admin/admin` — first boot now requires an explicit `OCTOPUS_BOOTSTRAP_PASSWORD` (min 6 chars), otherwise startup refuses; default listen host is now `127.0.0.1` (bare-metal), with Docker compose pinning container `0.0.0.0` + loopback port mapping; password is never written to logs or the default config file.
- **Sync projection failure state** (F08): if managed-channel projection fails after a successful snapshot persist, the account status is corrected from `success` to `failed` and projection is marked stale (no more "sync succeeded" while routes stay stale).
- **Restore transient discard** (F07): WebDAV/upload restore discards in-process pending relay-log/usage state before import (with parse-validation guard and an in-flight flush barrier), preventing post-snapshot events from being replayed into the restored database.
- **Frontend**: double-click no longer starts duplicate paid batch probes; batch polling failures surface a toast; tools-only filter is rendered independently of vendor-chip count; touch devices can reach row-level tools actions.

## 🧹 Docs & Housekeeping

- Review reports archived to `docs/reviews/`; removed 10 one-off planning/handoff documents and orphan resources.
- Multi-round adversarial review incorporated for the tools pipeline (4 P0, 12 P1, 9 P2 fixed across review rounds).

---

## 🐙 v1.4.0 发布说明（中文）

> 自 v1.3.5 起，12 个提交。English above.

## ✨ 新功能

- **两态倍率上限策略**：默认倍率上限只拦截「已确认（known=true）且超限」的分组倍率；暂定/未知倍率放行（原先保守默认可能过度拦截）。倍率溯源（known/unknown）贯穿所有写路径，UI 以「暂定」徽标展示。
- **渠道×模型 tools 能力探测**：
  - 五态判别矩阵（accepted / executed / required-unsupported / unsupported / pending），按协议检测工具调用（OpenAI chat、Anthropic、Gemini、OpenAI Responses）。
  - Key 级**「仅 tools」路由**：API Key 可只路由到已确认支持 tools 的渠道，确认不支持的渠道被跳过。
  - 证据层级写库（手动探测 > T9 真实失败 ≥2 > 弱 2xx）、管理员**强制标不支持 / 恢复**端点、异步**批量探测** + 任务轮询、编辑器内探测与本地结果回写。
- **熔断管理界面**：新增「熔断」页面，展示熔断状态（渠道/key/模型、状态、连续失败、熔断次数、剩余冷却），支持单条 / 按渠道 / 全部重置；快照惰性清理闲置条目并导出结构化状态。
- **models.dev 视觉能力预填**：基于 models.dev `modalities.input` 给模型入口标注多模态（图像/视频输入），模型名后缀（`5v`/`vision`/`-vl`）兜底；模型卡片只读徽标展示。
- **LLM 价格更新超时**：更新上下文无 deadline 时 30s 兜底——models.dev 挂起不再永久阻塞后续价格更新。

## 🐛 修复

- **T9 反馈 5xx 误判**：真实请求失败反馈仅在 4xx 判定（排除 5xx 网关故障与 429/408 限流）——502 包裹上游错误不再把健康渠道误标为不支持 tools。
- **NULL 来源守卫修复**：旧库 `supports_tools_source` 为 NULL 的行曾被 SQL 守卫静默排除，现所有写路径均 COALESCE 空值安全。
- **证据层级修正**（审查 R1–R11）：executed（手动探测）可被 ≥2 次真实失败覆盖（仅管理员 `manual-force` 永久）；preset 重建继承全部 tools 元数据；工具调用检测改为结构化 JSON 解析（替代字符串扫描）。
- **Bootstrap 凭据加固**（F01）：取消固定 `admin/admin`——首启必须显式设置 `OCTOPUS_BOOTSTRAP_PASSWORD`（最少 6 位），否则拒绝启动；默认监听改为 `127.0.0.1`（裸机），Docker compose 固定容器内 `0.0.0.0` + 回环端口映射；密码绝不写入日志或默认配置文件。
- **同步投影失败状态**（F08）：快照持久化成功后托管渠道投影失败时，账号状态从 `success` 纠正为 `failed` 并标记投影 stale（不再出现「同步成功」而路由仍旧值）。
- **恢复时丢弃瞬态**（F07）：WebDAV/上传恢复在导入前丢弃进程内 pending 的 relay-log/用量状态（带解析校验保护与在途 flush 栅栏），防止备份时点之后的事件回灌进恢复后的数据库。
- **前端**：双击不再重复启动付费批量探测；批量轮询失败弹出提示；「仅 tools」筛选独立于厂商标签数量渲染；触屏设备可触达行级 tools 操作。

## 🧹 文档与清理

- 审查报告归档至 `docs/reviews/`；删除 10 份一次性计划/交接文档与孤儿资源。
- tools 管线经多轮对抗审查（各轮修复 4 P0、12 P1、9 P2）。
