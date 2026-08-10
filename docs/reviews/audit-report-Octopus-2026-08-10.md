# Fuck My Shit Mountain Audit Report

**Project:** Octopus
**Audit mode:** full
**Date:** 2026-08-10
**Reviewer:** OpenAI Codex (GPT-5)

---

## 1. Executive Summary

本次审计覆盖当前 `dev` 分支 HEAD `27febc546bf0aaabe802d3ab03f0e1b6e696cca6` 的后端、前端、启动/停机、同步、备份恢复、发布流水线和依赖树。项目已经具备较完整的业务能力和一定的防御性基础：请求头策略会屏蔽敏感代理头，verification bridge 使用 hash token 和账号范围约束，stream processor 区分终态与传输终止，数据库启用 WAL、busy timeout 和 foreign keys，现有 Go/前端构建门禁在本地均通过。

主要风险集中在跨层状态的生命周期，而不是单个算法错误：删除 site/account 时 binding 被先删，导致 managed channel 失去受控删除路径；在线 restore 不冻结后台队列，旧请求状态可能回灌恢复后的数据库；同步状态在投影前标记成功；API key 额度采用完成后记账，存在并发穿透。安全面还存在固定 `admin/admin` 首启凭据、明文凭据备份和默认全网卡监听。前端则有 SSE 首屏丢日志、站点动作目标重复批量查询、错误 React Query key 和虚拟列表卸载导致批任务停止轮询等可复现状态问题。

结论适合按“先阻断数据/凭据/发布风险，再修复一致性和前端状态”的顺序处理。没有把纯样式偏好、理论上的重构愿望或无法从当前入口触发的缓存契约问题冒充为核心 finding；未做真实端口抢占、并发额度压测、OOM 压测和移动端 Playwright 复现的项目在报告中明确标注。

### Score Dashboard

```
Security        ██████░░░░  5.5  C   默认凭据、明文备份凭据和全网卡管理面暴露
Stability       ██████░░░░  5.8  C   启动假成功、在线恢复回灌和非优雅停机
Performance     ██████░░░░  6.2  B   relay body 无上限，日志 SSE 会放大查询
Testing         █████░░░░░  4.8  C   Go 单测丰富，但核心并发/恢复/前端状态链缺集成测试
Maintainability ██████░░░░  5.6  C   大型模块与跨层缓存约定增加变更成本
Design          ██████░░░░  6.0  B   分层总体清晰，但生命周期和查询 key 契约分叉
Release         █████░░░░░  5.2  C   tag release 不依赖测试和前端 lint 结果
─────────────────────────────────────
Overall         ██████░░░░  5.6  C
```

分数为判断性评分，10 分为最好；不是按 finding 数量机械扣分。

### Finding Statistics

| Severity | Count | Confirmed | Suspected |
|----------|-------|-----------|-----------|
| Critical | 0 | 0 | 0 |
| High | 9 | 9 | 0 |
| Medium | 11 | 11 | 0 |
| Low | 0 | 0 | 0 |
| Info | 0 | 0 | 0 |
| **Total** | **20** | **20** | **0** |

## 2. Project Map

- **Runtime entry:** `main.go`/`cmd/start.go` 依次完成配置、数据库迁移、`op.InitCache`、`op.UserInit`、HTTP server，然后启动 relay log writer、task runner、更新检查、统计回填和索引任务。
- **HTTP/API 边界:** `internal/server/server.go` 注册 Gin middleware 和全部路由；管理面使用 JWT，租户 relay 使用 API key；OpenAI/Anthropic/embedding/compact 走 HTTP，Responses 还存在 WebSocket 路径。
- **Relay 数据流:** inbound body → transformer canonical model 解析 → group/catalog 规划 → balancer 选 channel/key → outbound transform/上游 HTTP 或 WS → stream/non-stream transform → metrics/quota/log 持久化。
- **站点同步:** `internal/sitesync/core.go` 拉取 site account 快照，先写 groups/tokens/models 和 sync status，再 `ProjectAccount` 创建/更新 managed channels/bindings，最后同步 catalog/pricing。
- **状态所有权:** API key、channel、group、setting、catalog 和统计均有进程内 cache；relay logs、usage facts 和统计采用异步内存队列/dirty set 后台落库。
- **持久化与恢复:** SQLite/MySQL/Postgres 由 GORM 统一抽象；JSON/ZIP 增量备份包含核心表、统计和日志；WebDAV restore 与管理面 upload restore 都是在线接口。
- **外部接口:** 上游站点、LLM provider、WebDAV、HTTP/WS relay、浏览器 SSE 和 GitHub Actions release。
- **高风险热点:** `SiteDel/SiteAccountDel`、`ProjectAccount`、`RestoreFromBackup`、`relay.Handler`、`APIKeyAuth`、前端 `useLogs`/`GroupCard`/`site-channel` mutation。

## 3. Top Risks

| Priority | Finding | Severity | One-line impact |
|----------|---------|----------|-----------------|
| 1 | F05 删除 site/account 遗留 managed channels | High | 站点删除后旧 channel/key/group item 仍可能参与路由 |
| 2 | F07 在线 restore 不冻结后台写入 | High | 恢复快照可能被旧请求队列和统计桶污染 |
| 3 | F14 备份包含未脱敏凭据 | High | WebDAV/下载文件泄露即可获得上游和租户凭据 |
| 4 | F04 API key 额度并发穿透 | High | 并发请求可同时通过额度检查，成本/配额超限 |
| 5 | F01 固定首启管理凭据并全网卡监听 | High | 首次部署的管理面可被默认凭据直接接管 |
| 6 | F02 监听失败仍上报 ready | High | 端口占用时服务和健康检查显示成功但无 HTTP 服务 |
| 7 | F08 同步先报 success 再投影 | High | UI 状态成功而路由数据仍旧或半更新 |
| 8 | F15 tag release 绕过测试门禁 | High | 未经当前 HEAD 验证的制品可以直接发布 |
| 9 | F06 删除遗漏新报价表 | High | 已删除账号的报价仍参与展示/计费选择 |
| 10 | F20 tools 轮询绑定虚拟卡片生命周期 | Medium | 卡片滚出视口后异步任务完成也不会刷新状态 |

## 4. Detailed Findings

### Finding: F01 固定默认管理凭据且绑定所有网卡

- Severity: High
- Confidence: High
- Category: Security
- Status: Confirmed
- Affected area: 首次启动、管理面鉴权、Docker 部署
- Evidence:
  - File: `internal/op/user.go:15-28`
  - File: `internal/conf/config.go:119-121`
  - File: `docker-compose.yml:3-11`
  - Function / Module: `UserInit`, `setDefaults`
  - Relevant behavior: 空库无条件创建用户名和密码均为 `admin` 的用户并写入明文日志；默认 `server.host` 为 `0.0.0.0`，compose 暴露 `8080:8080`。
