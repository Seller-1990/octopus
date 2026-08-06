# Octopus PR-0：内网防御性编码审计清单

> 生成日期：2026-08-06
> 范围：`internal/relay/`、`internal/sitesync/`、`internal/op/`（验证/离群/备份/任务）、`internal/transformer/`、`internal/server/`（middleware/auth）、`internal/client|helper|grouphealth|globalprice|outlierwindow|update|webdav|utils|conf/`
> 方法：全量源码通读 + 全仓 grep 调用方/测试引用；判定标准：基础合同不可删（认证边界、凭据加密脱敏、CAS/事务一致性、context cancellation、真实输入校验、关键错误分类、可观测性）
> 状态：**执行中**（2026-08-06 已在 feature/slim-pr0 分支执行，见下方"执行记录"）

---

## 执行记录（2026-08-06，feature/slim-pr0）

| 项 | 结果 | 说明 |
|----|------|------|
| 删除清单 13 处 | ✅ 全部执行 | 第 7 项修正：`cache_control.go` 含 3 个函数，仅 `convertToAnthropicCacheControl`/`sanitizeCacheControlPair` 无调用；**`convertToLLMCacheControl` 有 11 处调用（messages.go），已保留**——文件重建为仅含该函数 |
| 测试改写 | ✅ | `ws_state_test.go` 两个专测 no-op 函数的测试已删除；`TestClashSwitchLeaseRejectsStaleOwnerRelease` 已删除 |
| P0 Report 开关 | ✅ | `outlierwindow` 新增 `SetReportEnabled`（atomic.Bool，默认 true）；`task/site_outlier.go` 每轮按 POR 设置同步 |
| P1-a sanitize 单层化 | ✅ | `recovery.go` 拼接后不再整体二次清洗，仅截断（两段均已单独清洗，行为等价） |
| P1-b HTML 摘要命名 | ✅ | `anyRouterExtractHTMLErrorSummary` → `extractHTMLErrorSummary`（3 处调用 + 定义同步） |
| P1-c 登录合并 | ⏸️ **评估后不执行** | 两份实现协议相同（同 `/api/user/login` 端点），但 token 提取**顺序存在语义差异**（`loginManagedSession` 先 `data.*` 后顶层，`resolveAnyRouterManagedAccessToken` 先顶层后 `data.*` + 凭据类型分派 + 会话缓存）。合并会改变管理平台登录行为，收益（约 30 行）< 回归风险，留待 P2 批次单独评估 |
| P1-d Clash 租约降级 | ✅ | 切换互斥改为纯进程内锁（单实例部署）；删除 `ClashSwitchLease` 表/类型/注册/租约函数（guard 内保留 git 历史指引注释） |
| 缺口修复 1 | ✅ | `helper/delay.go` NewRequest 错误传播 |
| 缺口修复 2 | ✅ | `helper/fetch.go` 3 处 NewRequest 错误传播；`applyDefaultModelRequestHeaders` 删除不可达的 `req==nil` 检查（加前置条件注释） |
| 缺口修复 3 | ✅ | `ws_client.go` InsecureSkipVerify 加内网信域决策注释 |
| 缺口修复 4 | ✅ | `stream/processor.go` `terminalEventFromSSE` 改用 `StreamConfig.MaxEventSize`（3 处生产调用传入 `maxSSEEventSize`，0=默认 32MB 不变） |
| 条件裁剪 6 类 | ⏸️ 待用户按部署形态确认 | /images、route_learning、CF 链、浏览器验证、POR、Clash（见第三章） |
| P2/P3 重构项 | ⏸️ 未执行 | 流实现统一、双心跳合并、首字超时统一、7 处关键字分类收敛等属重构级，逐个 PR 单独 review |

---

## 零、总体结论（先读这段）

