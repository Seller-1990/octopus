# Octopus 项目瘦身计划（Slimming Plan）

> 生成日期：2026-08-06
> 依据：当前 `dev` 源码静态调研（前端 7 个主页面 + API Key 模式；静态统计 179 条 `NewRoute` 路由、13 个后台任务注册项；PR-0 已删除无调用的 `GetRouterCount()`，运行时以实际注册结果和启动验证为准）
> 状态：**PR-0 已在 PR #2 执行；后续 PR 待确认**

---

## 一、总体结论

项目功能全部为**完整实现**（未发现仅 UI 无逻辑的占位组件），但存在四类可瘦身空间：

1. **功能层合并/删除**（需产品决策）：使用分析并入 Home、删除旧版全局价格体系、Header 策略简化为全局开关、模型目录 UI 精简
2. **静态清理候选**：前后端仍有多处低使用或无生产调用的定义；其中包含测试支撑 API，不能整体当作“零风险死代码”
3. **重复实现重构**（不删功能减代码）：多处公共函数副本、巨型组件拆分等
4. **疑似缺口/兼容项复核**：5 处，其中 API Key 登录与 Verification Bridge 已确认不是“空实现/无认证”

### 内网部署裁剪原则

本项目主要部署在内网。瘦身应优先移除没有真实调用方、没有可复现故障模式、或只是重复表达同一约束的代码；不要为了假设中的公网攻击面保留多层重复防御，也不要把“理论上可能发生”当作删除或保留的唯一依据。

以下逻辑仍属于基础合同，不能因为内网而删除：

- 认证与权限边界，以及 Verification Bridge 的 pairing/task/request token 作用域校验
- Cookie/token 加密、日志和备份脱敏
- credential revision 的 CAS/fence、事务一致性和并发覆盖保护
- context cancellation、真实业务输入校验、关键错误分类和可观测性

以下内容进入优先审查范围：仅为公网暴露准备且本部署未启用的兼容路径；多层重复的 nil/empty/invalid fallback；没有真实调用方的 HTTP 重试和泛化 fallback；AnyRouter、Cloudflare、浏览器验证等未启用路径；同一错误在多层重复 sanitize/翻译。每个防御分支都应注明对应故障模式、真实调用方和测试证据；缺乏证据的分支进入候选清理，但删除前必须确认不会削弱上述基础合同。

---

## 二、四项重点决策的研究结论与建议

### 2.1 使用分析（Log 分析页）合并到 Home 仪表盘 —— **建议：合并，方案可行**

**现状对比（两套数据源，双轨统计）：**

| 维度 | Home 仪表盘 | 使用分析（log analytics） |
|------|------------|--------------------------|
| 数据源 | 内存统计（`op/stats.go`，StatsDay/Hour/Total） | 事实表（UsageRequestFact/AttemptFact → UsageAggregate，`task/usage.go` 10min 聚合） |
| 实时性 | 10s 级（relay/metrics.go 直接记录） | 10min 级（任务聚合） |
| 指标 | 总成本、请求数、Token、等待时间 | 请求/成功率/Token 明细/成本/平均与 P95 延迟/平均与 P95 FTUT/取消数 |
| 独有能力 | 活动热力图、渠道排行、分组健康总览 | 维度下钻（7 维）、scope（request/attempt）、CSV 导出 |

**重叠**：成本、请求数、Token、耗时（home 只有均值，analytics 有 P95）——**analytics 严格覆盖 home 的图表指标并更精细**。

**合并可行性（已验证）：**
- `Analytics.tsx` 是独立组件，其查询 hooks（`useUsageAnalyticsSummary/Timeseries/Breakdown`）无页面依赖
- 全部状态在全局持久化 store（`log/analytics-store.ts`），跨页共享天然成立
- 下钻联动（分析行点击 → 写 filters → 切明细）依赖全局 store，合并后改为**跨页跳转到 Log 页明细**即可（复用 `stores/jump` 机制）
- 需要一并迁移的控件：scope/dimension 下拉、日期预设、时区（目前在 `log/Controls.tsx`）

**建议方案：**
1. Home 页新增"使用分析"区块（独立 tab 或卡片，默认展示概览图表，分析能力按需展开）
2. Log 页移除 analytics tab，只保留：明细列表 + 筛选 + 历史修复工具
3. 分析下钻 → 跳转 Log 页并自动应用筛选（store 已持久化）
4. 长期可选：统一两套统计口径（stats 与 usage 双轨并存是数据层的历史包袱，但**不在本次瘦身范围**，合并 UI 不要求合并数据管线）