- Problem: 首次启动没有 bootstrap secret、一次性密码或强制初始化步骤，管理面默认对所有网卡可达。
- Why it matters: 在共享网络、云主机或端口转发环境，攻击者可以把固定凭据作为稳定的登录入口；日志采集系统还会保存密码痕迹。
- Realistic failure scenario: 运维直接执行 `docker compose up`，未立即修改凭据；同网段扫描到 8080 后使用 `admin/admin` 登录管理面并创建 API key 或修改上游配置。
- Minimal fix: 取消固定密码，要求显式 bootstrap 密码或生成一次性随机密码并在首次登录后失效；默认监听 `127.0.0.1`，公网部署由配置显式开启。
- Better long-term fix: 引入一次性初始化 token、密码策略、首次登录强制改密和管理面反向代理/网络访问策略。
- Regression test suggestion: 空数据库启动测试断言不产生固定凭据和明文密码日志；默认配置测试断言 host 不为全网卡。
- Estimated effort: 1-2 天

### Finding: F02 监听失败仍上报启动成功

- Severity: High
- Confidence: High
- Category: Stability
- Status: Confirmed
- Affected area: HTTP server startup、health/readiness、后台任务启动
- Evidence:
  - File: `internal/server/server.go:56-65`
  - File: `cmd/start.go:80-103`
  - Function / Module: `server.Start`, `runService`
  - Relevant behavior: `ListenAndServe` 在 goroutine 中调用，错误只记录日志；`Start()` 立即返回 nil，随后调用 `onReady` 并启动 task/relay-log/stat goroutine。
- Problem: 端口占用、非法地址或监听权限失败不能通过 `Start()` 返回值传递给上层。
- Why it matters: 进程活着不等于服务可用，桌面启动器、容器探针和自动化部署会得到假阳性。
- Realistic failure scenario: 8080 已被另一进程占用；Octopus 打印 ready 并继续后台写数据库，健康检查认为部署成功，直到用户请求超时才发现没有服务。
- Minimal fix: 先 `net.Listen`，成功后再创建 serving goroutine；监听错误通过 `Start()` 返回。
- Better long-term fix: 将 listener 生命周期纳入 server 对象，暴露明确的 `Started/Failed/Stopped` 状态并让 readiness 依赖真实 listener。
- Regression test suggestion: 预占端口后调用 `Start()`，断言返回错误、`onReady` 不触发、后台 runner 不启动。
- Estimated effort: 0.5-1 天

### Finding: F03 停机直接 Close，没有 graceful drain

- Severity: Medium
- Confidence: High
- Category: Stability
- Status: Confirmed
- Affected area: HTTP/SSE/WS shutdown
- Evidence:
  - File: `internal/server/server.go:68-69`
  - File: `cmd/start.go:91-95`
  - Function / Module: `server.Close`, shutdown registry
  - Relevant behavior: `Close()` 直接调用 `httpSrv.Close()`，没有带超时的 `Shutdown(ctx)`；上游 WS pool 在后续 shutdown hook 才关闭。
- Problem: 活动 HTTP、SSE 和 WebSocket 连接被硬切断，长响应没有排空窗口。
- Why it matters: 客户端收到中途断流，relay metrics/log 可能与上游完成状态分叉；容器滚动升级会放大重试和重复请求。
- Realistic failure scenario: 一个 Responses stream 正在发送最后几个事件时收到 SIGTERM，连接立即关闭，客户端重试并可能产生重复上游调用。
- Minimal fix: 先 `httpSrv.Shutdown`，使用 10-30 秒 grace period，超时后再 `Close()`。
- Better long-term fix: 将 HTTP、WS、后台 writer/task 纳入统一 drain state machine，并在 readiness 中先摘除新流量。
- Regression test suggestion: 延迟 handler + shutdown 测试，断言 grace period 内请求完成，超时后才强制关闭。
- Estimated effort: 0.5-1 天

### Finding: F04 API key 额度在完成后记账，存在并发穿透

- Severity: High
- Confidence: High
- Category: Security
- Status: Confirmed
- Affected area: API key `MaxCost`/`QuotaLimit` admission、relay metrics
- Evidence:
  - File: `internal/server/middleware/auth.go:74-117`
  - File: `internal/op/apikey.go:205-223`
  - File: `internal/relay/metrics.go:366-374`
  - Function / Module: `APIKeyAuth`, `APIKeyIncrementQuotaUsed`, `SaveOutcomeWithChannelStats`
  - Relevant behavior: 准入读取缓存的已用成本/配额；请求成功后才执行 DB `quota_used + cost` 和缓存回写。
- Problem: 检查与预留不是一个原子操作，多个并发请求会基于同一旧值同时通过。
- Why it matters: 额度是安全和计费边界；超限后再拒绝已经无法撤销上游成本。
- Realistic failure scenario: quota limit 只有 1，两个请求各自成本 0.8，几乎同时经过 middleware，均被转发，最终 `quota_used` 约为 1.6。
- Minimal fix: 在 admission 阶段执行原子 compare-and-increment reservation，失败则立即拒绝；完成时按实际成本校正 reservation。
- Better long-term fix: 建立带 reservation id、过期回收和幂等结算的 quota ledger，跨进程部署使用数据库条件更新或集中式配额服务。
- Regression test suggestion: 并发 N 个请求，每个成本足以触顶，断言只有预算范围内的请求进入 relay。
- Estimated effort: 2-4 天

### Finding: F05 删除 site/account 遗留 managed channels 及其路由依赖

- Severity: High
- Confidence: High
- Category: Security
- Status: Confirmed
- Affected area: site/account deletion、channels/group_items/channel_keys/route_candidates
- Evidence:
  - File: `internal/op/site.go:449-490`
  - File: `internal/op/site.go:882-910`
  - File: `internal/op/channel.go:406-505`
  - Function / Module: `SiteDel`, `SiteAccountDel`, `ChannelDelManaged`, `channelDel`
  - Relevant behavior: 删除事务先删除 `site_channel_bindings`，随后删除 account；没有枚举 binding 对应 channel 并调用 `ChannelDelManaged`。`channelDel` 才负责清理 group items/channel keys 并归档 route candidates。
- Problem: managed channel 在 binding 消失后变成普通孤儿记录，删除保护和清理级联被绕开。
- Why it matters: 已删除站点的 channel 仍带有效上游 key，可能继续被 group/balancer 使用；缓存和数据库同时留下错误路由面。
- Realistic failure scenario: site account 已完成一次 `ProjectAccount`，管理员删除账号；binding 被删但 channel/group item/key 未删，下一次请求仍选中旧 channel 并向已删除站点发流量。
- Minimal fix: 删除事务内先枚举 account 的 binding/channel IDs，调用受控 managed delete 或在同一事务中等价清理 group items、keys、stats、route candidates、channel/cache。
- Better long-term fix: 为 managed projection 建立明确 owner FK/级联删除协议，禁止直接删除 binding 而不处理 channel，并增加 orphan sweeper。
- Regression test suggestion: 创建 site/account+projected channel 后删除，断言 `channels`、`group_items`、`channel_keys`、`route_candidates` 和缓存均不可路由。
- Estimated effort: 1-2 天

### Finding: F06 删除 site/account 遗漏 `site_model_price_quotes`

