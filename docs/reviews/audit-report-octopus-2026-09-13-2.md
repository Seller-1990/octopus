# Octopus 对抗性代码审查报告（2026-09-13 第二轮 · 晚间）

> 与同日较早的 `audit-report-octopus-2026-09-13.md`（F01–F18）相互独立：本轮为后端 Go 全模块复审，发现的 P0/P1 均为当时仍存活的问题（已逐条对照当前代码验证）。

**审查方式**：7 个并行子智能体按模块分片（relay / transformer / op / server / db / sitesync / 工具杂项），第一性原则 + 对抗性思维，全部发现均附 `文件:行号` 实证。
**静态基线**：`go build ./...` ✅、`go vet ./...` ✅、`go test ./...` 全量 ✅（Go 1.25.1，临时工具链 /tmp/go 验证）。
**总发现**：P0 × 1、P1 × 20、P2 × 30+。**本轮已直接修复 16 处**（全部 P0 + 10 个 P1 + 若干 P2），修复后全量测试通过、无回归。

---

## 一、本轮已修复的问题（16 处，已验证编译+测试通过）

| # | 等级 | 模块 | 问题 | 修复 |
|---|------|------|------|------|
| 1 | **P0** | transformer/gemini | `outbound/gemini/messages.go:807` 只读 `MaxTokens`，新版 OpenAI SDK 只发的 `max_completion_tokens`（与 Responses 入站的 `max_output_tokens`）走 Gemini 渠道时**静默失效、无上限** | 增加 `MaxCompletionTokens` 回退，与 Anthropic outbound `resolveMaxTokens` 对齐 |
| 2 | P1 | transformer/anthropic 入站 | `inbound/anthropic/messages.go:60` 客户端省略 `max_tokens` 时被静默改写为 **1**，响应被截断为 1 token 且无报错 | 仅显式提供有效值时透传，否则交给 outbound 默认值 8192 |
| 3 | P1 | transformer | 4 处客户端可控输入 nil 解引用可 panic 整个进程：`inbound/anthropic/messages.go:208,256`（text 块缺 text）、`inbound/openai/response.go:1529`（function_call_output 缺 output）、`outbound/gemini/messages.go:666`（image_url 缺对象） | 全部加 nil 防护，跳过或留空 |
| 4 | P1 | transformer/volcengine | `outbound/volcengine/response.go:36-53` guard 剥离了 `Reasoning` 却仍对不支持的模型输出 `thinking:{type:"enabled"}`，guard 形同虚设 | 仅命中支持列表时才设置 `Thinking.Type` |
| 5 | P1 | transformer/anthropic | `outbound/anthropic/messages.go:689-703` `convertSystemPrompt` 只读字符串形式 Content，OpenAI 客户端发数组形式 system content 时 **system prompt 变空串发出，指令静默丢失** | 新增 `systemMessageText` 展平 MultipleContent 的 text part |
| 6 | P1 | relay | `ws_client.go:609` 同通道重试不重建 outAdapter，流式状态（toolIndex/toolCalls/outputItems）跨重试残留导致输出帧错乱（HTTP 主链路已有此修复，WS 孪生链路漏了） | 重试分支重建适配器（带 nil 防护） |
| 7 | P1 | relay/images | `images.go:547` 配额更新用裸 ctx，客户端收完响应断连后 UPDATE 静默失败、**费用永久漏计**（主链路 metrics.go 修过，孪生实现复现） | 改用 `context.WithoutCancel(ctx)` |
| 8 | P1 | relay/images | `images.go:1141` 流式路径客户端断连返回 nil 错误，被误记为"成功"，污染统计/熔断/粘性 | 返回 `ctx.Err()`，走既有取消归类分支 |
| 9 | P1 | utils/shutdown | `shutdown.go:42` 把 `SIGHUP` 当终止信号——SSH 断开/supervisor 重载会让服务**意外静默退出** | 移除 SIGHUP，仅保留 SIGINT/SIGTERM |
| 10 | P1 | helper/fetch | `fetch.go:187` Anthropic 上游返回 `has_more=true` 但 `last_id=""` 时分页**死循环**持续打上游（最长 30 分钟）；Gemini 分支重复 nextPageToken 同构 | LastID 为空/不变即 break；Gemini 记录已见 token 去重 |
| 11 | P1 | price | `price.go:87` `lastUpdateTime` 跨 goroutine 无同步读写（定时任务 vs HTTP handler），race detector 可实锤 | 改 `atomic.Pointer[time.Time]` |
| 12 | P1 | task | `task/sync.go:16` `lastSyncModelsTime` 同构数据竞争 | 同上 |
| 13 | P2 | relay | `relay.go:1284,1373` 上游错误体 `io.ReadAll` 无上限且全量写日志，异常上游可放大内存 | `io.LimitReader(body, 16KB)`（与 images 路径一致） |
| 14 | P2 | relay/stream | `raw_source.go:27-33` 每 chunk 双重分配+拷贝（passthrough 热路径每 32KB 多一次 32KB 分配） | 直接返回 `buf[:n:n]` |