**风险**：中。注意 logDateRange 目前存在 toolbar 的 view-options-store，迁移时需保持两个 store 的日期同步或统一收纳，并验证下钻跳转和明细筛选的一致性。

---

### 2.2 删除旧版全局价格 Tab（model 页 global-prices）—— **建议：删除，影响面已确认**

**现状**：model 页 4 个 tab 中 global-prices 走旧 `LLMInfo` 体系（`LegacyPrices/Item/ItemOverlays/Create`），与新 Catalog/定价体系功能重叠，形成"模型价格在两处维护"的割裂。

**影响面（已逐一核实）：**

| 层 | 可删除 | 必须保留 |
|----|--------|---------|
| 前端组件 | `LegacyPrices.tsx`、`Item.tsx`、`ItemOverlays.tsx`、`Create.tsx` | `vendor-options.ts`（仍被 `VendorBadge.tsx`、`DiscoveryToolbar.tsx` 使用） |
| 前端 hooks | `useModelList`/`useUpdateModel`/`useCreateModel`/`useDeleteModel` | `useUpdateModelPrice`/`useLastUpdateTime`（设置页同步任务卡片在用）、`useModelChannelList`（分组编辑器在用） |
| 后端路由 | `/model/create`、`/model/update`、`/model/delete`；`/model/list` 需在移除旧 UI 并处理 Model 页预取后再废弃 | `/model/channel`、`/model/update-price`、`/model/last-update-time`、`/model/catalog/*` 全部 |
| 后端数据 | LLMInfo CRUD 写入路径（`op/llm.go` 的 Create/Update/Delete） | **LLM 价格表本身保留**：`price.GetLLMPrice` 运行时兜底（DB → globalprice），`task/channel.go` 价格同步任务持续写入 |

**建议方案：**
1. 前端删除旧 Tab 整链及其专用 hooks；保留 `vendor-options.ts`
2. 处理 `web/src/components/app.tsx` 进入 Model 页时对 `/model/list` 的后台预取，再评估是否废弃该路由
3. 后端先废弃并观测 `/model/create`、`/model/update`、`/model/delete` 的外部消费者，再删除路由与对应 op 函数；保留价格表读取和任务写入
4. model 页剩 3 个 tab：目录 / 发现 / Header 策略（若 2.4 落地则 Header 策略也删，剩 2 个 tab）

**风险**：中。前端入口移除相对可控，但 `/model/list` 仍有 Model 页预取，后端 CRUD 还可能有仓库外调用方；必须先关闭 UI 入口、处理预取并完成消费者审计后再废弃 API。价格表和同步写入链路继续保留。

---

### 2.3 模型目录功能精简 —— **建议：删"能力矩阵"、折叠高级编辑，不删核心功能**

**逐项评估（候选编辑字段均对应真实路由行为，删不得）：**

| 功能 | 评估 | 建议 |
|------|------|------|
| 目录列表/详情（别名、候选编辑） | 核心（规范化模型解析与路由规划） | 保留 |
| 候选编辑：status / protocol_policy | 核心（路由参与、协议控制） | 保留 |
| 候选编辑：priority / weight / lossy 三态 | 真实路由逻辑（排序/降级开关），但**日常少改** | **折叠进"高级"区**（默认收起），减少主表单复杂度 |
| Discovery（发现/批量建组/映射） | 核心（上游模型自动接入） | 保留 |
| RouteTools 路由预览 | 实用（模拟路由排查问题） | 保留 |
| **RouteTools 能力矩阵**（CapabilityMatrixDialog） | 协议兼容性开发/排障工具；当前仓库内是 `/protocol/capabilities` 的唯一生产消费链路 | **可删除 UI**；后端 API 需先确认无外部消费者或走废弃周期 |
| PricingPanel 生效价链路/手动报价/汇率 | 定价核心 | 保留 |
| 目录同步按钮 | 核心 | 保留 |

**附带复核**：`catalog-options.ts` 的 `INBOUND_PROTOCOLS` 只有 4 项，而 `ProtocolName` 类型有 7 项；当前明确缺少 `gemini`、`volcengine`，`unknown` 是否应展示取决于协议语义，不能机械地把 7 项全部放入 UI。精简时补齐真实支持的入站协议，或随能力矩阵一起收窄并补测试。

**建议方案：** ① 先删能力矩阵 UI，后端 API 是否废弃单独决策；② 候选编辑器高级字段折叠；③ 其余保留。若后续仍嫌多，再评估"发现"与"目录"是否合并视图（两 tab 数据关联性强，但涉及交互重设计，不建议本次做）。