- Severity: High
- Confidence: High
- Category: Stability
- Status: Confirmed
- Affected area: pricing persistence、计费/路由报价选择
- Evidence:
  - File: `internal/op/site.go:475-476,901-902`
  - File: `internal/model/site_pricing.go:37-65`
  - File: `internal/op/site_pricing.go:373-416,546-567`
  - Function / Module: `SiteDel`, `SiteAccountDel`, `EffectivePriceForCandidate`
  - Relevant behavior: 删除只调用 legacy `deleteLegacySitePricesByAccountIDs`；新表 `site_model_price_quotes` 没有对应删除。报价选择会继续读取 site/account quote，并将旧报价标为 `site_stale` 而不是排除。
- Problem: 删除主体和新报价表没有同一生命周期边界。
- Why it matters: 删除的账号仍能影响控制台报价、计费快照或 route candidate 选择，造成数据隔离与财务正确性问题。
- Realistic failure scenario: 账号同步过 `/api/pricing` 后被删除；旧 quote 仍匹配同一 candidate，后续成本计算选中已删除账号的报价。
- Minimal fix: 删除 account/site 时按 `site_id/site_account_id` 清理非 manual quote，并清理相关 route-candidate 关联。
- Better long-term fix: 增加外键约束和删除策略，所有 projection-owned tables 使用统一 owner lifecycle service。
- Regression test suggestion: 写入 quote，删除 account 后断言 `SiteModelPriceQuoteList` 和 `EffectivePriceForCandidate` 不再命中该 quote。
- Estimated effort: 0.5-1 天

### Finding: F07 在线 restore 不冻结后台队列和统计状态

- Severity: High
- Confidence: High
- Category: Stability
- Status: Confirmed
- Affected area: WebDAV/管理面 restore、relay logs、usage facts、stats
- Evidence:
  - File: `internal/webdav/backup.go:131-199`
  - File: `internal/server/handlers/setting.go:180-281`
  - File: `cmd/start.go:97-131`
  - File: `internal/op/log.go:101-128`
  - File: `internal/op/usage_facts.go:375-425`
  - Function / Module: `RestoreFromBackup`, `DBImport*`, `RelayLogWriterRun`, `UsageFactsFlushPending`, `StatsSaveDB`
  - Relevant behavior: restore/import 是在线 handler；导入前未停止 scheduler、flush 并清空 pending/dirty/in-memory hourly state，导入后只刷新 cache。
- Problem: restore 的数据库快照和旧进程内事件流没有一致的时间点或冻结协议。
- Why it matters: 恢复完成后旧请求可能写回新库，覆盖或污染刚恢复的数据，审计日志和统计不再对应备份时点。
- Realistic failure scenario: restore 开始时 queue 中已有 relay log；导入快照后 writer 继续 drain，旧 log 使用新数据库 ID/关联写入，恢复结果被回灌。
- Minimal fix: restore 前进入 maintenance/freeze，停止 task/writer，flush 可接受数据，清理 transient state；导入并完成 `InitCache` 后再 resume。
- Better long-term fix: 引入 restore coordinator 和 generation/epoch，所有异步写入携带 generation，恢复时旧 generation 自动丢弃。
- Regression test suggestion: restore 期间构造 pending relay log、usage fact 和 stats dirty set，断言恢复后不会写入 restore 之前的事件。
- Estimated effort: 2-4 天

### Finding: F08 同步状态先落库为 success，再执行 projection

- Severity: High
- Confidence: High
- Category: Stability
- Status: Confirmed
- Affected area: `sitesync` 状态、managed channel projection、运维界面
- Evidence:
  - File: `internal/sitesync/core.go:45-69`
  - File: `internal/sitesync/storage.go:64-195`
  - File: `internal/sitesync/project.go:160-218`
  - Function / Module: `SyncAccount`, `persistSyncSnapshot`, `ProjectAccount`
  - Relevant behavior: `persistSyncSnapshot` 先写 `last_sync_status`、groups/tokens/models；`ProjectAccount` 失败时直接返回，没有把状态改为 failed/partial。
- Problem: 控制面状态提交与数据面投影不是一个原子状态机。
- Why it matters: UI、自动同步调度和运维判断会得到“最近同步成功”，但实际路由仍是旧值或半更新。
- Realistic failure scenario: 上游快照拉取成功，创建 managed channel 因 DB constraint 失败；账号状态保留 success，下一轮任务被延后，旧路由继续服务。
- Minimal fix: 将 success 写入推迟到 projection 完成后，或在 projection 失败时显式更新 `failed/partial` 并标记 projection stale。
- Better long-term fix: 使用 `snapshot -> persisted -> projected -> cataloged` 状态机和 projection revision，UI 同时展示控制面与数据面版本。
- Regression test suggestion: mock `ProjectAccount` 返回错误，断言 `last_sync_status != success` 且 projection 被标记 stale。
- Estimated effort: 1-2 天

### Finding: F09 pricing refresh 只 upsert，不淘汰本轮消失的 quote

- Severity: Medium
- Confidence: High
- Category: Stability
- Status: Confirmed
- Affected area: 站点定价同步、报价选择、计费展示
- Evidence:
  - File: `internal/sitesync/pricing.go:16-64`
  - File: `internal/op/site_pricing.go:26-80,261-280`
  - File: `internal/op/site_pricing.go:546-567`
  - Function / Module: `refreshSitePricingQuotes`, `SiteModelPriceQuotesUpsert`, `priceQuoteFresh`
  - Relevant behavior: 同步只 upsert observed quote；只删除 superseded unbound quote，不处理同一 site/account 本轮未出现的历史 identity；选择逻辑允许 stale quote 作为最后已知报价。
- Problem: 上游删除模型或分组报价后，历史 quote 长期存在并可被选中。
- Why it matters: `site_stale` 只是来源标记，不是失效状态；控制台和计费可能继续展示已撤销模型的旧价格。
- Realistic failure scenario: 第一次同步有 model A，第二次上游移除 A；A 的 quote 超过 24 小时仍在匹配查询中，成为无 fresh quote 时的候选。
- Minimal fix: 每次同步记录 observed identity 集合，将未观察到的非 manual quote 置 rejected/失效。
- Better long-term fix: 引入 per-refresh generation、TTL 和显式 tombstone，区分“暂时刷新失败”与“上游明确删除”。
- Regression test suggestion: 两次同步 payload，第二次删除 model A，断言有效价格结果不再返回 A 的 site quote。
- Estimated effort: 1-2 天

### Finding: F10 channel→site binding 统计缓存在重绑后不失效

- Severity: Medium
- Confidence: High
- Category: Maintainability
- Status: Confirmed
- Affected area: site-model hourly stats、binding projection
- Evidence:
  - File: `internal/op/stats_site_model.go:401-447`
  - File: `internal/sitesync/project.go:160-218`
  - Function / Module: `lookupChannelSiteBinding`, `invalidateSiteBindingCache`, `ProjectAccount`
  - Relevant behavior: lookup 对正/负结果永久缓存；失效函数只在删除 site/account 时调用；projection create/update binding 路径没有调用。
- Problem: channel 复用、换组或重建 binding 后，统计仍按旧 `site_account_id/base_group_key` 归因。
- Why it matters: 监控图表和失败率会错误归属，影响容量判断、站点 SLA 和故障定位。
- Realistic failure scenario: channel 从 group A 重绑到 group B，下一次请求写 hourly stats 时命中旧 cache，数据继续记到 group A。
- Minimal fix: binding create/update/delete 全路径失效 channel 映射，至少覆盖 `ProjectAccount` 的三种分支。
- Better long-term fix: 用 binding revision/短 TTL 作为缓存版本，避免全局 Clear 带来的抖动。
- Regression test suggestion: 先记录一次统计，更新 binding 到新 group，再记录一次并断言两个 bucket 的归属正确。
- Estimated effort: 0.5-1 天