1. **项目防御编码整体克制，不存在大面积冗余**。绝大多数防御分支（重试/退避/熔断/取消分类/协议转换防护）都有真实调用方、真实故障模式和测试证据，且大多在**冷路径**（仅失败/异常时执行），不拖累正常运行。
2. **真正的热路径冗余只有 1 处**：`outlierwindow.Report` 在"离群退役（POR）"开关**默认关闭**的情况下仍对每个请求执行环形缓冲写入（relay.go:387,395 / compact.go:288,296）——建议加原子 enabled 开关，属低风险纯优化。
3. **可立即删除的零风险死代码 13 处**（约 200 行）：无调用方、无测试、grep 实证。
4. **可简化项 14 类**：保留语义、消除重复（sanitize 最多洗 3 次、HTML 摘要 3 份、登录逻辑 2 份、7 处关键字分类、三套流实现、双心跳、三套首字超时）。
5. **条件裁剪项 6 类**（按部署形态决策，非默认删除）：/images 端点、route_learning、Cloudflare/浏览器验证链、POR 特性、Clash 控制器。
6. **审计顺带发现 3 个真实防御缺口（需修复而非删除）**：`helper/delay.go` 与 `helper/fetch.go` 共 4 处忽略 `NewRequest` 错误的 nil 解引用隐患；`ws_client.go:46` `InsecureSkipVerify: true`；`stream/processor.go:385` 硬编码 32MB 绕过 env 配置。

---

## 一、建议删除清单（零风险死代码，13 处）

| # | 位置 | 内容 | 证据 |
|---|------|------|------|
| 1 | `relay/relay.go:1720-1749` | `collectOpenAIResponsesPassthroughMetrics` | 全仓 grep 仅定义处 |
| 2 | `relay/relay.go:1897-1926` | `collectAnthropicPassthroughMetrics` | 同上 |
| 3 | `relay/relay.go:1866-1894` | `streamReachedTerminalEvent` | 被 stream/processor 的 Result 机制取代 |
| 4 | `relay/relay.go:1751-1765` | `responsesPassthroughTerminalEvents`/`anthropicPassthroughTerminalEvents` 两 map | 被 `clientSuccessTerminalEvents`+`cfg.TerminalEvents` 取代 |
| 5 | `relay/relay.go:1329-1347` | `mergeBetaHeader` | 无调用方（已内联进 outbound/anthropic） |
| 6 | `relay/ws_client.go:716-717`（+173 调用点） | `injectWSPreviousResponseID` **空函数体 no-op** | 无实际行为；需同 PR 改写 `ws_state_test.go` 3 处测试 |
| 7 | `transformer/inbound/anthropic/cache_control.go`（整文件） | `convertToAnthropicCacheControl`/`sanitizeCacheControlPair` | 零调用方零测试；outbound 有等价实现（messages.go:1543） |
| 8 | `transformer/inbound/anthropic/thinking.go:16-19` | `defaultReasoningEffortMapping` | 零调用方（在用的是 thinkingBudgetToReasoningEffort） |
| 9 | `utils/xslice/`（整个包） | `Unique`/`UniqueFunc` | 全仓零调用方 |
| 10 | `sitesync/stub.go` | `const Stub = true` | 无引用（SLIM_PLAN 已列） |
| 11 | `task/init.go` | `TaskPriceUpdate` 等 6 个未用常量（含未注册的 `TaskCleanLLM`） | 注册用的是 SettingKey 键名；确认无外部配置按名称引用后删 |
| 12 | `router/router.go:81` | `GetRouterCount` | 无生产调用 |
| 13 | `op/site_proxy_preference.go:191` | `SiteProxyPreferenceClear` | 无调用方（Clear 已内联） |

> 注：`op/group.go` 的 `GroupItemAdd/Update/Del/List`、`op/log.go:548 RelayLogList`、`op/protocol.go:183 AssessProtocolRoute`、`op/usage_analytics_breakdown.go` 两个 Get、`op/relay_log_index.go:106 RelayLogEnsureIndexesSync`、`handlers/log_analytics_export.go:134 writeUsageAnalyticsCSV` 均为**测试支撑 API**——不是零成本删除，若删须同 PR 改写测试（SLIM_PLAN 已列，此处不重复）。

> **复核记录（2026-08-06）**：本清单 13 项删除候选已逐项 grep 复核（含测试引用），全部确认"仅定义处、无生产调用"；`injectWSPreviousResponseID` 有 3 处测试引用（ws_state_test.go:45/51/302），删除时须同 PR 改写，清单已标注。

---

## 二、建议简化清单（保留功能、减冗余/热路径成本，14 类）

### 按优先级