---

### 2.4 Header 策略简化为"设置页全局透传开关" —— **建议：可行，但属中高风险兼容改造**

**关键发现：** `op/header_policy.go:326` `ResolveHeaderPolicy` 在**未命中任何已配置策略时**默认 `ForwardClientHeaders: true`，按内置白名单（`defaultAllowedClientHeaderPatterns`）和受保护 Header 注册表过滤。因此，只有“未配置策略的请求”可以认为默认行为等价；源码无法证明所有现有部署都未使用自定义策略。

**解析/应用入口（静态定位 7 类调用点）：**

| 位置 | 用途 |
|------|------|
| `relay/relay.go:1302` | 主链路代理请求头组装 |
| `relay/compact.go:438` | /responses/compact 代理 |
| `relay/images.go:947` | 图片生成代理 |
| `relay/ws_pool.go:515` | WS 上游连接请求头 |
| `grouphealth/probe.go:56` | 健康检查探针 |
| `sitesync/http.go:78`、`anyrouter.go:833` | 站点同步请求 |

代理主链路调用 `ResolveHeaderPolicy(channel, canonical, candidate)`，Site sync 则通过 `ResolveSiteHeaderPolicy(site, account)` 解析。两者的 scope 与失败降级语义不同，不能把所有入口机械替换为一个无参全局函数。

**建议方案：**
1. **新增设置项**：`relay_forward_client_headers`（bool，默认 true），放在设置页"网络"卡片
2. **op 层**：为 relay 和 Site sync 分别保留明确解析入口，共享从设置缓存生成的默认策略；**保留** `ApplyHeaderPolicy`/`HeaderIsProtected`/白名单黑名单常量和 trusted-header 最后注入顺序
3. **调用点迁移**：分别验证 relay HTTP/SSE/WebSocket/images/compact、group health 与 Site sync/AnyRouter；不做一次性全局文本替换
4. **删除**：
   - 前端：`HeaderPolicies.tsx`/`HeaderPolicyEditor.tsx`/`HeaderPolicyPreview.tsx`、model 页 header-policy tab、`api/endpoints/header-policy.ts`、`header-policy-options.ts`
   - 后端：`handlers/header_policy.go`（7 条路由：list/registry/user-agents/preview/upsert/delete/user-agent）、`op/header_policy.go` 的策略 CRUD/解析部分（约 2/3）、`model/header_policy.go` 的表模型
   - 备份：`backup_extended_*` 中 header_policies/user_agent_profiles 两表的导出导入
   - migration 数据残留：表保留（不写新 migration 删表，避免破坏备份兼容），数据自然弃用
5. **迁移门禁**：发布前统计已启用 Header Policy/UA 档案，导出或映射可表达的全局设置，对无法映射的 Set/Unset/scope 规则显式告警；后端管理 API 先废弃再删除

**风险**：中高。已配置的 scope 规则、自定义 Set/Unset Header 和 UA 档案都会产生可见行为变化；删除管理 API/备份字段还涉及外部客户端与数据兼容。只有经数据审计证明未配置策略的部署，才能按低风险迁移。

---

## 三、按页面功能清单与保留评估

### 1. Home 仪表盘（`web/src/components/modules/home/`）

| 功能 | 状态 | 建议 |
|------|------|------|
| 统计卡片 + 趋势面积图（4 档周期） | ✅ | 保留，但指标将被 2.1 的分析区块覆盖，可合并呈现 |
| 活动热力图（54 周） | ✅ | 保留 |
| 渠道排行（成本/请求/Token） | ✅ | 保留 |
| 分组健康总览（摘要条+弹窗） | ✅ | 保留 |
| 自动刷新（10s/30s/1h） | ✅ | 保留 |

### 2. Site 站点管理（`index.tsx` 2622 行）

