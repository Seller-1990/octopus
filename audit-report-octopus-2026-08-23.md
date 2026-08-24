# Fuck My Shit Mountain Audit Report

**Project:** Octopus（LLM API 聚合与负载均衡服务）
**Audit mode:** performance, stability, maintainability, fallback
**Date:** 2026-08-23
**Reviewer:** AI software engineer（本地部署视角）

---

## 1. Executive Summary

Octopus 是一个功能密度很高的本地 LLM 网关，Go 后端 + Next.js 前端。整体工程质量明显高于平均水平：`go test ./...` 全量通过，`go vet ./...` 零告警，`pnpm lint` 仅 1 个未使用变量警告；并发设计（分片缓存、per-key 锁、增量 ChannelKey 更新、WS 连接池 busy 检查）已有不少针对性修复。核心分层 `conf/db/model/op/relay/transformer/server/task/sitesync/web` 清晰，本地单机定位也基本没有云原生过度设计。

但本次审查仍确认了若干真实风险：**事务 recover 吞 panic 后返回“成功”**、**GroupList 写穿缓存底层数组**、**流式 passthrough 在客户端断连时重复喂聚合器**、**OpenAI Responses 多 tool call done 事件顺序随机**、**SQLite 单连接 + 每请求多次 DB 查询**等。维护性方面存在多个 2000+ 行 Go 文件和 3000+ 行前端组件，以及一批可安全删除的死代码/无用包装。

结论：**有条件通过**。建议优先修复稳定性 P0/P1 类问题（事务 recover、缓存数组写穿、重复指标采集），再处理性能与死代码清理。本地规模下性能问题不会立刻致命，但会随着并发请求数和 relay_logs 增长逐步恶化。

### Score Dashboard

```
Stability       ██████░░░░  6.0  B   recover 吞 panic、重复指标采集、协议事件顺序随机
Performance     ██████░░░░  6.0  B   SQLite 单连接 + 每请求多次 DB 查询 + 双重 tokenize
Maintainability ██████░░░░  5.5  B   多个 2000+ 行文件、死代码/无用包装、巨型前端组件
Design          ██████░░░░  6.0  B   recover 过度防御、fail-open/fail-closed 不一致、无谓抽象
─────────────────────────────────────
Overall         ██████░░░░  5.9  B
```

> Security / Testing / Release 未纳入本次评估（未选择对应审计模式）。

### Finding Statistics

| Severity | Count | Confirmed | Suspected |
|----------|-------|-----------|-----------|
| Critical | 0 | 0 | 0 |
| High | 5 | 5 | 0 |
| Medium | 10 | 10 | 0 |
| Low | 8 | 8 | 0 |
| Info | 2 | 2 | 0 |
| **Total** | **25** | **25** | **0** |

---

## 2. Project Map

- **入口/生命周期**：`main.go` → `cmd/root.go` / `cmd/start.go` → DB → Cache → UserInit → HTTP Server → 后台任务 → shutdown hooks。
- **请求流**：`/v1/*` → `middleware.APIKeyAuth` → `relay.Handler` → `op` 路由规划（group/catalog/header-policy/price）→ `balancer` → `client`（HTTP/TLS 指纹/WS）→ `transformer` inbound/outbound → relay_log/usage/stats 异步落库。
- **数据层**：SQLite（默认，单连接）或 MySQL/Postgres；大量业务数据走内存分片缓存，`relay_logs`/`usage_facts` 异步批量写。
- **前端**：React 19 + TanStack Query + Zustand + 自定义 SPA 路由；SSG 输出嵌入 Go 静态目录。
- **风险高发区**：`relay`（热路径、流状态机）、`transformer`（协议边界）、`op`（缓存与事务）、`sitesync`（上游抓取）、`web/src/components/modules/site-channel/index.tsx`（3400 行巨型组件）。

---

## 3. Top Risks

1. **[High] 事务 recover 吞 panic** —— `op/channel.go`、`op/group.go`、`op/group_preset.go` 的 `defer recover` 只 rollback 不 re-panic，panic 后函数以零值返回，调用方可能误判成功。
2. **[High] GroupList 写穿缓存底层数组** —— `groupCache.GetAll()` 是浅拷贝，`applyGroupItemMultiplierPolicies` 原地修改 `group.Items`，与并发读共享 backing array。
3. **[High] OpenAI Responses 多 tool call done 事件顺序随机** —— `for idx, tc := range i.toolCalls` 遍历 map，output_index 投递顺序不确定。
4. **[High] passthrough 流断连时指标/内容重复聚合** —— `stream/processor.go handleDisconnect` 调 `OnFinish`，`relay.go` 断连分支又调一次 `collectPassthroughMetrics`，聚合器被喂两次。
5. **[High] SQLite 单连接 + 每请求多次 DB 查询** —— 路由规划、倍率策略、HeaderPolicy、usage 维度补全都在请求路径上同步查 DB。
6. **[Medium] 非流式上游无整体超时** —— `http.Client` 未设置 `Timeout`，上游响应头之后挂死时请求可能无限等待。
7. **[Medium] 前端 Analytics 页后台刷新失败即整页报错** —— 已有旧数据也被错误页覆盖。
8. **[Medium] 协议转换边界问题** —— Anthropic error 事件可重入、tool call 按 index 合并、Gemini tool args 解析失败发 nil。
9. **[Low] 多处死代码** —— `ChannelHttpClient`、`StaticLocal`、`RegisterBeforeAutoMigration`、`RelayLogDroppedTotal` 等无调用。
10. **[Medium] 维护性债务** —— 多个超大文件，单文件职责过重。

---

## 4. Detailed Findings

### 模块：internal/op（业务操作层）

---

### Finding: 事务函数中 recover 吞掉 panic，函数以“成功”零值返回

- Severity: High
- Confidence: High
- Category: Stability
- Status: Confirmed
- Affected area: internal/op（channel / group / group_preset 事务写路径）
- Evidence:
  - File: `internal/op/channel.go:242-247`, `internal/op/channel.go:499-503`
  - File: `internal/op/group.go:112-117`, `internal/op/group.go:286-292`
  - File: `internal/op/group_preset.go:443-448`
  - Function / Module: `ChannelUpdate` / `channelDel` / `GroupUpdate` / `GroupDel` / `GroupPresetActivate`
  - Relevant behavior: `tx := db.GetDB().WithContext(ctx).Begin()` 后 `defer func(){ if r := recover(); r != nil { tx.Rollback() } }()`，但 recover 后没有 `panic(r)`，也没有把 panic 转成 error。