| 优先级 | 位置 | 简化内容 | 收益 |
|--------|------|---------|------|
| P0 | `outlierwindow/window.go:106` + `relay/relay.go:387,395` + `compact.go:288,296` | `Report` 加原子 enabled 标志，POR 开关关闭时不记录 | **唯一"关闭特性仍在每请求热路径"的代码**，纯优化零语义变化 |
| P1 | `sitesync/recovery.go:597-600` + `core.go:48,54` + `sanitize.go:34-64` | sanitize 单层化：同一错误目前最多洗 3 次，改入口一次 | 注意：在**失败路径**（非热路径），收益为代码简化而非性能 |
| P1 | `sitesync/sanitize.go:206` + `http.go:444` + `anyrouter.go:1103` | HTML 摘要收敛：实际为**两套外层包装**（sanitize.go 中文语义版 `summarizeHTMLForStatus` / http.go 英文短文案版 `extractSiteHTMLResponseSummary`）+ **共享核心** `anyRouterExtractHTMLErrorSummary`（命名带 anyRouter 前缀但被全平台调用）——收敛命名与包装、保留共享核心，注意两套文案语义不同需保留 | 去命名误导与包装重复 |
| P1 | `sitesync/sync.go:346-387` vs `anyrouter.go:166-224` | 登录流程两份实现（`loginManagedSession` 服务管理平台 / `resolveAnyRouterManagedAccessToken` 服务 AnyRouter，各有真实调用方）合并 | 分属两套平台协议，合并前需确认协议差异；重构级操作，需测试护航 |
| P1 | `op/clash_controller.go:294-398` | Clash 切换 DB 租约降级为进程内互斥+确认轮询 | 有测试 `TestClashSwitchLeaseRejectsStaleOwnerRelease` 支撑，降级需同 PR 改写测试；仅限单实例部署 |
| P2 | `sitesync/sanitize.go:67-149` + `anyrouter.go:403,989` + `sub2api_auth.go:174` + `recovery.go:734,775` + `sub2api_schedule.go:155` | 7 处重复关键字分类（unauthorized/forbidden/未登录/过期）收敛为统一分类函数族 | 去重复、保语义差异 |
| P2 | `relay/relay.go:1384-1536` + `images.go:1053-1194` | 三套流式读取实现（HTTP V2 / passthrough V2 / images proxySSE）统一 | 重构级，需测试护航 |
| P2 | `heartbeat.go` + `stream/processor.go` | 两套心跳 ticker 合并（时序交接已是正确设计，仅实现重复） | 减一个常驻协程/请求 |
| P2 | `first_token_timeout.go` + `processor` + `images.go:1113` | 三套首字超时实现统一为 ctx budget + processor 双层 | 需保留"dial 阶段也受约束"语义 |
| P3 | `sitesync/anyrouter.go:1222-1285` | 删除 gob 二进制指纹 userID 提取（无测试，JWT/self/正则兜底） | 减死路径 |
| P3 | `sitesync/anyrouter.go:1128-1136` | 删除 HTML `<span>Error</span>NNN` 精确错误码匹配（title 提取已够） | 减死路径 |
| P3 | `sitesync/route_probe.go:210-218` | 删除路由探测的匿名请求尝试（管理平台 API 匿名可读≈0） | 减无谓请求 |
| P3 | `sitesync/anyrouter.go:775` | userID 探测硬编码列表 18 个候选缩减到 5 个 | 减请求数 |
| P3 | `server/middleware/cors.go:41-47` | 删除剥 scheme 的 host-only 匹配（保留 空=禁止/`*`/精确匹配） | 每请求少一段逻辑 |

### 低优先级（可选）

- `sitesync/balance.go:76-98`：余额 attempt 复用 discover 结果（3→1 次）
- `sitesync/anyrouter.go:684-699`：厂商 user-id header 7 个→2-3 个主流
- `sitesync/balance.go:143-198`：今日收入日志回退页数 6→2（当日收入在前 1-2 页）
- `sitesync/http.go:842-848`：每请求新建 cookiejar → 复用/懒加载
- `sitesync/recovery.go:205-229`：成功路径每次写 2 行 DB → 失败全量/成功采样（**需产品确认：放弃成功审计？**）
- `op/backup_zip_import.go:118-213` + `backup_zip_stream_import.go:539`：ZIP 中央目录手工扫描与 JSON token 预算**二选一保留**（内网自产备份场景；保留大小/条目/白名单限额）
- `server/auth/auth.go:31-38`：JWT secret 的 rand 失败降级（username+password 当密钥）改为直接返回错误——降级分支本身削弱认证边界
- `webdav/backup.go:87-93` + `client.go:76-86`：两个"从未触发"的失败降级分支改错误传播
- `grouphealth/probe.go:43-46`（timeout≤0 兜底）、`probe.go:130-137`（随机提示词池改固定单条）、`utils/cache/cache.go:25-27`（shards≤0 兜底）、`helper/param_override.go:13-16`（多重 nil 检查收敛）、`transformer/model/schema.go` 3 处不可达 nil 防护