### Finding: F11 relay HTTP 请求体没有大小上限

- Severity: Medium
- Confidence: High
- Category: Performance
- Status: Confirmed
- Affected area: HTTP relay、`/responses/compact`
- Evidence:
  - File: `internal/relay/relay.go:773-797`
  - File: `internal/relay/compact.go:43-59`
  - Function / Module: `parseRequest`, `HandleResponsesCompact`
  - Relevant behavior: 直接 `io.ReadAll(c.Request.Body)`，没有 `http.MaxBytesReader` 或等效上限。
- Problem: 公网可达的请求入口会把任意大小 body 一次性读入内存。
- Why it matters: 单个超大 JSON 请求即可制造高内存峰值和 GC 压力，多个并发请求可能导致 OOM；这属于边界资源保护缺失。
- Realistic failure scenario: 攻击者向 `/v1/chat/completions` 发送数百 MB body，Gin 在解析前已分配完整缓冲，服务 RSS 快速增长并影响其他请求。
- Minimal fix: 按接口配置最大 body，使用 `http.MaxBytesReader`，超限返回 413。
- Better long-term fix: 统一 API body budget middleware，按 content type、租户和并发预算限制，并在监控中记录拒绝原因。
- Regression test suggestion: 超限 body 返回 413，合法边界 body 仍能处理，且不会调用 transformer。
- Estimated effort: 0.5-1 天

### Finding: F12 HTTP malformed JSON 返回 500，与 WS 的 400 契约不一致

- Severity: Medium
- Confidence: High
- Category: Stability
- Status: Confirmed
- Affected area: HTTP/WS relay request validation
- Evidence:
  - File: `internal/relay/relay.go:775-795`
  - File: `internal/relay/ws_client.go:184-189`
  - Function / Module: `parseRequest`, `processWSResponseCreate`
  - Relevant behavior: HTTP `TransformRequest` 失败统一 `500`；WS 同类 decode/validation 失败写 `400 invalid_request`。
- Problem: 客户端输入错误被 HTTP 路径归类为服务端故障。
- Why it matters: 客户端可能错误重试，错误率和可观测性统计失真，HTTP 与 WS SDK 行为不一致。
- Realistic failure scenario: malformed JSON 被 HTTP relay 返回 500，SDK 按上游暂时故障重试多次；同一请求通过 WS 则立即得到 400。
- Minimal fix: 为 transformer 错误定义可识别的输入错误类型，JSON decode/required field 映射到 400。
- Better long-term fix: 建立统一协议错误模型和 HTTP/WS 映射表，所有 inbound adapter 复用。
- Regression test suggestion: 对 chat/messages/responses HTTP 发送 malformed JSON，断言 400；WS 维持 `invalid_request` 400。
- Estimated effort: 0.5-1 天

### Finding: F13 HTTP/WS `supported_models` 对 alias 的处理分叉

- Severity: Medium
- Confidence: High
- Category: Maintainability
- Status: Confirmed
- Affected area: API key model allowlist、OpenAI Responses WebSocket
- Evidence:
  - File: `internal/relay/relay.go:67-84`
  - File: `internal/relay/ws_client.go:210-223,420-444`
  - Function / Module: `relay.Handler`, `processWSResponseCreate`, `newWSRelayRequest`
  - Relevant behavior: HTTP 同时接受 request model 和 canonical routing model；WS 只按 `executionRequest.Model` 字符串精确匹配后才进入同一 canonical planner。
- Problem: 同一 API key 与 canonical/alias 配置在 HTTP 和 WS 上得到不同授权结果。
- Why it matters: Responses continuation 客户端无法根据协议选择稳定路径，容易出现“HTTP 可用、WS 不支持”的生产分叉。
- Realistic failure scenario: allowlist 记录 canonical 名称，客户端使用 alias；HTTP 请求通过，WS 在 `supported_models` 检查处返回 `model not supported`。
- Minimal fix: WS 复用 HTTP 的 identity resolver，同时接受 request 和 routing identity。
- Better long-term fix: 将 supported-model policy 变成共享服务，输出 canonical identity、原始别名和拒绝原因。
- Regression test suggestion: alias→canonical fixture 下，对 HTTP 与 WS 同一 key/model 断言结果一致。
- Estimated effort: 0.5-1 天

### Finding: F14 备份导出包含大量可直接使用的凭据，ZIP 未加密

- Severity: High
- Confidence: High
- Category: Security
- Status: Confirmed
- Affected area: JSON/ZIP backup、WebDAV、敏感凭据存储
- Evidence:
  - File: `internal/op/backup.go:60-111`
  - File: `internal/op/backup_extended_export.go:73-100`
  - File: `internal/model/channel.go:125-134`
  - File: `internal/model/site.go:218-227`
  - Function / Module: `dbExportAllWithConn`, `sanitizeSiteAccountsForBackup`, `writeZipTable`
  - Relevant behavior: sanitization 只移除 JWT/WebDAV 设置和部分 cookie 字段；`ChannelKey.ChannelKey`、`APIKey.APIKey`、SiteAccount password/access/API/refresh token、SiteToken 仍写入备份；ZIP 仅压缩无加密。
- Problem: 备份文件同时成为租户 API key、上游 key 和站点登录凭据的明文集合。
- Why it matters: WebDAV 账号、备份存储、下载链接或本地文件任一处泄露都会造成横向接管和上游费用风险。
- Realistic failure scenario: 自动 WebDAV 备份被共享存储读取，攻击者解压 `channel_keys.json`/`api_keys.json` 后直接调用 relay 或上游 provider。
- Minimal fix: 默认导出只保留不可逆摘要/掩码；恢复需要显式“包含 secret”选项并使用受保护密钥加密。
- Better long-term fix: 引入 envelope encryption、密钥轮换、备份访问审计和 secret reference（恢复时从密钥库取值）。
- Regression test suggestion: 导出 fixture 后断言备份文本不包含真实 key/password/token；加密备份只能用正确密钥恢复。
- Estimated effort: 2-5 天

### Finding: F15 tag release 绕过后端测试和前端 lint/typecheck

- Severity: High
- Confidence: High
- Category: Release
- Status: Confirmed
- Affected area: GitHub Actions release pipeline
- Evidence:
  - File: `.github/workflows/ci.yml:3-62`
  - File: `.github/workflows/release.yaml:3-140`
  - File: `.github/workflows/build.yml:1-18,58-80`
  - Function / Module: `CI`, `release`, `Build` workflows
  - Relevant behavior: CI 只监听 `dev` push/PR；release 由 `v*` tag 直接触发并 build/push；常规 build 还是手动或 PR `build` label，release job 没有 `go test`、`pnpm lint` 或 `tsc` 步骤，也没有 `needs` 依赖。
