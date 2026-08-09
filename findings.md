# Findings

## Confirmed Before Implementation

- `default_multiplier_cap` 保存接口只更新设置；只有 `/api/v1/group/apply-defaults` 调用 `ApplyGroupDefaults`。
- `ApplyGroupDefaults` 使用 group cache 快照，并对已知 `multiplier > cap` 的 `GroupItem` 做物理删除；未知倍率用 `+Inf` 且被明确跳过。
- 分组创建、更新新增、单条/批量新增、Catalog wiring、自动分组、预设激活、备份导入、站点投影重写等入口没有持续执行 cap。
- `CatalogPlanGroup` 当前只做健康/协议准入，倍率仅参与排序；Balancer 直接消费 `group.Items`。
- Group Card 可见倍率徽标使用 `group_multiplier`，不是 `multiplier`；Apply 对同一渠道也优先使用站点分组倍率。
- `projection_suspended` 同时承载同步暂停和倍率策略暂停；成功同步/恢复逻辑可以清除它。

## Required Implementation Constraints

- 不删除历史 GroupItem，避免配置、权重和优先级丢失。
- 在共享路由规划层过滤超限/未知候选，不能只在写入口过滤。
- 策略状态需要可恢复，并且同步成功不能覆盖策略阻断。
- API 字段必须保持 nullable 语义，前端不能把未知倍率变成 1x。

## Verification After Implementation

- `ApplyGroupDefaults` 不再删除 `GroupItem`，返回的 `items_removed` 保留为兼容字段且为 0；超限条目（known=true 且 >cap）由路由策略持续阻断。
- `default_multiplier_cap` 保存接口拒绝负数、NaN、Inf 和非数字，并立即更新独立策略状态；设为 0 时解除策略上限并恢复策略阻断组。
- `CatalogPlanGroup`、`GroupGetEnabledMap` 和 balancer iterator 都遵循策略结果，倍率阻断发生在 priority/weight/余额排序之前。
- 同步持久化与成功同步流程复制/保留 `policy_blocked` 字段，不清除独立策略状态；前端在同步暂停与策略阻断同时存在时分别展示。
- 前端生产构建、lint、TypeScript 检查和 `git diff --check` 通过；默认 `proxy.golang.org` 超时，但使用 `GOPROXY=https://goproxy.cn,direct` 后定向及 `./...` 全量 Go 测试通过。

---

# 多顾问审查风险清单（2026-08-09）

> 背景：对「Group Multiplier Policy Fix」未提交改动（35 文件）执行首席审查官编排的全栈审查。正面线 3 位（反驳者/本质追问者/无情行者）+ 对抗组 6 位（后端/数据/性能/安全/逻辑/前端对抗者），全部真实派发，无派发失败。
> 结论：**否决**，当前状态不可提交推送。核心目标「硬性准入」与「解耦可见性」在实现上存在实质性断裂。
> P0-1 于 2026-08-09 经用户确认产品语义后重审（见下方「P0-1 重审结论」），其余条目维持原判定。

## 最终判定与审查构成

- 判定：**否决** —— 需按 P0/P1 修复后提交。
- 8/9 顾问独立给出「不能上线 / 需整改」；P0-1 被 6 位顾问独立击穿，P0-2 被 5 位击穿，P0-3 被 6 位击穿，属于多专业交叉证实而非单点误判。
- 分歧裁决：① cap 度量对象（分组倍率 vs 计费乘积）→ 用户确认**只限分组倍率（key 倍率）**，P0-1 重审；② 迁移 024 无数据回填但主 AutoMigrate 已覆盖 → 无漏迁移，真正缺口是「无启动/周期兜底重算」；③ GroupList 缓存数组污染 → 需补数据竞争测试验证。

## P0 / P1 风险对照（预期 → 现状逻辑 → 实际后果）

### P0-1 倍率上限可被绕过【重审中，见下节】

### P0-2 双层「blocked」各说各话（5 位顾问击穿）