---

## 三、条件裁剪清单（按部署形态决策，6 类）

| # | 特性 | 当前状态 | 判断条件 | 若裁剪涉及 |
|---|------|---------|---------|-----------|
| 1 | `/images/*` 图片端点 + `bodycache/` | 完整实现，无测试 | 内网客户端是否调用 images 端点？不用则整组删除（bodycache 仅此一个消费者） | `relay/images.go`、`relay/bodycache/`、`handlers/images.go`、前端无 UI（纯 API） |
| 2 | `route_learning.go`（错误信息学习路由） | 完整实现，无测试 | 是否使用 site-channel 绑定（站点镜像渠道）？无绑定则 `binding==nil` 早退**从未触发** | 整文件 `relay/route_learning.go` |
| 3 | Cloudflare 检测链（`errors.go` + `http.go:377-412` 的 IsCloudflareProtectionResponse + `anyrouter.go` acw_sc__v2 盾破解） | 完整实现，有测试（fixture） | 内网上游是否可能返回 CF/阿里云 WAF 响应？确认无则整链可下线 | `sitesync/errors.go`、`http.go` CF 判定、`anyrouter.go:1287-1506` |
| 4 | 浏览器验证链（`verification_browser*` + `verification_retry*` + task 1min 扫描） | 端到端接线，事件驱动 | 是否部署外部浏览器客户端做人工验证？无则**从未触发**（配对永不存在） | `op/verification_browser*.go`、`verification_retry.go`、`sitesync/verification_*.go`、`handlers/site_recovery.go` 部分、前端 `VerificationPairingPanel.tsx`、5 个测试 |
| 5 | POR 离群退役（`site_outlier` + `outlierwindow` + task 2min） | 默认关闭（开关 false），有 7 个测试 | 是否启用自动退役？不启用则建议**保留开关化设计**，仅执行 P0 的 Report 开关优化 | （不删代码，只优化热路径） |
| 6 | Clash 控制器（`clash_controller.go` + 前端 panel） | 未配置即休眠 | 是否使用 Clash 节点切换？不用则不配置即可，零成本；删除属特性移除 | `op/clash_controller.go`、`handlers/site_recovery.go` Clash 部分、前端 `ClashControllerPanel.tsx` |

> **决策建议**：1-4 项若内网确认不用，可随功能 PR 移除（范围清晰、测试可删）；若只是"不配置"，现状零流量零成本（除 P0 外），保持即可。**不建议**为删除而删除——条件裁剪项都有真实功能价值，只是内网未必触发。

---

## 四、必须保留清单（基础合同，删除风险高）