- Problem: 发布门禁与发布事件解耦，tag 可以指向未验证 commit。
- Why it matters: 生产制品、安装包和镜像可能包含编译通过但行为回归的代码，问题直到发布后才暴露。
- Realistic failure scenario: 开发者直接 push `v1.3.1` tag；release workflow 构建成功并推送 GHCR，即使同一 commit 的前端类型检查会失败。
- Minimal fix: release job 在 build 前复跑 `go test ./...`、`go vet ./...`、`pnpm lint`、`pnpm exec tsc --noEmit`，失败即停止。
- Better long-term fix: 只允许从已通过 required checks 的 immutable commit/tag 发布，并加入 provenance、制品签名和回滚指针。
- Regression test suggestion: workflow 静态检查或 CI 故障注入测试，验证测试失败时 release/push job 不执行。
- Estimated effort: 0.5-1 天

### Finding: F16 SSE 新日志溢出第一页时丢失旧首屏记录

- Severity: Medium
- Confidence: High
- Category: Maintainability
- Status: Confirmed
- Affected area: 日志实时流、React Query infinite cache
- Evidence:
  - File: `web/src/api/endpoints/log.ts:430-449`
  - Function / Module: `useLogs` EventSource `onmessage`
  - Relevant behavior: 新日志 prepend 后 `slice(0, pageSize)`；当已有多页时只 `invalidateQueries`，但没有把被挤出的尾项传入下一页。
- Problem: 被挤出的旧记录既不在当前缓存，也不保证被下一次分页补回；`next_cursor` 仍来自旧页面结构。
- Why it matters: 审计日志在用户界面上出现不可恢复的缺口，实时模式下越活跃越容易发生。
- Realistic failure scenario: 首页 20 条、后台连续到达 5 条 SSE；前 5 条旧记录被切掉，用户向下分页时仍沿原 cursor，无法看到这些记录。
- Minimal fix: overflow item 级联到下一页，或 SSE 收到溢出时整 query 失效并从 cursor 重新拉取。
- Better long-term fix: 服务端提供按序号/游标的 append-only stream，前端将 SSE 新项与 infinite pages 统一重排而不是直接截断。
- Regression test suggestion: 构造多页缓存和连续 SSE，断言所有 ID 仍可在某页访问且无重复。
- Estimated effort: 0.5-1 天

### Finding: F17 日志站点动作目标查询被每条 SSE 放大

- Severity: Medium
- Confidence: High
- Category: Performance
- Status: Confirmed
- Affected area: 日志详情页、site-action-targets API
- Evidence:
  - File: `web/src/components/modules/log/index.tsx:138-147`
  - File: `web/src/api/endpoints/log.ts:259-282,430-450`
  - Function / Module: `useLogSiteActionTargets`, SSE cache update
  - Relevant behavior: query key 包含完整排序后的 `logs.map(id)`；每条新日志改变 ID 数组，React Query 重新请求当前全部 IDs，最多按 100 分块。
- Problem: 增量事件触发全量目标重查，而不是只补新 ID。
- Why it matters: 日志页越长，单条事件产生的请求和后端查询越大，形成实时流放大器。
- Realistic failure scenario: 每秒 10 条日志、当前 200 条可见记录时，页面持续发出 2 个全量目标请求/秒，挤压日志本身和其他 API。
- Minimal fix: 以单个 `log.id` 分桶缓存，只请求新 ID；或服务端 SSE payload 附带 action targets。
- Better long-term fix: 后端提供批量目标缓存版本/增量 cursor，前端按版本合并而非用完整 ID 数组作为 key。
- Regression test suggestion: mock 两次 SSE，仅新增一个 ID，断言只发送新增 ID 的请求。
- Estimated effort: 0.5-1 天

### Finding: F18 生产依赖树包含 8 个已知漏洞

- Severity: Medium
- Confidence: High
- Category: Release
- Status: Confirmed
- Affected area: Web production dependency tree、pnpm lockfile overrides
- Evidence:
  - File: `web/pnpm-lock.yaml:6-24`
  - File: `web/pnpm-lock.yaml:3540,3714,3911`
  - Function / Module: pnpm production dependency resolution
  - Relevant behavior: `pnpm audit --prod` 报告 1 high、6 moderate、1 low；涉及 `nanoid@3.3.16`、`postcss@8.5.18`、`mermaid@11.16.0`、`dompurify@3.4.12`，其中部分版本由 lockfile override 固定。
- Problem: 当前生产依赖图包含已有修复版本的已知漏洞，且 release workflow 不运行依赖审计。
- Why it matters: Mermaid/DOMPurify advisories涉及 CSS injection、prototype pollution、DoS 和 XSS 类行为；nanoid/PostCSS 还影响构建或服务端工具链。即使当前 UI 未直接调用 Mermaid，也不应依赖树长期固定在已知脆弱版本。
- Realistic failure scenario: 上游 UI 组件在后续页面启用 Mermaid 渲染，攻击者可控内容命中当前 transitive vulnerability；release 流水线仍正常发布且没有任何告警。
- Minimal fix: 升级/调整 overrides 至 patched versions，重新生成 lockfile，并验证 `pnpm audit --prod` 无 high/moderate。
- Better long-term fix: 在 PR 和 release 中加入依赖审计/SBOM，删除未使用的 `@lobehub/ui` 重依赖链或按需拆包。
- Regression test suggestion: CI 执行 `pnpm audit --prod --audit-level=moderate`，并对实际使用的富文本/图表渲染入口增加恶意 payload 安全测试。
- Estimated effort: 0.5-2 天

### Finding: F19 site-channel mutation 写入未订阅的 React Query key

- Severity: Medium
- Confidence: High
- Category: Maintainability
- Status: Confirmed
- Affected area: site-channel list mutations
- Evidence:
  - File: `web/src/api/endpoints/site-channel.ts:444-461`
  - File: `web/src/api/endpoints/site-channel.ts:472-655`
  - Function / Module: `useSiteChannelList` 和多个 mutation `onSuccess`
  - Relevant behavior: 活跃 query key 是 `['site-channel','list',{includeHistory}]`；mutation 却对 `['site-channel','list']` 调用 `setQueryData`，之后再 invalidate。
- Problem: 乐观/即时替换写入不到当前带 `includeHistory` 的缓存，所谓即时更新失效。
- Why it matters: 慢网络或 mutation 后短窗口内 UI 显示旧 account 状态，用户可能重复操作或误判保存失败。
- Realistic failure scenario: 修改 projected channel settings 成功，页面先继续显示旧模型；直到 30 秒 refetch 或 invalidate 完成才跳变。
- Minimal fix: 抽取 query-key factory，使用完整 key；或者用 `setQueriesData` 按前缀更新所有 includeHistory 变体。
- Better long-term fix: 以资源版本/服务器返回对象为单一 cache source，统一 mutation invalidation 和 optimistic rollback 策略。
- Regression test suggestion: mutation 成功且关闭自动 refetch，断言当前列表缓存立即替换目标 account。
- Estimated effort: 0.5-1 天

### Finding: F20 tools 批任务轮询绑定虚拟卡片生命周期

