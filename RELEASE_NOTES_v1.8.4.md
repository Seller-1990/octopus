# Octopus v1.8.4 Release Notes

> English first, 中文见下方. — v1.8.3 → v1.8.4.

## ⚠️ Breaking / Action Required

- **Graceful shutdown drains before closing**: `Close` now runs `http.Server.Shutdown` (10s drain) before closing listeners, and the stats/log flush budget needs more than the Docker default grace window. Docker users should set `stop_grace_period: 40s` (the bundled `docker-compose.yml` already does); with the default 10s, SIGKILL may truncate tail writers (daily stats / logs).
- **Listen failure is fatal**: an occupied port or invalid bind address now returns `EADDRINUSE` and stops startup (IPv6-safe `JoinHostPort` + synchronous bind) instead of continuing without a working listener.
- **ZIP export fails loud**: export pre-checks the import-side capacity contract (4 MiB record / 128 MiB entry / 256 MiB archive, per-dialect byte-length accounting) and writes through a temp file — an oversized export now errors instead of producing an archive the importer would reject, and a failed export can no longer be mistaken for success.

## ✨ Highlights

- **Channel management rebuilt (AxonHub-style)**: the manual-channel page is now a full management page — a data-table view (10 columns, colored protocol badges, sticky header, pagination with 10/20/50/100 rows), type/status/free-transit filters with persistent state, always-visible search across name/model/URL/key-remark, batch enable/disable/delete with failure-aware selection, per-row connection test, and a global "sync channel models" entry (`/channel/sync` + `/last-sync-time` existed without any UI). The detail dialog now shows type/proxy/protocol/models/keys at a glance; the 735-line form was split into four sections with dead i18n keys removed and hardcoded Chinese migrated (zh_hans/zh_hant/en key-parity).
- **Provider templates for creating channels**: a grouped quick-fill picker with 26 providers — OpenAI (Chat & Responses), Anthropic, Gemini, DeepSeek (OpenAI & Anthropic-compatible), Moonshot Kimi, Zhipu GLM, MiniMax (CN/Intl), DashScope, Qianfan v2, Hunyuan, Volcengine Ark, SiliconFlow, OpenCode Zen, OpenRouter, Groq, xAI, Mistral, Together, Fireworks, Cerebras, Ollama, LM Studio, vLLM. Every base URL was cross-checked against each provider's docs and Octopus's per-adapter path rules.
- **Sync→checkin reconcile**: after a successful site sync, a failed same-day check-in is retried immediately (gated by same-day-success protection, a 30-minute minimum interval, and a fail-streak cap) instead of waiting out the whole backoff — the "sync succeeded but check-in still shows failed" case is closed.
- **One-paste verification-bridge pairing**: the web side shows one copyable `address#token` line; the extension popup pairs from a single field. Sync reuses the fixed pairing and rotates tokens.

## 🐛 Fixes

- **Stats correctness**: snapshot→DB saving is serialized through one lock (a stale snapshot can no longer overwrite newer totals); day-rollover persists the previous day *before* flipping the cache (crash window closed); hourly filtering uses the snapshot-time date (no more lost tail-window rows after midnight); lock-timeout on the hot path enqueues a retry instead of blocking requests.
- **Web session hardening**: a late 401 from a pre-login request no longer logs out the fresh session (ownership guard); SSE reconnect uses snapshot-replacement and live-trace ids converge with snapshots (no more ghost running entries).
- **Backup/WebDAV**: export-side token-limit enforcement with preflight NULL handling; WebDAV targets on plaintext HTTP and parse failures now leave audit logs.
- **Gemini model listing**: bare-hostname channels (e.g. the official API domain) gain the same `/v1beta` fallback the chat path already had — fetch-models no longer 404s while chat works.
- **Clipboard**: copy falls back to `execCommand` when `navigator.clipboard` is unavailable (plain-HTTP deployments).
- **Verification bridge**: pairing lookup errors are no longer treated as "not found" (which could silently re-pair); sync reuses the fixed pairing and rotates the token.

## 🔧 Upgrade Notes

- **No database migrations in this release** — rollback to v1.8.3 is schema-safe. Take your usual backup anyway.
- **Docker**: after upgrading, verify `stop_grace_period: 40s` and check the version on Settings → info. If you keep a self-update binary in the data volume, make sure it does not shadow the image binary.
- **Frontend**: a new persistence key `octopus:channel-filters` stores channel filters (type/status/free-transit). Broken values self-heal via the visible "clear filters" button. The channel page defaults to the new table view; the old grid/list card views remain available from the in-page view switcher.
- Quality gates: full `go test`/`go vet`, `tsc`/ESLint/Next build green; six adversarial review rounds over the channel-page work with all findings fixed or explicitly registered (see `audit-report-octopus-2026-09-12.md` for the round that motivated this release).