- **预期**：界面显示的「倍率阻断」= 路由实际执行的阻断，二者一致。
- **现状**：两套互不相干的 blocked —— 路由层每次请求实时重算（`group_multiplier_policy.go:88-94`，看得全 model 倍率/报价，未知即拦）；持久层（UI 显示用）`EnforceMultiplierCap` 只读 `group.Multiplier` 一个字段且倍率 nil 永不标记（`group_defaults.go:251`）。
- **后果**：站点无倍率数据 + cap 开启 → 路由全拦（503）但 UI 显示健康、投影照常生成，管理员无法定位；反向 UI 标红但路由已恢复。

### P0-3 「自动恢复」名不副实（6 位顾问击穿）

- **预期**：task_plan 承诺「条件恢复时自动恢复」——上游倍率回落应自动解除阻断。
- **现状**：解除重算仅 3 个触发点（改 cap / apply-defaults / 定价刷新，`group_defaults.go:157`、`setting.go:83`、`site_pricing.go:95`）；站点同步路径写入新倍率但不触发重算，`storage.go:143-145` 反而刻意保留旧 blocked。
- **后果**：上游倍率 8x→2x 同步落库后 `policy_blocked` 仍 true，分组可能永久停摆，直到管理员手动触发。

### P1-4 每请求 2 倍冗余计算 + 出错 fail-closed 雪崩（4 位顾问击穿）

- **预期**：准入过滤对网关性能影响小。
- **现状**：每请求两遍 evaluate（`relay.go:95` + `relay.go:100`），每遍 5-6 条 DB 查询（SQLite 单连接 `SetMaxOpenConns(1)`），净增 10-12 条；查询错误被吞（`group_multiplier_policy.go:55`）→ 查不到 = blocked。
- **后果**：QPS 越高连接池越排队；DB 抖动时不是降级而是「全组无可用通道」——越卡越拒绝，可用性归零。

### P1-5 缓存数组数据竞争（单顾问发现，待验证）

- **预期**：读缓存无副作用。
- **现状**：`group.go:20` 把派生字段写回 group 的 Items，切片头指向缓存底层数组 → 并发读写共享 backing array。
- **后果**：并发下可能读到半写字段；缓存混入过期派生值。因全字段覆写暂未爆，属未爆弹，需数据竞争测试证实。

## P2 可修复项

- `"blocked"` 字符串双源：路由过滤字面量（`iterator.go:36`）与 op 包常量（`group_multiplier_policy.go:14`）耦合，常量改名即路由静默全放行。
- `routeCandidateKey` 不 TrimSpace：手输模型名带空白 → key 不匹配 → 有价误判 unknown → cap 开启误拦。
- cap fail-open：解析失败 = 无限额，与「管理员关停」不可区分（计费控制点应 fail-closed）。
- 设置保存重算非事务：cap 已写库、重算失败返 500 但策略已生效。
- 前端：dropdown 与徽章判定谓词不一致（同屏矛盾）；blocked 组倍率变 null 时「倍率未知」掩盖阻断、无解除入口；内联中文未走三语 locale；toast 计数与徽章口径不一致。
- API 契约：`GroupGet` 无 policy 字段、`GroupsSuspended` 语义被改、`ItemsBlocked` 口径与路由不符。

## P3 观察项

- 迁移 024 无数据回填、无启动/周期兜底重算；cap 附近浮点无容差 → 候选集 flapping；`unknown` 状态 UI 完全不可见；`findings.md / progress.md / task_plan.md` 入库策略待定。

## 下一步行动（原判定）

1. （阻塞）统一倍率口径并打通双层判定：`EnforceMultiplierCap` 与 `evaluateGroupItemMultiplierPolicies` 共用同一判定函数，nil 倍率组同样落组级 blocked。
2. （阻塞）闭环恢复链 + 消除热路径重复：`persistSyncSnapshot` 末尾触发重算并补「同步降倍率→自动恢复」测试；`CatalogPlanGroup` 去掉二次 apply；`GroupList` 先 copy 再 apply。
3. （应做）前端收口：统一 dropdown/徽章谓词；blocked 优先展示 reason、null 倍率显示「站点未提供倍率」并给重算入口；内联中文迁入 locale。
4. （门禁）补 balancer blocked 过滤测试 + R5 数据竞争测试；确认文档文件入库策略；留全量测试日志。