- Problem: Go 中 recover 会终止 panic 展开；对于无命名返回值的函数，函数会直接以零值返回。于是事务内发生 panic（例如 nil 指针、类型断言）时，DB 被回滚，但调用方拿到 `nil, nil` 或 `nil`，看起来像成功。
- Why it matters: 它把“可见的崩溃”变成“静默的错误成功”，后续 handler 可能解引用 nil 再次 panic，或被上层当作成功继续处理，造成状态不一致。
- Realistic failure scenario: `ChannelUpdate` 在事务处理中因某个内部 bug panic → rollback → 返回 `(*Channel)(nil), nil` → handler `channel.ID` panic，被 Gin Recovery 兜住返回 500；若某个调用方不立即解引用，则可能继续走“成功”逻辑。
- Minimal fix: 在 recover 分支中 rollback 后 `panic(r)`；或改为 `return fmt.Errorf("panic in transaction: %v", r)`（需要函数具名返回值或逐点改造）。
- Better long-term fix: 移除手写 recover，依赖 GORM error 处理；如需兜底，用统一 `safe.Run` 或中间件恢复，并在事务层保留 re-panic 语义。
- Regression test suggestion: 注入一个会在事务中途 panic 的 mock，断言函数返回 error（或 panic 继续向外传播），而不是 `nil, nil`。
- Estimated effort: 1 小时

---

### Finding: GroupList 原地写穿 groupCache 的 Items 底层数组

- Severity: High
- Confidence: High
- Category: Stability
- Status: Confirmed
- Affected area: internal/op/group.go + group_multiplier_policy.go
- Evidence:
  - File: `internal/op/group.go:18-24`
  - Function / Module: `GroupList`
  - Relevant behavior: `groupCache.GetAll()` 返回的 `model.Group` 是值拷贝，但 `group.Items` 仍共享缓存中的底层 slice 数组；`group.Items = applyGroupItemMultiplierPolicies(ctx, group.Items)` 会原地写 `items[index].Multiplier/PolicyStatus/...`。
- Problem: `applyGroupItemMultiplierPolicies` 在 `internal/op/group_multiplier_policy.go:112-126` 中直接写传入 slice 的元素。`GroupList` 是只读 API，却修改了缓存内部数据。
- Why it matters: 多个 goroutine 并发时，`GroupList` 的写与 relay 路径 `GroupGetEnabledMap` 的读可能访问同一 backing array，属于数据竞争；即使未触发 panic，也会把派生策略字段永久写进缓存。
- Realistic failure scenario: 管理面板轮询 `GroupList` 的同时，relay 正在读同一 group 的 Items 做路由规划，`go test -race` 会报 race；生产环境可能读到半写字段。
- Minimal fix: `GroupList` 中先深拷贝 Items：`items := append([]model.GroupItem(nil), group.Items...); group.Items = applyGroupItemMultiplierPolicies(ctx, items)`。
- Better long-term fix: 让缓存只存不可变数据，所有派生字段在返回时生成；或在 `Cache.GetAll` 上提供深拷贝语义。
- Regression test suggestion: 并发执行 `GroupList` 与 `GroupGetEnabledMap`，用 `-race` 运行；并断言 `GroupList` 后缓存中的 Items 不被改写。
- Estimated effort: 30 分钟

---

### Finding: 每请求同步 DB 查询过多，且 SQLite 单连接放大排队

- Severity: High
- Confidence: High
- Category: Performance
- Status: Confirmed
- Affected area: internal/db + internal/op（header_policy / catalog / group_multiplier / usage_facts）
- Evidence:
  - File: `internal/db/db.go:50-53`（SQLite `SetMaxOpenConns(1)`）
  - File: `internal/op/header_policy.go:346-389`（每个 attempt 先查 binding，再逐 scope 查 policy）
  - File: `internal/op/catalog.go:1174-1193`（每次路由规划查 candidates、performance、reserve、balance）
  - File: `internal/op/catalog.go:1081-1090`（`LOWER(model_name)` 非 sargable 查询）
  - File: `internal/op/group_multiplier_policy.go:57-62`（每次请求查 site_channel_bindings）
  - File: `internal/op/usage_facts.go:463-506`（每次 `RelayLogAdd` 同步查一次 binding 维度）
- Problem: 这些查询大多数是低频变化数据（binding/policy/candidate/price），却放在每请求/每 attempt 的同步路径上。SQLite 又限制为单连接，relay_logs 批量写事务期间所有读查询都会排队。
- Why it matters: 本地部署在并发请求升高、日志表变大时会出现周期性卡顿；首 token 延迟被 DB 排队直接拉高。
- Realistic failure scenario: 100 QPS + 每秒 flush 一次大体积 relay_log 批次 → 路由规划/HeaderPolicy 查询全部排在写事务后，用户观察到偶发秒级延迟。
- Minimal fix: ① 将 `binding/policy/candidate/price` 加入现有分片缓存并在写路径失效；② SQLite 读连接放开到 4-8（写仍由业务层串行化）；③ `relay_log` 批次加字节上限（如 ≤4MB 分多事务）。
- Better long-term fix: 把路由规划结果做短 TTL 缓存；usage 维度补全移到异步 flush 阶段而非 `RelayLogAdd` 同步路径；`model_name` 入库统一小写并建普通索引。
- Regression test suggestion: 基准测试“100 次路由规划”统计 DB 查询次数；断言每次请求查询数从 10+ 降到 ≤3。
- Estimated effort: 1-2 天

---

### Finding: 非流式上游响应无整体超时

- Severity: Medium
- Confidence: Medium
- Category: Stability
- Status: Confirmed
- Affected area: internal/client + internal/relay
- Evidence:
  - File: `internal/client/http.go:139,169`（`http.Client{Transport: cloned}`，无 `Timeout`）
  - File: `internal/relay/relay.go:1480-1510`（`sendRequest` 使用请求 ctx，但非流式没有整体 deadline）
  - Relevant behavior: 只有 TCP Dial/TLS 握手有超时；非流式请求在拿到响应头后 `io.ReadAll(response.Body)` 没有 deadline。
- Problem: 上游返回 200 后不再发送数据也不关闭连接时，请求 goroutine 可能无限阻塞。
- Why it matters: 本地网关若某个上游异常挂起，会占住连接和 goroutine，最终拖垮服务。
- Realistic failure scenario: 上游 LLM 服务半死状态，HTTP 响应头已返回但 body 永不结束，客户端等待数十分钟无结果。
- Minimal fix: 对非流式请求使用带超时的 ctx（如 `context.WithTimeout`）或给 `http.Client.Timeout` 设置合理上限；流式请求继续使用首 token 超时机制。
- Better long-term fix: 在配置中增加 `client.response_timeout_seconds`，区分流式/非流式超时策略。
- Regression test suggestion: 用 `httptest` 返回永不关闭的 body，断言请求在超时后返回错误。
- Estimated effort: 2 小时

---

### Finding: 热路径对同一份内容多次 tokenize

- Severity: Medium
- Confidence: High
- Category: Performance
- Status: Confirmed
- Affected area: internal/transformer/inbound/anthropic + internal/relay/metrics
- Evidence:
  - File: `internal/transformer/inbound/anthropic/messages.go:105-405`（system/message/tool 逐段 `tokenizer.CountTokens`）
  - File: `internal/relay/metrics.go:118`（`SetTransportRequestPayload` 对整个出站 body 再 `CountTokensBytes`）
  - File: `internal/relay/relay.go:1009,1442`（WS 与 HTTP 路径都设置 transport payload）