---

## 中文更新说明

> 自 v1.8.3 起。English above.

### ⚠️ 破坏性 / 需要操作

- **停机协议改为先排空再关闭**:`Close` 先执行 `http.Server.Shutdown`(10s 排空)再关闭监听,统计/日志落库需要超出 Docker 默认宽限的时间窗。Docker 用户请设置 `stop_grace_period: 40s`(仓库自带 `docker-compose.yml` 已设置);沿用默认 10s 时,SIGKILL 可能截断尾部落库(日统计/日志)。
- **监听失败即启动失败**:端口被占用或绑定地址非法时返回 `EADDRINUSE` 并终止启动(IPv6 安全的 `JoinHostPort` + 同步 bind),不再带着不可用监听继续跑。
- **ZIP 导出快速失败**:导出前预检导入侧容量契约(单记录 4 MiB / 单条目 128 MiB / 归档 256 MiB,按方言计算字节长度),并经临时文件中转——超限导出直接报错,失败不再被误当成功。

### ✨ 主要更新

- **渠道管理整页重构(AxonHub 风格)**:普通渠道页升级为完整管理页——数据表格视图(10 列、彩色协议徽章、吸顶表头、10/20/50/100 每页分页)、类型/状态/公益中转筛选(持久化)、常驻搜索(名称/模型/地址/Key 备注)、批量启用/停用/删除(失败感知)、行内连通性测试、全局「同步渠道模型」入口(端点早已存在但没有 UI)。详情弹窗补全类型/代理/协议/模型/Key 展示;735 行表单拆为四分区;硬编码中文全部迁入 i18n(三语言 key 对齐)。
- **新建渠道提供商模板**:分组快速填充 26 家——OpenAI(Chat 与 Responses)、Anthropic、Gemini、DeepSeek(OpenAI 与 Anthropic 兼容)、Moonshot Kimi、智谱 GLM、MiniMax(国内/国际)、阿里百炼、百度千帆 v2、腾讯混元、火山方舟、硅基流动、OpenCode Zen、OpenRouter、Groq、xAI、Mistral、Together、Fireworks、Cerebras、Ollama、LM Studio、vLLM。全部 base_url 逐一对照各官方文档与 Octopus 各适配器的路径拼接规则核验。
- **同步后补签对账**:站点同步成功后,当天失败的签到立即重试(受当日成功保护、30 分钟最小间隔、连续失败上限约束),不再等完整退避周期——「同步完成了但签到状态还是异常」就此关闭。
- **验证桥一键配对**:网页端展示可整行复制的 `地址#令牌`,扩展弹窗单字段粘贴完成配对;同步复用固定配对并轮换令牌。

### 🐛 修复(节选)

- 统计正确性:快照→落库经单一锁串行化(旧快照不再覆盖新累计值);日切翻转前先落库昨日快照(关闭崩溃窗口);hourly 过滤使用快照时刻日期(跨零点尾窗不再丢失);热路径锁超时挂起重试队列不再阻塞请求。
- Web 会话加固:登录前旧请求的迟到 401 不再登出新会话(归属守卫);SSE 重连采用快照替换语义,实时追踪 ID 集合随快照收敛(幽灵 running 清除)。
- 备份/WebDAV:导出端 token 上限校验与 NULL 预检;明文 HTTP 目标与解析失败留审计日志。
- Gemini 拉取模型:裸主机渠道获得与 chat 一致的 `/v1beta` 回退,fetch-models 不再 404。
- 剪贴板:HTTP 非安全上下文回退 `execCommand`。
- 验证桥:配对查询错误不再被当作「不存在」而静默重建配对;同步复用固定配对并轮换令牌。

### 🔧 升级说明

- **本版本无数据库迁移**——回滚到 v1.8.3 无Schema 风险。照常做数据备份即可。
- **Docker**:升级后确认 `stop_grace_period: 40s`,并在设置→信息面板核对版本号。数据卷中若保留自更新二进制,确认它不会遮蔽镜像内二进制。
- **前端**:新增持久化键 `octopus:channel-filters`(渠道筛选),异常值可通过「清除筛选」按钮自救。渠道页默认表格视图,旧网格/列表卡片视图仍可在页内切换。
- 质量门禁:`go test`/`go vet`、`tsc`/ESLint/Next build 全绿;渠道页相关工作共经历六轮对抗审查,命中项全部修复或登记(见 `audit-report-octopus-2026-09-12.md`)。