## P0-1 重审结论（2026-08-09，用户确认前提后）

**用户确认的产品语义**：cap 的度量对象**只限分组倍率（key 倍率）**，不纳入模型倍率——模型倍率口径复杂（按次收费、输入/输出/缓存分项倍率不一），难以统一界定。

**重审构成**：正面线 3 位（反驳者/本质追问者/无情行者）+ 对抗组 4 位（数据/后端/逻辑/安全对抗者），7 位全部真实派发。

### 重审判定：原「致命」降级为「P1 修复包」

- **一致结论**：新前提下「cap 只用分组倍率」是设计意图而非漏洞，原「上限可绕过」指控不成立；**但仅改文档不可接受，必须改代码**（无情行者明确）。
- **事实勘误（数据/逻辑对抗者双重证实）**：原审查称「计费按 model×group 乘积」（`catalog.go:988-992`）有误——真实计费乘数恒为 `quote.GroupMultiplier`（`site_pricing.go:550-570` `effectivePriceFromQuote` 只乘分组倍率），模型倍率在同步时折进 Input/Output 单价（`sitesync/pricing.go:187-192`）；`catalog.go:988-992` 的乘积是**排序/展示倍率**而非计费倍率。故「展示 A 计费 B」错位方向需修正，且 cap 与计费在 key 倍率维度上本就对齐。

### 残留风险（新前提下仍成立，P1 优先）

| # | 风险 | 位置 | 说明 |
|---|---|---|---|
| R1 | candidate 兜底把乘积混入 cap 判定 | `group_multiplier_policy.go:73-79,91` | 分组倍率未知时 `effectiveMultiplier` 填入 model×group 乘积并比 cap——违反「cap 只限 key 倍率」，造成错杀（同渠道：key 已知 1x 放行、未知时候选 10x 被拦）；reason 文案 "excludes candidate" 与实现矛盾 |
| R2 | 「未知倍率」三条路径三个结论 | `group_multiplier_policy.go:88-90` / `sitesync/pricing.go:214-222`（补 1x）/ `group_defaults.go:251` | 条目级 cap 开启→blocked；补 1x 持久化→已知 1x 永不超限；组级 nil→永不 block。结论由「是否已持久化」时序决定 |
| R3 | key 倍率无单一数据源 | `channel.go:658-686`（列+raw_payload）vs `quote.GroupMultiplier`（计费） | multiplier 列 / raw_payload / quote 三个快照，cap 判定与计费分叉（列=1、quote=10 时展示放行但计费 10x）；`NormalizeSiteGroupKey` 大小写敏感 vs raw_payload EqualFold 裂缝 |
| R4 | `effective_multiplier` 字段命名误导 | `model/group.go:42`、`routing.go:217` | 承诺「最终生效倍率」，实际 source=group 时=key 倍率、source=candidate 时=乘积。分歧：无情行者主张不改名（前端未渲染、成本高）、逻辑/数据对抗者主张改名或权威注释 |
| R5 | fail-open + 无审计升级（安全对抗者） | `group_multiplier_policy.go:30-40`、`handlers/setting.go:34-44` | 新前提下 cap 成唯一成本控制点：解析失败与设 0 关停不可区分（P2→P1）；无角色分离/审计，单会话可无痕改写；UI 文案缺「不含模型倍率」明示 |
| R6 | candidate 查询在 cap 路径是死重 | `group_multiplier_policy.go:57` | 默认 cap=0 时也每请求查 route_candidates+价格报价；删除可缓解 P1-4 |

### 待拍板口径（D1/D2/D3，代码才有意义）