| 功能 | 状态 | 建议 |
|------|------|------|
| 站点卡片列表（健康徽章/指标/展开账号） | ✅ | 保留 |
| 搜索/排序/多维筛选 | ✅ | 保留 |
| 新建/编辑站点（平台检测/Header/路由覆盖） | ✅ | 保留（站点级 Header 随 2.4 删除自定义能力，仅剩固定透传） |
| 账号管理（用户名密码/Access Token/Cookie/API Key 四类凭证，以及随机签到参数） | ✅ | 保留 |
| 账号快捷操作（同步/签到/启停） | ✅ | 保留 |
| 签到状态推导/筛选（有单测） | ✅ | 保留 |
| 批量失败分类（有单测） | ✅ | 保留 |
| 签到概览面板 | ✅ | 保留 |
| 批量编辑（标签+Header） | ✅ | 保留（Header 批量编辑随 2.4 移除） |
| 批量操作栏 | ✅ | 保留 |
| 标签输入 | ✅ | 保留 |
| 数据导入（All API Hub/Metapi） | ✅ | 保留 |
| 归档/恢复 | ✅ | 保留 |
| 统一删除确认 | ✅ | 保留 |
| 全量同步/签到（toolbar 联动） | ✅ | 保留 |
| 跨页跳转定位 | ✅ | 保留 |
| 恢复中心（代理偏好/已学路径/尝试历史） | ✅ | 保留 |
| 人工验证（会话/任务/重试） | ✅ | 保留 |
| 错误消息本地化（有单测） | ✅ | 保留 |
| 自动同步/代理模式/签到标记 | ✅ | 保留 |
| 倍率设置（global_weight） | 🕳️ 已持久化并出现在 Site API/表单状态中，但无 UI 控件，也未见生产路由/定价消费方 | **需决策**：视为 dormant 兼容字段；先确认外部 API/备份依赖，再选择补 UI 或分阶段废弃，不能按纯前端残留直接删除 |

### 3. Channel 渠道管理

| 功能 | 状态 | 建议 |
|------|------|------|
| 渠道卡片列表（双 tab/虚拟滚动） | ✅ | 保留 |
| 渠道卡片（双布局） | ✅ | 保留 |
| 详情/编辑/删除（diff patch/keys 三向 diff） | ✅ | 保留 |
| 新建渠道 | ✅ | 保留 |
| 共用表单（模型拉取/代理/高级区） | ✅ | 保留 |
| TabSwitcher / tab 持久化 | ✅ | 保留 |

### 4. Group 分组管理

| 功能 | 状态 | 建议 |
|------|------|------|
| 分组卡片列表（虚拟网格） | ✅ | 保留 |
| 分组卡片（模式切换/置顶） | ✅ | 保留 |
| 成员列表（DnD/权重） | ✅ | 保留 |
| 分组编辑器（正则匹配/自动添加） | ✅ | 保留 |
| 自动分组配置 | ✅ | 保留 |
| 预设（保存/激活/克隆） | ✅ | 保留 |
| 健康检查（徽章+详情） | ✅ | 保留 |
| 成员自动排序（有单测） | ✅ | 保留 |

### 5. Model 模型管理

| 功能 | 状态 | 建议 |
|------|------|------|
| 模型目录（规范化/别名/候选） | ✅ | 保留（候选高级字段折叠，见 2.3） |
| 目录详情（草稿冲突检测） | ✅ | 保留 |
| 模型发现（批量建组/映射） | ✅ | 保留 |
| 定价面板（价格链路/汇率/手动报价） | ✅ | 保留 |
| Header 策略（编辑器/预览/UA 档案） | ✅ | **删除**（简化为全局透传开关，见 2.4） |
| 路由工具-路由预览 | ✅ | 保留 |
| 路由工具-能力矩阵 | ✅ | **建议删除**（调试工具，见 2.3） |
| 旧版全局价格 Tab（LegacyPrices 等 4 组件） | ⚠️ 旧体系 | **删除**（见 2.2） |

### 6. Log 日志页

| 功能 | 状态 | 建议 |
|------|------|------|
| 日志明细（SSE 实时流/游标分页/虚拟列表） | ✅ | 保留 |
| 日志卡片（结果/WS 模式/重试链/倍率徽章） | ✅ | 保留 |
| 详情诊断（请求响应体/禁用站点模型） | ✅ | 保留 |
| 筛选控件/维度选择器 | ✅ | 保留（分析专用控件随 2.1 迁移） |
| 使用分析（KPI/趋势/下钻/CSV） | ✅ | **迁移到 Home**（见 2.1） |
| 历史日志修复工具 | ✅ | 保留 |

### 7. Setting 设置页

| 功能 | 状态 | 建议 |
|------|------|------|
| API Key 管理 | ✅ | 保留 |
| API Key 导出（CC Switch/Cherry Studio 深链） | ✅ | 保留 |
| 信息（版本/自更新） | ✅ | 保留 |
| 外观（主题/语言） | ✅ | 保留 |
| 网络（代理/CORS/SSE 心跳/WS 模式） | ✅ | 保留（**新增"透传客户端请求头"开关**，见 2.4） |
| 账号（改用户名/密码） | ✅ | 保留 |
| 可靠性（熔断/离群退役/健康检查） | ✅ | 保留 |
| 同步任务（4 个定时任务） | ✅ | 保留 |
| 数据（日志保留/清空/备份导入导出） | ✅ | 保留 |
| WebDAV 备份 | ✅ | 保留 |