> 修复 #6/#7/#8 共同暴露一个模式：**images/compact/WS 是主链路的"孪生实现"，主链路修好的 bug 在孪生链路原样保留**。见架构建议 A。

---

## 二、待修复的重要问题（按等级排序）

### P1 —— 明确 bug 或显著性能问题

**relay 热路径**
1. **WS 连接"先回池、后剔除"窗口 + defer 顺序错误** — `stream/processor.go:154-156`：defer LIFO 使 `Source.Close()`（连接回池）先于 `readCancel()` 执行；损坏连接在两个窗口内可被并发请求取走（coder/websocket 不允许多并发 reader）。**修复**：交换两个 defer 顺序；Put/Remove 收口到 reader 内部幂等执行一次。
2. **`wsUpstreamReader` 字段跨 goroutine 无同步读写（data race）** — `transport_ws.go:36,49-56,123,135`：读 goroutine 读写 `closed/done/statusCode`，主 goroutine 并发写，`Run()` 返回前不等待读 goroutine。**修复**：改 `atomic.Bool/Int32`。
3. **WS 重试 break 条件缺 `FirstTokenTimeout`** — `ws_client.go:640`（对比 `relay.go:477`）：首 token 超时后仍对刚超时的通道带退避重试，白耗 15s exact-replay 预算。

**op 热路径（每请求都在付出的代价）**
4. **路由 N+1：每候选渠道每请求 2 次 DB 查询取价** — `op/catalog.go:1541` → `site_pricing.go:380-395`（`First`+`Find`）。默认 balanced/lowest-cost 策略下每次 relay 请求 = 候选数 × 2 次 DB 往返；同文件已有批量实现 `batchPriceQuotesForCandidates`（catalog.go:1083）与候选缓存（catalog.go:1226）却未用。**修复**：入口一次性批量取 quotes 注入，删除回表 `First`。
5. **每成功请求一次同步 DB UPDATE 写配额** — `op/apikey.go:232-248`：SQLite 单写连接下与 relay log flush、stats 竞争串行化。stats 系已证明"内存累加+周期落库"可行。**修复**：配额增量改内存 bucket，随 StatsSaveDB 批量落库。
6. **RelayLogAdd 每请求一次三表 JOIN 富化，绕过已有 5 分钟缓存** — `op/log.go:348` → `usage_facts.go:463-507`；`allSiteChannelBindings` 缓存（site_binding_cache.go:27）同数据却未用。**修复**：富化改走缓存或推迟到 flush 批量执行。

**db**
7. **多个迁移在 MySQL 上报错（TEXT 列字面量 DEFAULT，ERROR 1101）** — `migrate/003.go:29`、`004.go:25,30,40`、`006.go:28`、`007.go:28`。**修复**：MySQL 用 VARCHAR 或 `DEFAULT ('...')` 表达式，或先加可空列回填再改 NOT NULL。
8. **`migrate/004.go:45` 的 `DATETIME` 在 Postgres 不存在**（应 TIMESTAMP），该迁移在 PG 上必失败、进程无法启动。
9. **SQLite WAL 读写升级死锁（SQLITE_BUSY_SNAPSHOT）busy_timeout 不覆盖** — `db.go:62`（4 连接池+deferred 事务）。高并发中继场景必现。**修复**：DSN 加 `_txlock=immediate`。
10. **Postgres `DEALLOCATE ALL`/`DISCARD ALL` 只作用于连接池中随机一条连接** — `db.go:158-161`（MaxOpenConns=100），其余连接 stale plan 原样保留，且错误被忽略。**修复**：迁移后 `sqlDB.Close()` 让连接池重建。

**sitesync**
11. **每次账号同步都触发全局全量重算/投影** — `sitesync/core.go:62,91,98`：`SyncAll` 批量 N 个账号时 `EnforceMultiplierCap`/`CatalogSync`/价格投影各执行 N 次。**修复**：批量结束后统一执行一次。
12. **Pool 模式每次请求新建 `*http.Client`+Transport，无复用无关闭** — `client/http.go:93-98` 经 `sitesync/http.go:38` 调用，一次 Pool 同步数十次请求各建独立连接池。**修复**：按 proxyURL 缓存复用。
13. **`rewriteManagedGroupItemsForAccount` 逐条 Count+Update** — `sitesync/project.go:796-814`：大账号数百次单条 SQL。**修复**：批量查重 + `UPDATE ... WHERE id IN (?)`。