| 类别 | 位置 |
|------|------|
| 认证/权限/配额边界 | `server/middleware/auth.go` 全部（sk- 预检、禁用/过期/成本/RPM 检查）；`server/auth/auth.go` 15min 过期与 48 字符 APIKey；bridge 的 pairing/task/request token 作用域校验（`op/verification_bridge.go:436-459`） |
| 凭据加密与脱敏 | `op/secret.go` 全部（AES-GCM + JWT 派生 + 跨库重加密）；`sitesync/sanitize.go:15-25,233-249`（脱敏核心正则+截断，**只简化重复层不删核心**）；`relay/type.go:33-65` hopByHop 过滤（删=凭据/内网信息泄漏到上游）；备份导出清空验证凭据（`op/backup.go:650-660,1366-1430`） |
| CAS/事务一致性 | `sub2api_auth.go:253-268` credential_revision CAS；`sitesync/storage.go` persistSyncSnapshot 全量事务；`op/catalog_provision*.go` 供给事务；`op/backup*.go` 导入分批+幂等确定性 ID 重映射 |
| context cancellation/快速失败 | `client/http.go` 克隆 transport 5s Dial/TLS 超时（熔断前提）；`task/*.go` 各任务 ctx 超时；`relay/retry.go` 全部（重试判定/Retry-After/60s 封顶退避）；`cancel.go` 全部（熔断/统计归因） |
| 热路径 panic 隔离与并发正确性 | `utils/safe/`（10+ 处 safe.Go）；`outlierwindow/window.go` Report recover（保留，仅加 enabled 开关）；`globalprice/provider.go` RWMutex；`utils/cache` 分片锁；`client/http.go` 双重检查锁 |
| 协议转换正确性 | `transformer/model/alternation.go` EnforceAlternation；`compat/tool_calls.go` FixOrphanedToolCalls；`compat/gemini_signature_cache.go`；`schema.go` ErrSchemaLossy 降级；`outbound/gemini/budget.go` 家族钳制；outbound 白名单结构体 |
| 真实输入校验 | `transformer/inbound/anthropic/messages.go`（MessageContent 三形态/null 拒绝）；`relay/compact.go:44-121`；`op/usage_analytics.go:113-147`；`op/group_auto_group.go:74-130`；`task/settings.go` 间隔值域；`middleware/validate.go` RequireJSON |
| 背压/不丢数据 | `op/usage_facts.go:47-68`（队列 5000+同步刷盘，计费事实）；`op/stats.go:41-76`（跨日重试队列） |
| 可观测性 | `middleware/logger.go`；`relay/metrics.go:232-240`（usage 降级）+ `593-640`（日志过滤图片/音频） |
| 资源边界/运维 | `op/relay_log_index.go`（异步建索引防 OOM，有真实事故背景）；`webdav/backup.go` 128MB+retention；`update/` zip slip；`shutdown/` 优雅退出；`utils/tokenizer`（计费估算）；`conf/` 默认值 |

---

## 五、防御缺口修复清单（审计发现，需修复而非删除）

| # | 位置 | 问题 | 修复 |
|---|------|------|------|
| 1 | `helper/delay.go:11-13` | `GetUrlDelay` 忽略 `NewRequestWithContext` 错误后直接 `req.Header.Set` → 非法 URL 时 **nil 解引用 panic** | 改为错误传播 |
| 2 | `helper/fetch.go:58-64, 90, 138` | 三个 fetch 函数同样忽略 NewRequest 错误后设 Header/Authorization | 改为错误传播；随后可删 `fetch.go:180-187` 的 `req==nil` 检查 |
| 3 | `relay/ws_client.go:46` | `InsecureSkipVerify: true`（跨域校验缺失） | 内网纯信域可保留但应显式注释；否则改为校验 Origin |
| 4 | `stream/processor.go:385` | `terminalEventFromSSE` 硬编码 32MB，绕过 `maxSSEEventSize` env 覆盖 | 改用配置值（一致性修复） |
| 5 | `update/update.go` | zip slip 防护无测试用例 | 补越界路径测试（防护本身保留） |
| 6 | `op/relay_log_index.go` 相关测试 | `isRetryableStatus`/`isPassthroughStatus`/`computeBackoff`/`balancer` 四种策略边界无单元测试 | 保留代码，补测试（可选） |

---

## 六、建议执行顺序

| 步骤 | 内容 | 验证 |
|------|------|------|
| 1 | 删除清单 13 处死代码（+ 测试改写：ws_state_test 3 处） | `go test ./...` |
| 2 | P0：outlierwindow.Report 加 enabled 开关 | `go test ./internal/outlierwindow ./internal/task/...` |
| 3 | P1：sanitize 单层化 / HTML 摘要三合一 / 登录合并 / Clash 租约降级 | 对应包测试 |
| 4 | 缺口修复清单 1-4（nil 解引用×4、InsecureSkipVerify 注释、32MB 一致性） | `go test ./...` + review |
| 5 | 条件裁剪 1-6：**等用户按部署形态逐项确认后**单独 PR | 每项独立提交+测试 |
| 6 | P2/P3 简化（流实现统一、双心跳、首字超时、关键字分类收敛等） | 重构级，逐个 PR 逐个 review |

> 原则：每个删除/简化项单独成 PR；测试改写与删除同 PR；每步 `go test ./...` + `go vet` 通过后才进入下一步。