### 8. 附加模块

| 模块 | 状态 | 建议 |
|------|------|------|
| API Key Dashboard（290 行） | ✅ | 保留 |
| site-channel（3343 行） | ✅ | 保留（建议拆组件） |
| proxy-pool（4 组件） | ✅ | 保留 |
| 登录/Logo/导航/Toolbar | ✅ | 保留 |

---

## 四、瘦身候选清单（按风险分级）

### A 级：静态清理候选（需逐项证明无生产与测试合同影响）

> PR-0（PR #2）已完成并从本计划中移除以下候选：relay passthrough 旧指标/终态 helper、`injectWSPreviousResponseID` no-op、Anthropic 入站两个无调用 helper、`defaultReasoningEffortMapping`、`utils/xslice`、`sitesync.Stub`、task 6 个未用常量、`GetRouterCount`、`SiteProxyPreferenceClear`。剩余条目仍需按真实调用方和测试合同逐项处理。

**前端（12 处）**

| 位置 | 内容 |
|------|------|
| `api/endpoints/log.ts:213` | `useLogPage` + `LogPageResponse`/`LogStatusFilter`（被 useLogs 取代） |
| `api/endpoints/apikey.ts:205` | `useAPIKeyStats` |
| `api/endpoints/stats.ts:69` | `useStatsToday` |
| `api/endpoints/group-health.ts:136` | `useGroupHealth(groupId)` |
| `api/endpoints/site.ts:730` | `useSiteAvailableModels` |
| `api/endpoints/group.ts:369` | `useRunGroupAutoGroup`（+ 注释掉的 `useAutoAddGroupItem`） |
| `api/types.ts` | `PaginationParams`/`PaginatedResponse`/`HttpStatusCode` |
| `group/utils.ts:28` | `buildChannelNameByModelKey` |
| `toolbar/ToolbarMenu.tsx` | `menu-only` 优先级分支（无使用者） |
| `toolbar/index.tsx:79-89` | `CreateDialogContent` 的 site/log 不可达分支 |
| `nav-store.ts` | `prevItem` 字段（写而未读） |
| `log.ts` 类型 | RelayLog 7 个未消费字段（transport_input_tokens、bill_input_tokens、price_original_cost、price_match_reason、header_policy_trace、canonical_model_name、request_api_key_id） |

**后端候选（需区分真死代码与测试支撑 API；PR-0 已完成项不再重复列入）**

| 位置 | 内容 |
|------|------|
| `op/stats.go:357` | `StatsModelUpdate` |
| `op/usage_analytics_breakdown.go` | `UsageAnalyticsBreakdownGet`/`UsageAnalyticsBreakdownExportGet`（测试支撑 API；删除前需改写测试） |
| `op/protocol.go:183` | `AssessProtocolRoute`（测试支撑 API；删除前需改写测试） |
| `op/log.go:548` | `RelayLogList`（旧查询包装，多个跨包测试使用；不是零成本纯删除） |
| `op/group.go` | `GroupItemAdd/Update/Del/List` 主要被测试使用；`GroupItemBatchAdd` **有生产调用**（`projected_channel_auto_group.go:108`），不属于删除候选 |
| `op/relay_log_index.go:106` | `RelayLogEnsureIndexesSync`（测试支撑 API；删除前需改写测试） |
| `handlers/log_analytics_export.go:134` | `writeUsageAnalyticsCSV`（非分页版测试支撑函数；删除前需调整测试入口） |

> 注：“只有测试调用”不等于“无任何价值”。这类 exported helper 可以收口或改为更贴近生产入口的测试，但应将测试改写与删除放在同一 PR 中完成。

### B 级：功能级删除/合并（需产品决策，即第二章四项）

| 项 | 决策 | 涉及 |
|----|------|------|
| 使用分析并入 Home | ✅ 建议合并 | log 页 2 组件 + home 新增区块 + store 复用 |
| 旧版全局价格 Tab | ✅ 建议删除 | 前端 4 组件 + 4 hooks + 后端 4 路由/op 函数 |
| Header 策略 → 全局透传开关 | ⚠️ 可行，需迁移设计 | 前端 3 组件 + 后端 7 路由 + op 策略层 + relay/Site 两类解析入口 |
| 模型目录能力矩阵 | ✅ 建议删除 | 前端 1 对话框 + 后端 1 API |
| 倍率 global_weight dormant 字段 | ⏳ 待定（先做 API/DB/备份兼容盘点） | Site 持久化、API 与 SiteEditDialog 表单状态 |