- Problem: 同一份请求内容在 Anthropic 入站转换时逐消息计数，之后又对整个出站 body 再计一次；且该估算值只在“上游未上报 usage”时才被消费。
- Why it matters: 对长上下文/大请求会重复付出 BPE 分词 CPU；在首 token 延迟敏感路径上属于可避免开销。当前 tokenizer 已有 64KB 阈值与单例，风险从 P0 降为 Medium。
- Realistic failure scenario: 32KB 以上的长 prompt 经 Anthropic 入站 + 出站转发，同一文本被分词 2 遍，单请求增加数十毫秒。
- Minimal fix: 延迟到“响应完成且确认 usage 缺失”再估算；或在入站已统计时复用结果，避免对出站 body 二次计数。
- Better long-term fix: 让 metrics 只保存 raw body，usage 缺失时惰性计算；移除 Anthropic 入站的逐段统计。
- Regression test suggestion: 对同一 prompt 断言 `CountTokens` 调用次数不超过 1 次；或 mock tokenizer 统计调用次数。
- Estimated effort: 3-4 小时

---

### Finding: HeaderPolicy 失败时 fail-closed 静默丢弃客户端头

- Severity: Medium
- Confidence: High
- Category: Stability
- Status: Confirmed
- Subtype: SilentFallback
- Affected area: internal/op/header_policy.go + internal/relay/relay.go
- Evidence:
  - File: `internal/op/header_policy.go:96-104`（`HeaderPolicyFailureFallback` 返回空策略）
  - File: `internal/relay/relay.go:1450-1457`（DB 查询失败时 log warn 后继续请求）
- Problem: 策略查询失败时，所有 client headers 被清空，请求继续发往上游。
- Why it matters: 如果某个渠道依赖客户端自定义头（如 API key 透传、Cookie），DB 抖动会直接导致上游 401/403；该行为安全但会让“局部 DB 故障”变成“全渠道不可用”。
- Realistic failure scenario: SQLite 单连接被大事务阻塞，`ResolveHeaderPolicy` 超时/失败 → 所有请求以空头转发 → 上游拒绝。
- Is the fallback necessary? Partial（安全侧 fail-closed 合理，但应可观测）
- If yes, is it monitored? 仅 `log.Warnf`，无指标/计数
- Recommended action: KeepWithAlert
- Minimal fix: 在 fallback 时增加指标计数（如 `header_policy_fallback_total`）或返回 503 让客户端重试。
- Regression test: 模拟 DB 错误，断言请求被拒绝或 fallback 被计数。
- Estimated effort: 1 小时

---

### Finding: 倍率绑定查询失败时 fail-open，与 HeaderPolicy fail-closed 不一致

- Severity: Medium
- Confidence: High
- Category: Stability
- Status: Confirmed
- Subtype: SilentFallback
- Affected area: internal/op/group_multiplier_policy.go
- Evidence:
  - File: `internal/op/group_multiplier_policy.go:57-62`
  - Relevant behavior: `SiteChannelBindingMapByChannelIDs` 失败时仅 `log.Warnf`，随后把倍率视为 unknown 并放行。
- Problem: 同一个“DB 查询失败”场景，HeaderPolicy 选择 fail-closed，倍率策略选择 fail-open；当 `default_multiplier_cap` 开启时，DB 故障会导致超限渠道被放行。
- Why it matters: 计费/配额控制点 fail-open 可能造成成本失控；至少应和 HeaderPolicy 一样可观测并明确产品决策。
- Realistic failure scenario: SQLite 繁忙导致 binding 查询失败，cap=2x，实际 10x 渠道被放行。
- Is the fallback necessary? Partial（本地工具可接受降级，但应有告警）
- If yes, is it monitored? 仅 warn 日志
- Recommended action: KeepWithAlert / Restructure
- Minimal fix: 增加 fallback 计数，或在 cap 开启时对查询失败采取 fail-closed。
- Regression test: 模拟 binding 查询失败，断言策略状态与预期一致且产生告警指标。
- Estimated effort: 2 小时

---

### Finding: tokenizer 错误被静默映射为 0 token

- Severity: Low
- Confidence: High
- Category: Stability
- Status: Confirmed
- Subtype: SilentFallback
- Affected area: internal/utils/tokenizer
- Evidence:
  - File: `internal/utils/tokenizer/tokenizer.go:20-38`
  - Relevant behavior: `CountTokens` / `CountTokensBytes` 在 `encoder().Count` 出错时 `return 0`，不记录日志。
- Problem: token 计数错误被静默当作 0，可能导致 usage/cost 统计偏低。
- Why it matters: 计费与用量分析依赖 token 数；静默 0 比明确报错更难排查。
- Realistic failure scenario: 模型名/词表兼容问题导致 `Count` 返回 error，所有该模型请求 usage 记 0。
- Is the fallback necessary? No（至少应告警）
- Recommended action: FailFast / KeepWithAlert
- Minimal fix: error 时 `log.Warnf` 并返回 `len(content)/4` 粗估值，而不是 0。
- Regression test: mock encoder 返回 error，断言返回非 0 粗估值或产生 warn。
- Estimated effort: 30 分钟

---

### Finding: ApplyParamOverride 对非法 override 静默忽略

- Severity: Low
- Confidence: High
- Category: Stability
- Status: Confirmed
- Subtype: SilentCorrection
- Affected area: internal/helper/param_override.go
- Evidence:
  - File: `internal/helper/param_override.go:14-44`
  - Relevant behavior: override JSON 无法解析时恢复原 body 并 `return nil`，无任何日志。
- Problem: 渠道配置里的 `param_override` 写错时，请求静默按原参数转发，管理员难以发现配置失效。
- Why it matters: 配置错误被隐藏，用户以为 override 生效，实际没有。
- Is the fallback necessary? Partial（不应因配置错误阻断所有请求）
- If yes, is it monitored? 否
- Recommended action: KeepWithAlert
- Minimal fix: 解析失败时 `log.Warnf("param_override invalid")` 并继续原请求。
- Regression test: 非法 override 时断言产生 warn 日志。
- Estimated effort: 20 分钟

---

### 模块：internal/relay（转发核心）

---

### Finding: passthrough 流断连时聚合器被重复喂入

- Severity: High
- Confidence: High
- Category: Stability
- Status: Confirmed
- Affected area: internal/relay/stream + internal/relay/relay.go
- Evidence:
  - File: `internal/relay/stream/processor.go:317-322`（`handleDisconnect` 调用 `OnFinish`）
  - File: `internal/relay/relay.go:1700-1706`（`OnFinish` 内调用 `collectPassthroughMetrics`）
  - File: `internal/relay/relay.go:1739-1741`（Run 返回 `context.Canceled` 后再次 `collectPassthroughMetrics`）