- Severity: Medium
- Confidence: High
- Category: Maintainability
- Status: Confirmed
- Affected area: group tools probe、虚拟列表、异步 task status
- Evidence:
  - File: `web/src/components/modules/group/Card.tsx:91-127,358-400`
  - File: `web/src/components/common/VirtualizedGrid.tsx:175-189,224-263`
  - Function / Module: `GroupCard.startBatchPolling`, `VirtualizedGrid`
  - Relevant behavior: `GroupCard` 本地 `setInterval` 在 unmount cleanup 中清除；虚拟网格只渲染可视行。
- Problem: 异步任务的观察者与展示卡片生命周期耦合，滚出视口、筛选或 tab 切换会停止轮询。
- Why it matters: 后端任务仍执行但前端丢失最终状态，groups list 不会在完成时刷新，用户看到永久进行中或旧能力徽标。
- Realistic failure scenario: 启动批量 tools 测试后滚动到其他分组，卡片卸载清除 interval；任务完成后没有任何组件负责查询 status。
- Minimal fix: 将 taskId 状态提升到 React Query/store 层，按 taskId 持续轮询并在终态 invalidate groups。
- Better long-term fix: 后端任务事件/SSE + durable task store，前端只订阅任务状态，不由虚拟行承载业务生命周期。
- Regression test suggestion: 启动任务后模拟卡片卸载/重挂载，任务完成时断言 groups cache 被刷新且显示终态。
- Estimated effort: 0.5-1 天

## 5. Architecture Concerns

| Finding | Assessment |
|---------|------------|
| F05 | projection-owned channel 的 owner 生命周期没有封装在 site deletion 边界内，binding 与 channel 删除职责分裂。 |
| F07/F08 | restore、sync snapshot、projection 和异步写入缺少统一 generation/状态机。 |
| F10 | 统计模块直接依赖 sitesync 的 binding mutation 细节，缓存失效契约未形成共享接口。 |

**Verified checklist:** 分层入口和主要数据流已通过 `code-review-graph` 与源码抽查重建；未发现需要立即重写的循环依赖。最大的架构缺口是跨模块生命周期协议，而非包目录本身。

## 6. Security Concerns

| Finding | Risk |
|---------|------|
| F01 | 固定管理凭据 + 全网卡监听。 |
| F04 | 额度边界在并发下可穿透。 |
| F05/F06 | 删除主体后旧路由/报价仍可能生效。 |
| F14 | 备份成为明文 secret 集合。 |

**Verified checklist:** Header policy 屏蔽 `authorization`、`x-api-key`、`cookie` 和代理头；verification bridge token 使用 hash 且 claim 限定账号；CORS 默认空白名单。未发现硬编码生产 token。

## 7. Stability Concerns

| Finding | Risk |
|---------|------|
| F02 | listen error 不向上游传播。 |
| F03 | shutdown 无 drain。 |
| F07 | restore 与后台写入并发。 |
| F08 | sync status 与 projection 非原子。 |

**Verified checklist:** 网络客户端大多设置 context/dial/TLS timeout；stream processor 区分终态和 transport termination；SQLite 已启用 WAL、busy timeout 和 foreign keys。

## 8. Performance Concerns

| Finding | Risk |
|---------|------|
| F11 | relay body 无上限，存在内存型 DoS。 |
| F17 | SSE 每条事件重查全量 action targets。 |

**Verified checklist:** API key RPM 计数使用 per-key mutex；上游客户端有超时；未做真实 RSS 压测，因此 body 风险的资源阈值仍需运行时确认。

## 9. Testing Gaps

| Missing contract | Escaped findings |
|------------------|------------------|
| 端口占用/启动失败与 shutdown drain 集成测试 | F02, F03 |
| 并发 quota reservation 压测 | F04 |
| 删除 site/account 后完整数据面清理 | F05, F06 |
| 在线 restore 与 pending queue 竞态 | F07 |
| projection failure 后 sync status | F08 |
| pricing prune、binding cache invalidation | F09, F10 |
| HTTP body limit/error mapping、HTTP/WS alias parity | F11-F13 |
| React Query/SSE/virtualized task 生命周期 | F16, F17, F19, F20 |
| 生产依赖漏洞门禁 | F18 |

**Verified checklist:** `go test ./...` 通过；当前仓库有 153 个 Go test 文件，但前端只有 5 个测试文件，主要是纯函数/辅助逻辑，未覆盖 React Query、SSE、虚拟列表、移动端布局和异步批任务。

## 10. Maintainability Concerns

| Finding / hotspot | Assessment |
|------------------|------------|
| F05/F07/F08/F10 | 业务生命周期由多个包各自维护，调用者必须记住隐含清理/刷新顺序。 |
| `web/src/components/modules/site-channel/index.tsx`、`web/src/components/modules/site/index.tsx` | 超大组件承载查询、mutation、展示和状态编排，增加局部修复回归面。 |
| `internal/relay/relay.go`、`internal/op/backup.go` | 核心文件体量大，协议、持久化和错误映射集中。 |

**Verified checklist:** 未发现 `_v2` 复制实现或宽泛吞错作为主要模式；当前 tools 修复已集中在既有 helper/cache 路径，没有新增 fallback 分叉。

## 11. Design / Principles Concerns

| Finding | Principle |
|---------|-----------|
| F05 | SRP / ownership：删除 site 同时应协调其 projection-owned 资源。 |
| F07/F08 | Fail-fast 与状态机完整性：中间状态不能对外宣称完成。 |
| F17/F19/F20 | Command-query separation：mutation、query cache 和展示生命周期没有共享资源契约。 |

**Verified checklist:** 认证、路由规划、stream processor 和数据库迁移职责总体可识别；问题主要是跨边界协议缺乏单一 owner。

## 12. Release Concerns

| Finding | Evidence |
|---------|----------|
| F15 | tag release 直接 build/push，未 `needs` CI 或重跑测试。 |
| F18 | `pnpm audit --prod` 当前 8 vulnerabilities，锁文件还显式 override 到受影响版本。 |

**Verified checklist:** release 会校验 archive naming、生成 checksum、构建多平台镜像；但没有发现自动回滚或“仅从已通过 required checks 的 commit 发布”的约束。

## 13. Documentation Concerns

| Area | Assessment |
|------|------------|
| 启动/恢复运维说明 | 当前源码存在 `InitCache`、队列 flush 和启动任务，但没有看到 restore maintenance/freeze 契约文档。 |
| 初始凭据 | 默认 `admin/admin` 行为属于高影响运维事实，应在修复前明确迁移策略和风险提示。 |
| tools 注释 | `Card.tsx` 注释准确描述了轮询意图，但没有说明虚拟列表卸载限制。 |

**Verified checklist:** README 与主要 API 说明可用于启动和配置；本节不把“缺少设计文档”本身升级为功能 finding。

## 14. Configuration Safety Concerns

| Finding | Configuration issue |
|---------|--------------------|
| F01 | `server.host=0.0.0.0`、port 8080 和固定初始凭据组合过于开放。 |
| F11 | relay body limit 没有配置项或全局默认。 |

**Verified checklist:** CORS 默认非全开放；client dial/TLS timeout、cache init timeout 有默认值；敏感 config JWT/WebDAV password 在备份 settings 中会被移除。

## 15. Observability Concerns