### C 级：重复实现/代码瘦身（不删功能，重构减代码）

- `getErrorMessage` 当前有 **6 个**实现（site 模块内及同步任务），`formatDateTime` 在多处重复、`PLATFORM_LABELS` 2 处
- 健康状态样式函数 `home/group-health-overview.tsx` 与 `group/health.tsx` 双份
- `log/Item.tsx` 相邻尝试合并算法两份拷贝
- Setting 页 4 张卡片未用公共 `SettingCard`（样式重复）
- 手写 fetch + FormData 3 处（`useImportDB`/`useImportAllAPIHub`/`useImportMetAPI`）
- `CustomHeader`/`ApiResponse` 类型各双份
- `site/index.tsx`（2622 行）、`site-channel/index.tsx`（3343 行）巨型组件拆分
- 后端 `relay/relay.go`（1926 行）、`op/backup.go`（1603 行）拆分候选
- 前端硬编码中文多处（toolbar/site/log），与 i18n 混用

### D 级：疑似缺口/兼容项的复核结论

| # | 位置 | 内容 | 建议 |
|---|------|------|------|
| 1 | `SiteEditDialog.tsx:60` | global_weight 无 UI 控件，但已进入持久化与 API 合同 | 先盘点兼容，再决定补 UI 或分阶段废弃 |
| 2 | `handlers/apikey.go:141` | `/api/v1/apikey/login` handler 本身只返回成功，但路由前的 `APIKeyAuth()` 负责实际校验 | **保留**：前端登录与持久会话恢复均调用该入口 |
| 3 | `task/init.go` | `TaskPriceUpdate`、`TaskSyncLLM`、`TaskCleanLLM`、`TaskSiteSync`、`TaskSiteCheckin`、`TaskWebDAVBackup` 仅声明未使用；其中 `TaskCleanLLM` 也未注册 | ✅ 已在 PR-0 删除；后续若新增按名称的外部配置，再单独补兼容说明 |
| 4 | `catalog-options.ts` | INBOUND_PROTOCOLS 缺少 `gemini`、`volcengine`；`unknown` 的 UI 语义未定 | 按真实入站协议补全；对 `unknown` 先明确语义并补测试 |
| 5 | `handlers/site_recovery.go:53-61` | `/verification/bridge/*` 7 条路由不走后台 `Auth()`，但使用 `pairing_token`、账号作用域及一次性 `task_token`/`request_token` 内建认证 | 保留协议和现有认证；仅在实际存在公网暴露或请求体风险时增加边界限制，不默认叠加重复的速率限制/防护层 |

---

## 五、建议执行顺序（PR 拆分）

| 顺序 | 内容 | 风险 | 预计影响 |
|------|------|------|---------|
| PR-0 | ✅ 已在 PR #2 执行：完成内网防御分支审计、13 处死代码清理、P0 热路径开关和 4 项缺口修复；条件裁剪项按用户确认保留 | 低至中 | 已完成，后续候选以本计划当前内容为准 |
| PR-1 | A 级静态候选分批清理 | 低至中 | 先删真无调用定义；测试支撑 API 必须同 PR 改写测试，排除 `GroupItemBatchAdd` |
| PR-2 | 删除旧版全局价格 Tab（2.2） | 中 | UI 删除可先行；后端 4 路由需先确认外部消费者/废弃策略 |
| PR-3 | Header 策略 → 全局透传开关（2.4） | 中高 | 先完成数据审计与迁移，再分别改造 relay 和 Site sync 入口 |
| PR-4 | 使用分析并入 Home（2.1） | 中 | log 页瘦身 + home 新增区块 + 跳转联动 |
| PR-5 | 模型目录精简（2.3） | 低至中 | 能力矩阵 UI 删除 + 高级字段折叠；API 是否删除单独决策 |
| PR-6 | global_weight 取舍 | 中 | 补 UI 或通过兼容周期废弃 API/DB/备份字段 |
| PR-7 | C 级重构（抽公共/拆组件） | 中 | 多个小 PR，逐个 review |

> 每步完成后跑 `go test ./...`、`pnpm build`、`pnpm lint`、`tsc` 验证。