- Problem: 客户端断连且已有部分流数据时，`handleDisconnect` 先把 raw buffer 喂给聚合器，随后 relay 又用 `rawStreamBuf` 再喂一次。`collectResponse` 有 CAS 幂等，但 `collectPassthroughMetrics` 没有。
- Why it matters: 日志中的响应内容会重复拼接，DB 体积虚增；依赖严格聚合的协议状态可能被污染。
- Realistic failure scenario: Anthropic passthrough 流式请求在 `message_stop` 前断连，`relay_logs.response_content` 出现两遍文本。
- Minimal fix: 在 relay 断连分支增加 `passthroughMetricsCollected` 原子标志，或删除第二次 `collectPassthroughMetrics`。
- Better long-term fix: 统一由 StreamProcessor 的 `OnFinish` 负责一次指标收集，relay 只处理 `OnFinish` 未覆盖的路径。
- Regression test: 构造断连流，断言 `collectPassthroughMetrics` 只被调用一次。
- Estimated effort: 1-2 小时

---

### Finding: 热路径对出站 body 全量读取用于 metrics

- Severity: Low
- Confidence: High
- Category: Performance
- Status: Confirmed
- Affected area: internal/relay/relay.go
- Evidence:
  - File: `internal/relay/relay.go:1435-1443`（`applyParamOverride` 后无条件 `readOutboundRequestBody`）
  - File: `internal/relay/relay.go:1412-1424`（`readOutboundRequestBody` 可能 `io.ReadAll`）
- Problem: 即使没有 `param_override`，每次 attempt 也会重新读取整个 outbound body 用于设置 metrics。
- Why it matters: 大请求体（含图片 base64）在转发路径上被多一次全量拷贝与后续 tokenize。
- Minimal fix: 仅当需要 transport payload 时才读取；或直接复用 `rawBody`/`internalRequest` 估算。
- Better long-term fix: 将 metrics 采集延迟到请求结束，且只在 usage 缺失时计算。
- Regression test: 对无 override 的请求断言不调用 `readOutboundRequestBody`。
- Estimated effort: 1-2 小时

---

### 模块：internal/transformer（协议转换）

---

### Finding: OpenAI Responses 多 tool call 的 done 事件按 map 随机顺序输出

- Severity: High
- Confidence: High
- Category: Stability
- Status: Confirmed
- Affected area: internal/transformer/inbound/openai
- Evidence:
  - File: `internal/transformer/inbound/openai/response.go:846-881`
  - Function / Module: `ResponseInbound.closeCurrentOutputItem`
  - Relevant behavior: `for idx, tc := range i.toolCalls` 遍历 `map[int]*model.ToolCall`，生成 `response.function_call_arguments.done` 与 `response.output_item.done` 时顺序随机。
- Problem: 含 ≥2 个 function_call 的流式 Responses 响应，output_index 投递顺序不确定。
- Why it matters: 依赖 output_index 单调递增匹配 tool call 的严格客户端可能错配参数。
- Realistic failure scenario: Pydantic AI / 部分 Agent SDK 解析多工具调用流时把 arguments 挂到错误的 tool call。
- Minimal fix: 收集 `maps.Keys(i.toolCalls)` 后 `sort.Ints`，按 index 升序输出。
- Better long-term fix: 把 toolCalls 改为有序结构（slice + map 索引）。
- Regression test: 构造 3 个 tool call 的流，断言 done 事件 output_index 严格递增。
- Estimated effort: 1 小时

---

### Finding: Anthropic error 事件无重入保护

- Severity: Medium
- Confidence: High
- Category: Stability
- Status: Confirmed
- Affected area: internal/transformer/inbound/anthropic
- Evidence:
  - File: `internal/transformer/inbound/anthropic/messages.go:1023-1038`
  - Function / Module: `MessagesInbound.TransformStreamEvents`
  - Relevant behavior: `StreamEventKindError` 分支没有检查 `i.messageStopped`，每次 error 事件都会再次输出。
- Problem: 上游或 relay 重试时重复 error 事件会被原样转发给客户端。
- Why it matters: Anthropic 客户端收到多个 `event: error`，协议流被污染。
- Realistic failure scenario: 上游发 error 后再发一个 error，或 relay 重试时重复投递同一 error。
- Minimal fix: 在 error 分支开头加 `if i.messageStopped { continue }`。
- Regression test: 连续两个 error 事件，断言只输出一个。
- Estimated effort: 20 分钟

---

### Finding: StreamAggregator 按 Index 合并 tool call，index 复用时拼接无效 JSON

- Severity: Medium
- Confidence: High
- Category: Stability
- Status: Confirmed
- Affected area: internal/transformer/model
- Evidence:
  - File: `internal/transformer/model/stream_aggregator.go:137-160`
  - Function / Module: `MergeToolCallDelta`
  - Relevant behavior: 仅按 `tc.Index == delta.Index` 合并；若不同 tool call 复用 index，会把 name/arguments 拼到一起。
- Problem: Responses API 的 output_index 可能在 message/reasoning item 后重新计数；index 复用时产生损坏的 tool call。
- Why it matters: 聚合后的完整响应 tool call 无效，relay log 与 WS replay 状态错误。
- Realistic failure scenario: 上游某模型流中两个 function call 使用相同 output_index，聚合后 arguments 变成两段拼接。
- Minimal fix: 增加 `(ID, Index)` 复合匹配；ID 不同时视为新 tool call。
- Regression test: 构造 index 复用但 ID 不同的 delta，断言生成两个独立 tool call。
- Estimated effort: 1 小时

---

### Finding: Gemini outbound tool call arguments 解析失败时发送 nil

- Severity: Medium
- Confidence: High
- Category: Stability
- Status: Confirmed
- Affected area: internal/transformer/outbound/gemini
- Evidence:
  - File: `internal/transformer/outbound/gemini/messages.go:735-746`
  - Function / Module: `MessagesOutbound` 的 tool call 转换
  - Relevant behavior: `json.Unmarshal` 失败时只 `log.Warnf`，`args` 保持 nil。
- Problem: Gemini API 要求 `args` 为对象，nil 会触发 `INVALID_ARGUMENT`。
- Why it matters: 单个工具参数损坏会导致整个请求失败，且日志只有 warn。
- Minimal fix: 解析失败时 `args = map[string]interface{}{}`。
- Regression test: 传入非法 arguments JSON，断言发送给上游的 Args 是空对象而非 nil。
- Estimated effort: 20 分钟

---

### 模块：internal/server / web 前端

---

### Finding: 前端日志分析页在后台刷新失败时整页报错并丢弃旧数据

- Severity: Medium
- Confidence: High
- Category: Stability
- Status: Confirmed
- Affected area: web/src/components/modules/log
- Evidence:
  - File: `web/src/components/modules/log/Analytics.tsx:102-117`
  - Function / Module: `Analytics`
  - Relevant behavior: 先判断 `isLoading && !summary`，随后 `if (summaryQuery.error || timeseriesQuery.error || breakdownQuery.error)` 直接返回错误页。