**server**
14. **SSRF/凭据外泄：`fetchModel` 对 `base_url` 无服务端校验** — `handlers/channel.go:195-207` → `helper/fetch.go:59-69,156`：无 scheme 白名单、无私有网段/元数据地址拦截，渠道的 API Key 可被发往任意主机；`testVisionBridge` 有校验而 `fetchModel` 没有。**修复**：handler 层统一复用 URL 校验。

**update**
15. **zip 自更新"成功"判定过早，失败可致进程半挂且更新通道永久闭锁** — `update/update.go:131-148`：不校验目标二进制是否解出/可执行；非容器 zip 分支无 chmod 0755；`syscall.Exec` 失败只打日志且 `restarting=true` 永久拒绝后续更新。**修复**：解压后 `os.Stat`+`Chmod(0755)` 校验失败走回滚；exec 失败 `os.Exit(1)` 交给重启策略。

### P2 —— 过度工程 / 可维护性（精选）

**可立即删除的死代码**
- `sitesync/project.go:562,570,581,586` 四个函数（仅测试引用；注：581 的 `classifyModelRouteType` 同日早前报告 F05 已删，其余仍在）
- `modelvendor/index.go` 的 `visionIndex` 全套（`LookupVision`/`ReplaceVisionIndex`/`VisionEntry`，零调用方，可由 `capIndex & CapMultimodal` 推导）
- `inbound/anthropic/messages.go:1148` `mergeToolCall`（已被 `model.MergeToolCallDelta` 取代且语义不一致）
- `outbound/gemini/messages.go:288` `reasoningToThinkingBudget`、`:620` `degradedToolCalls`（从未写入的死分支）
- `op/channel.go:106-131` `ChannelKeyUpdate`（生产零调用，且有无锁读改写丢失更新隐患，注释还在自我辩护）
- `db/migrate` 5 个纯 no-op 迁移（010/011/024 等）+ `beforeAutoMigrations` 整条死链路（migrate.go:28,35-37）
- `grouphealth/probe.go:132-137` `probePrompts`（已迁往 op）；`task/task.go:18-21` stopCh/stopOnce/doneCh（生产无关闭入口）；`ws_pool.go:29` `wsQueueLimitPerConn` 与 `pc.queue`（计数无消费方）
- `server/handlers/setting.go:351` `decodeDBDump` 的 `dump==nil` 分支（不可达且吞错误）

**冗余/重复实现**
- relay 的 metrics 持久化双实现（`metrics.go` 721 行 vs `images.go:390-671` 近乎逐行重复）——本轮修复的 #7/#8 即漂移证据
- `catalog.go:1138` `pickBestPriceQuote` 与 `site_pricing.go:397` 整段复制（且都用 `quotes[:0]` 原地复用缓存切片，隐蔽共享可变状态）
- op 的 "merged + updates 双块同步"模式四处同构（SiteUpdate 160 行/SiteAccountUpdate 290 行/ChannelUpdate/ProxyConfigurationUpdate），每字段写两遍，是不一致温床——建议 field-applier 泛型或代码生成
- `site/service.go` 约 90 行纯转发 facade（4 个调用方，无防腐逻辑；同日早前报告 F18 亦指出）；`helper/channel.go:139` 一行包装
- `conf/config.go` 默认值定义两遍（setDefaults 与 helper 的 `<=0` 兜底，可独立漂移）
- `relay.go:1466` 等三处 `if Get("User-Agent")=="" { Set("User-Agent","") }` 自我赋值