- **D1 未知倍率语义**：统一为「未知=blocked」（数据/后端对抗者建议，需与组级共用判定函数）还是「未知=unknown 放行」（无情行者建议，向组级 nil 不 block 看齐）？两派都同意：**当前三态互斥必须收敛为一个**。
- **D2 补 1x 去留**：`pricing.go:214-222` 缺省补 1x 是「未知→已知 1x」的静默固化，与「未知=blocked」矛盾。选：删补 1x 保持 nil；或补 1x 时标记 `multiplier_known=false`（数据对抗者方案），仅影响计费语义不影响 cap。
- **D3 权威源**：建议 multiplier 列（pricing/group_ratio 刷新写入）为权威，raw_payload 降级为补偿，quote 只作计费，并把「cap 判定与计费乘数来自不同表」显式写文档。

### 方案出具（最小正确改动）

1. `evaluateGroupItemMultiplierPolicies` 删除 candidate 对 `effectiveMultiplier` 的回退（candidate 只写 `multiplier` 供展示/排序，不参与 cap 比较）；reason 改为 "key multiplier unknown" / "key multiplier exceeds cap"。
2. `EnforceMultiplierCap`（组级）与 evaluate（条目级）共用同一 key 倍率判定函数（含 raw_payload 兜底或写回列确立单一真源）；`ApplyGroupDefaults` 的 `ItemsBlocked` 计数改为仅按 group 倍率。
3. 迁移 024 补数据回填（raw_payload 高倍率组升级后补 `policy_blocked`）。
4. `cap==0` 时 evaluate 短路，跳过 candidate 查询（缓解 P1-4）；`effective_multiplier` 改名或加权威注释 + UI/设置文案明示「仅限分组倍率，不含模型倍率；开启后未知倍率渠道将被排除」。
5. fail-open 修复：`configuredMultiplierCap` 拆分「读取/解析错误」与「合法 0」，错误态 fail-closed 并告警；cap 修改记审计。

### 最终判定

P0-1 **不阻塞主流程**（cap 默认关闭、默认配置零影响），降级为 P1 修复包，**随下一个常规版本修复**（无情行者：约半天工作量，核心集中在 `group_multiplier_policy.go` evaluate 一个函数 <100 行）。安全对抗者条件：不修复 fail-open/审计/UI 明示则不得把 cap 当计费兜底上线。

---

## 用户决策反馈轮（2026-08-09，D1/D2 拍板后 7 顾问复核）

### 用户最终决策
- **D1 选 A（从严，未知=拦下）**，补充：站长未设倍率的分组 → 暂定 1x + 标注「缺失/暂定」，分组可用，使用者知道要注意。
- **D2 选方案 2**（填 1x 打标签「猜的」：计费按 1x，闸门不拿猜的数当真，未知按未知处理；界面显示标注）。

### 顾问反馈综合：有条件同意（方向认可，但「暂定 1x」按字面实现有 2 个致命障碍）

**障碍一（7 顾问一致）：系统无法判定「站长没设」** —— 系统只能观测「没查到」（multiplier 列为 nil），「没查到」至少混了 5 种原因（真没设/同步没跑/解析失败/接口字段变更/分组名不匹配），`storedSiteGroupMultiplierValue` 对「字段存在但值非数字」和「字段不存在」返回同一结果（`site_group_multiplier.go:93-103`）。「暂定 1x 标注」的载体也不存在：`GroupMultiplierKnown` 是 `gorm:"-"` 瞬态字段（`site_pricing.go:55`），写入即丢；`SiteUserGroup` 无任何 provenance 字段。