- Problem: React Query 后台 refetch 失败时仍保留旧 `data` 并设置 `error`；当前逻辑把有效旧数据整页替换为错误。
- Why it matters: 后端重启/更新 30s 窗口内，日志分析页整页变红，用户无法看到已有数据。
- Realistic failure scenario: 页面打开后后端短暂重启，30s 轮询失败 → 整页错误，即使旧数据完整。
- Minimal fix: 错误判断改为“无数据时显示错误，有数据时显示顶部错误条 + 继续渲染旧数据”。
- Regression test: 模拟 query 先成功后有 error，断言旧数据仍渲染。
- Estimated effort: 1 小时

---

### Finding: 巨型前端组件难以维护

- Severity: Medium
- Confidence: High
- Category: Maintainability
- Status: Confirmed
- Affected area: web/src/components/modules
- Evidence:
  - File: `web/src/components/modules/site-channel/index.tsx`（3405 行）
  - File: `web/src/components/modules/site/index.tsx`（2739 行）
  - File: `web/src/components/modules/log/Item.tsx`（1363 行）
  - File: `web/src/components/modules/setting/APIKey.tsx`（1140 行）
- Problem: 单组件承担过多 UI + 状态 + 业务逻辑，改动影响面大，测试难写。
- Why it matters: 后续每加一个站点/渠道功能都在这几个文件里堆积分支。
- Minimal fix: 先把纯 UI 子区块拆成独立组件，状态逻辑抽到 hooks/store。
- Better long-term fix: 按功能域拆分模块（站点列表/编辑/批量操作/checkin 等）。
- Regression test: 拆分后跑现有 `pnpm lint` + 相关组件测试。
- Estimated effort: 2-3 天

---

### Finding: 前端存在未使用变量

- Severity: Low
- Confidence: High
- Category: Maintainability
- Status: Confirmed
- Affected area: web/src/components/modules/site
- Evidence:
  - File: `web/src/components/modules/site/index.tsx:1114`
  - Relevant behavior: `handleBrowserSync(account: SiteAccount, site: SiteRecord)` 中 `site` 未使用。
- Problem: `pnpm lint` 报 1 个 warning。
- Minimal fix: 删除未使用参数或加 `_` 前缀。
- Estimated effort: 5 分钟

---

### 模块：全局死代码 / 过度设计

---

### Finding: 多个导出函数无任何调用，可安全删除

- Severity: Low
- Confidence: High
- Category: Maintainability
- Status: Confirmed
- Affected area: helper / op / apperror / db/migrate / model / server/middleware
- Evidence:
  - File: `internal/helper/channel.go:17` — `ChannelHttpClient` 无调用
  - File: `internal/op/clash_controller.go:369` — `ClashControllerProxyConfiguration` 无调用
  - File: `internal/apperror/apperror.go:59,152` — `Wrapf`、`IsCode` 无调用
  - File: `internal/db/migrate/migrate.go:31` — `RegisterBeforeAutoMigration` 无调用
  - File: `internal/op/log.go:219` — `RelayLogDroppedTotal` 是无调用且恒返回 0 的 stub
  - File: `internal/op/site_channel.go:17` — `SiteChannelList` 仅包装 `SiteChannelListWithOptions`，无调用
  - File: `internal/model/site.go:810` — `SiteModelRouteTypeName` 无调用
  - File: `internal/server/middleware/static.go:25` — `StaticLocal` 无调用
- Problem: 这些函数增加 grep 噪音和维护面，`RelayLogDroppedTotal` 还会误导后续开发者以为有丢弃统计。
- Why it matters: 死代码是“未来可用”的投机成本；删除后 git 历史仍可找回。
- Minimal fix: 逐个确认后删除；`RelayLogDroppedTotal` 若不再需要直接删。
- Regression test: `go build ./...` + `go vet ./...` 通过即可。
- Estimated effort: 1 小时

---

### Finding: 巨型 Go 文件职责过载

- Severity: Medium
- Confidence: High
- Category: Maintainability
- Status: Confirmed
- Affected area: internal/relay, internal/transformer, internal/op, internal/sitesync
- Evidence:
  - `internal/transformer/outbound/gemini/messages.go` 2126 行
  - `internal/relay/relay.go` 2049 行
  - `internal/transformer/model/model.go` 2014 行
  - `internal/transformer/outbound/openai/response.go` 2009 行
  - `internal/transformer/inbound/openai/response.go` 1883 行
  - `internal/op/backup_extended_import.go` 1712 行
  - `internal/sitesync/anyrouter.go` 1585 行
- Problem: 单个文件包含协议转换、重试、日志、指标、状态机等多种职责。
- Why it matters: 代码审查、冲突合并、单测定位成本高。
- Minimal fix: 优先拆分纯函数/私有 helper 到独立文件（同一 package，不改公共 API）。
- Better long-term fix: 按“请求准备 / 转发 / 流处理 / 指标 / 日志”拆分文件。
- Regression test: 拆分后全量 `go test ./...`。
- Estimated effort: 1-2 天

---

### Finding: 单实现抽象与“未来扩展”预留

- Severity: Low
- Confidence: Medium
- Category: Maintainability
- Status: Confirmed
- Affected area: internal/db/migrate
- Evidence:
  - File: `internal/db/migrate/migrate.go:31-32`
  - Relevant behavior: `RegisterBeforeAutoMigration` 暴露但从未被调用，而 `RegisterAfterAutoMigration` 被大量使用。
- Problem: 这是一个没有实际使用方的抽象入口，属于 YAGNI。
- Why it matters: 维护者会以为“before migration”是受支持路径，实际注册了也不会执行（因为无人调用时不会进入 slice）。
- Minimal fix: 删除 `RegisterBeforeAutoMigration` 及对应 `beforeAutoMigrations` 分支。
- Regression test: 全量 `go test ./...`。
- Estimated effort: 20 分钟

---

## 5. Performance Concerns

| 问题 | 位置 | 影响 | 建议 |
|------|------|------|------|
| SQLite 单连接 + 每请求多次 DB 查询 | db.go:50-53, header_policy.go:346-389, catalog.go:1174-1193, usage_facts.go:463-506 | 并发下路由与日志写互相排队 | 读写连接分离、缓存低频元数据、日志批写限字节 |
| 同一内容多次 tokenize | anthropic/messages.go:105-405, metrics.go:118 | 长请求 CPU 浪费 | 延迟到 usage 缺失时再估算，或复用入站计数 |
| 非流式无整体超时 | client/http.go:139,169, relay.go:1480 | 上游挂死导致连接泄漏 | 增加响应级 timeout |
| LOWER() 非 sargable | catalog.go:1089, site_pricing.go:472 | 表增长后查询变慢 | 入库统一小写 + 普通索引 |
| 出站 body 每 attempt 全量读取 | relay.go:1435-1443 | 大请求体多一次拷贝 | 仅 metrics 需要时读取/复用 rawBody |
| 日志清理每批 sleep 30ms | log.go:433-457 | 大表清理慢 | 可接受，本地低频，暂不处理 |