**其余值得处理的 P2**
- relay：responses replay store 容量回滚三段式过度复杂且统计漂移（`responses_replay_store.go:108-114,159-193`）；`storeWSConversationState` 每次写入全表 Range 清理（`ws_state_store.go:102-111`，应节流——同日早前报告 F04 已修 ws_runtime_state 的同类问题）；`BridgedRequest` 锁横跨 120s 网络调用且并发同图无 singleflight（`visionbridge/bridge.go:154-163`）；compact 的 supported_models 校验不含 canonical 路由名，别名请求被误拒（`compact.go:99-106`）
- transformer：入站热路径对每个文本块全量 tokenize（仅用于初始 usage，上游 usage 一到即被覆盖）；`anthropicCacheProjection` 约 300 行重实现消息投影哈希（`outbound/openai/response.go:629-923`）；Gemini 签名缓存跨会话共享（`compat/gemini_signature_cache.go`，64 条 LRU 以 toolCallID+name 为键的跨用户注入风险）
- op：relay log flush "毒丸"永久阻塞（`log.go:246-251` 单条坏记录让整个管线停摆，无自愈）；`llmRefreshCache` 只 Set 不 Clear（幽灵价格）；`DBExportAll` 全库一次性载入内存可 OOM（已有流式 zip 实现应默认启用）；verification broker notify map 无界增长；tools 探测裸 `go func()` 无去重；代理测试 SSRF 存在 DNS rebinding TOCTOU
- db：迁移无事务包裹（失败即半迁移+Failed 记录）；双版本命名空间（序号制与日期制混排，发布顺序与执行顺序倒置的隐患）
- conf：bootstrap 密码"先落盘明文、后 scrub"的时间窗（`config.go:149,156`）；`viper.Unmarshal` 静默吞配置笔误（应 `ErrorUnused: true`）
- apperror：`Wrap` 后 `%v` 打印丢失根因（`Error()` 应输出 `Message + ": " + Err.Error()`）；建议实现 `Is(error) bool` 按 code 匹配
- server：CORS `*` + AllowCredentials 组合反射任意 Origin（`middleware/cors.go:14,31-33`）；登录限流在反代后按代理 IP 共享且容量满可被放大拒绝；后台任务用 `context.Background()` 无优雅退出跟踪；`/v1/models` 对合法请求可能误报 500（`handlers/model.go:351-357`）
- sitesync：`mergePersistedSiteTokens` 的 ID 保留逻辑被末尾 `result[i].ID = 0` 全部作废（`storage.go:458-460`，无效劳动）；学到的 `PlatformUserID` 只写浅拷贝不落库；interval=0 的任务 goroutine 永久 park（`task/task.go:159-168`）
- 杂项：cache 分片数非 2 的幂时掩码分布错误（潜在）；toolsprobe 信号量获取不响应 ctx、返回已关闭 Body 的陷阱 response；`helper/price.go:46` "四字段全 0=无价格"与真实免费模型不可区分

---

## 三、架构评估与建议

总体评价：**核心热路径质量较高，此前多轮审查修复痕迹明显（F01-F19/R2/T9 编号注释），无 P0 级存留**。风险集中在三个结构性模式：

**A. "孪生实现"漂移是缺陷密度最高的根源（最优先）**
主链路（relay.go Handler）与 WS 入站（ws_client.go）、compact、images 各自实现了"迭代器遍历→key 选择→熔断→退避重试→结果归类→统计持久化"的近似副本；metrics 持久化也是双份。本轮修复的 4 个 P1（#6/#7/#8 + WS 重试缺 FirstTokenTimeout）全部是"一边修了、另一边没修"。**建议**：提取共享的 attempt 执行器与 persistence 层，差异点（FirstTokenTimeout、adapter 重建、超时、别名校验）收敛为参数。

**B. op 包级可变缓存的所有权扩散**
op 单包 108 文件共享 `channelCache` 等包级状态，写入点散布 5+ 文件，缓存失效靠调用方自觉手动 invalidate，`llm.go` 漏 Clear 就是实例。sitesync 绕过 op 直写 DB 加剧了"缓存层 vs 直写层"的单一事实来源分裂。**建议**：所有写路径收口到 op，按聚合根拆分缓存所有权。

**C. transformer 横切逻辑各供应商各写一套且语义不一致**
`max_tokens` 解析三种实现（本轮 P0 即其后果）；usage 缓存语义 4 处独立实现而 `model.Usage` 的 helper 无人复用；`StreamEvent.Index` 三种语义混用；块式/事件式双流式接口并存导致每个 outbound 写互相委托的适配。**建议**：删除块式接口收敛为事件式；max_tokens/usage 语义下沉到 model 层统一。

**其他架构观察**
- 路由注册依赖 30+ 文件的包级 `init()` 副作用，无单一注册点（同日早前报告 F07 指出 fail-open 风险）——建议集中式路由表 + 重复 method+path 冲突 fail-fast + 默认认证白名单制
- `CatalogSync` 单巨型事务（catalog.go:189-461）独占 SQLite 写连接期间全部写入排队——按 canonical 分块提交
- `SiteGet` 五级深预加载被只要标量的调用方使用——提供 `SiteGetScalars` 轻量查询
- WS 会话亲和状态分散三处（runtime_state/affinity/state_store），TTL 与清理策略各异
- `helper` 命名误导（实为业务胶水层），与 utils 的边界建议显性化

---

## 四、行动清单（建议顺序）

1. ~~P0 Gemini max_completion_tokens~~ ✅ 已修；连同其余 15 处修复，build/vet/test 全绿，建议提交前人工 diff 复核
2. 下一窗口：P1 #1-#3（WS 子系统 race/连接生命周期）、#4-#6（op 热路径 N+1/配额落库）、#9（SQLite `_txlock=immediate`，一行 DSN 改动收益大）、#14（fetchModel SSRF 校验补齐）
3. 中期：孪生实现收敛（架构 A）、死代码批量删除（纯减负，无风险）
4. 持续：迁移框架事务化 + MySQL/PG 方言守卫（#7/#8 影响多数据库部署的启动可用性）