**障碍二（数据/安全对抗者判致命）：「暂定 1x 放行」会让 cap 空转，且可被恶意站点绕过**
- 计费根本不读 multiplier 列，只读 `quote.GroupMultiplier`（`site_pricing.go:550-570`），「计费按 1x」是空承诺；
- 暂定 1x 一旦写入列，闸门（`group_defaults.go:251`、`group_multiplier_policy.go:91`）把它当真实 1x → cap 判定空转（1x 永不超过任何合理 cap）；
- 恶意/粗心站点只需在 `group_ratio` 省略该分组、在 `enable_groups` 列出 → 摄入端自动补 1x（`pricing.go:214-222`）→ 静默绕过 cap，且绕过路径上连「暂定」徽章都不出现。

**用户决策语句本身有内在矛盾（逻辑对抗者）**：「暂定 1x 可用（放行）」与「闸门不拿猜的数当真（按未知处理）」在同一分组上互相排斥——闸门是二元的（allowed/blocked），放行=拿 1x 当真，拦下=不拿当真，没有第三个输出位。「按未知处理」真正想表达的应收窄为：猜测值**不落库为真实值、不参与 >cap 判定、不参与排序权值、可被下次同步覆盖**。

**「使用者」定义缺位（安全对抗者）**：实际付费的 API 调用方只能看到 `/v1/models` 模型列表，看不到后台的「暂定/缺失」标注——「使用者知道要注意」实际只对管理员成立；若真实倍率 >1x，下游不知情付费/操作者漏收。

### 修正后的落地共识（7 顾问一致建议路径）

1. **新增持久化 provenance 列**：`SiteUserGroup.multiplier_known`（bool）或 `multiplier_source`（'explicit'|'defaulted_missing'|NULL），走 **025 迁移**（024 已立 AutoMigrate 模板，勿动 024）；`GroupMultiplierKnown` 从 `gorm:"-"` 改为真列。
2. **「站长未设」定义收窄**：仅「enable_groups 列出且 group_ratio 省略」这一个强信号算 S（暂定）；groups 端点无倍率、raw_payload 解析失败、行不存在一律算 U\S（按 A 拦下）。不得把任何「multiplier nil」都当 S（那是从严承诺空心化通道）。
3. **「暂定」只用于计费与标注，不用于 cap 放行**：cap 评估用真实上报倍率；未知→按 A 拦下；暂定 1x 只进计费（现状 quote 层已按 1x）与 UI 徽标，进 `allowed_tentative` 三元状态（区别于 allowed）。
4. **修复 Known 摄入逻辑**：`parseSitePricingQuotes` 的 known 必须从**原始 group_ratio** 计算（先算 rawKnown 再补默认 1x），不能用补完 1x 的 map 反推（`pricing.go:155-158` + `214-222` 当前是坏标记）。
5. **标注出现在计费可见面**（用量/配额页标「暂估倍率」），不只后台分组列表；`ItemList.tsx`/`site-channel` 的「倍率未知」与「暂定 1x」文案区分。
6. **cap 失效发声**（fail-open 修复）：`configuredMultiplierCap` 解析失败记 `log.Warnf` + UI 横幅 + 审计（`setting.go:64-118`）。

### 工作量评估（无情行者/后端对抗者）
- 最小版（持久化标记 + UI 标注，闸门不改）：约 2.5 人天
- 完整版（三态判定 + 计费链路贯通 + 审计）：约 3-4 人天
- 分歧：无情行者认为「闸门不改、暂定 1x 落库走 allowed」已可落地；数据/安全对抗者认为这样 cap 会空转且可绕过，必须「暂定不参与 cap 放行」。

### 用户最终确认（2026-08-09，决策定稿）
- **「暂定 1x」分组的闸门处置：放行**（采纳顾问方案 1，符合「分组可用」本意），但暂定值**不得入库为真实倍率**（保持可区分）。
- **「使用者」= 管理员**；本项目为**内网自用**，不存在下游 API 调用方知情权问题（安全对抗者此条担忧解除）。
- **需求**：从头梳理应用内与倍率相关的全部流程设定，确保按三态方案修改后不被其他环节的既有设定击穿（本次全链路梳理待回填）。

### 三态方案落地共识（待全链路梳理验证后回填）

---