| Finding | Observability impact |
|---------|----------------------|
| F02 | listener 失败只打 error，外部 readiness 仍成功。 |
| F08 | sync status success 掩盖 projection failure。 |
| F10 | site-model hourly stats 错误归因。 |
| F16 | SSE 丢记录没有 gap/sequence 告警。 |

**Verified checklist:** relay outcome、transport termination 和 tools policy 有结构化状态；主要缺口是跨层状态和 sequence gap 的一致性指标。

## 16. Fallback / Defensive Code Analysis

### Fallback Summary

| Subtype | Count | KeepWithAlert | FailFast | Remove |
|---------|-------|---------------|----------|--------|
| SilentFallback | 2 | 1 | 1 | 0 |
| EmptyCatch | 0 | 0 | 0 | 0 |
| CompatibilityBranch | 2 | 2 | 0 | 0 |
| SilentCorrection | 1 | 0 | 1 | 0 |
| DefensiveGuess | 1 | 0 | 1 | 0 |

- F09 的 stale quote 是显式 fallback，但当前没有区分“刷新失败”和“上游删除”，应保留并加告警/失效边界。
- F12 的错误映射属于 defensive classification 缺失，应 fail fast 为 400，而不是把输入错误 fallback 成 500。
- F13 的 alias/canonical 分叉是兼容路径重复实现，应收敛到共享 identity resolver。

**Verified checklist:** 未发现空 `catch` 或 broad exception swallowing；tools 反馈已改为真实成功后才回写证据，5xx 不再误判 unsupported。

## 17. Testing Authenticity Analysis

### Confidence Assessment

| Test Area | Real Confidence | Risk | Action |
|-----------|-----------------|------|--------|
| Go transformer/stream/cache unit tests | Medium-High | 纯函数行为有保护，跨服务生命周期未覆盖 | Keep + add integration |
| `internal/op` backup/site pricing tests | Medium | 测试导入/报价局部逻辑，但没有在线 restore 与删除全链路 | Add scenario tests |
| Frontend `.test.mjs` | Low | 主要测试排序、query key 和消息纯函数，UI 状态链可完全逃逸 | Add React Query/SSE tests |
| CI/release workflow | Low | CI 只在 dev 事件触发，tag release 门禁未测试 | Add workflow contract test |

### Valuable Tests

- `internal/relay/stream` 对终态与 transport termination 的分离测试。
- `internal/op/backup_extended_test.go` 对 ZIP manifest、ID remap、回滚和重复导入的测试。
- `internal/sitesync/pricing_test.go` 对报价解析和单位转换的测试。

### Missing Tests

- F02-F04 的真实启动、停机、并发 admission。
- F05-F10 的数据库级资源生命周期和 cache invalidation。
- F16、F17、F19、F20 的 React Query、EventSource 和虚拟列表卸载行为。

## 18. Type Safety Analysis

### Summary

| Subtype | Count | Critical | High | Medium | Low |
|---------|-------|----------|------|--------|-----|
| UnsafeBlock | 0 | 0 | 0 | 0 | 0 |
| TypeAssertion | 1 | 0 | 0 | 1 | 0 |
| InputBoundary | 3 | 0 | 0 | 3 | 0 |
| OutputLeak | 1 | 0 | 0 | 1 | 0 |
| BooleanTrap | 0 | 0 | 0 | 0 | 0 |
| StringlyTyped | 2 | 0 | 0 | 2 | 0 |
| ErrorType | 1 | 0 | 0 | 1 | 0 |

- F11/F12：HTTP body 和 transformer 输入边界依赖未限制的 `[]byte` 与通用 error。
- F13：supported model policy 使用逗号分隔 string，HTTP/WS 自行解析。
- F19：React Query key 使用裸数组字面量，`includeHistory` 维度未进入 mutation 更新类型。

**Verified checklist:** TypeScript `tsc --noEmit --incremental false` 通过；Go vet 通过；未发现 unsafe、忽略 type error 或 `any` 大面积扩散作为主要问题。

## 19. Frontend State Analysis

### Summary

| Subtype | Count | Affected Components |
|---------|-------|---------------------|
| ComponentSize | 2 | site、site-channel |
| StateDuplication | 1 | logs/SSE/infinite pages |
| PropDrilling | 0 | 未升格 |
| EffectChain | 1 | SSE target query |
| UIBusinessCoupling | 1 | GroupCard tools polling |
| DOMasState | 0 | 未升格 |
| RequestState | 2 | F17、F19 |
| RenderPerf | 1 | F17 |

**Finding mapping:** F16、F17、F19、F20。`VirtualizedGrid` 的底部安全区已覆盖通用网格，但这不能替代 task 生命周期；日志时区和模型 Catalog nested interactive 观察未列为核心 finding。

**Verified checklist:** 主要页面通过 React Query/Zustand 分层；本地构建和 lint 通过；缺少 EventSource/virtualization integration test。

## 20. Backend API Analysis

### Summary

| Subtype | Count | Affected Endpoints |
|---------|-------|-------------------|
| ApiConsistency | 2 | HTTP relay、Responses WS |
| Validation | 2 | relay、responses/compact |
| Auth | 2 | API key admission、管理面 bootstrap |
| NplusOne | 1 | log site-action-targets |
| Caching | 2 | API key quota、site-channel query key |
| ErrorResponse | 1 | malformed JSON |
| BusinessLogic | 3 | delete、sync、pricing |
| DataFlow | 2 | restore、stats binding |

**Finding mapping:** F01-F13、F17、F19。HTTP handlers 的 route/response helper 总体统一，但 shared policy 在 WS 和 HTTP 之间存在分叉。

**Verified checklist:** 已抽查 `/api/v1/apikey/login`、`/api/v1/log/stream-token`、site import/recovery、channel fetch-model 等前后端契约，路由均存在；F12/F13 是语义一致性问题而非缺少 endpoint。

## 21. Dependency Weight Analysis

### Dependency Scoreboard

| Dependency | Status | Weight | Transitives | Used For | Recommended Action |
|------------|--------|--------|-------------|----------|-------------------|
| `modernc.org/sqlite` | Overweight | High | Large pure-Go DB stack | SQLite runtime | 保留，评估发行包体积与启动成本 |
| `tls-client` + `fhttp` | Overweight | High | 自有 HTTP/TLS transport tree | provider fingerprint/proxy | 保留必要能力，限制边界并做版本审计 |
| `quic-go` | Heavy | Medium | QUIC stack | 特定网络客户端 | 确认生产使用率，必要时拆可选构建 |
| `@lobehub/ui` → `mermaid` | Risky | Medium | Mermaid/DOMPurify/PostCSS | UI transitive | 复核是否可移除，并升级到无告警版本 |
| `nanoid@3.3.16`, `postcss@8.5.18`, `mermaid@11.16.0`, `dompurify@3.4.12` | Vulnerable (F18) | Medium | pnpm audit 报告 | Next/UI transitive | 升级锁文件并删除不必要 override |

**Verified checklist:** `pnpm audit --prod` 当前返回 8 vulnerabilities（1 high、6 moderate、1 low）；`pnpm build` 通过，但通过构建不等于依赖安全门禁通过。

## 22. Code Consistency Concerns