---

## 6. Stability Concerns

| 问题 | 位置 | 严重度 |
|------|------|--------|
| recover 吞 panic | op/channel.go, op/group.go, op/group_preset.go | High |
| GroupList 写穿缓存数组 | op/group.go:18-24 | High |
| passthrough 断连重复喂聚合器 | stream/processor.go:317-322, relay.go:1739-1741 | High |
| Responses tool call 顺序随机 | transformer/inbound/openai/response.go:846-881 | High |
| Anthropic error 事件重入 | transformer/inbound/anthropic/messages.go:1023-1038 | Medium |
| tool call 按 index 合并 | transformer/model/stream_aggregator.go:137-160 | Medium |
| Gemini nil args | transformer/outbound/gemini/messages.go:735-746 | Medium |
| Analytics 旧数据被错误页覆盖 | web/src/components/modules/log/Analytics.tsx:102-117 | Medium |
| 倍率查询失败 fail-open | op/group_multiplier_policy.go:57-62 | Medium |
| tokenizer 错误静默返回 0 | utils/tokenizer/tokenizer.go:20-38 | Low |
| live log detail 断线不重连 | web/src/api/endpoints/log.ts:550-595 | Low |

---

## 7. Maintainability Concerns

- 超大 Go 文件：`relay.go` 2049 行、`gemini/messages.go` 2126 行、`model.go` 2014 行、`response.go` 2009 行等。
- 超大前端组件：`site-channel/index.tsx` 3405 行、`site/index.tsx` 2739 行。
- 死代码：`ChannelHttpClient`、`StaticLocal`、`RegisterBeforeAutoMigration`、`RelayLogDroppedTotal`、`SiteChannelList`、`SiteModelRouteTypeName`、`IsCode`、`Wrapf`、`ClashControllerProxyConfiguration`。
- 过度防御：事务 recover 不 re-panic；`ApplyParamOverride` 静默吞配置错误；`RelayLogDroppedTotal` stub 误导。
- 值得肯定：op/relay/transformer 分层清晰；缓存/锁的并发注释充分；关键权衡（SQLite WAL、relay_logs AddColumn-only）有文档说明。

---

## 8. Fallback / Defensive Code Analysis

### Fallback Summary

| Subtype | Count | KeepWithAlert | FailFast | Remove |
|---------|-------|---------------|----------|--------|
| SilentFallback | 3 | 2 | 1 | 0 |
| EmptyCatch | 1 | 0 | 1 | 0 |
| CompatibilityBranch | 0 | 0 | 0 | 0 |
| SilentCorrection | 1 | 1 | 0 | 0 |
| DefensiveGuess | 0 | 0 | 0 | 0 |

- `HeaderPolicyFailureFallback`：fail-closed，保留但需指标告警。
- 倍率绑定查询失败 fail-open：保留但需与 fail-closed 策略统一，并加告警。
- tokenizer 错误返回 0：应 fail-fast/告警。
- `ApplyParamOverride` 非法 override 静默忽略：保留但需告警。
- 事务 recover 吞 panic：属于 EmptyCatch，应移除或 re-panic。

---

## 9. Principles Compliance

### Principles Violated

| Principle | Violations | Severity | Affected Areas |
|-----------|------------|----------|----------------|
| Fail-Fast | recover 吞 panic、tokenizer 0、override 静默忽略 | High | op, utils/tokenizer, helper |
| DRY | 同一内容多次 tokenize、多个超大文件内重复逻辑 | Medium | relay, transformer |
| KISS / YAGNI | 单实现抽象、死代码 stub、未来扩展入口 | Low | migrate, log, middleware |
| SRP | 2000+ 行文件、3000+ 行组件 | Medium | relay, transformer, op, web |
| 缓存一致性 | GroupList 写穿缓存底层数组 | High | op/group |

### Principles Respected

- 并发安全：per-key 锁、分片缓存、CAS、defer 清理覆盖好。
- 资源管理：HTTP body close、timer stop、goroutine recover 在 `safe` 包内统一处理。
- 本地部署定位：未发现云原生过度设计（分布式锁/多租户/复杂编排）。
- 可观测性：关键路径有结构化日志和指标，问题大多有 trace 线索。

---

## 10. Recommended Fix Order

### Fix Immediately

1. 事务 recover 改为 re-panic 或返回 error（`op/channel.go`、`op/group.go`、`op/group_preset.go`）。
2. `GroupList` 深拷贝 Items 后再应用策略（`op/group.go:18-24`）。
3. passthrough 断连重复指标采集加幂等标志（`relay.go:1739-1741`）。
4. OpenAI Responses 多 tool call 按 index 排序输出（`response.go:846-881`）。

### Fix Before Stable Release

5. SQLite 读连接放开 + 路由元数据缓存（db.go / header_policy / catalog / group_multiplier）。
6. 非流式上游整体超时。
7. Anthropic error 事件重入保护。
8. StreamAggregator tool call 复合 ID 匹配。
9. Gemini tool args 解析失败发空对象。
10. Analytics 页错误处理保留旧数据。

### Schedule Later

11. 删除死代码。
12. 拆分超大 Go 文件/前端组件。
13. 统一 fallback 策略（fail-open/fail-closed 不一致）。
14. tokenizer 错误告警、param_override 告警。
15. 移除单实现抽象 `RegisterBeforeAutoMigration`。

### Ignore for Now

- `RelayLogCleanup` 每批 sleep 30ms（本地低频可接受）。
- `GroupListModel` 返回 string slice（无风险）。
- 前端 live log detail 断线不重连（低概率）。

---

## 11. Quick Wins

| 改动 | 收益 | 预估 |
|------|------|------|
| 事务 recover 后 `panic(r)` | 避免静默成功 | 1h |
| `GroupList` 深拷贝 Items | 消除缓存数据竞争 | 30m |
| Anthropic error 重入保护 | 协议正确性 | 20m |
| Gemini nil args → `{}` | 避免上游 400 | 20m |
| 删除 `RelayLogDroppedTotal` 等死代码 | 减少误导 | 1h |
| Analytics 错误页保留旧数据 | 提升可用性 | 1h |

---

## 12. Long-term Refactor Plan

1. **路由规划缓存层**：把 header policy、site binding、route candidate、price quote 从“每请求 DB 查询”改为“内存缓存 + 写路径失效”，预计可把单请求 DB 查询次数降低 70% 以上。
2. **token 统计惰性化**：仅在“上游未返回 usage”时计算输入 token，并移除 Anthropic 入站逐段重复计数。
3. **文件拆分**：按职责拆分 `relay.go`、`transformer/model/model.go`、`site-channel/index.tsx`；先移动私有函数，保持公共 API 不变，用全量测试作为安全网。
4. **统一 fallback 策略**：对“配置/DB 异常”建立统一决策矩阵：安全控制点 fail-closed，非关键点 fail-open + 指标告警。
5. **事务模式标准化**：引入统一的 `withTx` 辅助函数，panic 时自动 rollback 并 re-panic，避免手写 `defer recover` 散落各处。