## 全链路流程梳理（2026-08-09，6 顾问并行：本质追问者/后端对抗者/数据对抗者/逻辑对抗者/无情行者/前端对抗者）

> 目的：按三态方案（真值/暂定1x/真未知）修改时，找出所有「其他环节既有设定会让方案失效」的点。结论：三态方案**不是改一个判定函数能落地的**，必须让「known 维度」贯穿 来源→存储→读取→判定→计费→展示 全链，否则会被现有的「静默归一化点」逐条打回二态。

### 最大的敌人：三处「静默补 1」（多位顾问独立证实）

系统里有三处代码会把「未知」**静默变成「已知 1x」写进数据库**——三态方案不先处理这三处，「真未知」永远不会产生，「从严拦下」是空操作：

| # | 位置 | 把什么变成什么 | 三态下必须改为 |
|---|---|---|---|
| S1 | `sitesync/pricing.go:214-222` | enable_groups 里列出但 group_ratio 缺省的分组 → 强制补 1x 并视为已知 | 降级为「暂定 1x」，不得视为真值 |
| S2 | `sitesync/pricing.go:155-158` | `!known` → `groupMultiplier=1` | known 必须从**原始 group_ratio** 计算（先算 rawKnown 再补 1x），不能用补后 map 反推（当前是坏标记） |
| S3 | `op/site_pricing.go:129-131` | quote 写入前 `GroupMultiplier==0 && !Known` → 1 | 保留计费按 1x，但 known 先落持久列再补 1 |

另有读时兜底：`op/channel.go:673-684`（persistedSiteGroupMultiplierMap 的 raw_payload 二次解析）会把「真未知」在**读时复活**成有效倍率——三态必须给它加 known 闸门或删除（6 位顾问中的 4 位点名此为最隐蔽失效点）。

### 关键事实：迁移版本号陷阱（数据对抗者发现，已证实）

- 现有**最大迁移版本号是 `2026080304`**，不是 24（024 只是文件名，Version 字段才是排序依据，`migrate.go:59-62` 按 Version 升序执行）。
- 新迁移**文件名可叫 025，但 Version 字段必须 > 2026080304**（如 2026081001），否则会在 `site_model_multiplier.go`(2026080303) 之前执行，导致顺序错误。
- 回填判据：`multiplier_known = (multiplier IS NOT NULL)` 会把历史「补的 1x」误标真值——保守方向是「无法证实的都标 false」，宁可让真值在下一次同步时升级。

### 失效环节清单（统一分级）

**致命（不改则三态形同虚设）**
- F1 三处静默补 1（S1/S2/S3）+ raw_payload 读时兜底（channel.go:673-684）→「真未知」被系统性消灭
- F2 闸门过滤只认字面量 `"blocked"`（`relay/balancer/iterator.go:36`、`catalog.go:1171`）→ 若「真未知」映射成新状态名（如 unknown_strict）则**静默放行**，从严失效
- F3 读路径 map 丢失 known 维度：`channelGroupMultiplierMap`/`persistedSiteGroupMultiplierMap`（channel.go:621-686）只返回 `*float64`/`map[string]float64`，无法区分「暂定1x」与「真未知」→ 三态语义在条目级无法成立
- F4 GORM bool 零值坑（无情行者 W6）：`MultiplierKnown bool` 带 `default:true` 时，结构体零值 false 被 GORM 省略 → INSERT 落到 DB 默认 true → 真未知被静默写成真值