| Finding | Inconsistency |
|---------|---------------|
| F12/F13 | HTTP/WS 对同一类输入和 model identity 使用不同校验实现。 |
| F19 | query key 工厂缺失，mutation 直接重复裸数组字面量。 |
| F07 | 两个 restore 入口都需调用者记住 `InitCache`/task reload，缺少统一 coordinator。 |

**Verified checklist:** Go 包命名、错误包装和 TypeScript import 风格总体一致；当前 tools 修复没有引入 fallback import 或 `_v2` 复制代码。

## 23. Comment Coverage Concerns

| Area | Assessment |
|------|------------|
| `GroupCard` tools polling | 有 P1/R9 注释解释意图，但没有记录虚拟卸载造成的生命周期限制。 |
| `internal/op/cache.go` | `InitCache` 说明启动顺序，但没有明确 `DBImport*` 调用者必须在事务后刷新。 |
| 大型 relay/backup 文件 | 公共函数有局部注释，跨层状态不变量缺少模块级文档。 |

**Verified checklist:** 未发现大面积无意义 narration；现有注释多数与近期修复一致。评论缺口属于风险放大因素，不单独升格成 Critical finding。

## 24. Principles Compliance

### Principles Violated

| Principle | Violations | Severity | Affected Areas |
|-----------|------------|----------|----------------|
| Single Responsibility | 删除/恢复同时协调数据库、缓存、异步任务，但没有统一 owner service | High | site/op、webdav、task |
| Fail-Fast | listen error 和 projection error 没有沿调用链传播为失败状态 | High | server、sitesync |
| Command-Query Separation | mutation 的 cache 写入不匹配 query key；SSE 事件触发全量 query | Medium | web site-channel/log |
| Data Ownership / Lifecycle | binding、managed channel、price quote 的 owner 生命周期断裂 | High | site/sync/pricing |
| Resource Boundaries | relay body 无上限 | Medium | relay |
| Dependency Inversion | HTTP/WS 各自实现 model allowlist 解析 | Medium | relay |
| File Size Limit | relay/backup/site 页面超大，变更需要跨责任面阅读 | Medium | internal/relay、internal/op、web |

### Principles Respected

- Header policy 对敏感头采用显式 denylist；verification bridge 使用 hash、过期和账号范围校验。
- stream processor 把业务终态与传输终止分开，避免把客户端断开简单判为上游失败。
- SQLite 初始化使用 WAL、busy timeout、foreign keys；Go 错误通常使用 `%w` 保留 cause。
- 当前工作区 tools 修复已处理 NULL source、5xx unsupported、HTTP 200 失败终态和结构化 tool-call 检测。

## 25. Recommended Fix Order

### Fix Immediately

1. F14：停止把真实 channel/API/site credentials 写入未加密备份；对现有备份执行轮换与泄露评估。
2. F01：取消固定 `admin/admin`，收紧默认监听地址并提供安全 bootstrap。
3. F05/F06：删除 site/account 时在同一事务清理 managed channels、keys、group items、route candidates 和新报价表。
4. F07/F08：建立 restore freeze 和 sync projection 状态机，避免数据回灌/假成功。
5. F04：实现 quota/cost reservation，防止并发穿透。

### Fix Before Stable Release

1. F02/F03：监听失败 fail-fast，停机使用 graceful drain。
2. F15：release workflow 自带完整测试、类型检查和 provenance gate。
3. F09/F10：pricing prune 与 binding cache invalidation。
4. F11-F13：统一 body limit、输入错误映射和 HTTP/WS model identity policy。
5. F16/F17/F19/F20：修复 SSE 分页、目标查询、query key 和 tools task 生命周期；F18 升级依赖树。

### Schedule Later

- 拆分超大 relay/backup/site 前端组件，抽出资源 owner、query-key factory 和 task coordinator。
- 清理不必要的 UI transitive dependencies，升级 lockfile overrides。
- 建立 restore/sync/release 运维文档和端到端测试矩阵。

### Ignore for Now

- 日志 footer 在 `hasMore=true` 时常显 spinner、Catalog nested interactive、Circuit 移动端底部安全区：它们已确认但影响局部体验，建议与前端状态修复一起安排，不阻塞当前后端稳定性修复。

## 26. Quick Wins

| Change | Value | Effort |
|--------|-------|--------|
| relay/compact 加 `MaxBytesReader` | 立即消除 body 型内存风险 | 1-2 小时 |
| HTTP transformer 输入错误映射 400 | 修复客户端重试误判 | 1-2 小时 |
| site-channel 统一 query key factory | 修复即时 UI 更新 | 1-2 小时 |
| SSE overflow 时 invalidate/reload | 避免实时日志首屏缺口 | 2-4 小时 |
| release job 添加 go test/lint/tsc | 关闭未经验证发布路径 | 1-2 小时 |
| binding mutation 后清理 stats map | 避免新旧 group 归因混淆 | 1-2 小时 |

## 27. Long-term Refactor Plan

仅列有证据支持的长期改造：

1. **Resource lifecycle coordinator:** 将 site/account、managed channel、binding、quote、stats 的创建/更新/删除统一为 owner-aware service，并为每次 projection 生成 revision。
2. **Restore generation:** restore 前后切换 generation；异步 writer/usage/stats 带 generation，旧 generation 在恢复后丢弃；统一 WebDAV 与管理面 upload 入口。
3. **Shared protocol policy:** HTTP/WS 共用 canonical model、supported-model allowlist、输入错误和 response error mapping。
4. **Durable frontend task state:** tools probe 用 React Query/store 或 SSE 订阅 task，而不是由虚拟卡片持有 interval；日志使用 cursor/sequence 合并。
5. **Release provenance:** tag 只能从 required checks 已通过的 immutable commit 生成，制品签名并保留上一版本回滚引用。

## 28. Verification and Scope Notes

### Commands executed

- `go test ./...` — 通过。
- `go vet ./...` — 通过。
- `cd web && pnpm lint` — 通过。
- `cd web && pnpm exec tsc --noEmit --incremental false` — 通过。
- `cd web && pnpm build` — 通过，Next.js static build 成功。
- `cd web && pnpm audit --prod` — 失败（发现 8 个漏洞：1 high、6 moderate、1 low）。
- `git diff --check` — 通过。

### Authenticity and limitations

- 静态证据均来自当前 HEAD；本次没有修改业务代码。工作区只有既有未跟踪文件 `CODE_REVIEW_LATEST_TWO_COMMITS.md`，报告本身为新增文件。
- 未执行真实端口抢占、优雅停机、并发 quota 压测、超大 body RSS 压测、在线 restore 竞态和移动端 Playwright；对应 finding 的触发链可由源码直接推出，但修复前应补充这些运行时测试。
- 复核后未将“DBImport API 被某个当前线上入口必然留旧 cache”列为核心 finding：现有 WebDAV 与管理面入口在导入后都会调用 `InitCache()`；剩余风险是公共 API 的调用契约容易被未来调用者误用。
- 未晋级观察：`internal/op/user.go:31-56` 先改进程内 `userCache` 再写 DB，DB 失败时会造成短暂认证分裂；日志页切换 timezone 不重算语义时间窗口；`Catalog.tsx` 有 nested interactive；日志 footer 可能误显示 spinner。它们建议纳入后续专项测试，但没有进入本次 20 条核心 finding 统计。