> 以上所有修复均建议在本地以 `go test ./...`、`go vet ./...`、`pnpm lint` 作为回归门槛；涉及行为变更的修复（尤其 fallback 语义、事务 panic 传播）应先补特征测试再改动。

---

## 13. 复审记录（二次核查，修正以本节为准）

### 13.1 复审方法

- 对报告中全部 25 项 findings 重新读码验证，重点复核 High/Medium 项。
- 对死代码逐项在仓库源码（go/ts/tsx/js/mjs/sh/json，排除 node_modules/.next/.git/.code-review-graph）交叉搜索，确认无测试、脚本、构建标签、反射引用。
- 核查每项 finding 的调用链与触发面，重新校准“本地部署”下的严重度。

### 13.2 复审结论：确认无误判的项

以下 finding 经二次读码确认，证据、位置、结论均成立：

1. 事务 recover 吞 panic（High）——5 处函数签名均为非命名返回值；recover 后未 re-panic，Go 语义下将以零值返回；`GroupPresetActivate` 与 `GroupUpdate` 的 handler 在 `err == nil` 时会直接向客户端返回成功，形成“事务已回滚但客户端看到成功”的静默失败，High 成立。
2. OpenAI Responses 多 tool call done 事件 map 随机序（High）——`i.toolCalls` 确为 `map[int]*model.ToolCall`，`closeCurrentOutputItem` 按 map 遍历输出，≥2 个 tool call 时顺序随机，High 成立。
3. Anthropic error 事件无重入保护（Medium）——`TransformStreamEvents` 顶部无 `messageStopped` 守卫，error 分支只置位不拦截，Medium 成立。
4. StreamAggregator 按 Index 合并 tool call（Medium）——`MergeToolCallDelta` 仅 `tc.Index == delta.Index` 即合并，index 复用时拼接，Medium 成立。
5. Gemini nil args（Medium）——补充修正：`GeminiFunctionCall.Args` 标签为 `omitempty`，解析失败时 args 为 nil 导致字段被**省略**而非发送 `null`；Gemini 仍要求 args 必填，400 结论不变，Medium 成立。
6. HeaderPolicy fail-closed（Medium）与倍率 fail-open（Medium）——两者 DB 失败语义确不一致，属实。
7. tokenizer 错误静默返回 0（Low）、ApplyParamOverride 静默忽略非法配置（Low）——属实。
8. Analytics 页错误覆盖旧数据（Medium）——TanStack Query 在 refetch 失败时保留旧 data 并置 error，当前条件确会把旧数据整页替换为错误，Medium 成立。
9. 巨型文件/组件（Medium）、前端未使用变量（Low）——行数统计与 lint 输出属实。
10. 全部 9 项死代码（Low）——交叉搜索确认均无调用点，确为死代码。

### 13.3 复审结论：需要修正的项

#### 修正 1：SQLite 单连接 + 每请求 DB 查询：High → Medium

- 原报告把该问题列为 High，并将 `usage_facts` 维度补全计入“首 token 延迟”来源。
- 复核发现：`RelayLogAdd` → `enrichUsageDimensions` 发生在 `SaveOutcomeWithChannelStats` 中，即响应已写回客户端之后；它不增加首 token 延迟，只增加请求 goroutine 占用与 DB 负载。
- 真正在请求路径上的是：`GroupGetEnabledMapForTools`（2 次倍率相关查询）+ `CatalogPlanGroup`（candidates、performance、reserve、balance 约 5-7 次）+ `ResolveHeaderPolicy`（每 attempt 最多 N 次 scope 查询）。对本地低并发场景，这属于性能债务而非高危缺陷，降为 Medium。

#### 修正 2：passthrough 断连重复聚合：High → Medium

- 复核确认重复喂入客观存在（`processor.go` 的 `handleDisconnect` 先经 `OnFinish` 喂一次，`relay.go` 断连分支再喂一次）。
- 但影响面仅限 `relay_logs.response_content` 内容重复与 replay/聚合状态，客户端收到的流式响应不受影响；usage 汇总对 Usage 采用“最后非空覆盖”语义，token/费用不会被翻倍。降为 Medium。

#### 修正 3：GroupList 写穿缓存底层数组：High → Medium

- 数据竞争本身确认存在：`groupCache.GetAll()` 返回浅拷贝，`GroupList` 原地写 `Items[i]` 与 relay 路径的读共享 backing array。
- 但全局视角看：触发源 `GroupList` 是管理端列表 API，频率低；relay 路径 `GroupGetEnabledMap` 会先把每个 item 拷贝到新 slice，再重新计算并覆盖这些派生字段，业务结果不会因该竞争而错乱。主要风险是 `-race` 命中与理论上的撕裂读。降为 Medium，但仍应立即修复。

#### 修正 4：性能/稳定性小节中 `usage_facts` 描述

- 原“SQLite 单连接 + 每请求 DB 查询”evidence 中把 `usage_facts.go:463-506` 列为每请求查询无误，但不应描述为“首 token 延迟被 DB 排队直接拉高”。复审修正为：该查询为响应后落库路径，影响吞吐而非客户端延迟。

### 13.4 复审后评分

```
Stability       ██████░░░░  6.5  B   2 High + 若干 Medium，较初版更准确
Performance     ██████░░░░  6.5  B   SQLite 每请求 DB 降为 Medium
Maintainability ██████░░░░  5.5  B   不变
Design          ██████░░░░  6.0  B   不变
─────────────────────────────────────
Overall         ██████░░░░  6.4  B
```

### 13.5 复审后修复优先级（不变部分从略，仅列变更）

- **立即修复**：事务 recover 吞 panic；OpenAI Responses map 顺序随机。
- **立即修复**（较初版降级，但仍应尽快）：GroupList 深拷贝 Items；passthrough 重复聚合加幂等。
- **发布前修复**：SQLite 读写分离 + 路由元数据缓存；非流式整体超时；Anthropic error 重入；tool call 复合 ID；Gemini args 空对象；Analytics 旧数据保留。

> 复审结论：初版报告无证据性误判（没有把不存在的代码问题当问题），存在 3 处严重度定级偏高和 1 处影响面描述不准确，已在本节修正。

---

## 14. 修复与复查记录（2026-08-23）

### 14.1 已修复项