**重伤（不改则状态分叉/标注失效）**
- F5 `SiteUserGroupMultipliersUpdate` 是 `Update` 非 upsert（site_pricing.go:85-89），行不存在时 RowsAffected=0 静默跳过 → 暂定组拿不到倍率；且签名 `map[string]float64` 无法携带 known
- F6 同步重建 `storage.go:108` 全删重建，`multiplier_known` 不加入保留清单（storage.go:139-142、copyPersistedGroupSyncState 265-287）→ 每次同步后标志丢失
- F7 `EnforceMultiplierCap`（group_defaults.go:251）与 evaluate 判定分叉：组级只认列且 nil 不拦，条目级认列+raw_payload 且 nil 拦 → 三态下必须统一判定函数；recover 分支（270-283）会把真未知组误放
- F8 candidate 救援（group_multiplier_policy.go:73-80）：真未知组被 candidate 乘积救回 → 需显式定义「真未知是否允许 candidate 救援」
- F9 排序（group_defaults.go:138-152 vs catalog.go:1273-1320）与计数（ItemsBlocked 116-127 vs 组级 157-164）两套口径；暂定条目排序位置未定义
- F10 前端 `=== 'blocked'` 单分支（ItemList.tsx:154、site-channel index.tsx:709-729）漏处理新状态 → 暂定无徽标、真未知误显示为「倍率阻断」
- F11 前端 normalize 层吞三态信息（site-channel.ts:296 归一化 null）+ Card.tsx:125-126 fallback 链把三态值污染成渠道真实值

**可修复**
- F12 备份/恢复丢标志：`GroupMultiplierKnown` 的 `json:"-"` 导出即丢（site_pricing.go:55）；`backup_extended_import.go:1632-1633` 恢复时 0→1 归一（真实免费分组 0x 恢复后变 1x，属已存在的数据损坏 bug）
- F13 启动不重算：`InitCache`（cache.go:11-39）不触发 EnforceMultiplierCap → 启动后状态陈旧
- F14 同步路径不触发重算：`SyncAccount`（core.go:39-117）只在 pricing 成功时经 SiteUserGroupMultipliersUpdate 间接触发；sub2api 平台永无 pricing → 同步后超限组/真未知组状态陈旧
- F15 计费兜底静默 1（site_pricing.go:393-401、metrics.go:278-292）→ 计费快照需带 known 语义，relay_logs 需新增 `price_group_multiplier_state` 列
- F16 API 无三态出口：`SiteChannelGroup` 视图（site_channel.go:380、model/site_channel.go:30-58）无 known 字段 → 前端标注无数据源
- F17 手动设价路径（SiteModelPriceManualUpsert site_pricing.go:284-304）把 0 归一成 1 且不标 known → 手工设价应直接标真值

### 决策定稿（2026-08-09，用户拍板，方案正式简化为两态）

| 定义 | 用户决策 | 方案影响 |
|---|---|---|
| **D1'** | **全放行**：放弃「真未知拦下」档。所有没有分组倍率的分组一律按暂定 1x 放行 + 标注「未知」。cap 只拦「已知真值超限」 | 方案从三态简化为**两态**（真值 / 暂定=未知全放行）。「从严」档取消 |
| **D2'** | **A'：候选倍率判定+展示都去掉**（模型价格千奇百怪难推算，不可靠的数不参与判定也不展示） | `candidateMultiplierByCandidate`/`channelCandidateMultiplierMap` 退出 evaluate 判定与展示 → F8 消除，同时缓解 P1-4 性能问题（省掉每请求候选倍率查询） |
| **D3'** | **A：值 1 入列 + `multiplier_known=false` 标记**，与真值 1x 可区分 | 新增 `multiplier_known` 列（F3/F4 规避）+ 写路径同步维护 |
| **D4'** | **C：暂定按 1x 正常参与排序** | 排序层无需改状态分层（只按数字排），实现最省 |
| **D5'** | 真 0x 保持 known=true，暂定 1x 写入 known=false，两套写路径物理隔离 | 必修项：修复已存在的备份恢复 0→1 bug |

**两态方案的最终规则（单一判定）**：
```
capEnabled && multiplier_known==true && multiplier > cap  →  blocked（拦下）
其余一切（known=false / multiplier==nil / 超限未知）      →  allowed + 标注「暂定/未知」
```

**相对三态方案的简化点**（全链路梳理 F 清单中受影响项）：
- F1（静默补 1）：不再破坏"从严"，但仍是问题——补 1x 必须标 known=false，否则暂定与真值混淆（标注失效）
- F2（iterator 只认 blocked）：不再有风险——两态下唯一拦截状态仍是 `blocked`（已知超限），无需新状态名
- F7（组级/条目级分叉）：两态下天然收敛——组级 EnforceMultiplierCap 与条目级 evaluate 都是「known && >cap 才拦」，判定函数可共用
- F8（candidate 救援）：随 D2' A' 整体移除
- F12（备份 0→1 bug）：D5' 必修项

**两态方案仍需处理的环节**（全链路梳理验证仍成立）：F3 读路径 known 维度、F4 GORM 零值坑、F5 upsert、F6 同步重建搬运、F10/F11 前端标注、F13/F14 重算触发、F15 计费快照、F16 视图 known 字段、F17 手动设价 known。

**两态方案完整实施计划（2026-08-09 定稿）**：见 `task_plan.md`「最终实施计划（两态方案）」章节。

### 改动面全貌（按依赖顺序，6 顾问合并）

1. **存储**：`site_user_groups` 加 `multiplier_known`（建议 `*bool` 或显式赋值规避 F4 零值坑）；`site_model_price_quotes` 的 `GroupMultiplierKnown` 去 `gorm:"-"` 持久化或读时 join SiteUserGroup（数据对抗者建议：quote 不落状态列，读时按 (site_account, group_key) join，保证单一事实源）
2. **迁移**：文件名 025，Version 必须 > 2026080304；AutoMigrate 加列 + 保守回填（同事务）
3. **写路径**：pricing.go 三处补 1 改三态；`SiteUserGroupMultipliersUpdate` 改签名携带 known + Update→upsert；storage.go 保留清单加 flag；sync_fetch.go/http.go 显式解析置 known=true
4. **读路径**：channel.go:621-686 改返回 `(value, known)` 三态结构；raw_payload 兜底加 known 闸门或删除
5. **判定**：evaluate（group_multiplier_policy.go:42-98）与 EnforceMultiplierCap（group_defaults.go:243-285）统一三态判定函数；真未知映射 `status=blocked`（配合 iterator.go:36 现有过滤）；recover 分支对 !known 保持 blocked
6. **重算触发**：SyncAccount 脱离 pricing 成功与否无条件补 EnforceMultiplierCap；InitCache 启动补一次
7. **展示**：SiteChannelGroup 视图 + model 加 known 字段；前端类型三处加 `multiplier_known?: boolean`（normalize 保留 undefined，条件写 `=== false`）；ItemList/site-channel 徽标改 switch 支持暂定；Card.tsx fallback 加 known 门控
8. **备份/计费**：known 进 export/import；relay_logs 加 `price_group_multiplier_state`；备份恢复 0→1 归一修复
9. **测试**：更新 T1-T5（group_multiplier_policy_test.go、site_pricing_test.go、storage_test.go、site_group_multiplier_test.go、pricing_test.go 中旧二态断言）；新增三态矩阵/迁移回填/iterator 过滤/前端 normalize 测试

### 内网自用场景下的可省略项（前端对抗者）
- 三语 locale 全量补齐（中文硬编码足够）；site.ts/SiteUserGroup 站点模块展示；RouteTools 预览增强；无障碍强化。

### 关键结论

- **最隐蔽的两个断链点**（无情行者/后端对抗者点名，均不在原计划清单盲区之外容易漏）：① channel.go:673-684 的 raw_payload 读时兜底天然对冲三态语义，必须显式关闭或加 known 闸门；② iterator.go:36 只认 `"blocked"`，真未知若不映射为 blocked 则静默放行。
- **回填保守原则**：历史数据「无法证实的都标 false」（宁可多拦不可错放），真值在下次同步时升级。
- **前置条件**：先修 F1（三处补 1 + raw 兜底）+ F2（状态映射）+ F3（map 改三态）这三件事，三态方案才谈得上自洽；否则被现有流程设定逐条打回二态。