| # | 问题 | 修复内容 | 涉及文件 |
|---|------|----------|----------|
| 1 | 事务 recover 吞 panic | rollback 后 `panic(r)` 重新抛出，避免以零值“成功”返回 | op/channel.go, op/group.go, op/group_preset.go |
| 2 | Responses 多 tool call done 顺序随机 | 按 output_index 升序遍历 map | transformer/inbound/openai/response.go |
| 3 | GroupList 写穿缓存数组 | Items 先深拷贝再应用倍率策略 | op/group.go |
| 4 | passthrough 断连重复聚合 | 新增 `passthroughMetricsCollected atomic.Bool`，`collectPassthroughMetrics` 幂等 | relay/type.go, relay/relay.go |
| 5 | Anthropic error 重入 | error 分支增加 `messageStopped` 守卫 | transformer/inbound/anthropic/messages.go |
| 6 | tool call 按 index 合并损坏 | `(ID, Index)` 兼容匹配，ID 冲突时不合并 | transformer/model/stream_aggregator.go |
| 7 | Gemini tool args 解析失败发空 | 失败时 `args = map[string]interface{}{}` | transformer/outbound/gemini/messages.go |
| 8 | Analytics 旧数据被错误页覆盖 | 有 summary 时错误降级为顶部提示条 | web/src/components/modules/log/Analytics.tsx |
| 9 | 前端未使用变量 | 移除多余参数 | web/src/components/modules/site/index.tsx |
| 10 | 9 项死代码 | 删除无调用函数及无用 import | apperror, migrate, helper/channel, model/site, op/clash_controller, op/log, op/site_channel, server/middleware/static |
| 11 | tokenizer 错误静默 0 | 失败时告警并返回 len/4 粗估 | utils/tokenizer/tokenizer.go |
| 12 | param_override 静默忽略非法配置 | 失败时 log.Warnf | helper/param_override.go |

### 14.2 新增回归测试

- `TestMergeToolCallDeltaTreatsIndexReuseWithDifferentIDAsNewCall`：验证 index 复用但 ID 不同时生成两个独立 tool call。
- `TestTransformStreamEventsOnlyEmitsSingleErrorEvent`：验证重复 error 事件只输出一个。

### 14.3 复查结果

| 检查 | 结果 |
|------|------|
| `go test ./...` | ✅ 通过 |
| `go vet ./...` | ✅ 零告警 |
| `go test -race ./internal/op ./internal/relay ./internal/transformer/model` | ✅ 通过 |
| `pnpm lint` | ✅ 零告警 |
| `pnpm build` | ✅ 构建成功 |

### 14.4 待后续处理（需更大改动，本次未实施）

- SQLite 读写连接分离 + 路由元数据缓存（性能优化，涉及 DB 配置与缓存失效设计）。
- 非流式上游整体超时（需新增配置并处理流式/非流式超时策略）。
- 2000+ 行 Go 文件与 3000+ 行前端组件拆分（纯结构重构）。
- HeaderPolicy/倍率策略 fallback 统一与指标化。

### 14.5 变更统计

- 23 个文件变更，+143 / -109 行。
- 所有变更集中在：5 处稳定性修复、4 处协议正确性修复、2 处前端修复、9 处死代码删除、2 处可观测性增强、2 个回归测试。

### 14.6 实施后二次复查（bug 排查）

对本次全部 23 个文件变更逐项复查后，未发现新引入的 bug。要点：

- **recover re-panic**：5 处均为非命名返回值函数，`panic(r)` 后按 Go 语义向外传播，不会出现“回滚后返回成功”；无其他路径依赖原吞 panic 行为。
- **tool call 排序**：仅改变遍历顺序，不改变每条事件内容；`tc` 为 nil 的行为与改动前一致（原代码同样会解引用）。
- **GroupList 深拷贝**：`append([]model.GroupItem(nil), ...)` 对 nil slice 安全；写入只作用于新数组。
- **passthrough 幂等**：`CompareAndSwap` 在空流早退之后，语义为“只采一次”；若首次解析失败，重试同一份字节仍会失败，不会漏记有效数据。
- **Anthropic error 守卫**：`messageStopped` 在 finalize/error 后置位，重复 error 被跳过；首个 error 仍正常输出。
- **MergeToolCallDelta**：ID 冲突时 `continue` 而非合并，保持 Chat Completions 原有 index 合并行为不变；新增测试覆盖复用 index 场景。
- **Gemini 空 args**：仅影响解析失败分支；正常解析路径不变。
- **前端 Analytics**：`!summary` 且非 loading 时才显示错误页；有 summary 后错误降级为顶部条。TS 构建通过。
- **死代码删除**：逐一确认无测试/脚本/构建标签引用，`go build ./...` 通过。

最终验证：`go build ./...` ✅、`go test ./...` ✅、`go vet ./...` ✅、`go test -race ./internal/op ./internal/relay ./internal/transformer/model` ✅、`pnpm lint` ✅、`pnpm build` ✅。

---

## 15. 第二批较大改动记录（2026-08-23）

### 15.1 已实施

| 项目 | 实施内容 | 涉及文件 |
|------|----------|----------|
| 非流式上游整体超时 | 新增 `client.response_timeout_seconds` 配置（默认 120），`sendRequest` / `sendFingerprintedRequest` 对非流式请求施加 context 超时，body 关闭时释放 cancel | conf/config.go, relay/relay.go |
| SQLite 读连接池 | SQLite `MaxOpenConns/MaxIdleConns` 由 1 调整为 4，依赖 WAL + busy_timeout(5000) | db/db.go |
| 路由元数据缓存 | 新增 `enabledHeaderPolicies` TTL 3s 缓存，`ResolveHeaderPolicy` / `ResolveSiteHeaderPolicy` 从逐 scope N 次查询降为 1 次批量查询；Upsert/Delete 显式清缓存，测试 DB 初始化同步清缓存 | op/header_policy_cache.go, op/header_policy.go, op/backup_test.go |
| fallback 可观测性 | 新增 `HeaderPolicyFallbackTotal` / `MultiplierBindingFailureTotal` / `MultiplierLookupFailureTotal` 原子计数，在三个降级点累加 | op/fallback_metrics.go, op/header_policy.go, op/group_multiplier_policy.go, op/channel.go |
| relay.go 拆分 | 将 7 个协议辅助函数移入 `protocol_helpers.go`，relay.go 从 2088 行降至 1988 行 | relay/relay.go, relay/protocol_helpers.go |

### 15.2 验证结果

| 检查 | 结果 |
|------|------|
| `go test ./...` | ✅ 通过 |
| `go vet ./...` | ✅ 零告警 |
| `go test -race ./internal/op ./internal/relay ./internal/transformer/model` | ✅ 通过 |
| `pnpm lint` | ✅ 零告警 |

### 15.3 仍未实施（需更谨慎的独立设计）

- 前端 `site-channel/index.tsx`（3405 行）、`site/index.tsx`（2739 行）等组件拆分：涉及 UI 状态与交互，需要独立 PR 配合人工走查。
- 路由元数据中 binding/price/candidate 的完整缓存层：本次仅缓存了 HeaderPolicy，binding 查询仍为每请求一次，待与缓存失效策略统一设计。
- fallback 语义统一（HeaderPolicy fail-closed vs 倍率 fail-open）：本次先补了计数器，行为未改变；是否 fail-closed 需要产品确认后再改。

### 15.4 提交

- `30a7f1d` perf: add non-streaming timeout, SQLite read pool, header policy cache
- `5afefb0` fix: address audit findings (stability, protocol, dead code)
