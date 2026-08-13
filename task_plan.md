# Group Multiplier Policy Fix

## Goal

将四个已确认问题落地修复：补齐分组倍率可见性、把倍率上限改为最高优先级且可恢复的路由准入规则、拆分策略暂停与同步暂停状态，并补齐跨层测试与验证。

## Phases

- [complete] 1. 审查四个问题与现有代码契约，确定最小兼容设计
- [complete] 2. 后端实现倍率策略与独立阻断状态
- [complete] 3. API 与前端补齐倍率/策略状态展示
- [complete] 4. 添加回归测试并运行后端/前端质量检查
- [complete] 5. 汇总变更、测试结果和现场数据边界

## Decisions

- 倍率上限是硬性准入规则，优先于排序、priority 和 weight。
- 超限 `GroupItem` 保留，不物理删除；路由规划时排除，条件恢复时自动恢复。
- 未知倍率在严格策略下排除，避免未知值绕过上限。
- 分组倍率、模型倍率、有效倍率和策略原因必须在 API 层明确区分。
- 策略阻断与同步健康阻断不能继续复用 `projection_suspended`。

## Errors Encountered

| Error | Attempt | Resolution |
|---|---|---|
| Octopus 仓库内不存在 `.trellis` | 1 | 使用父级 `.trellis` 读取规范；当前没有激活的 Octopus 任务，因此由本文件负责规划状态 |

## 全链路流程梳理留档（2026-08-09）

> 背景：用户拍板三态方案（分组倍率 = **真值** / **暂定 1x**（站长未设，闸门放行+标注，不入库为真值）/ **真未知**（从严拦下））。cap 只限分组倍率，不纳入模型倍率。使用者=管理员，内网自用。
> 为确认三态方案不被其他环节既有设定击穿，6 位顾问并行梳理全链路（本质追问者/后端对抗者/数据对抗者/逻辑对抗者/无情行者/前端对抗者）。详细版见 `findings.md`「全链路流程梳理」章节。

### 核心结论

- 三态方案**不是改一个判定函数能落地的**，必须让「known 维度」贯穿 来源→存储→读取→判定→计费→展示 全链；否则会被现有「静默归一化点」逐条打回二态。

### 三处「静默补 1」（三态最大敌人，必须前置处理）

| # | 位置 | 现状 | 三态下必须改为 |
|---|---|---|---|
| S1 | `internal/sitesync/pricing.go:214-222` | enable_groups 列出但 group_ratio 缺省 → 强制补 1x 视为已知 | 降级为「暂定 1x」，不得视为真值 |
| S2 | `internal/sitesync/pricing.go:155-158` | `!known` → `groupMultiplier=1` | known 从**原始 group_ratio** 计算（先算 rawKnown 再补 1x），不能用补后 map 反推 |
| S3 | `internal/op/site_pricing.go:129-131` | quote 写入前 `0 && !Known` → 1 | 保留计费按 1x，但 known 先落持久列再补 1 |

另有读时兜底：`internal/op/channel.go:673-684`（raw_payload 二次解析）会把真未知在**读时复活**——必须加 known 闸门或删除。

### 关键事实：迁移版本号陷阱

- 现有**最大迁移 Version 是 `2026080304`**（不是 24；024 只是文件名，`migrate.go:59-62` 按 Version 升序执行）。
- 新迁移文件名可叫 025，**但 Version 必须 > 2026080304**（如 `2026081001`）。
- 回填保守原则：`multiplier_known = (multiplier IS NOT NULL)` 会把历史「补的 1x」误标真值 → 无法证实的都标 false，宁可多拦不可错放，真值在下次同步升级。

### 失效环节分级（致命/重伤/可修复）

**致命（不改则三态形同虚设）**
- F1 三处静默补 1 + raw_payload 读时兜底 → 「真未知」被系统性消灭
- F2 闸门只认字面量 `"blocked"`（`relay/balancer/iterator.go:36`、`catalog.go:1171`）→ 真未知若用新状态名则静默放行
- F3 读路径 map 丢 known 维度（`channel.go:621-686`）→ 条目级无法区分暂定/真未知
- F4 GORM bool 零值坑：`MultiplierKnown bool` 带 default 时零值被省略 → 真未知被静默写成真值

**重伤**
- F5 `SiteUserGroupMultipliersUpdate` 是 Update 非 upsert（`site_pricing.go:85-89`），行不存在静默跳过；签名 `map[string]float64` 无法携带 known
- F6 同步全删重建（`storage.go:108`）不搬运 known 标记 → 每次同步后标志丢失
- F7 `EnforceMultiplierCap`（组级）与 evaluate（条目级）判定分叉；recover 分支（`group_defaults.go:270-283`）误放真未知组
- F8 candidate 救援（`group_multiplier_policy.go:73-80`）可绕过真未知从严
- F9 排序（`group_defaults.go:138-152` vs `catalog.go:1273-1320`）与计数两套口径
- F10/F11 前端 `=== 'blocked'` 单分支漏新状态；normalize 吞三态、Card fallback 污染三态值

**可修复**
- F12 备份/恢复丢标志：`GroupMultiplierKnown` `json:"-"` 导出即丢；`backup_extended_import.go:1632-1633` 恢复 0→1（免费分组变收费，已存在 bug）
- F13 `InitCache` 启动不重算；F14 `SyncAccount` 同步路径不触发重算（sub2api 平台永无 pricing）
- F15 计费兜底静默 1（`site_pricing.go:393-401`、`metrics.go:278-292`）→ relay_logs 需加状态快照列
- F16 `SiteChannelGroup` 视图无 known 字段 → 前端标注无数据源
- F17 手动设价（`SiteModelPriceManualUpsert`）0 归一 1 且不标 known

### 决策定稿（2026-08-09，用户拍板，方案正式简化为两态）

| 定义 | 用户决策 | 方案影响 |
|---|---|---|
| **D1'** | **全放行**：放弃「真未知拦下」档。所有没有分组倍率的分组一律按暂定 1x 放行 + 标注「未知」。cap 只拦「已知真值超限」 | 方案简化为**两态**（真值 / 暂定=未知全放行）。「从严」档取消 |
| **D2'** | **A'：候选倍率判定+展示都去掉**（模型价格千奇百怪难推算，不可靠的数不参与判定也不展示） | candidate 倍率退出判定与展示 → F8 消除，缓解 P1-4 性能问题 |
| **D3'** | **A：值 1 入列 + `multiplier_known=false` 标记**，与真值 1x 可区分 | 新增 `multiplier_known` 列 + 写路径同步维护 |
| **D4'** | **C：暂定按 1x 正常参与排序** | 排序层无需改状态分层，实现最省 |
| **D5'** | 真 0x 保持 known=true，暂定 1x 写入 known=false，两套写路径物理隔离 | 必修项：修复已存在的备份恢复 0→1 bug |

**两态最终规则（单一判定）**：
```
capEnabled && multiplier_known==true && multiplier > cap  →  blocked（拦下）
其余一切（known=false / multiplier==nil / 超限未知）      →  allowed + 标注「暂定/未知」
```

**简化点**：F1（补 1 需标 known=false 否则标注失效）；F2（唯一拦截状态仍是 blocked，无新状态名风险）；F7（组级/条目级判定天然收敛，可共用函数）；F8（candidate 整体移除）；F12（备份 0→1 修复，必修）。

**仍需处理**：F3 读路径 known 维度、F4 GORM 零值坑、F5 upsert、F6 同步重建搬运、F10/F11 前端标注、F13/F14 重算触发、F15 计费快照、F16 视图 known 字段、F17 手动设价 known。

## 最终实施计划（两态方案，2026-08-09 定稿）

> 目标：cap 只限分组倍率；两态（真值 known=true / 暂定=未知 known=false 全放行+标注「暂定/未知」）；candidate 倍率判定+展示全去；真 0x 保值。内网自用，中文硬编码文案。

### 阶段 1：存储与迁移（可独立验证）

1. `internal/model/site.go`（SiteUserGroup，361-382）：加 `MultiplierKnown` 列，用 `*bool`（nil=未迁移，显式 true=真值、false=暂定）。**三值语义锁定（修订 1 补充，逻辑对抗者）**：判定侧 nil 视同 false（不拦）；标注侧前端谓词统一用 `known !== true`（覆盖 nil）；**创建路径必须显式写 false**——`*bool` 无 F4 零值坑（F4 只对 `bool`+default 成立），「禁止写 false」的旧建议撤回，否则新建行 known=nil → 前端 undefined → 徽标永不显示（sub2api 平台系统性失效）。
2. `internal/db/migrate/025.go` + `025_test.go`：仿 024 模式（`RegisterAfterAutoMigration`）。**Version 必须 > 2026080304**（如 `2026081001`，不能直接用 25——现有最大 Version 是 `2026080304`，按数字升序执行）。AutoMigrate 加列；**回填用 raw_payload 自证（修订 1，替换原 `(multiplier IS NOT NULL)` 公式——该公式会把历史「补的 1x」误标真值导致系统性误拦）**：逐行 `multiplier IS NULL → false`；非空且 `storedSiteGroupMultiplier(raw_payload, group_key)` 解析出同值 → true；否则 false（无法证实标 false，真值在下次同步升级）。**注意：`storedSiteGroupMultiplier` 当前未导出（`site_group_multiplier.go:38`），migrate 包跨包调用需先导出或迁移内实现（逻辑对抗者）。**
3. 验收：`go test ./internal/db/migrate/...` 通过；新/老库升级后 `HasColumn(site_user_groups, multiplier_known)` 为真；回填后 `multiplier_known == (multiplier IS NOT NULL AND multiplier != 1 AND raw_payload 含该组同值倍率)`（v2 规则：multiplier==1 一律 false）；新增用例「S1 编造 1x 行（multiplier=1, raw_payload 无倍率）→ known=false」。

### 阶段 2：写路径三态化（不读列，行为不变，可独立验证）

4. `internal/sitesync/pricing.go:203-224`（parseSitePricingGroupMultipliers）：改返回携带 known。**S2 修正**：known 从**原始 group_ratio** 计算（先算 rawKnown 再补 1x），不能用补后 map 反推（当前是坏标记）。enable_groups 缺省补 1x（S1）→ 标 known=false（暂定）。
5. `internal/op/site_pricing.go:76-97`（SiteUserGroupMultipliersUpdate）：签名改携带 known（F5）；`Update` → upsert（修 0 行静默丢失）。
6. `internal/sitesync/storage.go:139-152` + `copyPersistedGroupSyncState`（265-287）：全删重建时搬运 `MultiplierKnown`（F6）。**修订 12 决策（已拍板，选 B）**：keep-block 只保留 `multiplier` **数值**，`known` 以 pricing 新快照为准——站点停发倍率时，同步 keep-block 保留旧值 5x，但 `known` 按新快照降为 false（暂定），**避免 keep-block 与 pricing 降级双写产生相反终态**；`copyPersistedGroupSyncState` 不搬 multiplier/known（现状即如此）。**keep-block 展示语义（逻辑对抗者缺口 3，P2）**：保留 5x 数值 + known=false 时前端显示「5x + 暂定徽标」——判定安全（known=false 不拦），标注语义待阶段 5 定：保留 5x+暂定标注，或回退显示 1x（实施阶段 5 时按 UI 可读性决定，记入发布说明）。
7. `internal/sitesync/sync_fetch.go:841-874`（mergeSiteGroups）、`http.go:626-676`（parseGroupCandidate/Object）：显式解析出倍率 → known=true。**V4 拍板（v2，已落版）**：`token.GroupMultiplier 非 nil → known=true`（sub2api token 携带倍率视为真值，作为 sub2api 迁移后被标 false 行的救援链）。
8. `internal/op/site_import.go`、`site_channel.go:591-625`、`sitesync/suspend.go`：创建行设 known=false。
9. **D5' 必修**：真 0x 保值——`preserveZeroGroupMultiplier`（site_pricing.go:29,60-67）保持 known=true 写 0；暂定 1x 写 1 + known=false；两套路径物理隔离。修备份恢复 0→1 bug（`backup_extended_import.go:1632-1633`）。
10. 验收：pricing 刷新后未设组=multiplier 1 + known=false；真 0x 组=multiplier 0 + known=true；同步重建后 flag 不丢（修订 12 语义：数值保留、known 随新快照）。~~老备份导入后免费分组仍为 0x~~（此验收引用备份 0→1 修复，该修复已随修订 7 后置，验收移至后置批次）。

### 阶段 3：读路径 + 判定（核心，两态规则生效）

11. `internal/op/channel.go:621-686`：`channelGroupMultiplierMap`/`persistedSiteGroupMultiplierMap` 改返回 `(value, known)` 结构（F3）。**raw_payload 读时兜底（673-684）关闭或加 known 闸门**（两态下未知放行，兜底只会掩盖"未知"标注，建议关闭）。
12. `internal/op/group_multiplier_policy.go:42-116`（evaluate）：
    - **D2' A'（修订 3 + 决策已拍板）**：删除 `channelCandidateMultiplierMap` 调用（`:57`）与 candidate 兜底分支（`:73-80`）。**只删调用，函数保留**——避免删函数导致 channel.go:521 / group_defaults.go:95 / group_sort.go:63 / catalog.go:1157 编译失败。**candidate 展示去留（已拍板，选 A 全去掉）**：`catalog.go:1157` 排序改按分组倍率（D4' C，无分组倍率按 1x）；`channel.go:521`（ChannelLLMList /v1/models）不再填充 candidate 倍率；`group_defaults.go:95`、`group_sort.go:63` 排序同步统一「无分组倍率按 1x」（修订 11，选 A）。`multiplier` 字段不再由 evaluate 填充。
    - **修订 4**：**显式删除 `:88-90` 的 `effectiveMultiplier == nil → blocked` 分支**——两态下 unknown/暂定一律 allowed（两态核心反转点，必须点名删除，不能只靠规则描述）。
    - 判定改两态：`capEnabled && known && value > cap → blocked`；其余 → allowed + 标注「暂定/未知」。`effectiveMultiplier` 语义 = 分组倍率（known 时）或 1（暂定）。
    - **修订 2**：`GroupItem`（`model/group.go`）加 `MultiplierKnown *bool \`gorm:"-"\``，由 `applyGroupItemMultiplierPolicies` 填充（`groupItemMultiplierPolicy` 结构体加 known 字段，与 policy 字段同源）——这是前端「暂定」徽标的后端数据源，缺它前端恒 undefined。`PolicyStatus` 增加 `"tentative"` 值（唯一确定值，不用「或复用 unknown」二选一）；`MultiplierSource` 增加 `"tentative"`。前端徽标谓词统一为 `known !== true` 或 `policy_status === 'tentative'`——**锁定一条（推荐 `known !== true`，覆盖 *bool nil）**。
13. `internal/op/group_defaults.go:243-285`（EnforceMultiplierCap）：与 evaluate 共用同一判定函数（F7）——`capEnabled && known && >cap` 才 policy_blocked；**recover 分支：known=false 一律解阻放行（两态语义，修订 8 + 修订 13②就地修订）**。`ApplyGroupDefaults` 计数（116-127）同步两态口径（ItemsBlocked 只计「known 超限」，删 Inf 计 blocked 分支）。
14. 验收：集成测试——已知 5x + cap=4 → blocked；暂定 1x + cap=4 → allowed + 标注；真未知（known=false）→ allowed；candidate 不再影响判定。

### 阶段 4：重算触发（F13/F14）

15. `internal/sitesync/core.go`（SyncAccount）：persistSyncSnapshot 后**无条件**调用 EnforceMultiplierCap（不依赖 pricing 成功与否；sub2api 平台同样生效）。
16. `internal/op/cache.go`（InitCache）：groupRefreshCache 后补一次 EnforceMultiplierCap。
17. 验收：sub2api 站点同步后超限组被标 policy_blocked；重启后状态与库一致。

### 阶段 5：API 与前端（标注「暂定/未知」）

18. `internal/model/site_channel.go:30-58` + `internal/op/site_channel.go:352-400`：SiteChannelGroup 视图加 `MultiplierKnown`（F16）。
19. 前端类型三处：`web/src/api/endpoints/group.ts`、`model-catalog.ts`、`site-channel.ts` 加 `multiplier_known?: boolean`；`policy_status` union 加 `'tentative'`。**normalize 保留 undefined**；**徽标与标注谓词统一为 `multiplier_known !== true`**（修订 2，覆盖 *bool nil——不能只写 `=== false`，否则 nil 漏标；老数据 undefined 也符合「非 true」判定，不误标）。
20. `web/src/components/modules/group/ItemList.tsx:154-175`：徽标改 switch——`blocked`→「倍率阻断」；`known !== true`→「暂定 1x」徽标（数据源为后端 GroupItem.MultiplierKnown 填充，见阶段 3 第 12 条修订 2）；真值 1x 显示普通 1x。**SelectedMember 接口与 Card.tsx:130 字段映射同步接线**（否则 TS 报错）。
21. `web/src/components/modules/group/Card.tsx:125-126`：**D2' A'（决策：candidate 展示全去）** 去掉两条 fallback——`item.multiplier ?? exactModelChannel?.multiplier` 与 `item.group_multiplier ?? exactModelChannel?.group_multiplier`；渠道倍率展示不再保留（`LLMChannel` 不填充 candidate 倍率，见阶段 3 第 12 条）。分组卡片倍率只显示 `group_multiplier` + `multiplier_known` 标注（「暂定 1x / 1x」）。
22. `web/src/components/modules/site-channel/index.tsx:704-729,1999-2043,2082-2086`：`formatGroupMultiplier` 输出「暂定 1x / 倍率未知 / 1x」；徽标加「暂定」分支；文案「真未知」与「超 cap 阻断」区分。文案**中文硬编码**（内网自用，跳过三语 locale）。
23. 验收：管理员打开分组卡片——暂定 1x 条目显示「暂定 1x」而非普通「1x」；真未知显示标注；超限显示「倍率阻断」。

### 阶段 6：计费快照（F15，可选后置）

24. relay_logs 加 `price_group_multiplier_state` 快照列（`metrics.go:484-501`、`images.go:626-643`），对账可区分「真值/暂定」计费。内网自用可后置。

### 阶段 7：测试收尾

25. 更新旧二态断言：`group_multiplier_policy_test.go`（T1：fixture 补 known=true 否则不再 blocked）、`site_pricing_test.go`（T2：真 0x fixture 置 known）、`storage_test.go`（T3：加 flag 保留断言）、`site_group_multiplier_test.go`（T4：raw 兜底语义收窄）、`pricing_test.go`（T5：known 判定改原始 group_ratio）。
26. 新增：迁移 025 回填测试；两态矩阵测试（known 真值×cap 开/关×值超限/不超限 + 暂定/未知×cap 开/关）；iterator 过滤测试（唯一拦截状态 blocked）；前端 normalize `!== true` 测试（v2：`=== false` 已废弃）；同步后 EnforceMultiplierCap 触发测试。

### 验收标准（整体）

- `GOPROXY=https://goproxy.cn,direct go test ./...` 全绿；`CI=true pnpm lint`、`tsc --noEmit`、`pnpm build` 通过。
- 手工：站点渠道页 → 分组卡片——真值显示具体倍率、暂定显示「暂定 1x」、超限显示「倍率阻断」；cap=0 时全部放行。
- 回归：cap 默认 0 时全站行为与改动前一致（唯一差异：暂定条目从「无标注」变为「暂定标注」）。

### 工作量评估

- 阶段 1-3（核心）：约 2-2.5 人天；阶段 4-5（触发+前端）：约 1 人天；阶段 6-7（快照+测试）：约 1 人天。合计约 4 人天（两态方案较三态节省约 1 人天）。

### 改动面全貌（按依赖顺序）

1. 存储：`site_user_groups` 加 `multiplier_known`（建议 `*bool` 或显式赋值规避 F4）；quote 侧不落状态列、读时 join SiteUserGroup（单一事实源）
2. 迁移：文件名 025、Version > 2026080304、AutoMigrate 加列 + 保守回填（同事务）
3. 写路径：三处补 1 改三态；`SiteUserGroupMultipliersUpdate` 改签名 + upsert；`storage.go` 保留清单加 flag；`sync_fetch.go`/`http.go` 显式解析置 known=true
4. 读路径：`channel.go:621-686` 改三态结构；raw_payload 兜底加 known 闸门或删除
5. 判定：evaluate 与 EnforceMultiplierCap 统一三态判定函数；真未知映射 `blocked`；recover 对 !known 保持 blocked
6. 重算触发：SyncAccount 无条件补 EnforceMultiplierCap；InitCache 启动补一次
7. 展示：SiteChannelGroup 视图加 known；前端类型三处加 `multiplier_known?: boolean`（normalize 保留 undefined，谓词 `!== true`）；徽标改 switch 支持暂定；Card fallback 加 known 门控
8. 备份/计费：known 进 export/import；relay_logs 加 `price_group_multiplier_state`；备份恢复 0→1 修复
9. 测试：更新旧二态断言（T1-T5）；新增三态矩阵/迁移回填/iterator 过滤/前端 normalize 测试

### 内网自用可省略项

三语 locale 全量补齐（中文硬编码够用）；site.ts/SiteUserGroup 站点模块展示；RouteTools 预览增强；无障碍强化。

### 最隐蔽的两个断链点

① `channel.go:673-684` raw_payload 读时兜底天然对冲三态语义，必须显式关闭或加 known 闸门；② `iterator.go:36` 只认 `"blocked"`，真未知不映射为 blocked 则静默放行。

---

## 对抗审查修订（2026-08-09，实施计划定稿前必改）

> 8 顾问对抗审查（反驳者/本质追问者/无情行者/后端/数据/逻辑/前端/性能——逻辑对抗者首轮派发失败，已补派成功）。以下修订覆盖原计划对应条目，**实施时以修订为准**。
>
> **落地状态**：修订 1-4 已落入原计划条目（阶段 1 第 1-3 条、阶段 3 第 12 条、阶段 5 第 19-20 条），**实施以落版后的原条目为准**；修订 5-10 仍以本章节为准。逻辑对抗者补派后新增发现见「修订 11-13（逻辑对抗者补充，待定）」。

### 修订 1（致命，6 顾问共识）：迁移回填公式与验收 —— ✅ 已落入阶段 1 第 1-3 条

- **原**：回填 `multiplier_known = (multiplier IS NOT NULL)` + 验收「全等」。
- **问题**：历史「补的 1x」（pricing.go:214-222 产生）非 NULL 会被标真值 → 迁移完成即把暂定组批量变成「真值」；两态下 known=true 且值>cap 会导致**系统性误拦**，方向与决策相反（多 false 只影响标注，多 true 才误拦）。
- **修订**：回填用 **raw_payload 自证**——逐行：`multiplier IS NULL → false`；非空且 `storedSiteGroupMultiplier(raw_payload, group_key)` 解析出**同值** → true；否则 false（无法证实标 false，下次同步升级）。
- **验收改**：`multiplier_known == (multiplier IS NOT NULL AND raw_payload 含同值倍率)`；新增用例「S1 编造 1x 行（multiplier=1, raw_payload 无倍率）→ known=false」。
- **逻辑对抗者补充（已并入落版）**：① `storedSiteGroupMultiplier` 未导出，migrate 包跨包调用需先导出或迁移内实现；② `*bool` 三值语义锁定——创建路径**必须显式写 false**（`*bool` 无 F4 零值坑，「禁止写 false」撤回），前端谓词用 `known !== true` 覆盖 nil；③ raw_payload 不可得时真实超限组被标 false → cap 豁免，记为已知方向并保证下次同步升级。

### 修订 2（致命，4 顾问共识）：前端「暂定」徽标必须补后端数据源 —— ✅ 已落入阶段 3 第 12 条 + 阶段 5 第 19-20 条

- **原**：仅前端类型加 `multiplier_known`，后端 GroupItem 无该字段 → 前端恒 undefined，徽标永不显示。
- **修订**：`internal/model/group.go` GroupItem 加 `MultiplierKnown *bool \`gorm:"-"\``，由 `applyGroupItemMultiplierPolicies` 填充（`groupItemMultiplierPolicy` 结构体加 known 字段，与 policy 字段同源）；`SelectedMember`/Card.tsx:130 映射同步接线。
- **落版修正（逻辑对抗者）**：取消「或复用 unknown」「徽标谓词二选一」等三处「或」——`PolicyStatus` 增加 `"tentative"` 唯一值；前端谓词锁定 `known !== true`（覆盖 *bool nil，仅写 `=== false` 会漏标）。

### 修订 3（致命，5 顾问共识）：candidate 删除范围写死——只删调用，不删函数 —— ✅ 已落入阶段 3 第 12 条

- **原**：第 12 条只点名删 evaluate 内调用（:57、:73-80），措辞易被误读为「删函数」。
- **修订**：**明确「删 evaluate 内调用，函数保留」**。`channelCandidateMultiplierMap`/`candidateMultiplierByCandidate` 仍有 4 个消费点存活：channel.go:521（ChannelLLMList 展示）、group_defaults.go:95（ApplyGroupDefaults 排序）、group_sort.go:63、catalog.go:1157（CatalogPlanGroup 排序）。删函数 → 4 处编译失败。
- **catalog.go:1157 每请求仍在执行**（性能对抗者）：D2' 声称「缓解 P1-4」只兑现 ~1/3（~15+2N → ~9+2N 条/请求）。排序改按分组倍率（D4' C）后 catalog.go:1157 的 candidate 查询可去——**标记为需用户确认**（连带 /v1/models 的 candidate 倍率展示去留）。

### 修订 4（致命，本质追问者）：两态核心反转点必须点名删除 —— ✅ 已落入阶段 3 第 12 条

- **原**：第 12 条描述了两态规则，但未点名删除 `group_multiplier_policy.go:88-90` 的 `effectiveMultiplier == nil → blocked` 分支。
- **修订**：第 12 条显式增加「**删除 :88-90 nil→blocked 分支**」——两态下 unknown/暂定一律 allowed。candidate 分支（:73-80）写了行号，这个核心反转点更必须写。

### 修订 5（P1，本质追问者）：pricing 提前返回必须处理

- **问题**：`parseSitePricingGroupMultipliers`（pricing.go:211-213）在 `parsePricingGroupRatios` 两处都为空时提前返回，enable_groups 补 1x 循环不执行 → 无 group_ratio 的站点（新 API 典型形态）暂定标记缺失。
- **修订**：第 4 条增加「删除/重构 211-213 提前返回，enable_groups 补 1x（标 known=false）必须无条件执行」。

### 修订 6（P1，后端对抗者）：known 搬运位置修正

- **原**：第 6 条指向 `storage.go:139-152` + `copyPersistedGroupSyncState`（265-287）。
- **修订**：known 搬运**必须在 `storage.go:138-145` 的 keep 块内**（与 multiplier 保留同处）：`snapshot.Multiplier==nil && existing.Multiplier!=nil` → 同时复制 known；`snapshot.Multiplier!=nil` → known=true。`copyPersistedGroupSyncState` 不碰 multiplier/known（现状即如此）。

### 修订 7（P1，3 顾问共识）：计费快照（第 24 条）整段后置或补全数据链

- **问题**：`EffectivePrice`（site_pricing.go:98-115）无 known 字段 → metrics.go/images.go 写不了状态 → 列建成即死列；且 relay_logs 在 SQLite 上**禁止新迁移 AutoMigrate**（db.go:124-136 排除 + 013.go OOM 警告），列靠 `ensureRelayLogColumnsSQLite` 自动 ADD。
- **修订**：**整段后置**（内网自用可接受）。若后置，`site_model_price_quotes` 的 `GroupMultiplierKnown` 保持 `gorm:"-"` 不持久化，避免双源分叉；`backup_extended_import.go:1632-1633` 的 0→1 修复（D5'）**随之后置**，但真 0x 保值（preserveZeroGroupMultiplier 路径）保留在阶段 2。

### 修订 8（P1，数据对抗者）：第 13 条 recover 措辞修正

- **原**：「recover 分支只对『不再超限的真值』解除，对 known=false 不误放」——三态残留措辞。
- **修订**：改为「known=false 一律解阻放行（两态语义）」。否则 known=false 组永远保持 policy_blocked 且不可恢复。

### 修订 9（P1，性能/反驳者）：EnforceMultiplierCap 重算降冗余

- **问题**：SyncAccount 每账号同步后无条件全表扫（N 账号=N 次全表扫），且与 site_pricing.go:95-96 既有触发叠加（一次同步最多 3 次全表扫）；SELECT 拉全列含 raw_payload。
- **修订**：`EnforceMultiplierCap(ctx, accountID)` 限定账号（WHERE site_account_id=?）；persistSyncSnapshot 内存 diff（倍率/known 有变化才触发）；SELECT 精简为 `id, multiplier, multiplier_known, policy_blocked, policy_block_reason`（不拉 raw_payload）；cap 关闭且无 blocked 行时提前返回。

### 修订 10（P2，多顾问）：杂项修正

- 第 8 条（site_import/site_channel/suspend 创建行）：**创建路径显式写 `MultiplierKnown: &false`**（v2 修正——`*bool` 无 F4 零值坑，F4 只对 `bool`+default 成立；「禁止写 false」的旧建议已废弃，见阶段 1 v2 章节 V2）。否则新建行 known=nil → 前端 undefined → 徽标永不显示。
- 第 9 条：新字段 json tag 必须显式 `json:"multiplier_known,omitempty"`（不能 `"-"`），否则 `backup.go:1480` 导出丢 known。
- 前端 `formatGroupMultiplier` 签名改 `(value, known)` 时，**6 个调用点全改**（index.tsx:1998/2043/2211/2259/2318/2486），不只 2 个区域。
- `getGroupStatusBadge` 分支插入顺序显式定义：暂定分支插在「沿用历史」之后、「待补全」之前。
- site-channel.ts normalize：文件内既有 `=== true` 惯例，新字段**必须保留 undefined**，**谓词统一为 `member.multiplier_known !== true`**（v2 修正——`=== false` 会漏标 nil，已废弃）。
- `member-sort.ts:19` 排序（`group_multiplier ?? multiplier ?? Inf`）：candidate 删除后无分组倍率条目排末尾，行为变化**显式声明**为有意（D2'）。
- token 填值路径 `site_channel.go:192-194`（视图 GroupMultiplier 为 nil 时用 token.GroupMultiplier 填充）：**token 来源倍率 known 语义已拍板（v2 V4）**：`token.GroupMultiplier 非 nil → known=true`（sub2api 迁移后被标 false 的救援链，写入阶段 2 第 7 条正文）。
- 迁移「同事务」在 MySQL 下不成立（DDL 隐式提交）：AutoMigrate 与回填分两阶段，回填失败可重入；SQL 侧过滤一律 `COALESCE(multiplier_known, false)`（`*bool` 三值逻辑）。
- 文档同步：task_plan.md 顶部 Decisions 第 3 条「未知倍率在严格策略下排除」、docs/group-multiplier-policy-analysis.md 相关承诺需按「全放行」修订。

### 修订后工作量（无情行者）

- 修订前 4 人天 → **修订后约 6-7 人天**（阶段 3 读路径签名波及 4 个消费点 + LLMChannel；阶段 5 GroupItem 断链必须解决；阶段 6 整段后置可省 1-1.5 天）。最小可行裁剪：阶段 6 后置 + 修订 9 部分简化后约 4.5-5 人天。

### 修订后开工判定

**可开工，但必须先落修订 1-4**（迁移回填公式、前端数据源、candidate 删调用不删函数、88-90 分支点名删除）——这四项是 P1 级自相矛盾或结构性缺口，改文字即可，不需重设计。修订 5-10 随实施推进处理。

### 修订 11-13（逻辑对抗者补派后新增，2026-08-09）—— ✅ 已全部拍板并落入原条目

> 逻辑对抗者补派成功。核心判定规则经 18 象限真值表穷举**无意外拦截**（幸存）；以下为新发现的逻辑缺口，已按用户拍板落版，**实施以落版后的原条目为准**：

- **修订 11（P1，排序三套口径未收敛）—— 已拍板选 A（统一按 1x）**：D4' C「暂定按 1x 排序」只处理了 catalog.go:1157，但 `group_defaults.go:119`、`group_sort.go:95`（后端）与 `member-sort.ts:19`（前端）三处仍用 candidate fallback。**落版**：阶段 3 第 12 条 + 阶段 5 第 21 条——三处统一「无分组倍率一律按 1x」，candidate 倍率不再参与排序（含 /v1/models 展示）。
- **修订 12（P1，storage keep-block 与 pricing 降级双写竞态）—— 已拍板选 B**：站点停发 group_ratio 时——路径甲 pricing 先跑补 1x known=false（放行）、路径乙 persistSyncSnapshot 后跑 keep-block 复制旧 5/true（拦下），同一上游条件得到相反终态，无顺序保证。**落版**：阶段 2 第 6 条——keep-block 只保留 multiplier 数值，known 以 pricing 新快照为准，避免双写相反终态。
- **修订 13（P2，文档就地矛盾）—— 已全部就地修订**：① 阶段 2 第 10 条验收已移除「老备份导入后免费分组仍为 0x」（随修订 7 后置）；② 阶段 3 第 13 条正文已改「known=false 一律解阻放行」；③ 阶段 3 第 12 条已收口 channel.go:521 / 两个排序点的 candidate 去留（选 A 全去掉）。

### 修订后开工判定（更新）

**全部修订（1-13）已落版，可开工**。修订 5-10 已并入对应条目或在实施推进中处理；修订 11-13 已按用户拍板落版。阶段 1 的解析器导出已按 v2 方案落地为 `model.StoredSiteGroupMultiplier`（见「阶段 1 详细实现方案 v2」改动 B'），不再待确认。剩余实施细节不阻塞开工。


---


## 阶段 1 详细实现方案 v2（2026-08-09，吸收实施前审查 6 顾问发现后修订，待复审）

> 首轮实施前审查发现 4 处必改问题（含方案级错误），v2 已全部吸收。**v2 复审通过后实施。**
> 首轮审查结论（6 顾问）：循环依赖（4 顾问独立发现，方案级错误）、修订1/修订10 落版矛盾（4 顾问）、回填跨源缺陷（3 顾问）、sub2api 救援链断裂（逻辑对抗者）。

### 首轮审查发现与 v2 修正对照

| # | 首轮发现 | 严重级 | v2 修正 |
|---|---|---|---|
| V1 | **循环依赖**：migrate(025 import op) → op(import db) → db(import migrate) 闭环，go build 必失败。原方案「无循环依赖已验证」只查了 op→migrate 一跳，漏了经 internal/db 的中转环 | 致命 | **解析器整体移入 `internal/model` 叶子包**（model 不依赖 op/db/migrate），op 与 migrate 都调 `model.StoredSiteGroupMultiplier`（见改动 B'） |
| V2 | **修订 1 vs 修订 10 落版矛盾**：task_plan.md:103「必须显式写 false」vs 修订 10「禁止写 false」——互斥；修订 10 的 F4 论证错误（F4 只对 bool+default 成立，*bool 写 &false 无零值坑） | 致命 | **统一为「创建路径显式写 `&false`」**，删除修订 10 的禁令与 `=== false` 谓词；前端谓词全链路锁 `known !== true` |
| V3 | **回填跨源缺陷**：raw_payload 只由 groups 接口写入（sync_fetch.go:61/133-135、anyrouter.go:267/579、site_import.go:715）；multiplier 列由 pricing(SiteUserGroupMultipliersUpdate 只写列)/token(mergeSiteGroups)/keep-block(storage.go:139-142) 写入——不同来源。自证对 sync 同源行是重言式；对 pricing 来源行必然 false → 系统性 cap 豁免（规模远大于「raw_payload 不可得」边角）；且编造 1x 可能因 payload 巧合(rate/ratio 字段碰撞)被标 true | 重伤 | 回填加保守规则：**`*multiplier == 1 一律 false`**（S1 编造集中在 1x，真 1x 标 false 无害、靠下次同步升级）；「pricing 来源被标 false」升格为预期主结果写进验收与发布说明；补跨源用例 E/F |
| V4 | **sub2api 救援链断裂**：修订 10 对 token 来源倍率 known 语义标「待定」，sub2api 迁移后真实超限组标 false 后无救援路径，「下次同步升级」落空 | 重伤 | **拍板：token.GroupMultiplier 非 nil → known=true**（写进阶段 2 第 7 条，非阶段 1 范围，仅记录） |
| V5 | 测试断言缺陷：用例 D 无法区分「值不匹配」与「解析失败」；用例 B payload 未排除 5 字段名；缺 0x/跨源用例 | 重伤 | v2 测试设计：用例 D 加正向控制断言；用例 B payload 显式排除字段；补 0x、E、F 用例 |
| V6 | 迁移失败 = 服务停机（非「静默放行」），比方案假定更安全 | 可修复 | 文档改「升级失败 = 服务不可用直到修复」 |
| V7 | 025.go 文件名与时间戳 Version 混排制造未来执行顺序陷阱（仓库惯例：时间戳 Version 配描述性文件名） | 可修复 | 文件名改为 `site_group_multiplier_known.go`（Version 2026081001 不变） |

### 改动清单（5 处）

**改动 A：`internal/model/site.go` — SiteUserGroup 加持久化列**

在 `Multiplier`（366 行）之后插入：

```go
// MultiplierKnown 标记倍率来源可信度：nil=未知（未迁移或创建未设置，判定侧视同 false），
// true=真实倍率（上游 group_ratio / token / payload 自证），false=暂定 1x 或未知。
MultiplierKnown *bool `json:"multiplier_known,omitempty"`
```

- json tag `multiplier_known,omitempty`（不能 `-`，backup 导出需携带）。
- `*bool` 不设 gorm default；**所有 Create 路径显式写 `&false`**（V2：统一修订 1，删修订 10 禁令；F4 只对 bool+default 成立，`*bool` 写 `&false` 无零值坑）。
- 主 AutoMigrate（db.go:137）自动加列，SQLite AddColumn 安全路径（db.go:65,224-226 确认不触发 recreateTable）。

**改动 B'：解析器移入 `internal/model`（消除循环依赖，V1）**

- 新建 `internal/model/site_group_multiplier.go`：把 `internal/op/site_group_multiplier.go` 的 `storedSiteGroupMultiplier` + 6 个辅助函数（`findStoredSiteGroupMultiplier`、`storedSiteGroupItemMatches`、`storedSiteGroupMultiplierValue`、`storedSiteGroupNumber`、`storedSiteGroupScalar`、`sameStoredSiteGroupKey`）**连同 3 个包级 var slices（`storedGroupMultiplierFields`/`storedGroupIdentityFields`/`storedGroupPayloadWrappers`）**整体迁入，导出为 `StoredSiteGroupMultiplier(rawPayload, groupKey string) (float64, bool)`。model 包内已用 `model.NormalizeSiteGroupKey` → 改同包调用。
- 删除 `internal/op/site_group_multiplier.go` 原文件。
- 改调用点：`internal/op/channel.go:681` → `model.StoredSiteGroupMultiplier(...)`。
- **测试迁移（v2 复审修正，后端对抗者）**：`internal/op/site_group_multiplier_test.go` **拆分成两个**——`TestStoredSiteGroupMultiplier`（纯解析器测试）移到 `internal/model/site_group_multiplier_test.go`（调用名改导出）；`TestPersistedSiteGroupMultiplierMapFallsBackToRawPayload` **留在 op 包**（依赖 `setupCatalogProvisionTest`/`mustCreatePricingRow`/`persistedSiteGroupMultiplierMap`/`siteAccountGroupKey`/`dbpkg`，整体迁移会编译失败）。
- 依赖验证（v2 复审修正证据）：model 包依赖 `internal/transformer/outbound`（model/channel.go:8、model/site.go:11），**不是「仅 stdlib」**；但 transformer 子树不 import op/db/migrate（grep 无回边）→ 无环成立：migrate → model ✓，op → model ✓，op → db → migrate 中 migrate 不再依赖 op ✓。
- 已知权衡（逻辑对抗者 V3 相关）：同一解析器阶段 3 读路径将关闭、迁移回填仍信任它——「raw_payload 自证真值」的唯一定义，逻辑单一来源；v2 回填保守规则（V3）已缓解其启发式误判面。

**改动 C'：`internal/db/migrate/site_group_multiplier_known.go` — 新迁移（Version 2026081001，文件名对齐惯例 V7）**

```go
package migrate

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 2026081001, // 必须 > 现有最大 2026080304
		Up:      migrateSiteGroupMultiplierKnown,
	})
}

func migrateSiteGroupMultiplierKnown(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.SiteUserGroup{}) {
		return nil
	}
	if err := db.AutoMigrate(&model.SiteUserGroup{}); err != nil { // 幂等保险（主 AutoMigrate 已加列）
		return fmt.Errorf("auto migrate site_user_groups: %w", err)
	}
	var groups []model.SiteUserGroup
	// v2 复审修正（数据对抗者）：只回填 multiplier_known IS NULL 的行——
	// 避免重跑回退阶段 2 已置 known=true 的升级态（pricing/token 来源行不自证）。
	if err := db.Select("id", "group_key", "multiplier", "raw_payload").Where("multiplier_known IS NULL").Find(&groups).Error; err != nil {
		return fmt.Errorf("query site_user_groups: %w", err)
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for i := range groups {
			g := &groups[i]
			known := false
			// v2 保守规则（V3）：multiplier==1 一律 false（S1 编造集中 1x，真 1x 无害靠同步升级）；
			// 非 1 值且 raw_payload 自证同值 → true。
			if g.Multiplier != nil && *g.Multiplier != 1 {
				if v, ok := model.StoredSiteGroupMultiplier(g.RawPayload, g.GroupKey); ok && v == *g.Multiplier {
					known = true
				}
			}
			if err := tx.Model(&model.SiteUserGroup{}).Where("id = ?", g.ID).Update("multiplier_known", known).Error; err != nil {
				return fmt.Errorf("update group %d multiplier_known: %w", g.ID, err)
			}
		}
		return nil
	})
}
```

- **回填只处理 `multiplier_known IS NULL` 的行**（v2 复审修正，数据对抗者）：首次迁移全表命中；重跑（DELETE records 后）只补新 NULL 行，**不回退阶段 2 已置 known=true 的升级态**。验收 4 的「值不变」由此天然满足。
- 单列 `Update("multiplier_known", known)`：GORM map 路径不做零值省略，false 正确落库（多顾问确认）。
- 事务内回填：中途失败整体回滚，migrate 框架记 Failed → 下次启动重跑，确定性重算天然幂等。
- **单行解析失败不阻塞**：`model.StoredSiteGroupMultiplier` 对无效 JSON/坏数据返回 (0,false)，不 panic（Go encoding/json 有 10000 深度上限保护；坏 payload 单行标 false 继续，不整体失败——V6：迁移失败=服务停机，需尽量不让单行坏数据阻塞启动）。用例 H 验证此路径。
- **回填后全表无 NULL**（迁移时刻）；部署后阶段 2 落地前新写入行可能为 nil（判定侧 nil 视同 false，正确），文档记录。

**改动 D'：`internal/db/migrate/site_group_multiplier_known_test.go` — 回填测试**

legacy 结构（无 multiplier_known 列）+ `TableName()="site_user_groups"`；测试数据用 legacy 结构 `db.Create` 插入（**给 SiteAccountID 赋值**，not null；**group_key 互不相同**——迁移内 AutoMigrate 会建 `idx_site_account_group` 唯一索引）；断言行存活（迁移前后行数一致）。

用例（v2，吸收 V3/V5）：

| # | multiplier | raw_payload | 期望 known | 正向控制断言 |
|---|---|---|---|---|
| A 真值自证 | 5 | `{"data":{"groups":[{"group_key":"vip","rate_multiplier":5}]}}` | true | `StoredSiteGroupMultiplier(payload,"vip")==(5,true)` |
| B S1 编造 1x | 1 | `{"data":{"groups":[{"group_key":"std"}]}}`（**显式不含 5 个倍率字段名**：rate_multiplier/group_multiplier/multiplier/ratio/rate） | false | `StoredSiteGroupMultiplier(...)==(0,false)` |
| C 无倍率 | nil | 任意合法 JSON | false | — |
| D 值不匹配 | 2 | `{"data":{"groups":[{"group_key":"gold","rate_multiplier":3}]}}` | false | **正向控制**：`StoredSiteGroupMultiplier(payload,"gold")==(3,true)`（证明 false 是值不匹配而非解析失败） |
| E 真值+payload 缺该组 | 5 | `{"data":{"groups":[{"group_key":"other"}]}}` | false | `StoredSiteGroupMultiplier(payload,"vip2")==(0,false)`（跨源形态：pricing 写 5、groups payload 无该组） |
| F payload 空 | 5 | `` | false | — |
| G 真 0x | 0 | `{"data":{"groups":[{"group_key":"free","rate_multiplier":0}]}}` | true（D5' 保值） | `StoredSiteGroupMultiplier(...)==(0,true)` |
| H 坏 payload | 5 | `{invalid json`（畸形结构，验证单行不阻塞） | false（不 panic） | `StoredSiteGroupMultiplier(...)==(0,false)` |

断言：`HasColumn(site_user_groups, multiplier_known)` + 迁移前后行数一致 + 逐行 multiplier_known 值正确。

### 验收标准（阶段 1 v2）

1. `GOPROXY=https://goproxy.cn,direct go test ./internal/db/migrate/... ./internal/model/... ./internal/op/...` 全绿。
2. 新库：`go build -o /tmp/octopus ./cmd && /tmp/octopus start` 启动跑 025；`sqlite3 data/data.db "SELECT version,status FROM migration_records WHERE version=2026081001;"` 记录成功；`PRAGMA table_info(site_user_groups)` 有 multiplier_known 列。
3. 老库升级：旧二进制建库造数 → 新二进制启动（migrate 框架自动跑 025）→ `SELECT group_key,multiplier,multiplier_known FROM site_user_groups` 回填符合 v2 规则（**pricing 来源行标 false 是预期主结果**，非缺陷——记入发布说明）。
4. 幂等：`DELETE FROM migration_records WHERE version=2026081001;` 后重启，025 重跑，值不变且 records 重新成功。
5. `go build ./...` 通过（解析器移 model 后无残留旧调用；gofmt -l 干净）。
6. **发布说明（写入 README 或 docs）**：回填后 pricing/token 来源真值行标 false → cap 豁免，靠阶段 2 写路径（pricing 刷新置 known=true / token 倍率置 known=true，见 V4 拍板）在下次同步升级；阶段 2 落地前新建行 known=nil（判定正确，列缺省）。

### 已知权衡（v2，实施时接受）

- **回填保守方向**：无法自证的真值标 false（cap 豁免），编造 1x 永不标 true（v2 的 `*multiplier==1 → false` 规则）；反向误判（编造被标 true）仅剩「非 1 值 + payload 巧合同值」低概率场景。
- **known=true 的定义** = 「可再解析同值」，非「独立双源证实」（sync 同源行是重言式）——阶段 3/5 标注语义按此理解。
- 迁移失败 = 服务停机直到修复（V6 认知修正，重试安全）。

---

## 阶段 2 详细实现方案（2026-08-09，实施前定稿，待顾问审查）

> 范围：写路径三态化（task_plan.md 阶段 2 第 4-10 条 + 修订 5/12 + V4）。目标：让 `multiplier_known` 列在所有写路径上被正确维护——解决阶段 1 遗留的「回填值被同步全删重建抹除」问题（4 顾问实施后审查一致发现）。阶段 2 实施后，同步/定价/导入/投影/暂停兜底建组全部携带 known，列数据持续有效。
> 前提说明：storage.go 的 keep-block 按修订 12（已拍板选 B）**不搬运 known**——新建行 known 由同步解析路径（sync_fetch/http）设置或保持 nil，pricing upsert 后来覆盖，即「known 以 pricing 新快照为准」。

### 改动清单（9 处）

**改动 A：`internal/op/site_pricing.go` 新增传输结构体**

```go
// GroupMultiplierValue 携带分组倍率值与其来源可信度（known=true=真实上报，false=暂定/缺省补）。
type GroupMultiplierValue struct {
	Value float64
	Known bool
}
```

放 op 包（sitesync 已 import op）；model 包保持纯数据模型。

**改动 B：`internal/sitesync/pricing.go` parseSitePricingGroupMultipliers（203-224）**

- 返回类型改 `map[string]op.GroupMultiplierValue`。
- **S2 修正（坏标记修复）**：known 从**原始 `parsePricingGroupRatios` 结果**计算——只有 group_ratio 明确出现的 key 标 known=true；enable_groups 补 1x 的 key 标 known=false。
- **修订 5（删提前返回）**：删除 211-213 的 `if len(result)==0 { return result }`——无 group_ratio 时 enable_groups 补 1x（known=false）仍须执行，否则无 group_ratio 站点（新 API 典型形态）拿不到暂定标记。

```go
func parseSitePricingGroupMultipliers(payload map[string]any) map[string]op.GroupMultiplierValue {
	result := make(map[string]op.GroupMultiplierValue)
	if payload == nil {
		return result
	}
	// 原始 group_ratio（rawKnown）
	for key, v := range parsePricingGroupRatios(payload["group_ratio"]) {
		result[model.NormalizeSiteGroupKey(key)] = op.GroupMultiplierValue{Value: v, Known: true}
	}
	if len(result) == 0 {
		for key, v := range parsePricingGroupRatios(nestedValue(payload, "data", "group_ratio")) {
			result[model.NormalizeSiteGroupKey(key)] = op.GroupMultiplierValue{Value: v, Known: true}
		}
	}
	// enable_groups 缺省补 1x（known=false，暂定）——无条件执行（修订 5，删提前返回）
	for _, item := range sitePricingItems(payload) {
		for _, group := range normalizeStringList(item["enable_groups"]) {
			groupKey := model.NormalizeSiteGroupKey(group)
			if _, configured := result[groupKey]; !configured {
				result[groupKey] = op.GroupMultiplierValue{Value: 1, Known: false}
			}
		}
	}
	return result
}
```

- 调用点同步：pricing.go:40（SiteUserGroupMultipliersUpdate 新签名）、pricing.go:71（parseSitePricingQuotes 内 `groupRatios[groupKey]` 取结构体 `Value`/`Known`，155-158 的坏标记逻辑删除——quote 的 GroupMultiplierKnown 直接取结构体 Known）。

**改动 C：`internal/op/site_pricing.go` SiteUserGroupMultipliersUpdate（76-97）**

- 签名：`func SiteUserGroupMultipliersUpdate(ctx context.Context, accountID int, multipliers map[string]GroupMultiplierValue) error`。
- `Update` → **upsert**（修 0 行静默丢失 F5）：`OnConflict(Columns: site_account_id+group_key, DoUpdates: multiplier+multiplier_known)`；Create 行带 `Name: NormalizeSiteGroupName` 兜底、`Multiplier`/`MultiplierKnown` 指针（局部 var 取地址）。
- 末尾 `EnforceMultiplierCap` 保留（pricing 刷新后重算策略状态，P0-3 关联）。
- 真 0x 保值（D5' 前半）：pricing 来源真 0x 写 `Value:0, Known:true` → upsert 落 multiplier=0 + known=true → 计费 preserve 路径（site_pricing.go:29,60-67）自然工作。

**改动 D：`internal/sitesync/sync_fetch.go` mergeSiteGroups（841-874）— V4 拍板**

- token.GroupMultiplier 非 nil 时：填 multiplier **同时置 known=true**（token 携带倍率视为真值，sub2api 迁移后被标 false 行的救援链）。

```go
if existing.Multiplier == nil && token.GroupMultiplier != nil {
	multiplier := *token.GroupMultiplier
	existing.Multiplier = &multiplier
	known := true
	existing.MultiplierKnown = &known
	merged[key] = existing
}
// 新建 token 组同理
```

**改动 E：`internal/sitesync/http.go` parseGroupObject（676-709）/parseGroupCandidate map 分支**

- 解析出 multiplier 非 nil → 置 known=true（groups 接口显式提供的倍率 = 真值）。

```go
multiplier := parseOptionalSiteGroupMultiplier(item["rate_multiplier"], ...)
group := model.SiteUserGroup{GroupKey: ..., Name: ..., Multiplier: multiplier}
if multiplier != nil {
	known := true
	group.MultiplierKnown = &known
}
```

**改动 F：`internal/op/site_import.go` prepareMetAPIImportedGroups（707-728）**

- 创建行 `MultiplierKnown: &false`（导入的组丢弃 multiplier，显式 false 保持「回填后无 NULL」不变式）。

**改动 G：`internal/op/site_channel.go` UpdateSiteGroupProjection 创建行（611-619）**

- `row` 加 `MultiplierKnown: &false`（该行仅 ProjectionDisabled，无倍率）。

**改动 H：`internal/sitesync/suspend.go` ensureStaleAccountGroups buildRow（36-48）**

- 创建行加 `MultiplierKnown: &false`。

**改动 I：storage.go — 不改（明确记录）**

- keep-block（139-142）只保留 multiplier 数值、不搬 known = 修订 12「known 以新快照为准」语义（新行 known 由 D/E 设置或 nil，pricing 的 C 后来 upsert 覆盖）。copyPersistedGroupSyncState 不碰 multiplier/known（现状即如此，保持）。

### 测试改动

| 测试 | 改动 |
|---|---|
| `internal/sitesync/pricing_test.go` | parseSitePricingGroupMultipliers 返回类型改 → 断言改；**新增**：无 group_ratio + enable_groups 列出 → 补 1x 且 known=false（修订 5）；group_ratio 有值 → known=true；两者同 key → group_ratio 优先 known=true |
| `internal/op/site_pricing_test.go` | SiteUserGroupMultipliersUpdate 签名改 → 调用改；**新增**：行不存在时 upsert 创建成功（F5 修复验证）；known 正确落库 |
| `internal/sitesync/storage_test.go` | 同步重建后 multiplier_known 保留断言（T3——keep-block 不搬 known，数值保留、known 由同步解析路径设置） |
| sync 相关（mergeSiteGroups 测试如存在） | token 倍率 → known=true 断言（V4） |

### 验收标准（阶段 2）

1. `go build ./...` + `go test ./internal/sitesync/... ./internal/op/... ./internal/db/migrate/...` 全绿。
2. pricing 刷新：group_ratio 有值 → 列 multiplier=真实值 + known=true；enable_groups 缺省 → multiplier=1 + known=false（无 group_ratio 站点也补，修订 5）。
3. 同步重建后：token 倍率组 known=true（V4）；groups 接口解析组 known=true（E）；keep-block 保留数值组 known 不误搬（修订 12）。
4. 导入/投影/暂停兜底建组：known=false（F/G/H）。
5. **阶段 1 遗留问题解决验证**：迁移回填后跑一次完整同步，multiplier_known 不再被抹成 NULL（D/E 已置位 + C 覆盖）。
6. 真 0x：pricing 真 0x 组 multiplier=0 + known=true。

### 已知边界（实施时接受）

- quote 表 GroupMultiplierKnown 保持 `gorm:"-"` 不持久化（修订 7，阶段 6 计费快照时再处理）；S3 归一化（site_pricing.go:129-131）暂不改（两态下「未知按 1x 计费」为预期行为）。
- 备份恢复 0→1 修复（backup_extended_import.go:1632-1633）随修订 7 后置阶段 6，本阶段不做。
- 阶段 2 结束时列在同步路径上「有值即正确」；阶段 3 读路径启用前无消费者（安全）。

---

## 阶段 2 方案审查修订 v2（2026-08-09，吸收 6 顾问审查 + 用户拍板，实施以此为准）

> 6 顾问审查发现：upsert 无条件覆盖吞真值（5 顾问击穿，逻辑判致命）、keep-block 矛盾（数据/逻辑）、阶段 2 窗口误拦（4 顾问）、V4 token 启发式标 true 风险（3 顾问）、&false 非法语法（后端）、pricing_test 断言反转未点名（无情行者/后端）、backup.go 第 6 条写路径（无情行者）、幽灵行（逻辑）。用户拍板：EnforceMultiplierCap 顺带改 known-aware。
> **v2 实施时覆盖原方案对应改动 A-I，改动以 v2 为准。**

### v2 修正对照

| # | 原方案 | 问题（顾问） | v2 修正 |
|---|---|---|---|
| X1 | C: upsert 无条件 DoUpdates multiplier+known | **enable_groups 补 1x/known=false 覆盖 known=true 真值（groups 自证/迁移回填/token/keep-block 全被吞）→ cap 漏拦 + 真值丢失**（5 顾问） | **known=true 保护**：SiteUserGroupMultipliersUpdate 分两路——known=true（group_ratio 证据）→ upsert 覆盖+创建；known=false（enable_groups 缺省补）→ **条件 UPDATE 只改已存在且 `multiplier_known IS NULL OR =false` 的行，不覆盖 true、不创建行**（顺带缓解幽灵行） |
| X2 | I: storage.go 不改（keep-block 不搬 known） | keep-block 行 known=nil 与「无 NULL 不变式」「修订 12 降 false」矛盾（数据/逻辑）；「5x+暂定」展示不可达 | **keep-block 分支保留数值时置 `MultiplierKnown=&false`**（显式，收口无 NULL + 修订 12 原意；known=false 不覆盖 multiplier 后「5x+暂定」展示可达） |
| X3 | 判定阶段 3 才改 | 阶段 2 窗口 keep-block 5x/false 行被旧 EnforceMultiplierCap 按「5x>cap」误拦，与修订 12 拍板矛盾（4 顾问） | **用户拍板：EnforceMultiplierCap 顺带改 known-aware**（阶段 2 内）：isBlocked 加 `MultiplierKnown!=nil && *MultiplierKnown` 条件；recover 对非 true 解阻（现有 recover 非 isBlocked 即解阻，加条件后自动成立） |
| X4 | D: token 倍率非 nil → known=true | 字段启发式（ratio/rate 碰撞）标 true 送判定 → 误拦，与 D2' 保守原则冲突（3 顾问） | **护栏：`*token.GroupMultiplier != 1` 才标 true**（==1 与阶段 1 迁移规则对齐，真 1x 标 false 无害） |
| X5 | E: groups 接口解析出 multiplier → known=true | 同 X4 字段碰撞面 | **护栏：`*multiplier != 1` 才标 true**（与 X4 统一规则） |
| X6 | F/G/H: `MultiplierKnown: &false` | **`&false` 非法 Go 语法（不能对字面量取地址）→ 编译失败**（后端） | **局部 var `known := false; MultiplierKnown: &known`**（或 op 包 helper `boolPtr`） |
| X7 | 写路径枚举 5 条 | backup.go:755 是第 6 条 Create 写路径（备份恢复），未覆盖（无情行者） | **backup.go:755 备份恢复创建行加 `MultiplierKnown: &false`** |
| X8 | C: Create 分支只点名 Name | Create 必须设 SiteAccountID（not null），漏了事务回滚（后端） | **Create 分支显式设 `SiteAccountID`/`GroupKey`/`Name`/非 nil known 指针** |
| X9 | B: 155-158 直接取结构体 | 隐式 default 组（enable_groups 空）取零值 {0,false} → quote 走 0 依赖 S3（后端） | **保留 ok-check**：`gv, ok := groupRatios[groupKey]; if !ok { gv = {Value:1, Known:false} }` |
| X10 | 测试表未点名反转 | pricing_test.go:158 enable_groups-only 组 known 从 true 翻 false，断言必挂且修复方向有二义（无情行者/后端/数据） | **显式点名**：pricing_test.go:134-162 断言反转（GPT-Plus-正价 known=false、GPT-Pro known=true）；sub2api_test.go:625 mergeSiteGroups known 断言；storage T3 措辞「known 由快照决定/不搬运」 |
| X11 | 未声明 | upsert 复活 sync 已删行（幽灵行，逻辑 #4）；pricing 200 空 body 批量写 1x（反驳者 #6） | **声明为已知边界**：known=true 的 pricing-only 行由 upsert 创建、下次同步全删重建清除（无 binding/无路由影响）；200 空 body 时 enable_groups 补 1x/false 因 X1 不覆盖 true，真值受保护 |

### v2 改动清单（实施顺序）

1. **op 包 `GroupMultiplierValue{Value,Known}` 结构体**（改动 A 不变）
2. **pricing.go parseSitePricingGroupMultipliers**：返回 `map[string]op.GroupMultiplierValue`；删 211-213 提前返回；known 从原始 group_ratio 算（S2）；enable_groups 补 1x known=false（改动 B）
3. **pricing.go parseSitePricingQuotes 155-158**：ok-check 取结构体（改动 B2/X9）
4. **site_pricing.go SiteUserGroupMultipliersUpdate**：签名改；known=true → upsert（覆盖+创建，Create 带 SiteAccountID/Name/非 nil 指针）；known=false → 条件 UPDATE 不覆盖 true 不创建（改动 C/X1/X8）
5. **group_defaults.go EnforceMultiplierCap**：isBlocked 加 known 条件（改动 C2/X3，用户拍板）
6. **sync_fetch.go mergeSiteGroups**：token 倍率 != 1 → known=true（改动 D/X4）
7. **http.go parseGroupObject/parseGroupCandidate**：解析出 multiplier != 1 → known=true（改动 E/X5）
8. **site_import.go / site_channel.go / suspend.go / backup.go**：创建行 known=&false 局部 var（改动 F/G/H/H2/X6/X7）
9. **storage.go keep-block 分支**：保留 multiplier 数值时置 known=&false（改动 I/X2）
10. **测试**：pricing_test.go 反转+新用例、site_pricing_test.go 签名+known 落库+不覆盖 true、storage_test.go T3 措辞+keep-block 终态、sub2api_test.go known 断言、新增「pricing 不覆盖已知 true」用例

### v2 验收标准（在原验收基础上）

1. build + `go test ./internal/sitesync/... ./internal/op/... ./internal/db/migrate/...` 全绿。
2. pricing 刷新：group_ratio 有值 → known=true（覆盖）；enable_groups 缺省 → 已存在非 true 行写 1/false，**不覆盖 known=true 行**。
3. 同步重建后：token/groups 解析倍率（≠1）行 known=true；keep-block 行 multiplier 保留 + known=false。
4. **EnforceMultiplierCap known-aware**：known=true 5x + cap=4 → blocked；known=false 5x + cap=4 → 放行（用户拍板项）。
5. 导入/投影/暂停/备份恢复建组：known=false。
6. 迁移回填值不再被同步抹成 NULL（keep-block 行落 false、解析行落 true/false 而非 nil）。
7. 真 0x：multiplier=0 + known=true（preserve 链完整）。

### v2 已知边界

- 幽灵行（pricing known=true 创建、下次同步清除）；pricing 200 空 body（X1 保护真值）；keep-block「5x+暂定」展示（X2 后可达）；阶段 2 结束列有值即正确（无读消费者，安全）。

---

## 阶段 2 完成记录（2026-08-09）

> 阶段 2（写路径三态化）+ 用户拍板「提前 evaluate 两态化」全部完成。全量测试 29 包 EXIT=0，gofmt/diff-check 干净。

### 已完成改动汇总

**写路径三态化（阶段 2 v2 方案 A-I + X1-X11）**
- op 包新增 `GroupMultiplierValue{Value,Known}`（site_pricing.go）
- `SiteUserGroupMultipliersUpdate` 分两路：known=true → upsert（覆盖+创建 pricing-only 行）；known=false → 条件 UPDATE 不覆盖 known=true、不创建（X1 保护）
- `EnforceMultiplierCap` known-aware（用户拍板提前落地）
- pricing 解析：返回结构体、删提前返回（修订 5）、known 从原始 group_ratio 算（S2 修正）、ok-check（X9）
- mergeSiteGroups/http：token/groups 解析倍率 !=1 → known=true（V4/X4/X5 护栏）
- 创建路径（导入/投影/暂停/备份恢复）known=&false（X6/X7）
- keep-block 保留数值时 known=&false（X2）

**提前 evaluate 两态化（用户拍板，消除 N1/N2/N3）**
- channel.go 读路径返回 `SiteUserGroupMultiplierValue{Value,Known}`；raw_payload 读时兜底关闭
- evaluate 两态化：删 candidate 参与判定（D2' A'）、删 nil→blocked、判定 `capEnabled && known && value>cap`
- GroupItem.MultiplierKnown 字段（无条件写，false 也落 API）
- group_defaults/group_sort：删 candidate 排序、ItemsBlocked 只计 known 超限、排序「known=true 真实值否则 1x」
- SyncAccount persist 后无条件 EnforceMultiplierCap（sub2api 平台也生效）
- 查询失败告警（fail-open 可观测，避免 DB 抖动误拦 503）

### 实施后审查结论（2 轮共 6 顾问）

- **首轮（4 顾问）**：N1 致命（组级两态 vs 路由级三态分叉）+ N2（sub2api 解阻不可达）+ N3（ItemsBlocked 口径）→ 用户拍板「提前 evaluate 两态化」解决
- **复审（2 顾问）**：N1/N2/N3 确认消除（判定同构、触发链闭合、口径对齐）；修复 fail-open 静默（加告警）+ MultiplierKnown 无条件写
- **记录为已知边界（阶段 5 处理）**：`multiplier` 字段恒 nil（candidate 移除，route preview 该字段空）；known=false 行 status=allowed + effectiveMultiplier 保留真值（「5x+暂定」展示，排序按 1x 双口径）；site_channel 视图直接读 DB 列 vs 分组列表读路径的口径差异

### 阶段 2 验收结果

- `go build ./...` ✅；`go test ./...` 29 包 EXIT=0 ✅；gofmt ✅；git diff --check ✅
- pricing 刷新：group_ratio 有值 → known=true；enable_groups 缺省 → known=false（不覆盖 true）✅（X1 保护有测试）
- 同步重建：token/groups 解析 !=1 → known=true；keep-block → 数值保留 + known=false ✅
- EnforceMultiplierCap：known=true 5x+cap=4 → blocked；known=false 5x → 放行 ✅（用户拍板项）
- 创建路径 known=false；真 0x multiplier=0 + known=true（preserve 链完整）✅

### 剩余阶段（未开始）

阶段 3（读路径+判定主体已随「提前 evaluate 两态化」完成大部分）；阶段 4 第 16 条（InitCache 启动重算，可选后置）；阶段 5（API/前端标注「暂定/未知」）；阶段 6（计费快照，后置）；阶段 7（测试收尾）。

---

## 阶段 5 详细实现方案（2026-08-09，实施前定稿，待顾问审查）

> 范围：API 与前端标注「暂定/未知」（task_plan.md 阶段 5 第 18-23 条 + 修订 2/10 落版）。目标：让管理员在分组卡片/站点渠道面板上区分「真值倍率」「暂定倍率（known=false）」「倍率未知」。文案中文硬编码（内网自用，跳过三语 locale）。

### 前置事实（已核实）

- **后端阶段 2 已让 `applyGroupItemMultiplierPolicies` 无条件写 `GroupItem.MultiplierKnown`**（true/false 都写）——新后端响应 GroupItem 一定有该字段；undefined 仅存在于升级过渡瞬间（老后端无此字段）。
- **evaluate 现状**：known=true 有分组倍率 → group_multiplier 填真实值 + known=true；known=false 有分组倍率（keep-block）→ group_multiplier 填保留值 + known=false；无分组倍率 → group_multiplier=nil + effectiveMultiplier=1 + status=unknown + known=false。
- **site-channel 视图（SiteChannelGroup）目前无 MultiplierKnown**——需后端新增（第 18 条）。
- 前端谓词决策（修订 2 落版 + 阶段 2 后端无条件写字段后）：**统一 `multiplier_known !== true`**。undefined（老数据/过渡期）→ 显示「暂定」是**保守正确方向**（不冒充真值）；`=== false` 会漏标 DB NULL 行（nil），更糟。

### 改动清单（8 处）

**改动 A：后端视图加字段（2 文件）**

1. `internal/model/site_channel.go`（SiteChannelGroup，33 行后）：
```go
GroupMultiplier  *float64  `json:"group_multiplier,omitempty"`
MultiplierKnown  *bool     `json:"multiplier_known,omitempty"`
```
2. `internal/op/site_channel.go` newSiteChannelGroupView（380 行附近）：
```go
GroupMultiplier:  group.Multiplier,
MultiplierKnown:  group.MultiplierKnown,
```

**改动 B：前端类型三处加 `multiplier_known?: boolean`（3 文件）**

3. `web/src/api/endpoints/group.ts` GroupItem 加 `multiplier_known?: boolean`；policy_status union 加 `'tentative'`（预防性，实际后端输出 allowed/blocked/unknown）。
4. `web/src/api/endpoints/model-catalog.ts` RouteDecisionReason 同样加（multiplier_known?: boolean + 'tentative'）。
5. `web/src/api/endpoints/site-channel.ts`：
   - `SiteChannelGroup` 加 `multiplier_known?: boolean`（77-105 行，group_multiplier 后）
   - `normalizeSiteChannelAccount`（296 行附近）：**保留 undefined**——spread `...group` 已透传，无需 ?? false；为明确加一行 `multiplier_known: group.multiplier_known`（undefined 透传，不兜底）。

**改动 C：ItemList.tsx 徽标 switch + SelectedMember 接线（1 文件）**

6. `SelectedMember` 接口加 `multiplier_known?: boolean | null`（45 行后）。
7. 倍率徽标区（169-175 行）改为三态：
```tsx
{member.policy_status === 'blocked' && (
    <Badge variant="destructive" ... title={member.policy_reason || undefined}>倍率阻断</Badge>
)}
{member.multiplier_known !== true && member.group_multiplier != null && (
    <Badge variant="secondary" ...>暂定 {formatMultiplier(member.group_multiplier)}</Badge>
)}
{member.multiplier_known !== true && member.group_multiplier == null && (
    <Badge variant="secondary" ...>暂定 1x</Badge>
)}
{member.multiplier_known === true && member.group_multiplier != null && (
    <Badge variant="secondary" ...>{formatMultiplier(member.group_multiplier)}</Badge>
)}
```
（blocked 徽标保留在 154-158 行现状；倍率徽标条件从 `group_multiplier != null` 改为按 known 分三态。）

**改动 D：Card.tsx 去 fallback + 接线（1 文件）**

8. `displayMembers` 映射（125-126 行）：**去掉两条 fallback**（D2' A'）：
```ts
multiplier: item.multiplier,                    // 后端已不填 candidate（恒 nil），不再 fallback 渠道值
group_multiplier: item.group_multiplier,        // 不再 fallback exactModelChannel
```
并加 `multiplier_known: item.multiplier_known,`（接线 SelectedMember）。

**改动 E：site-channel/index.tsx 标注（1 文件）**

9. `formatGroupMultiplier(value, known)`（704-707 行）改签名：
```ts
function formatGroupMultiplier(value: number | null | undefined, known?: boolean): string {
    if (value == null || !Number.isFinite(value)) return '倍率未知';
    return known ? `${Math.round(value * 100) / 100}x` : `暂定 ${Math.round(value * 100) / 100}x`;
}
```
10. **6 个调用点全改**（1998/2043/2211/2259/2318/2486）传 `group.multiplier_known`（或 activeGroup?.multiplier_known）。
11. `getGroupStatusBadge`（709-729 行）加暂定分支，**插入顺序**：blocked+暂停 → blocked → 暂停 → **暂定（group.multiplier_known !== true 且 has keys 或任意）** → 待建 → 沿用 → 待补全：
```ts
if (group.multiplier_known !== true) {
    return { label: '暂定', className: 'rounded-full bg-sky-500/15 px-1.5 py-0.5 text-[10px] text-sky-700 dark:text-sky-300' };
}
```
（放暂停之后、待建之前——倍率可信度优先于运营状态。）

**改动 F：site-channel 面板倍率展示（1 文件，E 内）**

12. 下拉行 `· ${formatGroupMultiplier(group.group_multiplier)}`（1998 行）→ 传 known；banner（2043）`activeGroupPolicyReason` 逻辑保持（policy_blocked 时显示 reason，否则倍率）。

### 验收标准（阶段 5）

1. `go build ./...` + `go test ./...`（后端视图改动）全绿。
2. 前端：`CI=true pnpm lint`、`pnpm exec tsc --noEmit --incremental false`、`pnpm build` 通过（无 TS 错误：SelectedMember/Card 接线、formatGroupMultiplier 6 调用点签名）。
3. 手工（内网管理员）：分组卡片——known=true 条目显示「Nx」；keep-block 组显示「暂定 Nx」；无分组倍率条目显示「暂定 1x」；blocked 显示「倍率阻断」。站点渠道面板——暂定组显示「暂定」徽标；超限组「倍率阻断」。
4. 老数据兼容：undefined multiplier_known → 显示「暂定」（保守方向，不冒充真值）——验收记录此行为。

### 已知边界

- 升级过渡期老后端无 multiplier_known → 前端显示「暂定」（可接受，前后端同版本部署）。
- 文案中文硬编码（跳过 locale）；log 面板（price_group_multiplier 真实路由倍率）不改。

---

## 阶段 5 方案审查修正 v2（2026-08-09，吸收 3 顾问审查，实施以此为准）

> 3 顾问审查（前端对抗者/后端对抗者/反驳者）发现 5 个实现级必改点 + 1 个待用户拍板项。v2 覆盖原方案对应改动，实施以 v2 为准。

### v2 修正对照

| # | 原方案 | 问题（顾问） | v2 修正 |
|---|---|---|---|
| Y1 | 改动 A 只加 newSiteChannelGroupView | **token 填值路径（site_channel.go:192-194）只填 GroupMultiplier 不填 known** → token 真值倍率显示「暂定」（3 顾问一致，恰在 sub2api 平台） | **token 兜底分支同步置 known**：`if multiplier != 1 { known := true; group.MultiplierKnown = &known }`（对齐 X4） |
| Y2 | 改动 C 徽标三态 | **ItemList 外层守卫（161 行 `balance != null || group_multiplier != null`）漏 `multiplier_known !== true`** → 「暂定 1x」在 balance=null 时不可达（前端对抗者） | 守卫加 `\|\| member.multiplier_known !== true` |
| Y3 | 改动 E getGroupStatusBadge 暂定插「暂停」后 | **暂定遮蔽「待建」运营状态**（前端对抗者/反驳者：无 key 新组显示「暂定」而非「待建」） | **分支顺序改**：blocked+暂停 → blocked → 暂停 → 待建 → 沿用 → 待补全 → **暂定（限定 has_keys）**（暂定最后，仅对已有 key 的组显示，不遮蔽待建） |
| Y4 | model-catalog.ts 加 multiplier_known | **RouteDecisionReason 后端无该字段 → 前端恒 undefined 死字段**（反驳者） | **后端 routing.go RouteDecisionReason 加 `MultiplierKnown *bool` + catalog.go 填充**（与 GroupItem 同源） |
| Y5 | 声明 member-sort 按 1x | **实际 Infinity 排末尾，与声明不符**（前端对抗者/反驳者） | **member-sort.ts 改代码**：`group_multiplier ?? multiplier ?? 1`（known 缺失按 1x，与后端修订 11 对齐） |
| Y6 | 待定 | **「暂定 Nx」双语义**：keep-block 保留旧值 5x（仍按 5x 计费）vs 缺省补 1x（按 1x 计费）同用「暂定 Nx」格式（反驳者） | **待用户拍板**（见下） |

### 幸存确认（顾问验证通过）

- 谓词 `!== true`：normalize 加 `?? false` 无行为差别（伪风险）；undefined 显示暂定是保守方向
- formatGroupMultiplier 6 调用点全对（value 均为 SiteChannelGroup 对象，known 可取）
- blocked + 暂定不双显（后端 blocked 时 known=true）
- 后端视图字段类型匹配、无 position literal、无测试破坏

### Y6 落版（2026-08-09，用户拍板「区分两种语义」）

- **ItemList 徽标 tooltip 区分**：`known !== true && group_multiplier != null` → 显示「暂定 Nx」+ tooltip「保留站点旧值 Nx，仍按 Nx 计费」；`known !== true && group_multiplier == null` → 显示「暂定 1x」+ tooltip「站点未设倍率，按 1x 计费」。
- **site-channel 面板 formatGroupMultiplier 加 tooltip/title**：同理区分（有值=保留旧值按 Nx 计费，无值=按 1x 计费）。
- 判定依据：group_multiplier 有值（keep-block 保留旧值）vs 无值（缺省补 1x）。

---

## 阶段 5 完成记录（2026-08-09）

> 阶段 5（API 与前端标注「暂定/未知」）完成。后端 build/test 29 包 EXIT=0；前端 tsc/lint/build 全绿；member-sort 单元测试 3/3 过。

### 已完成改动

**后端**
- `SiteChannelGroup` 视图加 `MultiplierKnown`（model/site_channel.go + newSiteChannelGroupView）
- token 兜底分支同步 known（!=1 置 true，V4/X4 对齐）
- `RouteDecisionReason` 加 `MultiplierKnown` + catalog.go 填充（Y4，route preview 可用）

**前端**
- 类型三处：group.ts/model-catalog.ts 加 multiplier_known + 'tentative'；site-channel.ts SiteChannelGroup 加字段 + normalize 显式透传（保留 undefined）
- ItemList 徽标三态：倍率阻断 / 暂定 Nx（tooltip「保留站点旧值，实际计费以站点报价为准」）/ 暂定 1x（tooltip「站点未再上报倍率，当前按 1x 处理」）/ 普通 Nx；**site_id 守卫**（非站点成员不标暂定，F1 修复）；外层守卫同步
- Card 去两条 fallback（D2' A'）+ 接线 multiplier_known
- site-channel：formatGroupMultiplier(value, known) 双参 + 6 调用点；getGroupStatusBadge 暂定分支（has_keys && known!==true，插在待补全后，不遮蔽待建）
- member-sort：getMultiplier「known===true 用值否则 1x」（与后端修订 11 对齐）+ 单元测试更新（F3 修复）

### 实施后审查（3 顾问）修复项

- **F1**（反驳者/前端对抗者）：非站点渠道成员被标「暂定 1x」（语义错误）→ 加 `member.site_id != null` 守卫
- **F2**（逻辑对抗者）：tooltip「仍按此计费」不可证明（计费走 pricing quote 与 site_user_groups 脱钩）→ 改不确定语气「实际计费以站点报价为准」
- **F3**（前端对抗者）：member-sort.test.mjs 悬空断言（无 test script 不触发）→ 更新为新语义 + 手动跑 3/3 过

### 已知边界（记录，非缺陷）

- catalog.go:1157 candidate 排序未收口（路由选路内部行为，与卡片展示排序分离）——记录为修订 11 的遗留，后续阶段处理
- 英文 reason 直出（multiplier exceeds cap 等）——发布说明明示
- site-channel「暂定」徽标与「倍率未知」文本粒度差异——管理员可接受的展示层差异
- 升级后已知真值行标 false → cap 暂失效（靠下次同步/刷新恢复）——发布说明需落地（docs 目录当前 0 命中，待补）
- 发布说明未落地（docs 无 multiplier_known/暂定/cap 豁免条目）——记录为阶段 7 收尾项

---

## 后置项实现方案（2026-08-09，阶段 4 第 16 条 + 阶段 6，实施前定稿，待顾问审查）

> 范围：① 阶段 4 第 16 条（InitCache 启动重算）；② 阶段 6（relay_logs 计费快照，完整链路）。按同样流程：方案 → 审查 → 实施 → 实施后审查。

### 后置项 1：InitCache 启动重算（阶段 4 第 16 条）

**改动 1（cache.go InitCache）**：groupRefreshCache 后补 EnforceMultiplierCap：

```go
// 阶段 4 第 16 条：启动后重算倍率策略状态——重启/外部改库后恢复 policy_blocked 一致性。
if _, _, err := EnforceMultiplierCap(ctx); err != nil {
    return fmt.Errorf("enforce multiplier cap error: %v", err)
}
```

位置：`groupRefreshCache(ctx)` 之后（group 缓存就绪）、`CatalogSync` 之前。ctx 是 CacheInitTimeout——全表扫小表（每账号几十行）量级可接受；失败返回 error → 启动失败（与迁移失败语义一致，fail-closed）。

### 后置项 2：relay_logs 计费快照（阶段 6，四段传播）

> 阶段 2 实施后审查（后端对抗者）已指出：EffectivePrice 无 known 字段 → metrics.go/images.go 写日志点无从取得状态 → 加列即成死列。本方案打通完整链路。quote known 持久化是唯一可行路径（site-wide quote 无 account 可 join，读时 join 不可行——数据对抗者建议的 join 方案在计费层不适用）。

**改动 2（model/site_pricing.go）**：`SiteModelPriceQuote.GroupMultiplierKnown` 去 `gorm:"-"`（**json 保持 `json:"-"` 不暴露 API**，仅落库）：
```go
GroupMultiplierKnown bool `json:"-"`  // 持久化：阶段 6 计费快照需在读取后恢复 known
```
- quote 生成时（parseSitePricingQuotes）已算好 gv.Known（阶段 2 改动 B2），落库保留、读取恢复。
- 老库升级后老 quote 行 known=NULL → 读取 false（保守，不冒充真值）。

**改动 3（op/site_pricing.go SiteModelPriceQuotesUpsert）**：AssignmentColumns 加 `"group_multiplier_known"`——否则 upsert 刷新不更新该列，known 漂移（站点停发后 quote 刷新 known=false 无法写入）。

**改动 4（model/site_pricing.go EffectivePrice）**：加字段：
```go
GroupMultiplierKnown bool `json:"group_multiplier_known,omitempty"`
```

**改动 5（op/site_pricing.go 三个填充点）**：
- `effectivePriceFromQuote`：`GroupMultiplierKnown: quote.GroupMultiplierKnown`（DB 读取后已从列恢复）
- `effectivePriceFromGlobal`：`GroupMultiplierKnown: false`（全局价非站点分组倍率，非真值）
- 兜底（`no matching site or global price`）：`GroupMultiplierKnown: false`

**改动 6（model/log.go RelayLog）**：加快照列（持久化主结构，140 行区）：
```go
PriceGroupMultiplierKnown *bool `json:"price_group_multiplier_known,omitempty"`
```
- 三值语义：nil=无价格/未知（EffectivePrice==nil 时不写）、true=真值计费、false=暂定计费。对账可区分「真值 vs 暂定 vs 无价格」。

**改动 7（metrics.go + images.go 写日志点）**：在 `PriceGroupMultiplier` 赋值后补：
```go
known := m.EffectivePrice.GroupMultiplierKnown
relayLog.PriceGroupMultiplierKnown = &known
```
（两处各加 2 行。）

**改动 8（relay_logs 列）**：**禁止新迁移对 RelayLog 跑 AutoMigrate**（db.go:124-136 已排除 + 013.go SQLite recreateTable OOM 警告）——新列由 `ensureRelayLogColumnsSQLite`（db.go:201-232）启动时自动 AddColumn，零额外代码。

### 验收标准

1. `go build ./...` + `go test ./...` 全绿。
2. InitCache 启动后 policy_blocked 与库一致（重启后状态不陈旧）。
3. relay_logs 表启动后自动含 `price_group_multiplier_known` 列（ensureRelayLogColumnsSQLite 生效）。
4. 单元/集成：EffectivePrice 构造处 GroupMultiplierKnown 正确传递（quote/global/兜底三路径）；写日志点 relayLog.PriceGroupMultiplierKnown 非 nil（EffectivePrice 非 nil 时）。
5. quote upsert 刷新后 known 不漂移（AssignmentColumns 含该列）。

### 已知边界

- 老 quote 行 known=NULL → 快照 false（保守，不冒充真值）。
- quote known 持久化后 json 保持 "-"，不改变现有 API 输出。
- 对账口径：`price_group_multiplier_known IS NOT NULL` 过滤 + 区分 true/false；nil 行（无价格/失败请求）不计入倍率统计。

---

## 后置项方案审查修正 v2（2026-08-09，吸收 3 顾问审查，实施以此为准）

> 3 顾问审查（后端/数据/逻辑对抗者）一致发现 3 个必改点 + 多个记录项。v2 覆盖原方案对应改动，实施以 v2 为准。

### v2 修正对照

| # | 原方案 | 问题（3 顾问一致） | v2 修正 |
|---|---|---|---|
| Z1 | 改动 2：quote known json 保持 "-" | **json:"-" → 备份导出（writeZipTable JSON 编码）丢列 → 恢复后 quote known 全 false**（F12 对 quote 复发，与修订 10 对 groups 的要求矛盾） | **json tag 改 `json:"group_multiplier_known,omitempty"`**（对齐修订 10；内网自用 API 暴露真实字段可接受）。需检查 quote 是否经 API 暴露（如 SiteModelPriceQuoteList）——暴露则前端多一字段（无害） |
| Z2 | 改动 6/7：nil=无价格 | **无价格行（EffectivePriceForCandidate 返回 Source=Unknown 非 nil）写 false 而非 nil** → 对账 IS NOT NULL 过滤把无价格计入「暂定计费」（3 顾问一致） | **写日志点补 Source 守卫**：`if m.EffectivePrice != nil && m.EffectivePrice.Source != model.PriceQuoteSourceUnknown` 才写 PriceGroupMultiplierKnown；对账口径改 `price_source NOT IN ('unknown') AND price_group_multiplier_known IS NOT NULL` |
| Z3 | 改动 5：effectivePriceFromGlobal 填 false | 全局价非「暂定倍率」（无分组倍率 GroupMultiplier=1）——语义错配（数据/逻辑对抗者） | **保留 false 但语义重新定义**：relay 快照三值 = nil（未解析到价格，Source unknown/EffectivePrice nil）、true（分组倍率真值计费）、false（非分组倍率真值：全局价/暂定缺省）。对账按此口径，发布说明明示 |
| Z4 | 遗漏 | manual upsert（F17）不设 known → 手动真值 5x 标 false | **SiteModelPriceManualUpsert 设 known=true**（管理员显式设价=真值）——顺手修 F17 |
| Z5 | 遗漏 | relay 侧 inline global 构造点（metrics.go:278/images.go:433）靠零值 false 碰巧正确 | **加注释声明**（降级到全局价时 known 归 false 是有意），验收补断言 |
| Z6 | 改动 1 | InitCache 重算后 groupCache 仍持旧 policy_blocked（外部改库场景） | **重算放 groupRefreshCache 之前**（settings 就绪后、group 缓存构建前），或重算后补 groupRefreshCache——选前者（重算写库 → 缓存按新库构建） |

### 记录项（发布说明已知边界，非缺陷）

- **X1 vs 修订 12 竞态**（逻辑对抗者 R1）：经时序分析——persist(keep-block 降 false) → Enforce(放行) → pricing(X1 条件 UPDATE 覆盖 false 行) → 终态一致放行（5x/false 或 1x/false）。「pricing 成功保持 5x/true」仅当 groups 接口仍在报倍率（keep-block 不触发）——那是「上游仍在报」非「停发」。两路径对应不同上游行为，非竞态。记录为边界。
- **quote known vs groups known 双源**：不同概念（计费可信度 vs 组倍率可信度），各自单一来源；对账用 quote known（计费实际乘数），UI 用 groups known。分叉记录为发布说明条目。
- **老 quote 无回填**：升级后历史 quote known=false（保守），靠下次 pricing 刷新恢复；sub2api 平台 quote（manual 来源）known=true（Z4 后）。
- **快照值与 UI 展示值解耦**（keep-block 5x 显示 vs quote 1x 计费）——发布说明明示。

### v2 验收补充

- 写日志点：Source=Unknown 行 PriceGroupMultiplierKnown=nil（不写）；quote/global/兜底三路径 + relay 侧 inline global 两路径断言。
- manual quote 落库 known=true（Z4）。
- 备份 roundtrip：导出含 group_multiplier_known、恢复后保留（Z1）。
- InitCache：重算在 groupRefreshCache 前（Z6）。

---

## 后置项完成记录（2026-08-09）

> 后置项 1（InitCache 启动重算）+ 后置项 2（relay_logs 计费快照）完成。全量测试 29 包 EXIT=0，gofmt/diff-check 干净。

### 已完成改动

**后置项 1：InitCache 启动重算**
- cache.go：settings 就绪后、groupRefreshCache 前执行 EnforceMultiplierCap（Z6）；失败降级 Warn 继续启动（实施后审查 B3，避免大实例超时启动失败）

**后置项 2：relay_logs 计费快照（四段传播）**
- quote.GroupMultiplierKnown 去 gorm:"-" 持久化（json:"group_multiplier_known,omitempty"，Z1 备份 roundtrip 携带）
- upsert AssignmentColumns 加 group_multiplier_known（站点停发倍率后 known 不漂移）
- EffectivePrice.GroupMultiplierKnown + 三填充点（quote/global/兜底，改动5）
- RelayLog.PriceGroupMultiplierKnown *bool（三值：nil=未解析到价格、true=真值、false=非分组倍率真值/暂定）
- metrics.go/images.go 写日志点 Source!=unknown 守卫（Z2，unknown 无价行不写 known 保持 nil）
- manual upsert known=true（Z4，修 F17）
- 备份恢复 0→1 修复（B1：known=true 的 0x 保留，与运行时 preserve-zero 一致）

### 实施后审查（2 顾问）修复

- **B1**（后端/数据对抗者一致，重伤）：备份恢复 normalizePriceQuoteBackup 无条件 0→1，会把免费组恢复成 (1x,true) 静默错误真值 → 改 `0 && !known` 才归一
- **B3**（后端对抗者，重伤）：EnforceMultiplierCap 进启动关键路径，超时=整体启动失败 → 失败降级 Warn
- B4（低）：db_test.go 未显式断言新列补列——记录为后续加固项

### 记录为已知边界（发布说明）

- **B2**：known 不透传到 usage facts（长期计费分析层）——阶段 6 范围是 relay_logs 快照；usage facts 透传留给后续（内网自用 relay_logs 已是审计主数据）
- **B5**：`''`（EffectivePrice==nil）/`'unknown'`（Source unknown）双编码盲区——对账口径需 `source IN ('') OR 'unknown'` 前置排除
- **两套 known 分叉**（quote vs groups）：不同概念（计费可信度 vs 组倍率可信度），对账用 quote known、UI 用 groups known，无一致性校验（记录）
- sub2api 平台老 quote 无自动恢复路径（无 pricing 刷新），靠手动重设（Z4）——发布说明明示

### 方案整体完成度

两态倍率方案全部阶段完成：阶段 1（迁移）→ 阶段 2（写路径+判定）→ 阶段 3（读路径，随阶段 2）→ 阶段 4 第 15/16 条（重算触发）→ 阶段 5（前端标注）→ 阶段 6（计费快照）→ 阶段 7 剩余（发布说明落地、测试收口）。

---

## 阶段 7 收尾方案（2026-08-09，实施前定稿，待顾问审查）

> 范围：① 发布说明落地（已知边界写进 README + analysis doc）；② 测试收口（两态矩阵 / iterator 过滤 / keep-block known 降级 / 同步触发核对）。

### 发布说明（载体 + 内容）

**载体**：`README.md` + `README_zh.md` 各加「倍率上限（Multiplier Cap）已知边界」小节（用户可见）；`docs/group-multiplier-policy-analysis.md` 末尾加「Implementation Notes（实施说明与已知边界）」（技术详细版）。

**内容**（汇总全部阶段审查记录的已知边界）：
1. **升级后 cap 暂失效**：迁移 025 保守回填（multiplier==1 → false、无法自证 → false）→ pricing/token 来源真值行标 false → cap 豁免；靠下次同步/定价刷新恢复（sub2api 平台无 pricing，靠 token 倍率 V4）。
2. **cap 只限分组倍率（key 倍率）**，不纳入模型倍率——模型按次/分项收费口径无法统一。
3. **「暂定」徽标两种语义**：保留站点旧值（仍按旧值计费）/ 站点未设（按 1x）——tooltip 已区分。
4. **两套 known 分叉**：计费快照（quote known）vs UI 展示（groups known）——对账用 relay_logs 的 price_group_multiplier_known，UI 用 groups 的 multiplier_known。
5. **relay_logs 计费快照三值语义**：nil=未解析到价格、true=真值、false=非分组倍率真值/暂定；对账口径 `price_source NOT IN ('unknown','') AND price_group_multiplier_known IS NOT NULL`。
6. **备份 roundtrip**：quote known 已携带（json 暴露）；known=true 的 0x 免费组恢复时保留（B1 修复）。
7. **排序口径**：卡片/自动排序按「known=true 用真实值否则 1x」；路由选路（catalog）仍按候选倍率——两处口径分离属设计。
8. **英文阻断原因**（multiplier exceeds cap 等）直出中文界面，暂不翻译。

### 测试收口

1. **两态矩阵测试**（`group_multiplier_policy_test.go` 新增 `TestTwoStateMultiplierCapMatrix`）：复用 setupCatalogProvisionTest 模式，覆盖：
   - known=true + cap=4 + value=6 → blocked
   - known=false + cap=4 + value=6 → allowed（keep-block 形态）
   - known=false（无倍率）+ cap=4 → allowed（unknown）
   - known=true + cap=4 + value=2 → allowed（不超限）
   - cap=0（关闭）+ known=true + value=6 → allowed
   （对每个 case 断言 evaluateGroupItemMultiplierPolicies 输出 status。）
2. **iterator blocked 过滤测试**（`internal/relay/balancer/iterator_test.go` 新建）：纯内存构造 GroupItem（blocked + allowed + unknown），断言 `NewIteratorWithPreference` 的 eligible 列表排除 blocked。
3. **keep-block known 降级测试**（`storage_test.go` 新增 `TestPersistSyncSnapshotKeepBlockDemotesKnown`）：旧行 multiplier=5+known=true、新快照无倍率 → keep-block 保留 5 + known 降 false（阶段 2 X2 落版的回归锁定）。
4. **同步后 EnforceMultiplierCap 触发**：core.go 改动已在阶段 2 实施（persist 后无条件调用）；EnforceMultiplierCap 触发语义已有 T1 覆盖——**记录为已覆盖，不新增集成测试**（内网自用成本考量）。
5. **前端 normalize `!== true`**：member-sort.test.mjs 已覆盖 known 排序语义；ItemList 三态徽标是组件层；前端无 test script（package.json 仅 dev/build/start/lint）——**记录为「组件层语义已覆盖」，不新增 normalize 单测**（需 TS 编译环境，成本高）。

### 验收标准

1. 发布说明：README.md + README_zh.md + analysis doc 三处落地，含上述 8 条边界。
2. 新增测试：两态矩阵 5 case + iterator 过滤 + keep-block 降级——`go test ./internal/op/... ./internal/relay/balancer/... ./internal/sitesync/...` 全绿。
3. 全量 `go test ./...` EXIT=0；gofmt / git diff --check 干净。

### 已覆盖核对（不新增）

- 迁移回填测试（025_test A-H 用例）✅ 阶段 1
- pricing 断言反转 + known（pricing_test）✅ 阶段 2
- 真 0x 持久化（site_pricing_test）✅ 阶段 2
- EnforceMultiplierCap known-aware 拦截（T1）✅ 阶段 2
- 同步后触发（core.go 逻辑 + T1 触发语义）✅ 记录
- 前端 normalize `!== true`（组件层语义）✅ 记录

---

## 阶段 7 方案审查修正 v2（2026-08-09，吸收 3 顾问审查，实施以此为准）

> 3 顾问审查（反驳者/本质追问者/后端对抗者）发现发布说明 3 处误导、载体分层需求、测试 4 处必改、2 处「已覆盖」措辞偷换。v2 覆盖原方案，实施以 v2 为准。

### v2 修正对照

| # | 原方案 | 问题 | v2 修正 |
|---|---|---|---|
| W1 | case 3 断言 allowed | 无分组倍率 evaluate 返回 **unknown**（group_multiplier_policy.go:85），断言 allowed 必挂 | case 3 断言 `MultiplierPolicyStatusUnknown`（区分 case2 allowed/case3 unknown） |
| W2 | 5 case 共享 fixture | site_user_groups (account,group_key) 唯一索引，同 DB 撞冲突 | 每子测试独立 group_key（或独立 account） |
| W3 | 矩阵 setup | SettingSetString 需 settingCache 已有 key | `settingRefreshCache` 前置 + cap 清理 t.Cleanup |
| W4 | 发布说明第 1 条 | 「迁移 025」旧名（已改名 site_group_multiplier_known.go / Version 2026081001）；「暂失效」过度保证 | 删旧名；「可能长期失效，恢复依赖上游再次上报可确认倍率（非 1）」；补三条子路径前提（同步需 AutoSync+非1倍率 / pricing 刷新仅当 group_ratio 仍含该组 / 真 1x 无 pricing 平台永不标 true） |
| W5 | 发布说明缺 fail-open | DB/绑定查询失败 cap 静默失效（仅告警）——成本控制点必须让管理员知道 | 补第 9 条：fail-open 边界 |
| W6 | 缺 keep-block 自愈条件 + 幽灵行 | keep-block「暂定 Nx」永不自愈；pricing-only 幽灵行 | 补进第 3/9 条 |
| W7 | README 放 8 条 | 产品门面过重 + 双语维护成本 | **载体分层**：README/README_zh 简版 4 条（管理员行为级）；analysis doc 全量 8+ 条（技术） |
| W8 | 排序变化未写 | member-sort Infinity→1x 明显前移；卡片顺序 vs 路由顺序不一致 | 补第 7 条「排序行为变化」显式说明 |
| W9 | analysis doc 过时承诺 | 严格模式/未知排除残留（107-108/331 行）与两态矛盾 | 写 Implementation Notes 时同步修订过时承诺 |
| W10 | 测试只测 item 级 | X3 用户拍板（known=false 5x 放行）无组级回归锁 | 矩阵补 1 case：EnforceMultiplierCap 对 known=false 5x+cap=4 放行 + recover |
| W11 | 矩阵缺 cap<1 | cap=0.5+value=1 管理员常用场景 | 补 cap<1 边界 case |
| W12 | iterator 测试未点名 | RoundRobin 全局计数器旋转 → 顺序断言 flaky；SessionKeepTime 粘性 | SessionKeepTime:0 + GroupModeFailover + 断言成员集合（Len/ChannelID set） |
| W13 | 「同步触发/前端 normalize 已覆盖」 | 断言范围偷换（函数已测≠触发已测；member-sort≠徽标） | 措辞改「人工验证，未纳入 CI」；keep-block 降级测试（W 新增）锁定 X2 落点 |

### v2 测试清单（实施）

1. `TestTwoStateMultiplierCapMatrix`（group_multiplier_policy_test.go）：6 case（item 级 5 + 组级 1），独立 group_key，settingRefreshCache 前置，t.Cleanup cap 清理
2. `TestIteratorFiltersBlockedItems`（balancer/iterator_test.go）：纯内存、Failover、SessionKeepTime:0、断言集合成员
3. `TestPersistSyncSnapshotKeepBlockDemotesKnown`（storage_test.go）：旧行 5x+true、新快照无倍率 → 保留 5 + known 降 false

### v2 发布说明结构

- **README.md/README_zh.md**（简版 4 条，管理员行为级）：① 升级后 cap 可能长期失效（恢复条件）；② 暂定徽标两语义 + 自愈条件；③ 排序行为变化（unknown 按 1x、卡片 vs 路由顺序可能不同）；④ 英文阻断原因
- **docs/group-multiplier-policy-analysis.md**（Implementation Notes 全量 9 条 + 修订过时承诺）

---

## 阶段 7 完成记录（2026-08-09）

> 阶段 7 收尾（发布说明 + 测试收口）完成。全量测试 29 包 EXIT=0，gofmt/diff-check 干净。**两态倍率方案全部阶段完成。**

### 已完成改动

**发布说明（载体分层，W7）**
- README.md / README_zh.md：「Multiplier Cap — Known Behaviors」简版 4 条（管理员行为级）
- docs/group-multiplier-policy-analysis.md：「Implementation Notes」全量 10 条（技术详细版）+ 修订过时承诺（4.3 严格模式等三态残留声明废弃）

**测试收口（3 个新测试）**
- internal/op/multiplier_two_state_test.go：两态矩阵 6 case（判定）+ 组级 recover case（W10 X3 回归锁 + W11 cap<1 边界 + W1/W2/W3 修正）
- internal/relay/balancer/iterator_test.go：blocked 过滤（W12：Failover + SessionKeepTime:0 + 集合断言）
- internal/sitesync/storage_test.go：keep-block known 降级（X2 落点回归锁）

### 实施后审查（2 顾问）修复

- **计费口径 P1**：README「仍按该值计费」错误（keep-block 保留值仅展示，计费恒为 quote/全局价 1x）→ 改「实际计费以站点报价为准」×3 处
- findings.md:21 三态残留 → 改两态口径
- analysis doc 已知未覆盖措辞（pricing_test 实际驱动了全链）→ 修正
- analysis doc 行号漂移 + 补第 10 条「组级/条目级准入绑定缺失分叉」

### 方案整体完成度（阶段 1-7 全部）

| 阶段 | 内容 | 状态 |
|---|---|---|
| 1 | 迁移 Version 2026081001 + MultiplierKnown 列 + 保守回填 | ✅ |
| 2 | 写路径三态化 + 提前 evaluate 两态化（N1/N2/N3 消除） | ✅ |
| 3 | 读路径 + 判定（随阶段 2） | ✅ |
| 4 | 重算触发（SyncAccount 无条件 + InitCache 启动降级） | ✅ |
| 5 | 前端三态标注（暂定/未知，site_id 守卫 + tooltip 区分语义） | ✅ |
| 6 | relay_logs 计费快照（quote known 持久化 + 三值 + Source 守卫） | ✅ |
| 7 | 发布说明（README 简版 + analysis doc 全量）+ 测试收口 | ✅ |

### 已知未覆盖（人工验证，analysis doc 记录）

同步触发副作用断言、前端徽标谓词（无 test script）、InitCache 重算单测。

---

> ⚠️ **本方案段（v1）已废弃**：过滤方向错误（「勾选=禁用→只走不支持 tools 的」），且触发点挂错函数。以下方 v2 为准。

## 渠道 tools 能力识别 + Key 级 tools 禁用方案（2026-08-10，实施前定稿，待顾问审查）

> 需求（用户拍板）：① 自动探测每个渠道×模型是否支持 tools（添加时 + 定期/手动）；② API Key 加「禁用 tools」勾选开关；③ 勾选后该 key 路由时跳过「支持 tools」的渠道模型（只走纯聊天/识图渠道）。
> 关键语义：**勾选=禁用 tools → 只使用 supports_tools=false 的渠道模型**（art tutor 场景：不需要 tools，只用识图/聊天渠道）。

### 现状事实（已核实）

- **请求检测**：`transformer/model/model.go:281` `InternalLLMRequest.Tools []Tool`——请求是否带 tools 可检测。
- **路由链路**：`relay.go:95` GroupGetEnabledMap → `relay.go:100` CatalogPlanGroup → balancer。`apiKeyID := c.GetInt("api_key_id")`（relay.go:87，middleware/auth.go:120 注入）。
- **路由粒度**：GroupItem{ChannelID, ModelName}（model/group.go:30）是组内最小路由单元——「渠道×模型」粒度正确（用户要求：有的渠道两个模型一个支持一个不支持）。
- **探测基础设施**：`grouphealth/probe.go` 有完整探测模式（RunCandidate 37 行、buildProbeRequest 107 行、buildProbeInternalRequest 157 行）——发最小请求验证渠道可用性。**可复用做 tools 探测**。
- **APIKey 模型**（model/apikey.go:3）：无 tools 字段，需加。
- **GroupItem 是 gorm 表**（db.go:81 在 AutoMigrate 列表）——加列走 AutoMigrate 自动补。

### 改动清单（8 处）

**改动 A：APIKey 加开关（1 文件 + 1 迁移）**

`internal/model/apikey.go` APIKey 加：
```go
DisableTools bool `json:"disable_tools,omitempty"` // 勾选=禁用 tools：该 key 路由只走不支持 tools 的渠道模型
```
- 加列走主 AutoMigrate（apikey 在 db.go models 列表）。

**改动 B：GroupItem 加能力标记（1 文件 + 迁移）**

`internal/model/group.go` GroupItem 加（gorm 列，非 `-`）：
```go
SupportsTools *bool `json:"supports_tools,omitempty"` // 渠道×模型是否支持 tools；nil=未探测
```
- `*bool` 三态：nil=未探测、true=支持、false=不支持。
- 主 AutoMigrate 自动加列（group_items 在 db.go:81）。

**改动 C：tools 探测器（新文件 internal/op/tools_probe.go）**

复用 grouphealth/probe.go 模式，发**带最小 tools 定义的请求**探测：
```go
func ProbeChannelModelToolsSupport(ctx context.Context, channel model.Channel, usedKey model.ChannelKey, modelName string) (bool, error)
```
- 构造 InternalLLMRequest：单条 "hi" 消息 + 一个最小 function 定义（`get_weather` 无参数）。
- 发请求（复用 buildProbeRequest 的 HTTP 构造逻辑或抽公共函数）。
- **判定**：请求成功（2xx）→ supports_tools=true；上游返回「tools/function calling 不支持」类错误（错误体含 `tools not supported` / `function calling not supported` / `unsupported parameter: tools` 等）→ false；其他错误（网络/限流/4xx 认证）→ **不判定**（返回 err，保持 nil 未探测态，避免误标）。
- 探测请求是**真实计费请求**（一条 "hi" + tools 定义），成本极低但会扣费——**文档明示**。

**改动 D：探测触发点（2 处）**

1. **添加时**：`GroupItemAdd`（op/group.go:287）在创建 item 后异步触发 `ProbeChannelModelToolsSupport`，回填 `item.SupportsTools`。用 goroutine + ctx 超时（不阻塞主流程）。
2. **定期/手动**：新增 handler `POST /api/v1/tools-probe/reprobe`（body: channel_id + model_name 或 group_id），手动触发；或同步/定时任务周期重探。**最小实现**：手动接口 + 随 grouphealth 周期（如有定时）复用。

**改动 E：路由过滤（核心，relay 链路）**

- relay.go:87 拿到 apiKeyID 后加载 key：`key, _ := op.APIKeyGet(apiKeyID, ctx)`，取 `key.DisableTools`。
- **GroupGetEnabledMap 过滤点**（relay.go:95 前）或 CatalogPlanGroup 内：
  - `key.DisableTools == true` → 过滤 `group.Items`，跳过 `SupportsTools != nil && *SupportsTools`（支持 tools 的条目）。
  - 语义确认：勾选后「只走不支持 tools 的」——过滤掉支持 tools 的 item。
  - **未探测（SupportsTools==nil）条目**：默认放行（保持现状），因为未探测不代表支持 tools——文档明示「勾选后未探测条目仍会参与，可能命中支持 tools 渠道；建议先探测」。

**改动 F：前端（2 文件）**

1. `web/src/api/endpoints/key.ts`（或 apikey 端点）：APIKey 类型加 `disable_tools?: boolean`。
2. Key 管理页（web/src/components/modules/key/ 或对应页）：加「禁用 tools」勾选，tooltip 说明「勾选后该 Key 的请求只使用不支持 tools 的渠道模型（适合纯聊天/识图场景）」。
3. （可选）分组页 item 行显示 `supports_tools` 标记（探测结果）。

**改动 G：重探接口（1 文件）**

`internal/server/handlers/tools_probe.go`：`POST /api/v1/tools-probe/reprobe`——按 channel_id+model_name 或 group_id 重新探测并回填。

**改动 H：测试（3 处）**

1. tools_probe 判定逻辑单测（成功→true、tools 不支持错误→false、其他错误→不判定 nil）。
2. 路由过滤单测（key.DisableTools=true 时支持 tools 条目被过滤）。
3. GroupItemAdd 异步探测触发（mock 探测函数，验证回填）。

### 验收标准

1. `go build ./...` + `go test ./...` 全绿；前端 tsc/lint/build 通过。
2. Key 页勾选「禁用 tools」后，该 key 请求只走不支持 tools 的渠道模型（支持 tools 的被过滤）。
3. 添加 GroupItem 后自动探测并回填 supports_tools；手动重探接口可用。
4. 未探测条目默认放行（不影响现有行为）。

### 已知边界 / 决策点

- **探测计费**：真实请求会扣费（极小），文档明示；可后续加「探测开关」。
- **未探测语义**：默认放行；勾选 key 可能命中未探测的支持 tools 渠道——建议先探测再勾选。
- **判定阈值**：仅「明确 tools 不支持」错误判定 false；其他错误不判定（防误标）。
- **异步探测失败**：保持 nil，下次重探。

---

## 渠道 tools 能力识别方案 v2（2026-08-10，吸收 3 顾问审查 + 用户拍板，实施以此为准）

> 3 顾问审查（后端/逻辑/本质追问者）发现 1 方案级错误 + 3 结构性缺口；用户拍板过滤方向与识图范围。v2 覆盖原方案 A-H，实施以 v2 为准。

### 用户拍板（最终语义）

- **过滤方向**：勾选「仅 tools」→ 该 key 路由时**只走支持 tools 的渠道模型，跳过不支持 tools 的（supports_tools=false）**；未探测（nil）默认放行。
- **识图范围**：本次**只做 tools 维度**，识图能力不在保证范围（文档/UI 声明：勾选只保证 tools 维度，识图由管理员自选渠道）。

### v2 修正对照

| # | 原方案 | 问题（顾问） | v2 修正 |
|---|---|---|---|
| T1 | D: 探测挂 GroupItemAdd | **GroupItemAdd 生产零调用**（真实路径是 GroupUpdate/GroupItemBatchAdd/preset 激活）→ 探测永不触发（后端对抗者，方案级错误） | **触发点改 3 处**：GroupUpdate ItemsToAdd 后、GroupItemBatchAdd 后、preset 激活/镜像后；抽公共 `probeToolsForNewItems(ctx, items)` |
| T2 | D: 异步回填无缓存刷新 | **回填 DB 后无 groupRefreshCacheByID → 路由读缓存永远 nil**（三顾问一致，结构性缺口） | **回填闭环**：`Model(&GroupItem{}).Update("supports_tools", v)` → `groupRefreshCacheByID` → `resetBalancerStateForChannel`；重探翻转同样触发 |
| T3 | E: 过滤点 relay.go 或 CatalogPlanGroup | **只改 relay.go 漏 compact/images/ws 三入口**；images 不调 CatalogPlanGroup（逻辑对抗者） | **新增 `GroupGetEnabledMapForTools(name, ctx, disableTools)`**（内部调 GroupGetEnabledMap 后过滤 `SupportsTools!=nil && *SupportsTools==false` 的条目）；relay/compact/ws_client 三个 chat 入口改用；**images 入口不应用**（images 请求无 tools 概念） |
| T4 | C: 判定 2xx→true | 静默剥参网关 2xx 判 true（伪正，本质追问者 H1）；探测 key（最低 cost）≠ 路由 key（带偏好）（H2） | **true 语义精确定义为「接受 tools 参数」**（非完整工具调用，文档明示）；false 判定要求 **≥2 次同文案错误或跨 key 重试**；白名单覆盖 OpenAI/Anthropic/Gemini/中文网关错误特征 |
| T5 | E: 未探测 nil 放行 | 逻辑对抗者：nil 放行=不安全侧 | **用户拍板：nil 放行**（默认什么都走）；勾选后只跳过明确 false。文档明示「未探测条目可能实际支持 tools，建议先探测」 |
| T6 | 缺：过滤后空集 | 勾选 key + 组内全支持 tools → 过滤后空 → 裸 503（逻辑对抗者反例1） | **空集返回明确错误**：「该 key 禁用了 tools 且无可选渠道」（distinct message，非通用 503） |
| T7 | C: 复用 buildProbeInternalRequest | 该函数无 Tools 字段，需新写（后端对抗者 #5）；embedding 渠道探测无意义 | **新写 `buildToolsProbeInternalRequest`**；`outbound.IsChatChannelType` 跳过 embedding；探测 key 固定选 Enabled 第一个（非 GetChannelKey 最低 cost，避免漂移） |
| T8 | 迁移描述 | 方案写「+迁移文件」多余（后端对抗者 #7） | **不加迁移文件**，只改 model 结构体（AutoMigrate 自动加列）；回填用 map 路径 Update 避免 *bool 指针零值歧义 |

### v2 改动清单（实施顺序）

1. `model/apikey.go`：APIKey 加 `ToolsOnly bool json:"tools_only,omitempty"`（AutoMigrate 自动加列）
2. `model/group.go`：GroupItem 加 `SupportsTools *bool json:"supports_tools,omitempty"`（AutoMigrate 自动加列）
3. `op/tools_probe.go`（新）：`ProbeChannelModelToolsSupport` + `buildToolsProbeInternalRequest` + 错误白名单（多协议）+ false 需 ≥2 次
4. `op/group.go`：`probeToolsForNewItems` + 回填闭环（Update + groupRefreshCacheByID + resetBalancerStateForChannel）；`GroupGetEnabledMapForTools` 新函数
5. 触发接线：GroupUpdate（ItemsToAdd 后）、GroupItemBatchAdd 后、group_preset 激活后（safe.Go + Background ctx + 全局信号量 4 + in-flight 去重）
6. `relay.go`/`compact.go`/`ws_client.go`：chat 入口改用 GroupGetEnabledMapForTools（apiKeyID 取 ToolsOnly）；空集 distinct error
7. `server/handlers/tools_probe.go`（新）：`POST /api/v1/tools-probe/reprobe`（channel_id+model_name 或 group_id 手动重探）
8. 前端：key 页加「仅 tools」勾选（tooltip 声明「只保证 tools 维度，识图自选渠道」）；key.ts 类型加 tools_only
9. 测试：探测判定（成功/false≥2/其他不判定/embedding 跳过）、过滤方向（toolsOnly 过滤 supports_tools=false 保留 true+nil）、回填缓存刷新、空集错误

### v2 验收标准

1. `go build ./...` + `go test ./...` 全绿；前端 tsc/lint/build。
2. 勾选「仅 tools」→ 该 key 路由只走 supports_tools=true 的渠道（false 被过滤，nil 放行）；未勾选不过滤。
3. 添加（GroupUpdate/批量/预设）后自动探测回填 + 缓存刷新；手动重探接口可用。
4. 空集返回明确错误。
5. images 入口不应用过滤（图片请求不受影响）。

### v2 已知边界（发布说明）

- **true=「接受 tools 参数」**非完整工具调用（静默剥参网关会误判，无法根治，文档明示）。
- **探测结果可能 stale**（无 TTL/自动重探，手动重探是唯一更新路径；上游能力变化后需手动重探）。
- **识图不在保证范围**（只做 tools 维度）。
- **未探测 nil 放行**（勾选后可能命中未探测的支持 tools 渠道）。
- **探测计费**（真实请求，极小，文档明示）。


---

## 外部项目借鉴与需求清单（2026-08-10）

> 来源：对比 my-ai-gateway（独立 Java 实现）与 octopus-customization（同源 fork，领先上游 53 commits）。定位：内网自用。
> 用户确认排除：直连渠道、分享链接、模型继承。

### 3 条可借鉴（同源或独立，按优先级）

| # | 功能 | 来源 | 说明 | 工作量 |
|---|---|---|---|---|
| 1 | **熔断管理面** | octopus-customization | 熔断状态列表（/api/v1/circuit/status）+ 手动重置（/reset）+ 前端 CircuitBreaker.tsx——我们熔断核心已有但无管理面 | 0.5 天 |
| 2 | **分组/渠道级参数覆盖**（param_override） | octopus-customization | 对特定分组/渠道注入请求参数覆盖（JSON object 校验 + 前端校验 + i18n），解决「某渠道需要特殊参数」 | 1 天 |
| 3 | **稳定性修补** | octopus-customization | 高并发日志刷写 makeslice panic、弹窗按钮遮挡、测试失败弹窗提示——直接 cherry-pick | 0.5 天 |

### 2 条新需求

| # | 功能 | 状态 |
|---|---|---|
| 1 | **渠道 tools 能力识别 + Key 级「仅 tools」开关**（本次方案 v2） | 进行中（方案已落版，待实施） |
| 2 | **多模态入口模型能力标记**（模型名规律：5v/vision 后缀） | 已认可「入口标识」简单方案（用户拍板：模型名能体现，无需渠道级规则）；未排期 |

### 已排除

- 直连渠道（channel/model 语法）、分享链接、模型继承——用户确认不做。

---

## tools 方案确认轮结论（2026-08-10，多来源调研 + 2 顾问确认后，v2 增补 T9）

> 多来源调研（smart-search：LiteLLM/new-api 靠转换层不做模型级探测；llm-probe 验证 tool_calls 产出）+ 2 顾问确认（本质追问者/反驳者）结论一致：
> **① 不采纳 tool_calls 验证作为主判定**——伪负>伪正（模型不遵循 tool_choice=required、MaxTokens=16 截断、三协议 required 语义不等价），且探测是内嵌在线判定 vs llm-probe 离线审计，语境不同。
> **② 必须补「失败反馈学习」闭环（P0）**——2xx→true 对静默剥参网关伪正是用户核心场景失效，方案自认「无法根治」且无缓解；失败反馈学习零成本起步、方向保守，是唯一值得借鉴的实质动作。

### T9 增补（v2 必改项）

**失败反馈学习闭环**（反驳者 P0 / 本质追问者「真实调用证据」）：
- relay 链路检测：真实请求 `InternalLLMRequest.Tools` 非空（model.go:281 可检测）+ 上游返回「tools 不支持」类错误（复用 T4 已建错误白名单）→ **反向回写该 GroupItem 的 `supports_tools=false`**（复用 ≥2 次规则防瞬态）+ 告警日志。
- 触发点：relay 各入口错误处理处（chat/compact/ws），识别到 tools 不支持错误时按 `(channelID, modelName)` 回写。
- 效果：静默剥参网关被真实请求「抓到」后自动降级，弥补 2xx 判定伪正；人工重探可翻转回来。

### 确认后维持（不改）

- **T4 主判定保持 2xx→true**（伪负>伪正；工具调用探测成本高、不稳定）。
- **用户拍板硬过滤**（勾选=仅 tools→跳过 supports_tools=false；nil 放行）——反驳者建议「降级为排序权重」，**不采纳**（用户明确「直接自动屏蔽/跳过」，语义是硬过滤）；文档明示 best-effort（伪正/伪负边界）。
- T1/T2/T3/T5/T6/T7/T8 全部维持。

### 确认轮新增已知边界（记录）

- **探测 key≠路由 key**（T7 固定第一个 enabled key vs 路由最低 cost key）：supports_tools 是「渠道整体能力」提示，不代表具体 key；多 key 渠道文档提示。
- **探测非流式 vs 真实流式**：流式 tools 需额外 beta 头（Anthropic），探测结果可能不覆盖流式形态。
- **批量探测节流**：预设激活/批量添加时探测队列限速，避免一次几十个真实扣费请求。
- **无 TTL stale**：手动重探唯一更新路径 + T9 失败反馈自动降级（可部分缓解）。

### 实施清单（v2 最终，含 T9）

1. model/apikey.go：ToolsOnly bool（AutoMigrate 加列）
2. model/group.go：GroupItem.SupportsTools *bool（AutoMigrate 加列）
3. op/tools_probe.go（新）：2xx→true 主判定 + 错误白名单（≥2 次 false）+ buildToolsProbeInternalRequest（跳过 embedding）
4. T9：relay 错误处理检测 tools 不支持 → 回写 false + 告警
5. 触发：GroupUpdate/GroupItemBatchAdd/preset 激活后异步探测 + 回填闭环（Update+groupRefreshCacheByID+resetBalancerStateForChannel）+ 信号量 4 + in-flight 去重 + 节流
6. 路由：GroupGetEnabledMapForTools（新增包装不改原签名，images 不应用）+ relay/compact/ws 三 chat 入口改用 + 空集 distinct error
7. 重探 handler：POST /api/v1/tools-probe/reprobe
8. 前端：key 页「仅 tools」勾选 + key.ts 类型
9. 测试：探测判定/过滤方向/回填缓存/T9 失败反馈/空集错误

---

## tools 方案 v3 修正（2026-08-10，正式审查 3 顾问 + 用户拍板后，实施以此为准）

> 正式审查（后端/数据/逻辑对抗者）发现 3 致命 + 多个重伤；用户拍板 2 项。v3 覆盖 v2+T9，实施以 v3 为准。

### 用户拍板（v3 定稿）

1. **过滤粒度：Key 级全部请求过滤**（不采纳逻辑对抗者「请求级带 tools 才过滤」建议）——接受「纯聊天流量也被收窄到支持 tools 渠道」的 trade-off，文档明示。
2. **功能定位重写**：本功能服务**「需要 function calling 的应用」**（保证带 tools 请求不落到不支持渠道）；art tutor 类「不需要 tools 的应用**无需勾选**」（勾选反而收窄可用渠道），文档/UI 明示，避免反向使用。

### v3 修正对照（3 顾问发现）

| # | 问题（顾问） | 严重级 | v3 修正 |
|---|---|---|---|
| U1 | T9 检测点错层：流式 passthrough（Responses HTTP/WS）错误消息在 relay 错误路径不可见（后端对抗者） | 致命 | **T9 检测点下移**：① OpenAI chat transform 流 → `handleStreamResponseV2` error 链；② Responses HTTP passthrough → `handleStreamResponsePassthroughV2` OnFinish/rawBuffer 解析 error 事件；③ WS → `ws_passthrough.go:390` wsUpstreamEventError 处挂白名单；④ 非流式 → relay.go:1233 错误体。白名单抽公共函数（探测 + T9 共用） |
| U2 | op import grouphealth 循环依赖（后端对抗者） | 致命 | **op 内独立实现** `buildToolsProbeInternalRequest` + 探测发送逻辑（复制 grouphealth 的 HTTP 构造模式，不 import）；删除「复用 buildProbeRequest」措辞 |
| U3 | 漏 GroupPresetUpdate 第 4 条创建路径（后端对抗者） | 重伤 | 触发点补 **GroupPresetUpdate 镜像重建**（group_preset.go:334 mirror）；触发点从行号改为函数语义：GroupUpdate 提交后 / GroupItemBatchAdd 提交后 / GroupPresetActivate 后 / GroupPresetUpdate mirror 后 |
| U4 | ≥2 计数无载体（后端/数据对抗者） | 重伤 | **进程内 map**：key=(channelID,modelName)，val={errorHash, count, timestamp}，TTL 10 分钟过期；探测侧与 T9 侧共用同一 registry；重启即清（文档明示） |
| U5 | preset 激活抹除探测结果（数据对抗者） | 致命 | **preset 重建时按 (channel, model) 匹配旧行继承 SupportsTools**（group_preset.go:421 Delete 前先读旧 map，重建时搬值）——避免每次切换全量重探 + 付费振荡 + true/false→nil 丢失 |
| U6 | T9 写回跨组单行歧义（数据对抗者） | 重伤 | **T9 写回按 (channel_id, model_name) 全量 UPDATE 所有组行** + `GroupRefreshCacheByIDs` 刷新所有受影响分组；新增导出 op 函数 `ReportToolsUnsupported(ctx, channelID, modelName)` |
| U7 | false→true 死锁（数据对抗者/逻辑对抗者） | 重伤 | **轻量反向反馈**：真实 tools 请求**成功**且当前标记 false → 回写 nil（待重探）——成本≈0，打破单向退化；false 永不自动回 true 但仍可经成功请求降级为 nil 再重探 |
| U8 | 标记无来源列（数据对抗者） | 重伤 | GroupItem 加 `SupportsToolsProbeKeyID *int` + `SupportsToolsProbedAt *time.Time` + `SupportsToolsSource string`（probe/manual/t9）——多 key 失真可审计 |
| U9 | 空集判断位置（数据对抗者） | 可修复 | T6 distinct error 在 **CatalogPlanGroup 之后**判断（区分「ToolsOnly 过滤空」与「协议不兼容空」）；三入口一致 |
| U10 | 语义矛盾（逻辑对抗者 L1/L2） | 致命 | **已由用户拍板解决**：重新定位为服务需要 tools 的应用，art tutor 无需勾选；文档/UI tooltip 重写 |
| U11 | 状态机单调退化（逻辑对抗者 L5） | 重伤 | U7 反向反馈 + 手动重探；文档明示「可用集可能随时间收缩，建议定期重探」 |
| U12 | 循环依赖（数据对抗者 #2） | 已修 | U2 覆盖 |
| U13 | 多 key 失真 | 记录 | U8 来源列可审计；文档明示「supports_tools 是渠道整体能力提示，不代表具体 key」 |

### v3 实施清单（最终）

1. model/apikey.go：ToolsOnly bool
2. model/group.go：SupportsTools *bool + SupportsToolsProbeKeyID *int + SupportsToolsProbedAt *time.Time + SupportsToolsSource string（全部 gorm 列，AutoMigrate 加）
3. op/tools_probe.go（新）：独立 buildToolsProbeInternalRequest（不 import grouphealth）+ 2xx→true 主判定 + 白名单公共函数 + ≥2 registry（内存 map TTL）
4. op/group.go：probeToolsForNewItems + 回填闭环（Update+GroupRefreshCacheByIDs+resetBalancer）+ GroupGetEnabledMapForTools（新包装不改原签名）+ ReportToolsUnsupported 导出
5. 触发：GroupUpdate / GroupItemBatchAdd / GroupPresetActivate / GroupPresetUpdate mirror 四函数语义点，异步探测（safe.Go+Background ctx+信号量 4+in-flight 去重+节流）
6. group_preset.go：重建时继承旧 SupportsTools（U5）
7. T9 检测点下移：4 个流层错误点挂白名单 → ReportToolsUnsupported（U1）；成功请求反向反馈（U7）
8. relay/compact/ws chat 入口：改用 GroupGetEnabledMapForTools + 空集 distinct error（CatalogPlanGroup 后判断）
9. 重探 handler：POST /api/v1/tools-probe/reprobe
10. 前端：key 页「仅 tools」勾选 + tooltip 重写（服务需要 tools 的应用，art tutor 无需勾选）+ key.ts 类型
11. 测试：探测判定 / 过滤方向 / preset 继承 / T9 检测各流层 / 反向反馈 / 空集错误 / ≥2 registry

### v3 已知边界（发布说明）

- Key 级过滤 trade-off：纯聊天流量也被收窄到支持 tools 渠道（用户拍板接受）
- 功能定位：服务需要 function calling 的应用；不需要 tools 的应用无需勾选
- 静默剥参网关 2xx→true 伪正：T9 无法纠正（无错误响应），仅靠手动重探 + 文档明示
- 探测 key≠路由 key：来源列可审计
- ≥2 registry 重启即清：重启后需重新积累
- 无 TTL：手动重探 + U7 反向反馈 + preset 继承

---

## tools 方案完成记录（2026-08-10）

> 「渠道 tools 能力识别 + Key 级仅 tools 开关」完成。全流程：多来源调研（smart-search）→ 2 顾问确认轮 → 3 顾问正式审查 → 实施 → 3 顾问实施后审查 → 修复闭环。全量测试 29 包 EXIT=0，前端 tsc/lint/build 全绿。

### 最终语义（用户拍板）

- **勾选「仅 tools」** → 该 key 全部请求路由时只走 `supports_tools=true` 的渠道模型（跳过 false；nil 未探测放行）；不勾选不过滤。
- **功能定位**：服务「需要 function calling 的应用」；不需要 tools 的应用请勿勾选（会收窄可用渠道且不保证识图）。
- **识图不在保证范围**（只做 tools 维度）。

### 实施清单（最终）

- 模型：APIKey.ToolsOnly；GroupItem.SupportsTools + ProbeKeyID + ProbedAt + Source（AutoMigrate 加列）
- 探测：internal/toolsprobe 独立包（2xx→true + 白名单 ≥2 false + 信号量 4 + 12s 超时 + MaxTokens=128）；op 经 ToolsProbeFn hook 注入（避开循环依赖）
- 触发：GroupUpdate / GroupItemBatchAdd / preset 激活+镜像 4 函数点异步探测 + 冷却期 6h 跳过已探测条目
- preset 继承：重建时按 (channel,model) 搬全字段（值+血缘）
- 路由过滤：GroupGetEnabledMapForTools 新包装（relay/compact/ws chat 入口；images 不应用）；空集 distinct error
- T9 失败反馈：非流式 passthrough + transform 标准路径均挂（键=internalRequest.Model 即 item.ModelName）+ ≥2 次确认
- U7 成功反向反馈：≥2 次独立成功才回写 nil（Source=u7）+ 打破 false→true 死锁
- 重探 handler：POST /api/v1/tools-probe/reprobe
- 前端：key 页「仅 tools」勾选 + 徽标 + 三语 locale（hint 明示「不需要工具的应用请勿勾选」）
- 测试：白名单匹配 + 过滤方向（tools_policy_test.go）

### 实施后审查修复（3 顾问发现 7 项）

- FIX-A：T9/U7 键 requestModel→internalRequest.Model（模型映射场景命中 0 行问题）
- FIX-B：T9 补挂 transform 标准路径（此前只挂 passthrough，覆盖不对称）
- FIX-C：U7 加 ≥2 成功门槛 + 元数据一致（Source=u7）
- FIX-D：探测白名单 ≥2 false 落地（此前命中白名单只返回 error 不回填，是死代码）
- FIX-E：探测冷却期 6h（避免每日全量重探风暴 + 翻转继承值）
- FIX-F：op 导出 ConfirmToolsUnsupportedOnce 供探测侧确认
- FIX-G：preset 继承搬全字段（值+血缘，防 Source 丢失）

### 已知边界（发布说明）

- 静默剥参网关 2xx→true 伪正：T9 无法纠正（无错误响应），手动重探唯一纠正
- ≥2 registry 进程内 + TTL 10min：重启清零、多实例不共享、不同错误文本不累计
- 探测真实计费（冷却期缓解，但初次/重探会扣费）
- U7 恢复依赖非 toolsOnly key 的带 tools 真实请求成功（纯 toolsOnly 部署下 false 需手动 reprobe）
- 流式 passthrough/WS 错误点未挂 T9（SSE 200+error event，记录为边界）

---

## tools 方案 v2 迭代（2026-08-10，用户拍板，待审查）

> 用户反馈：① 分组内需 tools 能力小徽标（一目了然 + 仅 tools 筛选依据）；② 探测时机改「新建分组 + 手动测试为主」（首次同步可能失败）；③ 与现有测活（grouphealth）结合——一次请求同时测活 + 测 tools，节省调用，且请求要避免易封禁的固定文案；④ key 维度维持「渠道×模型」。并请求提供真实 API 做端到端验证。

### v2 调整

| # | 调整 | 内容 |
|---|---|---|
| V2-1 | **分组内 tools 小徽标** | Group 卡片/编辑器每个条目加小徽标：`✓tools`（支持）/ `✗`（不支持）/ `·`（未探测），尺寸小（text-[9px]），不占布局（绝对定位或行内小点） |
| V2-2 | **手动测试为主** | 分组条目上加「测试 tools」按钮（逐条/批量），点击立即调 reprobe 并回填；自动探测降为辅：仅新建分组时尝试一次，失败不重试、不阻塞 |
| V2-3 | **与测活结合** | 复用 grouphealth 探测链路：请求带最小 tools 定义 + 随机 prompt 池（隐蔽，避免固定 "hi" 易封禁）；一次请求结果双写——测活健康状态 + group_items.supports_tools（2xx→活+true、白名单错误→活+false、其他错误→不活+不判定） |
| V2-4 | key 维度 | 维持「渠道×模型」，不按渠道 key 探测 |
| V2-5 | **真实 API 端到端验证** | 待用户提供测试 API 后，用真实上游验证：2xx→true、白名单→false、各协议 transform 路径、静默剥参场景 |

### V2-3 结合测活的实现要点（初稿，待审查细化）

- 改 `internal/grouphealth/probe.go` 的 `buildProbeInternalRequest`：chat 类型渠道的探测请求加最小 tools 定义；MaxTokens 16→128（工具参数可能超 16）。
- 判定双写：probe.go RunCandidate 返回后，按结果调 op 导出函数回填 supports_tools（grouphealth import op 已存在）。
- 风险：改探测请求影响现有测活回归（需测活测试全绿）；测活频率决定 tools 标记刷新频率（手动测试为主后自动频率次要）。
- embedding 渠道不带 tools（保持原样）。

### V2-5 端到端验证计划（待 API）

用户提供 2-3 个不同协议 API（OpenAI 兼容 / Anthropic / Gemini 或中转）+ 1 个已知不支持 tools 的，验证：
1. 探测 2xx→true 对真实支持 tools 渠道成立
2. 白名单对真实不支持渠道成立（实际报错文本 vs 白名单）
3. transform 路径（如 OpenAI→Anthropic）探测请求有效
4. 结合测活后的双写正确

---

## models.dev 定位修正 + tools 确认路径锁定（2026-08-10，用户拍板）

> 第二轮 smart-search 调研 + 端到端实测（prism.uno 三模型均支持 tools，2xx→true 判对）+ 用户质疑后定稿。

### models.dev 正确用法（用户拍板，已修正）

- **❌ 不得用于 tools 确认**：中转站模型名与官方一致，但 tools 是否被阉割是**实例级动态属性**——模型名/静态库都确认不了，预标只会制造「假 true」靠 T9 纠偏，纯添乱。
- **✅ 可用于模型能力分类**（可靠，能力是模型级静态属性）：
  - 厂商分类（provider 维度稳定）
  - 模型能力标记：文本/生图/多模态（vision 等）——对接已认可的「多模态入口模型能力标记」需求
  - 实现：models.dev `capabilities` 字段作为**默认预填值**，管理员可覆盖；models.dev 不可达时自动跳过（降级为不预填）；同名但能力被改的中转站靠管理员覆盖 + 实际使用修正
- **本次 v2 不做** models.dev 集成（先做核心 v2；能力分类预填列为 v2 后置项）

### tools 确认路径（锁定）

1. **手动测试为主**：分组条目「测试 tools」按钮（tool_choice=required 加强判定，实测逼出真实 tool_call）
2. **自动探测为辅**：新建分组时尝试一次，失败不阻塞、不重试（避免首次同步失败导致永久 nil）
3. **T9/U7 反馈收敛**：真实请求失败（≥2 次）→ false；真实请求成功（≥2 次，标记 false）→ 回写 nil 待重探
4. **语义边界**：「支持基础 function 调用」（协议层接受 tools + 能产出 tool_call）；复杂工具（终端/文件）可用性以实际调用为准，靠 T9 反馈收敛
5. **不做** models.dev 预填 tools

### 端到端验证结论（prism.uno 三模型）

- gpt-5.6-sol / gpt-5.6-terra / claude-sonnet-5 均真支持 tools（tool_choice=required 均返回 tool_calls）
- 「2xx→true」判定在真实上游**判对**（无 tool_choice 时 tool_calls:null 不误判——印证确认轮「不按 tool_calls 判」的决策）
- 「不支持→白名单→false」侧**未验证**（暂无确认不支持的 API）
- runanytime.hxi.me 当前网络不可达（DNS 正常、TCP 超时），测试暂停

## tools v2 迭代方案（2026-08-10，实施前定稿，待顾问审查）

> 用户拍板：① 分组内 tools 能力**小徽标**（避免占布局）；② **手动测试为主**；③ **与现有测活结合**（一次请求同时测活+测 tools，请求用随机 prompt 避免易封禁的固定文案）；④ key 维度维持「渠道×模型」；⑤ models.dev 正确用法已锁定（上述）。

### V2-1 分组内 tools 小徽标

- Group 卡片/编辑器每个条目加小徽标：`✓tools`（支持）/ `✗`（不支持）/ `·`（未探测）
- 尺寸小（text-[9px]）、行内，不占布局
- 「仅 tools」设置时可按标注筛选（辅助查看哪些条目会走）

### V2-2 手动测试为主

- 分组条目加「测试 tools」按钮（逐条/批量）
- 点击调 reprobe 语义：**带 tool_choice=required**（加强判定，逼出真实 tool_call）+ 超时/错误处理
- 结果立即回填标注 + 刷新缓存
- 自动探测降为辅：仅新建分组时尝试一次，失败不重试

### V2-3 与测活结合

- 改 `internal/grouphealth/probe.go` 的 `buildProbeInternalRequest`：chat 类型渠道的探测请求**加最小 tools 定义**；MaxTokens 16→128（工具参数可能超 16）
- 请求 prompt 用**现有随机池**（grouphealth 已有 4 条随机 prompt，防指纹；tools 探测沿用，避免固定 "Reply with the word ok" 易封禁）
- 判定双写：一次请求结果 → 测活健康状态 + group_items.supports_tools（2xx→活+true、白名单→活+false、其他→不活+不判定）
- grouphealth import op 已存在，回填调 op 导出函数
- embedding 渠道不带 tools（保持原样）
- 风险：改探测请求影响现有测活回归（需测活测试全绿）；测活频率决定 tools 标记刷新频率（手动测试为主后自动频率次要）

### V2-4 key 维度

维持「渠道×模型」，不按渠道 key 探测。

### V2 改动清单（初稿，审查后细化）

1. `internal/grouphealth/probe.go`：buildProbeInternalRequest 加 tools + MaxTokens 128（chat 类型）
2. `internal/op/tools_policy.go`：`ReprobeToolsWithRequired`（tool_choice=required 版探测）+ 回填导出
3. `internal/toolsprobe`：支持 tool_choice=required 参数（手动测试用）+ 随机 prompt
4. 后端 handler：`POST /api/v1/group/tools-test`（分组条目手动测试，批量）
5. 前端：Group 卡片/编辑器条目 tools 小徽标 + 「测试 tools」按钮 + 筛选
6. 测活判定双写接线
7. 测试：测活回归 + 手动测试 + 徽标渲染

### 验收标准

1. go test 全绿（含测活回归）+ 前端 tsc/lint/build
2. 分组条目显示 tools 小徽标（✓/✗/·），仅 tools 设置时可按标注筛选
3. 「测试 tools」按钮：带 tool_choice=required 手动测试，结果回填标注
4. 测活请求同时产出健康状态 + supports_tools（随机 prompt + MaxTokens 128）
5. 端到端：prism.uno gpt-5.6-sol 手动测试 → ✓tools（实测过 tool_choice 逼出 tool_call）

### 待办

- v2 方案审查（多顾问）
- v2 实施 + 验证
- v2 实施后审查
- （后置）models.dev 能力分类预填（多模态入口模型能力标记）

---

## tools v2 方案审查修正 v3（2026-08-10，3 顾问审查 + 用户拍板，实施以此为准）

> 3 顾问审查（后端/前端/逻辑对抗者）结论：V2-3（与测活结合）被三位独立推翻（健康维度被 tools 污染）；V2-1 前端三处接线遗漏（复现阶段 5 事故）；V2-2 required 判别矩阵缺失；T9 复杂工具收敛承诺不成立。用户拍板：**放弃 V2-3**。v3 覆盖 v2 方案，实施以 v3 为准。

### 用户拍板

- **放弃 V2-3**（测活保持纯净不带 tools，健康判定不被 tools 污染；tools 探测独立走「手动测试为主 + 自动辅助」）

### v3 修正对照

| # | v2 问题（顾问） | v3 修正 |
|---|---|---|
| W1 | V2-3 双写让纯聊天渠道测活被标不健康（三位一致） | **放弃 V2-3**：grouphealth 探测请求不加 tools、MaxTokens 保持 16、无双写。测活与 tools 完全分离 |
| W2 | V2-1 前端三处接线遗漏（前端对抗者，致命） | **V2-1 接线写死**：group.ts GroupItem 加 `supports_tools?: boolean`；ItemList SelectedMember 加 `supports_tools?: boolean \| null`；Card displayMembers 映射补 `supports_tools: item.supports_tools`；徽标谓词 `=== true/false` 严格比较（严禁 `!member.supports_tools`，nil 语义与 multiplier_known 相反） |
| W3 | V2-2 required 400 无判别矩阵（后端/逻辑对抗者） | **required 对照探测**：手动测试带 tool_choice=required；400 → 自动降级重发「带 tools 不带 required」——2xx → 判 true（source=manual-required-fallback）+ 返回「支持 tools 但 required 不可用」提示；required 200 无 tool_calls → 不判定（保留现值）+ 提示「模型不服从 required」；其余按现有判定 |
| W4 | 语义双定义（逻辑对抗者） | **true 收敛为单一谓词**：「协议层接受 tools 参数」（2xx→true）。「能产出 tool_call」降级为手动测试的**附加展示信息**（tool_choice 逼出的 tool_call 作为确认展示），不得作为 false 依据 |
| W5 | 双写/多 writer 覆盖无优先级（后端/逻辑对抗者） | **写路径优先级**：手动测试 > 自动探测；「不判定」= **不写列、保留现值**（禁止写 nil 覆盖 T9 结论）；自动探测保留现有 4 触发点 + 6h 冷却（不改为「仅新建一次」——审查证明「新建一次失败不重试」在创建时抖动下同样永久 nil，且与现有触发点冲突） |
| W6 | T9 复杂工具收敛承诺不成立（逻辑对抗者） | **承诺降级为已知边界**：T9 白名单是协议级措辞，终端/文件执行期失败（command not found 等）不命中 → 复杂工具失败不自动收敛，flag 可能 stale-true；且白名单误伤风险（某 function 失败文本含白名单词 → 整模型标 false）如实记录 |
| W7 | 手动测试按钮位置（前端对抗者） | 卡片级批量（复用 ManualProbeButton 先例）+ 编辑器 SortSection 头部；逐条 loading 本地化 + in-flight 去重；行内不做常驻按钮（右侧已满） |

### v3 实施清单（最终）

1. **后端**：
   - `internal/toolsprobe/tools_probe.go`：Run 加 `ToolChoice string` 参数（空=auto 现行为；"required"=手动测试用）；required 400 → 降级对照探测（W3）
   - `internal/op/tools_policy.go`：`ReprobeToolsWithRequired`（手动测试入口，含对照降级 + source=manual-required-fallback）；写优先级（手动>自动）；「不判定」不写列
   - `internal/server/handlers/tools_probe.go`：reprobe 支持 `tool_choice: "required"` + 返回判别信息（支持 auto / required 不可用 / 模型不服从 required / 不支持 tools）
   - 前端类型 group.ts 加 supports_tools
2. **前端**：
   - ItemList.tsx：SelectedMember 加字段 + 三态小徽标（✓绿 / ✗红 / 未探测不渲染，tooltip 显示血缘 source）
   - Card.tsx：displayMembers 透传 supports_tools
   - 卡片级「测试 tools」批量按钮 + 编辑器 SortSection 头部逐条按钮（复用 ManualProbeButton loading 模式）
3. **测试**：
   - 手动 required 对照探测单测（400→降级 2xx→true）
   - 写优先级单测（手动覆盖自动）
   - 前端徽标三态渲染（含老数据 nil→不渲染）
   - 测活回归（确认 grouphealth 未被 tools 改动——v3 不改 probe.go）

### v3 验收标准

1. go test 全绿（含测活回归，证明 grouphealth 未被污染）+ 前端 tsc/lint/build
2. 分组条目显示 tools 小徽标（✓/✗/未探测不渲染），仅 tools key 路由过滤（后端已做）+ 查看筛选（前端辅助）
3. 「测试 tools」按钮：手动 required → 对照降级 → 明确返回「支持(auto)/required 不可用/不服从 required/不支持」四态之一
4. 自动探测保留 4 触发点 + 冷却，手动结果优先
5. 端到端：prism.uno gpt-5.6-sol 手动测试 → ✓tools（required 逼出 tool_call 已实测）

### v3 已知边界（发布说明）

- 复杂工具（终端/文件）失败不自动收敛（T9 白名单是协议级措辞），flag 可能 stale-true；白名单误伤风险如实记录
- 静默剥参网关 2xx→true 伪正（无错误响应，手动 required 逼不出 tool_call 时提示）
- ≥2 registry 进程内 + TTL：重启清零、多实例不共享
- 探测计费（手动测试是真实扣费请求，UI 明示）

---

## tools v3 复审修正 v3.1（2026-08-10，4 顾问复审 + 用户拍板 3 项，实施以此为准）

> 4 顾问复审（后端/逻辑/本质追问/前端对抗者）发现 v3 逻辑仍未闭环：W4「单一谓词」与 W3 自相矛盾、W5「手动>自动」语义空转、W3 四态契约 ≥2 下违约、required 400 降级假设无数据、前端 W2 外层守卫致命缺口、批量/虚拟化/编辑器断链。用户拍板 3 项。v3.1 覆盖 v3，实施以 v3.1 为准。

### 用户拍板（v3.1 定稿）

1. **加强制标不支持入口**：手动 required 逼不出 tool_call 时，提供「强制标不支持」按钮（管理员显式覆盖，source=manual）+「恢复自动」可撤销——手动强证据有落库动作，不再空转。
2. **写覆盖按证据层级**：`required 逼出 tool_call > T9 双确认 false > 手动降级 2xx = 自动 2xx`。手动降级 2xx 不得覆盖 T9 false；T9 false 可被更强的手动 required 确认覆盖；手动「强制标不支持」为最高级（管理员显式）。
3. **五态契约**：返回态加「待确认」（白名单首次命中，≥2 确认机制）——诚实表达，前端五态文案。

### v3.1 修正对照（4 顾问复审发现）

| # | 复审问题 | v3.1 修正 |
|---|---|---|
| R1 | W4「单一谓词 2xx→true」与 W3「required 200 无 tool_calls 不判定」自相矛盾（逻辑对抗者 C1） | **改双证据层**：auto/降级 2xx → true（接受参数）；required 2xx + tool_call → true（执行确认，更强证据）；required 2xx 无 tool_call → 不判定。tool_call 是 required 分支的判定输入，非「附加展示」 |
| R2 | W5「手动>自动」空转：手动唯一强证据场景不落库；手动降级 2xx 与自动同构（本质 A1 + 逻辑 C3） | **证据层级 + 强制入口**（用户拍板 1/2）：手动 required 逼出 tool_call 强证据；逼不出 → 提供「强制标不支持」落库入口；手动降级 2xx 与自动 2xx 同层（不得覆盖 T9 false） |
| R3 | W3 四态契约 ≥2 下违约（逻辑 C6） | **五态**（用户拍板 3）：支持 auto / required 不可用 / 不服从 required（可强制标不支持）/ 不支持 / 待确认（白名单首次命中） |
| R4 | required 400→降级假设无数据（本质 A2） | 降级链保留但**不依赖常态假设**：无论 required 400 是否常见，判别矩阵穷举全分支；「不服从 required」（200 无 tool_call）升级为**可落库的纠正动作**（强制标不支持入口） |
| R5 | 前端 W2 外层守卫致命缺口（前端对抗者） | **ItemList.tsx:162 外层守卫加 `\|\| member.supports_tools != null`**（Y2 同构事故；本地渠道条目是 tools 徽标主受众） |
| R6 | 批量 sync/async 未定义（前端对抗者） | **批量接口改异步任务 + 轮询**（对齐 group-health 先例：POST 返回 accepted，前端轮询进度）；批量上限（如 20）；同步 reprobe 保留单条 |
| R7 | in-flight 去重载体未定义（前端对抗者） | **去重放 react-query mutation 缓存**（按 (groupID,itemID) key）或模块级 store——VirtualizedGrid 卸载卡片后组件 state 不可靠 |
| R8 | 编辑器断链（前端对抗者） | 编辑器内测试结果**回写本地 selectedMembers**；未提交新条目（无 item_id）触发测试 → **禁用 + 提示**（Update 按 (channel,model) 命中 0 行静默无效） |
| R9 | toolsprobe 固定 prompt 未防封禁（本质 A3，用户原始需求） | **抽 grouphealth 的 resolveProbePrompt 为公共函数复用**（随机池防指纹；用户「避免易封禁固定文案」需求在 V2-3 放弃后迁移到 tools 探测侧） |
| R10 | T9 保护窗口只有 6h（本质 A4） | **声明**：6h 冷却后自动探测可覆盖 T9 false（写 true），但按 R2 证据层级，手动降级 2xx 与自动同层——T9 双确认 false 可被 6h 后自动探测覆盖（如实记录，不隐藏）；U7 是唯一合法 nil 写路径 |
| R11 | 「仅 tools 查看筛选」清单与验收矛盾（前端对抗者） | **补进清单**：谓词 `supports_tools !== false`（隐藏 ✗，保留 true+nil，对齐路由语义）；文案与 key 页「仅 tools」区分（如「仅看 tools 条目」）；位置：组列表页 filter 区（仿 search+vendorFilter） |
| R12 | W7「复用 ManualProbeButton 先例」不存在（本质 A6） | **改「新建按钮组件，loading 仿 group_health 面板现有模式」**（grep 证实 ManualProbeButton 不存在） |
| R13 | 四态遗漏两格（逻辑 C2/F） | 判别矩阵补全：required 400 + auto 400（白名单第 1/2 次 → 待确认/不支持；非白名单 → 不判定）；required 200 无 tool_call 且当前 true（自动伪正残留）→ 提示 + 强制标不支持入口 |
| R14 | 不判定语义歧义（本质 A4） | 明确：自动探测的 2xx=判定且写 true、白名单≥2=判定且写 false、其余=不判定不写列；「不判定不写列」仅指 error 路径 |

### v3.1 判别矩阵（每格：返回态 / 写入值 / source）

| 请求序列 | 结果 | 返回态 | 写入 | source |
|---|---|---|---|---|
| required 200 + tool_call | 支持 | 支持（执行确认） | true | manual |
| required 200 无 tool_call | 不服从/剥参 | 不服从 required（可强制标不支持） | 不写（保留现值） | — |
| required 400 → auto 2xx | 支持但 required 不可用 | required 不可用 | true | manual-required-fallback |
| required 400 → auto 400（白名单第 1 次） | 待确认 | 待确认 | 不写 | — |
| required 400 → auto 400（白名单 ≥2） | 不支持 | 不支持 | false | probe/manual |
| required 400 → auto 400（非白名单） | 未知 | 不判定 | 不写 | — |
| required 非 400（5xx/超时） | 渠道故障 | 不判定 | 不写 | — |
| **强制标不支持（管理员）** | — | — | false | manual |
| **恢复自动（管理员）** | — | — | nil | u7/manual |

### v3.1 实施清单（最终，含 4 顾问修正）

**后端**
1. toolsprobe.Run 加 `ToolChoice` 参数；返回**结构化结果** `{Decided bool, Supports bool, Source string, State string(五态)}`（后端对抗者 R0：现有 (bool,error) 装不下五态）
2. op.ToolsProbeFn hook 签名同步；新增 `ForceToolsUnsupported` / `ResetToolsState`（强制标不支持 + 恢复自动，source=manual）
3. 判别矩阵全分支实现（含 required 降级独立 12s 预算——后端对抗者 R 发现共享 ctx 会超时）
4. required-400 的 400 不进 ≥2 registry（防污染，后端对抗者）
5. 批量接口：`POST /api/v1/group/tools-test`（items 列表 → 异步任务 + 轮询状态接口）
6. 写覆盖按证据层级：手动 required 确认 > T9 > 手动降级 2xx = 自动 2xx；强制标不支持最高级
7. resolveProbePrompt 抽公共函数（grouphealth 与 toolsprobe 复用）
8. T9 6h 后自动覆盖声明（R10）

**前端**
9. group.ts GroupItem 加 supports_tools；ItemList SelectedMember 加；Card displayMembers 透传（R 确认已到位）
10. **ItemList.tsx:162 外层守卫加 `|| member.supports_tools != null`**（R5 致命）
11. 三态小徽标（✓绿/✗红/未探测不渲染）+ tooltip 血缘 source + probed_at（不显示 probeKeyID）
12. 手动测试按钮（卡片级批量 + 编辑器 SortSection 头部）：异步任务+轮询；in-flight 去重放 mutation 缓存；编辑器结果回写本地 state；未提交条目禁用
13. 「强制标不支持」/「恢复自动」按钮（手动 required 逼不出时出现）
14. 「仅 tools 查看筛选」（谓词 `!== false`，文案区分，组列表 filter 区）
15. supports_tools 保留 undefined 不兜底（防照抄 multiplier_known 的兜底模式）

**测试**
16. 判别矩阵全分支单测（8 格）；写覆盖优先级单测；强制标不支持；批量异步；前端徽标三态 + 守卫

### v3.1 验收标准

1. go test 全绿 + 前端 tsc/lint/build
2. 分组条目 tools 小徽标（✓/✗/未探测不渲染），本地渠道条目也能显示（R5 守卫）
3. 手动测试返回五态之一 + 判别矩阵各分支写入正确
4. 「强制标不支持」/「恢复自动」可落库可撤销
5. 写覆盖按证据层级（手动 required > T9 > 降级 2xx=自动 2xx）
6. 批量异步 + 轮询 + 去重 + 编辑器回流
7. 端到端：prism.uno gpt-5.6-sol 手动 required → ✓（执行确认）

### v3.1 已知边界（发布说明）

- 复杂工具失败不自动收敛（T9 白名单协议级），「强制标不支持」是管理员持久收敛手段
- 静默剥参网关：自动 2xx 伪正 + required 逼不出 → 提示 + 可强制标不支持
- T9 双确认 false 在 6h 后可能被自动探测覆盖（如实记录）
- ≥2 registry 进程内 + TTL 重启清零
- 批量测试 = N 个真实扣费请求（UI 计费警示）

## v3.1 实施完成记录（2026-08-09，含实施后 4 顾问联合审查 + 修复）

> 流程：实施（批量接口/按钮/筛选/编辑器回流/测试）→ 4 顾问实施后联合审查（后端/前端/逻辑/数据对抗者）→ 3 P0 + 7 P1 修复 → 全量验证。

### 实施清单完成情况
- 后端：判别矩阵 8 分支 mock 测试（matrix_test.go）、批量异步任务 + 轮询（batch.go + /api/v1/tools-probe/batch + /batch/status/:task_id）、写覆盖优先级/force/reset/批量共 9 用例全过
- 前端：卡片批量按钮（事件驱动轮询）、行级 测试/强制/恢复 三按钮、编辑器 SortSection 头部按钮 + 回写本地 + 未提交禁用、组列表「仅 tools 查看筛选」、三语 locale

### 实施后联合审查发现并修复（3 P0 + 7 P1）
| # | 级别 | 问题 | 修复 |
|---|---|---|---|
| 1 | P0 | manual-force 守卫极性错误：unsupported/T9 守卫写 `NOT(supports_tools=true AND manual-force)`，保护永不存在的行，forced-false 被静默降级 | 守卫改 `supports_tools_source NOT IN (manual, manual-force)`（4 处：ApplyToolsProbeResult unsupported 分支 + ReportToolsUnsupported + T9） |
| 2 | P0 | ≥2 registry 按全文 hash：probe 侧带 `upstream error:` 前缀 vs T9 裸 body → 两侧 hash 永不相等交叉重置；动态文本（trace_id/时间戳）永不累计 → unsupported=false 真实数据形态下几乎不可达 | 改按「命中的白名单 pattern」计数（matchToolsUnsupportedPattern），跨路径/动态文本同 pattern 即累计 |
| 3 | P0 | batch 并发：Running 无锁写 + handler 序列化 goroutine 仍在 append 的活指针 | Running 置位放锁内；StartBatch 返回 BatchStatus 快照副本 |
| 4 | P1 | executed 强 true 可被后到的 unsupported/T9 覆盖（层级按到达顺序而非证据比较） | unsupported/T9 守卫排除 source=manual 行（与 1 同修复） |
| 5 | P1 | responseHasToolCall 只认 OpenAI `"tool_calls"`，Anthropic/Gemini/Responses 协议下 executed 不可达（误引导强制标不支持） | 按 channelType 分格式检测（tool_use/functionCall/function_call）+ tool_calls null/[] 空白变体 token 级排除 |
| 6 | P1 | 白名单缺口：`does not support tool calls`（复数）、`currently`/`the` 插入、OpenAI 规范 `Unrecognized request argument supplied: tools` 漏报 | 补 6 条主流网关模式（21 条） |
| 7 | P1 | FIX-E 冷却只看 SupportsTools 非 nil：U7/Reset 写 nil 绕过冷却；preset 路径传裸结构体（无 ProbedAt）每次激活全量付费重探 | 冷却只认 probed_at + 内部合并 DB 已有行最近探测时间 |
| 8 | P1 | 编辑器未提交守卫死代码（channel_id>0 恒真）→ 未保存条目触发真实扣费 | 改 `typeof m.item_id === 'number'` 判定 |
| 9 | P1 | 编辑器 writeback 无条件涂 true，与后端证据层级不一致（弱 true 覆盖 ✗、executed 覆盖 manual-force） | writeback 镜像后端守卫（弱 true 不覆盖 false / 强 true 不覆盖 manual-force / ≥2 false 不覆盖 manual） |
| 10 | P1 | 单轮询槽位：第二个批任务杀死第一个的轮询 → 重复扣费 + 孤儿任务 | 行级/批量统一受 batchActive/toolsRunning 互斥（Card + Editor），进行中发 toast「已有批量测试进行中」 |

### 验收结果
- go test 全量 30 包 EXIT=0（含新增：判别矩阵 8 分支、协议检测 8 用例、P0 回归 3 项、批量 3 项）
- 前端 tsc/lint/build 全绿（lint 2 个 set-state-in-effect → 事件驱动轮询重构解决）
- 待办：prism.uno 端到端手测（验收标准 7，需真实渠道）

### v3.1 已知边界更新
- T9 双确认 false 在 6h 后**不会被**自动探测覆盖（accepted 弱 true 不覆盖 false）；false→true 唯一路径 = U7（≥2 真实成功写 nil）或手动 executed/强制（发布说明以此为准，修正原 R10 声明）
- ≥2 registry 按 pattern 计数 + TTL 10min 重启清零；probe 与 T9 跨路径共用
- 批量测试 = N 个真实扣费请求（UI 计费警示）

## v3.1 复审记录（2026-08-09，修复后 4 顾问再审 + 修复）

> 用户要求「重新多子智能体审查」。4 顾问（后端/逻辑/数据/前端对抗者）对修复后代码再审。结论：上一轮 3 个 P0 修复本体验证正确（守卫极性、pattern-keyed registry、batch 锁与快照），但发现 **1 个新 P0 层级绕过** + 4 个 P1 + 若干 P2，已全部修复。

### 复审发现与修复
| # | 级别 | 问题（双顾问独立确认） | 修复 |
|---|---|---|---|
| 1 | **P0** | accepted（弱 true）分支 WHERE 命中 executed 的 true 行并把 source 覆盖成 probe → 后续 unsupported/T9 的 `source NOT IN (manual, manual-force)` 不再保护 → **≥2 false 覆盖 executed 强 true**（保护标记被低层级写入者先行拆除） | accepted 分支 source 改 `CASE WHEN source IN (manual, manual-force) THEN 原 source ELSE result.Source END`——probed_at 照常推进，只保留保护标记（逻辑对抗者推荐方案 B，避免与冷却耦合） |
| 2 | P1 | pattern 计数是「措辞级」非「类别级」：`tools not supported` vs `does not support tools` 同义不累计；`not support tools` 是多 pattern 子串致同义分裂 | pattern 重构为「语义类别组」map（tools_param_rejected / tool_calls_rejected / function_calling_rejected / chinese_rejected），计数 key 用类别——同义不同措辞跨路径累计 |
| 3 | P1 | 白名单漏报 40-50%：引号参数 `does not support the 'tools' parameter`、`tool calling` 形态、缩约 `doesn't`、中文 `不支持tool`/`tools 参数不支持` | 补 `doesn't support tools`、`not support the tools parameter`、tool calling 系列、中文倒装等模式（现 38 条） |
| 4 | P1 | TTL 10min 对低频渠道确认率清零：probe 命中后下次真实失败 >10min 即过期，永远 ≥2 不到 | TTL 10min→24h（平衡近期性与低频可确认；进程内 registry 重启清零不变） |
| 5 | P1 | 冷却合并查询自指失败：`Order id DESC` 取到刚创建的新行（probed_at=nil）→ 冷却从不生效 → 重复付费探测 | 查询加 `supports_tools_probed_at IS NOT NULL` 排除新行（preset 激活 FIX-G 显式继承路径仍生效） |
| 6 | P1 | 中文否定语境误伤：`不支持 tools 以外的参数` 语义=支持 tools 却命中 `不支持 tools` | matchToolsUnsupportedPattern 对 chinese_rejected 类别遇 `以外的` 排除 |
| 7 | P2 | 前端：行级按钮在批量进行中仍可点（toast 报错而非禁用）、Card force/reset 无 batchActive 互斥、预设编辑器 tools 按钮恒死（快照无 item_id）、✓/✗ 徽标 title 硬编码中文 | `toolsDisabled` 经 MemberList→MemberItem 下传（batchActive/toolsRunning/预设禁用）；Card force/reset 加 batchActive 守卫；PresetEditor 传 `enableToolsTest={false}` 隐藏；徽标 title 改 locale 键 |
| 8 | P2 | 前端 writeback 镜像滞后：后端 CASE WHEN 保护标记后前端仍用 `r.source` 覆盖 | writeback 加 `isProtectedSource` 分支：weak true 不抹 manual/manual-force source |

### 新增回归测试（8 个）
- TestAcceptedDoesNotEraseExecutedSource（P0 链路：executed→accepted→unsupported 全程保护）
- TestAcceptedPreservesManualForceSource
- TestToolsProbeRegistryCategoryAccumulates（同类别不同措辞累计）
- TestMatchToolsUnsupportedErrorNegationContext（否定语境 + 引号参数 + tool calling + 缩约）
- 保留：CrossPath/UnsupportedDoesNotOverride* 等 6 项

### 验收
- go test 全量 30 包 EXIT=0（gofmt 干净）
- 前端 tsc/lint/build 全绿
- 剩余 P2 观察项（不阻塞）：NULL source 旧行漏写 false（保守方向，可加 `source IS NULL OR`）、batch 返回 200 非 202（纯契约）、半开筛选口径、coarse pointer 触屏按钮、批信号量排队无超时

## v3.1 第三轮复审记录（2026-08-09，过度修改检测 + 真实遗漏）

> 用户要求再 review，且重点检查「过度修改」（干到 10 即可却改到 20）。后端对抗者复审 + 主席自检。

### 过度修改检测结论（用户核心关切）
**整体恰到好处，非明显过度**——6 项候选修改中 4 项被证明为真实修复（非对抗压力产物）：
- **TTL 10min→24h：必要**。TTL 是计数窗口上限，风暴内 5 次失败在 10min 与 24h 下都确认，行为无差异；24h 只让相距数小时的独立失败可累计（证据质量更高），且低频渠道在 10min 下 ≥2 几乎不可达。
- **冷却 `IS NOT NULL` 排除新行：必要**。bug 不在新行而在**重建行**：同 (channel,model) 旧行近期 probed_at、行删后重加时 Order id DESC 取到新行自己（nil）→ 必重探 → 付费重复 + ApplyToolsProbeResult 全量更新翻转旧行已确认结论。
- **CASE WHEN 保护 source：可商榷但保留**。替代（WHERE 跳过）牺牲 probed_at 推进 → 冷却耦合 → 探测风暴。
- **死代码删除：必要**。
- **可商榷（非过度）**：pattern 23→38 条膨胀（同义屈折变体多，可用正则精简，非正确性影响）；两条 registry 测试轻度重复（可合并，非病态）。

### 真实遗漏（本轮新发现 + 修复）
| # | 级别 | 问题 | 修复 |
|---|---|---|---|
| 1 | **P1** | **Reset/Force 不清 ≥2 计数 registry**：管理员「恢复自动/强制」后，24h TTL 内残留失败计数让单次新失败立即绕过 ≥2 门槛（「重新评估」只给 1 次新证据资格） | ForceToolsUnsupported 调 `toolsProbeCounts.reset`；ResetToolsState 调 `toolsProbeCounts.reset` + `toolsSuccessRegistry.reset`；新增 TestResetToolsStateClearsFailureRegistry |
| 2 | P2 | batch 任务 runBatchItem panic 时 Running 永 true → cleaner 不回收 + 前端永轮询 | Running=false 改 defer 兜底（safe.Go recover 后仍置位） |
| 3 | P2 | reprobe/batch 对 pending/unknown/required_ignored 返回 supports:false（未判定伪装成不支持） | 记录为已知边界（DB 安全，state 字段可消歧，前端不依赖 supports 布尔） |
| 4 | P2 | 多类别同命中时 map 迭代序致计数 key 抖动（永不累计，保守失败） | 记录观察（罕见场景） |
| 5 | P2 | 审计 key（firstEnabledKeyID）与实际探测 key（首个 enabled 非空 secret）可能不一致 | 记录观察（多 key 渠道审计失真） |

### 验收
- 死代码 UpdateGroupItemToolsSupport 已删净（grep 无引用）
- go test 30 包 EXIT=0，gofmt 干净；前端本轮零改动
- 前端 3 项候选（props 膨胀/行级 3 按钮/writeback 镜像守卫）经前两轮评估为「内网自用可接受」，未过度收口

## 剩余计划整体方案（2026-08-09，登记 4 项 → 摸底收窄为 3 项实施）

> 用户拍板：① 参数覆盖**不做分组级**（渠道级已完整实现：helper.ApplyParamOverride + channel.ParamOverride + 前端 Create/site-channel 编辑）；② 稳定性修补**改为本仓库自查**（外部仓库 makeslice bug 基于自研日志，本仓库用 zap 线程安全，不适用）；③ 4 项整体方案一次过。

### 摸底结论

| # | 功能 | 现状 | 实际缺口 |
|---|---|---|---|
| 1 | models.dev 能力分类预填 | price.go 拉 models.dev api.json，**只解析 ID+Cost**；modelvendor 仅厂商索引；CanonicalModel 无能力字段 | 能力字段解析 + 索引 + 入口标记 + 前端徽标 |
| 2 | 熔断管理面 | circuit.go 核心完整（IsTripped/RecordSuccess/RecordFailure/GetCooldown，sync.Map）；设置前端已有 | **状态快照导出 + 手动重置端点 + 前端面板** |
| 3 | 分组级参数覆盖 | —— | **取消**（渠道级已满足「某渠道特殊参数」需求） |
| 4 | 稳定性修补 | 日志 zap（线程安全）；MorphingDialog z-50 | **改为自查**：弹窗 z-index 遮挡、测试失败提示 |

### 方案 A：models.dev 能力分类预填（多模态入口模型能力标记）

**目标**：模型入口（CanonicalModel）标注「多模态」能力，models.dev `capabilities` 作默认预填 + 管理员可覆盖（用户拍板于 task_plan 1402 行）。

- 后端：
  1. `price.go` registryModel 加 `Capabilities` 字段（models.dev 能力字段如 vision/multimodal/reasoning——实施时按 api.json 实际字段解析，宽松容错）
  2. `modelvendor` 加能力索引 `ReplaceCapabilityIndex(modelName → capabilities)` + `LookupCapability(name)`；与厂商索引同源（price.go UpdateLLMPrice 一并更新）
  3. `CanonicalModel` 加 `VisionCapable *bool`（`json:"vision_capable,omitempty"`，nil=未知/未预填）+ `VisionManual bool`（管理员覆盖后不再被 models.dev 覆盖，仿 VendorManual 模式）；迁移补列
  4. `catalog.go` 建 CanonicalModel 时预填：models.dev capabilities → 有值则写，模型名规则（5v/vision 后缀）兜底，两者都没有 → nil
- 前端：模型管理列表 CanonicalModel 行加「多模态」徽标（vision_capable=true 显示，nil/false 不渲染）+ 手动开关（可覆盖）
- 边界：models.dev 不可达 → 跳过预填（降级为模型名规则，再 nil）；同名能力被改的中转站靠管理员覆盖

### 方案 B：熔断管理面

**目标**：熔断状态可视化 + 手动重置（外部项目可借鉴项 #1）。

- 后端：
  1. `circuit.go` 加 `Snapshot() []CircuitStatus` 导出全部条目（key=channelID:keyID:modelName → State/ConsecutiveFailures/TripCount/LastFailureTime/剩余冷却）；`ResetCircuit(channelID int, keyID int, modelName string)` 手动重置（单条或按渠道全清）
  2. handlers 加 `GET /api/v1/circuit/status` + `POST /api/v1/circuit/reset`（body: channel_id?/model_name?，空=全量重置，需 Auth）
- 前端：新增 CircuitBreaker 管理面板（状态列表：渠道/模型/状态/连续失败/熔断次数/剩余冷却 + 重置按钮）；入口放设置页 Reliability 或独立菜单（内网自用，可放 Reliability 节内）
- 边界：进程内状态（重启清空，文档明示）；熔断是同步热路径，Snapshot 用 RLock 浅拷贝

### 方案 C：稳定性自查（替代外部 cherry-pick）

- 日志刷写：zap 线程安全，无 makeslice 类问题 → 无需改动
- 弹窗遮挡：核对 MorphingDialog（z-50）与 AlertDialog/Dialog 层级冲突；实测多弹窗叠加
- 测试失败提示：确认 group-health 测试失败是否有 toast（tools 批量/健康检查路径）

### 验收

- 项 A：go test 全绿 + 前端 tsc/lint/build；CanonicalModel 建表后 vision_capable 正确预填（models.dev 可达时）
- 项 B：熔断状态列表/重置端点 + 前端面板可用；设置阈值/冷却生效
- 项 C：自查报告（无问题则记录「无此类问题」）

### 待顾问审查项

- 项 A：CanonicalModel 加列的迁移方式（仿 VendorManual）；预填触发时机（建表时 vs 定期）
- 项 B：ResetCircuit 粒度（单条/渠道/全量）；Snapshot 热路径开销
- 项 C：自查范围是否覆盖实际风险

## 剩余计划实施完成记录（2026-08-09，A/B/C 三项 + 实施后 3 顾问审查 + 修复）

> 用户拍板：参数覆盖不做分组级（渠道级已完整）；稳定性改为本仓库自查；整体方案一次过。代理 127.0.0.1:7897 拉取 models.dev 成功，字段结构先验真。

### 实施内容
**A. models.dev 视觉能力预填（只读徽标版）**
- 先验真：models.dev api.json 无 capabilities 对象，真实结构为 `modalities:{input:[...],output:[...]}`（tool_call/reasoning 为独立 bool，绝不触碰——用户红线）
- modelvendor：ReplaceVisionIndex（VisionEntry + prefixAliases 过滤防托管方）+ LookupVision
- price.go：visionIndex 遍历**全部** provider（不套价格白名单，覆盖 pixtral 等非价格厂商）+ registryHasVision 宽松解析（RawMessage，单条异常不炸价格链路）+ key/ID 双写（vendor/ 前缀可查）
- catalog.go：resolveVisionCapable（索引优先 + 后缀兜底）+ create 预填 + 存量回填（VisionCapable==nil 守卫）
- 迁移 026：存量后缀回填（迁移时 models.dev 未加载）
- 前端：CatalogCard 只读「🖼️ 多模态」徽标（vision_capable===true 显示，nil/false 不渲染）

**B. 熔断管理面**
- circuit.go：Snapshot()（逐 entry 持锁 + 惰性清理）+ ResetCircuit（scope=item/channel/all）
- handlers：GET /api/v1/circuit/status + POST /api/v1/circuit/reset（Auth，全量需显式 scope=all）
- 前端：独立 Circuit 菜单（默认只看熔断中 + 渠道筛选 + 三色状态徽标 + useNow 本地倒计时 + 全量重置强确认 3s 超时还原）

**C. UpdateLLMPrice 30s 超时兜底**（models.dev 挂起不再永久阻塞后续价格更新）

### 实施后审查发现并修复
| # | 级别 | 问题 | 修复 |
|---|---|---|---|
| 1 | P1 | 熔断惰性清理抹掉低频故障计数（Closed 且 >10min → 删 → 计数归零 → 熔断免疫） | 清理条件加 `ConsecutiveFailures==0`（仅完全健康才删）+ 回归测试 |
| 2 | P1 | modalities 解析耦合价格主链路（单条结构异常炸整个价格更新） | registryModel.Modalities 改 RawMessage 防御解析，异常降级 false |
| 3 | P1 | vision 索引绑死价格白名单（pixtral 旗舰等漏标）+ vendor/ 前缀查不到 | visionIndex 遍历全部 provider + key/ID 双写 |
| 4 | P1 | 熔断状态未知 fallback「正常绿」→ 管理员误判无熔断 | fallback 改灰色「未知」+ state.unknown locale 键 |
| 5 | P1 | navbar circuit 键三语缺失 + NavItem 类型未同步 | navbar locale 补 circuit 键 + NavItem/NAV_ORDER 加 'circuit' |
| 6 | P1 | 全量重置确认无取消/超时（双击即触发全清） | 确认态 3s 超时自动还原 |
| 7 | P2 | omni 后缀误判（nvidia-nemotron-3-nano-omni 纯文本被标多模态） | 记录已知边界（纯展示，可接受） |

### 验收
- go test 30 包 EXIT=0（新增：vision 索引过滤/大小写 2、resolveVisionCapable 1、熔断 Snapshot/清理/Reset 5、清理 P1 回归 2）
- 前端 tsc/lint/build 全绿
- 已知边界（发布说明）：后缀兜底有误判可能（纯展示）；半开/closed 无倒计时；重置精确匹配大小写敏感

## NAS 同步故障修复记录（2026-08-11）

> 现象：NAS 容器（镜像 2b66078，端口 8088）多个站点同步 500，`UNIQUE constraint failed: group_items.group_id, group_items.channel_id, group_items.model_name (2067)`。多顾问（本质/后端/逻辑/数据）复审 + NAS 实测验证后修复。

### 根因（复审确认）
- `internal/sitesync/project.go` 原 `rewriteManagedGroupItemsForAccount` 对 group_items 逐条裸 `UPDATE channel_id`，无冲突保护（全仓库唯一无 OnConflict 的改写点）。同 (group_id, model_name) 合法分布在多个 split-route channel 的条目被重定向到同一目标渠道且目标已有该组合时，撞唯一索引 → 同步 500，failed 账号半提交残留态每次重试复现。
- 数据佐证（NAS 实测）：同账号内 (group,model) 跨 ≥2 channel 命中 15 组，报错账号 2/23/30 均在列（如账号2 的 grok-4.5 分布在 Grok/default/Grok::anthropic/default::anthropic 4 渠道）。
- 范围修正：10 个 failed 账号仅 3 个（2/23/30）是 rewrite 类，其余 7 个为各自上游问题（Cloudflare 403×3、401 暂停、404、超时、系统升级）。

### 修复（3 处顾问修正）
1. 判定域对齐唯一索引三列 (GroupID, targetChannelID, ModelName)，而非 binding 的 baseGroupKey（逻辑对抗者 P1-3）
2. 事务内先删 stale 再搬运（消除碰撞源）
3. 「目标组合已存在（排除自己）」→ 删除当前条目（目标行已代表该组合），覆盖残留态与同批互相撞（事务读己写）

### 实施与验证
- 提交 `3832383`，30 包 go test EXIT=0；新增 2 个回归测试（残留态 + 同批碰撞）
- 交叉编译 Linux amd64 部署到 NAS（scp 临时名 + mv 规避 root 文件权限，旧二进制备份 `octopus-updated.pre-3832383`）
- 验证：容器版本 v1.4.0-dev/3832383；账号2 从 failed → partial，grok-4.5 从 4 条收敛到 2 条（按 routeType 合法拆分）；账号30 自动 success

### 修复后同步状态（NAS 实测）
- success 21（账号 1/12/16/17/18/24/26/28/29/30/31/34/36/40/41/44/45/46/55/59/77）
- partial 36 / idle 15 / failed 9
- failed 明细：anyrouter.top(4)=无可用 key（cookie 会话拉不到 token，待处理）；11=模型未确认；21=303 非 JSON；37=404；38=401 暂停；47/52/56=Cloudflare 403；57=系统升级

## anyrouter.top 同步问题调研记录（2026-08-11，网络实测）

### 实测结论（匿名 curl 探测）
- **anyrouter.top 基于 new-api**：`/v1/models` 无 token 返回 `{"error":{"type":"new_api_error",...}}`（new-api 专有错误类型）、`x-oneapi-request-id` 头 → 源站 new-api（或兼容分支）
- **全站被阿里云 ESA WAF + JS 挑战拦截**（`acw_sc__v2` cookie，`server: ESA`），仅 `/v1/*` 路径穿透
- `/api/token/`、`/api/user/self` 等管理接口 curl 均返回 JS 挑战页（2714B 混淆脚本），**真实接口被 WAF 挡在挑战层**
- 源站反代 Caddy（`via: 1.1 Caddy`）；公开文档在 `docs.anyrouter.top`（腾讯 EdgeOne，非主站 /docs）

### 对 Octopus 同步失败的解释
- 账号4（anyrouter.top，cookie 凭据）报「无可用 key」：`fetchAnyRouterManagementTokens` 用 cookie 请求 `/api/token/`，被 ESA JS 挑战拦截 → 拉不到真实 token → `newNoAvailableKeyError`
- 项目已有 CF JS 挑战处理（verification-bridge、anyRouterChallengeACW），但 ESA 挑战脚本（acw_sc__v2）与 CF acw 挑战形态可能不同，需确认现有 handler 是否覆盖

### anyrouter 调研补充（new-api 对接模式，第二调研 agent）
- **anyrouter = new-api/one-api 老版本**：`/api/user/sign_in` 旧签到路径、session cookie + `New-Api-User` 头鉴权（旧式），非新版 JWT
- **WAF 需三件套**：acw_tc / cdn_sec_tc / acw_sc__v2；部分网络须走代理
- **New-Api-User 头必须**：缺失报「未提供 New-Api-User」，与 session 不匹配报「不匹配」——`anyRouterDiscoverUserID` 探测失败会导致 401
- **本地实现已覆盖**：New-Api-User 头（anyrouter.go:692）、cookie fallback（fetchAnyRouterTokensByCookie）、acw_sc__v2 求解器（:1292）
- **候选失败原因（运行态）**：① cookie session 过期（~1 月，账号4 cookie 可能 8/5 前保存）② WAF 三件套不全（本地只显式解 acw_sc__v2）③ userID 探测失败
- **第三方参考**：metapi（cita-777/metapi，newApi.ts 整套 cookie 候选+user_id 探测+acw 求解）、anyrouter-check-in（millylee，CloakBrowser 拿 WAF cookie）、anyrouter-pool（FastAPI 多账号聚合，HTTP_PROXY 必需）

### anyrouter 调研补充（anyrouter API 对接，第三调研 agent）
- **anyrouter = NewAPI 系魔改平台**（非 one-api/独立网关）：登录页/控制台/token 页/pricing 均 NewAPI 前端路由风格
- **管理 API 路径**（NewAPI 标准）：`GET /api/token/?p=0&size=100`（token 列表）、`GET /api/user/self`（余额）、`GET /v1/models` 或 `/api/user/models`（模型）、`GET /api/pricing`（定价）、`POST /api/user/login`（登录）
- **认证双通道**：① `Authorization: Bearer sk-...`（API Key）；② `Cookie: session=...` + `New-Api-User: <user_id>` 头（cookie 会话，JWT 内含 uid，gob 编码）
- **成功案例（可直接参考）**：metapi（cita-777/metapi，newApi.ts + newApiShield.ts node:vm 解 acw_sc__v2，最接近 Octopus 场景）、anyrouter-pool（多账号聚合+渠道同步）、anyrouter-check-in（CloakBrowser 拿 WAF cookie）、anyrouter-gateway（CF Worker 透传+Claude Code 指纹头）
- **已知问题**：WAF 强风控（acw_tc/cdn_sec_tc/acw_sc__v2 三件套）；session cookie ~1 月失效提前 401；Access Token 入口可能隐藏（需 cookie 导入）；官方数据库不稳（1040 Too many connections）；Anthropic 接口指纹校验（cc_version 等头）

### 本地代码 vs 调研对照（账号4 失败诊断）
- 本地 anyrouter.go **已实现**：New-Api-User 头（:692）、cookie fallback（fetchAnyRouterTokensByCookie）、acw_sc__v2 求解器（:1292）、shield challenge 检测（:1101）、token 分页（/api/token/?p=0&size=100）
- 功能覆盖完整 → 账号4「无可用 key」更可能是**运行态**：① cookie session 过期（~1月，账号4 cookie 可能 8/5 前保存）；② WAF 三件套不全（本地显式解 acw_sc__v2，acw_tc/cdn_sec_tc 依赖 jar 自动存）；③ userID 探测失败（New-Api-User 头带错 id → 401）

### anyrouter 账号4 断点实测（2026-08-11，用用户新 cookie）
- **决定性实测**：session + 无 New-Api-User 头 → 401「未提供 New-Api-User」；+ `New-Api-User: 137417` → 成功拉用户+token
- **结论**：WAF 三件套（acw_tc/cdn_sec_tc/acw_sc__v2）非断点——代码自动维护（jar 存 Set-Cookie + acw_sc__v2 自动求解），**手动存账号会过期失效**
- **真正断点**：① DB 里账号4 的 session 是 8/5 前保存的，已过期（session ~1 月有效期）；② `New-Api-User` 头必须且 userID 须解对（137417）
- **待做**：更新账号4 session（UI/API）；验证 anyRouterDiscoverUserID 能从新 session 解出 137417
- **不需要**：账号管理加 acw_tc/cdn_sec_tc 字段（会话级，非账号级）

### anyrouter 账号4 同步成功（2026-08-11，切换 clash 节点后）
- **结果**：用户切换 clash 出口节点后，anyrouter.top 同步成功
- **诊断修正**：真正的阻断因素大概率是「NAS 出口节点被 anyrouter WAF/上游风控」——切换节点（出口 IP 变干净）后 WAF 放行、session 生效。此前「session 必然过期」的推断不成立或不是主因（本地 7897 代理下用户 session 实测有效）
- **印证调研发现**：anyrouter-pool 明确要求 HTTP_PROXY；anyrouter 对出口 IP 风控敏感
- **结论**：
  - 不需要给账号管理加 acw_tc/cdn_sec_tc/acw_sc__v2 字段（会话级反爬 cookie，Octopus 自动求解/维护，手动存会过期失效）
  - 账号身份只需 session（UI 填裸值，normalizeCookieValue 自动加 session= 前缀）
  - 若再次同步失败，优先排查 clash 出口节点被风控（换节点重试），而非凭据问题

## 模型能力标识扩展方案（2026-08-11，待顾问审查）

> 用户需求：把现有「多模态（vision）标识」扩展为完整能力分类，徽标加在 ① 分组卡片分组名后 ② 模型目录卡片 ③ 模型发现卡片。

### 用户拍板
- **不显示文本**（人人都有，噪音）
- **tools 不来自 models.dev**——沿用现有 `supports_tools` 实例级探测（之前已拍板 models.dev 不得用于 tools 确认，tools 是实例级动态属性）
- **多模态合并一个徽标**（image/video/pdf 不细分）
- **分组聚合用并集**——按分组名（规范模型名）匹配 CanonicalModel，任一个支持就显示；关注模型名能力声明，不关注渠道是否阉割（阉割由 tools 探测负责）

### 能力维度（4 个静态 + tools 实例级）
| 徽标 | 来源 | 判定（models.dev 实测字段） |
|---|---|---|
| 🖼️ 多模态 | 静态 | input 含 image/video/pdf |
| 🧠 推理 | 静态 | `reasoning=true` |
| 🎤 语音 | 静态 | input 含 audio 或 output 含 audio |
| 🎨 生图 | 静态 | output 含 image |
| ~~🔧 工具~~ | 实例级 | 沿用 supports_tools（行级 ✓/✗ 徽标已存在，分组不聚合静态） |

### 后端改动
1. `model/routing.go`：CanonicalModel 加 `Capabilities uint8` + 位常量（CapMultimodal/CapReasoning/CapVoice/CapImageGen）
2. `modelvendor/index.go`：VisionEntry → CapabilityEntry（4 位），ReplaceVisionIndex 扩展为 ReplaceCapabilityIndex（保留旧函数）
3. `price/price.go`：visionIndex → 构建 4 位能力（读 input/output + reasoning）
4. `op/catalog.go`：resolveVisionCapable → resolveCapabilities（建表预填 capabilities；存量回填保留 VisionCapable 兼容）
5. 迁移 027：存量 CanonicalModel 回填 capabilities
6. **分组读路径**：Group 响应注入 capabilities（分组名→CanonicalModel 并集聚合）

### 前端改动
1. `CapabilityBadges.tsx`（新组件）：4 徽标渲染，支持 compact 模式
2. `model-catalog.ts`：CanonicalModel 加 capabilities
3. `group.ts`：Group 加 capabilities
4. 三处接入：Catalog.tsx（目录卡片，替换现有 vision 徽标）、DiscoveryRow.tsx（发现行）、Card.tsx（分组名后）

### 方案修订 v2（2026-08-11，吸收 3 顾问审查 P0/P1 修正，实施以此为准）

**3 顾问审查发现并修正：**
| # | 审查问题 | v2 修正 |
|---|---|---|
| P0-1 | `Capabilities uint8` 丢三值语义（零值=0 无法区分未知/不支持，存量回填 `==nil` 幂等失效） | 改 `Capabilities *uint8`（指针三值：nil=未知/0=明确不支持/非0=能力位图） |
| P0-2 | 多模态位与 VisionCapable 判定不同源（image/video vs image/video/pdf）双写不一致 | 多模态位 = **image/video**（与 VisionCapable 完全同源）；pdf 不算（实测 deepseek-chat input=[text,pdf] 纯文本，标多模态是污染） |
| P0-3 | 同模型多 provider 条目 map 覆盖竞态（deepseek-reasoner 的 reasoning true/false 混杂随机漂移） | 合并策略 = **并集**（任一条目位=1 则置位，与分组聚合语义一致） |
| P1-4 | pdf 判多模态污染；语音覆盖窄（gpt-4o-realtime 无数据源）；reasoning 字段不可靠（ernie-x1.1 reasoning:false） | pdf 排除（见 P0-2）；语音按 input/output audio 分 2 bit；reasoning 保守方向（==true 才标，false 不代表非推理） |
| P1-5 | 迁移 027 只能回填多模态位（语音/生图/推理无后缀规则） | 027 回填多模态（从 vision_capable 转）+ 推理用 naming 兜底；语音/生图等 CatalogSync 预填；声明已知边界 |
| P1-6 | 分组聚合规则/注入层未定义；改名/手建分组静默无徽标 | **normalized 精确名匹配**（复用 NormalizeModelIdentity + canonicalByNormalized 缓存）；改名/手建无匹配 → 空徽标（可接受，tooltip 说明） |
| 前端 P0 | 发现行是空数据源（discovered 端点未注入 capabilities） | **补后端**：discovered 端点按模型名查 modelvendor 能力索引注入 |
| 前端 P1 | capabilities 传输契约未定义（uint8 位域穿透 TS） | API 输出解码后 `capabilities: string[]`（如 ["multimodal","reasoning"]），前端不复制位常量 |
| 前端 P1 | 目录卡片 4 徽标过载（~280px 挤没模型名） | CapabilityBadges 组件 + `size` prop：目录卡用 sm（emoji+文字换行）、发现行用 xs（纯 emoji+tooltip）、分组卡用 xs |
| 前端 P1 | compact 纯 emoji 丢双通道（色盲/黑白渲染） | 每徽标 `aria-label` + Tooltip 组件（非 title）+ 颜色+字形+文字三通道 |
| 前端 P1 | i18n 未提（visionCapable 键去留） | 新建 `model.catalog.capability.{multimodal,reasoning,voice,imageGen}` 三语键；visionCapable 替换后删除 |

**v2 最终能力维度（4 静态位 + tools 实例级）**
| 位 | models.dev 判定（并集，多 provider 合并） |
|---|---|
| CapMultimodal | input 含 image 或 video（不含 pdf） |
| CapReasoning | reasoning == true |
| CapVoiceInput / CapVoiceOutput | input 含 audio / output 含 audio（2 bit，展示合并 🎤） |
| CapImageGen | output 含 image（video 输出记位暂不展示） |

**tools 不来自 models.dev**（用户拍板）：沿用 supports_tools 实例级，行级 ✓/✗ 徽标；分组卡片能力徽标不含 tools，tooltip 注明「模型名声明能力，渠道阉割由 tools 探测反映」。

## NAS 一键更新问题排查记录（2026-08-11）

### 现象
- NAS 一键更新失败：下载 60MB release 二进制经 clash 代理 HTTP/2 流中断（`PROTOCOL_ERROR`），`io.ReadAll` 超时
- 且版本检查接口 `GET /api/v1/update` 返回 500（`api.github.com ... EOF`）
- 前端版本显示 dev（手动编译未注入 NEXT_PUBLIC_APP_VERSION）

### 根因（3 个独立问题）
1. **下载 HTTP/2 流中断**：默认 transport 协商 HTTP/2，大文件经代理多路复用流易断 → 修复：下载专用 client 禁用 HTTP/2（ForceAttemptHTTP2=false）
2. **HTTP/1.1 一刀切破坏版本检查**（7358360 前）：api.github.com 对 Go HTTP/1.1 稳定 EOF（实测复现）→ 修复：按主机选择——github.com/releases 下载用 HTTP/1.1，api.github.com 版本检查保留 HTTP/2
3. **前端版本 dev**：`APP_VERSION = NEXT_PUBLIC_APP_VERSION || 'dev'`，Makefile deploy 未注入 → 修复：make deploy 自动注入 `NEXT_PUBLIC_APP_VERSION=VERSION`

### 修复提交
- 7db3ab4：更新下载 HTTP/1.1
- 7358360：按主机选择协议（api.github.com HTTP/2 + github.com HTTP/1.1）
- d9a281a：Makefile 注入前端版本号

### NAS 当前状态
- v1.4.2（Commit 7358360，手动部署含全部修复）
- 版本检查 EOF 已清零；下载走 HTTP/1.1 规避代理流中断
- 前端版本 v1.4.2（非 dev）
- 后续 v1.4.3+ 可一键更新（修复后逻辑）

## v1.4.3 发布记录（2026-08-11）

### 内容（9e15bb9，dev → v1.4.3 tag）
- 能力徽标数据源修复：models.dev 拉取按「有代理走代理、无代理直连」策略（P0-1 代理 fallback，price.go）——NAS 直连超时问题解决，能力索引成功填充
- 迁移 027 冻结修复（P1-1）：仅当 `VisionCapable == nil` 才写 `vision_capable`（只写 true，不写 false），不再用后缀猜测覆盖注册表/历史证据
- Makefile 版本注入（P1-2）：`make deploy VERSION=...` 同时注入后端 `conf.Version` 与前端 `NEXT_PUBLIC_APP_VERSION`，前端不再显示 dev
- API Key 卡片内联开关：启用 + 仅 tools 开关直接放在密钥卡片上，无需进编辑对话框
- 保留 v1.4.2 的一键更新修复（HTTP/1.1 下载 + api.github.com HTTP/2）

### 验证
- 全量测试 31 包 EXIT=0；go build + make build（注入 v1.4.3）通过
- NAS 部署 9e15bb9：Version v1.4.3 / Commit 9e15bb9；前端版本 v1.4.3
- 能力位图落库验证：canonical_models 22/33 有 capabilities（值 1/2/3/7），4 个模型 =7（多模态+推理+语音，如 gemini-3.5-flash）——语音位只能来自 models.dev，证明代理拉取成功

### Release
- tag v1.4.3 + workflow success（含 Docker Alpine/Debian 镜像）
- 双语 release notes 已更新（RELEASE_NOTES_v1.4.3.md）

## Vision Bridge 调研与集成方案（2026-08-12）

### 背景
用户提出「Image Recognition Tool」调研：让不支持视觉（multimodal）的纯文本模型也能识图。

### 调研结论
- 不存在知名同名开源项目；该能力是 2026 年爆发的小生态，通用原理 = **网关层拦截 image_url 内容块 → 调独立 VLM 把图转文字描述 → 替换图片块喂给纯文本模型**。
- 候选对比（集成适配性视角）：
  - **Zesuy/Plugin-Deepseek-Vision**（Go/MIT/102⭐/活跃，CLIProxyAPI v7 原生预处理插件）——**最值得借鉴**：三协议图片发现/重写算法、VLM 调用契约、fail-closed 错误契约、LRU 文本缓存、SSRF 防护校验、多图联合分析+编号 marker 替换、模型回退链。但深度耦合 CLIProxyAPI cgo 插件 ABI，只能提取算法层，不能直接移植。
  - liustack/modlens（TS/572⭐）：图片→结构化 JSON 证据（OCR+布局+实体），prompt 设计思路可参考。
  - thomasunise/visionbridge（48⭐）：独立 OpenAI 兼容微型代理形态，文档最好；作为「不改网关的旁路方案」备选。
  - one-api/new-api/ollama 均无内置此能力；CLIProxyAPI 是唯一做成官方插件体系的网关。
- **集成形态结论：做 Octopus 网关内建中间件（不引入 cgo 插件框架），复用其算法设计。**

### Octopus 架构关键事实（读码确认）
- 三协议入站已统一为 `InternalLLMRequest`（internal/transformer/model/model.go），content parts 规范化含 `image_url`（MessageContentPart.ImageURL）。
- 已有降级先例：`FlattenUnsupportedBlocks`（alternation.go）把 document 块→text hint —— vision bridge 语义可挂靠此处。
- 已有能力索引：`modelvendor.LookupVision(name)` + `model.CapMultimodal` 位（v1.4.3 已能从 models.dev 拉取填充）。
- relay 主流程：`Handler()` → `parseRequest()`（inbound 归一化）→ 路由规划 group/iter → 通道循环内 `internalRequest.Model = item.ModelName`（relay.go:278）→ `ra.attempt()`。**插入点 = 通道选定、outbound transform 之前**。

### 集成方案（Vision Bridge 中间件）
**触发条件**（全部满足才走 bridge）：
1. 请求含 `image_url` 内容块（含 data URI 与 URL 两种形式，Anthropic/Gemini inbound 已归一化）；
2. 目标通道模型 `LookupVision == false`（无 multimodal 能力）；
3. 全局开关 `vision_bridge.enabled=true`（按 channel/model 可配白名单）。

**执行时序**（插入 relay.go 通道循环内、`ra.attempt()` 前）：
1. 扫描 `InternalLLMRequest` 所有 message 的 MultipleContent，收集 `image_url` 块（含 tool_result 内图片，参照 Plugin-Deepseek-Vision 的 walker/rewrite 设计）；
2. 单次 VLM 请求联合分析全部图片（多图保留顺序关系）；
3. 逐图替换为文本块：`[Image N — Visual analysis]` + Visible text/Visual description 段落；追加 `[Images N — Joint visual analysis]` 联合块；
4. 替换后 re-scan 校验无残留 image_url，否则报错（fail-closed）。

**VLM 后端**：OpenAI 兼容 client，可配 `base_url + model + api_key_env`。默认建议免费 GLM-4V-Flash；本地隐私场景可指到 Ollama（moondream2 / llava），同一 client 天然支持。支持最多 3 个 fallback 模型按序回退（超时预算公平切分）。

**失败策略**（网关语义优先）：
- 含图请求的通道排序优化：优先路由到支持视觉的通道（零延迟透传）；仅当无视觉通道可用时才启用 bridge。
- VLM 失败：跳过当前通道尝试下一个；全部视觉通道/VLM 均失败 → 502 `vision_fallback_exhausted` + attempts 明细。**绝不把原图透传给纯文本模型**（上游会 400，且属静默降质）。

**流式**：先预处理完成再开始响应流（TTFB 叠加 VLM 延迟，参照插件设计），响应流不触碰。

**安全**（复用 Plugin-Deepseek-Vision 的 ValidateImageReference 范本）：
- 只接受 http(s)/data:image URI；拒绝 file://、内网/loopback/link-local IP（防 SSRF）；
- 字节上限：单请求 20MB、单图引用 15MB；非法 base64 / image/* 通配拒绝；不回显上游文本/URL/凭证。

**缓存**：进程内 LRU，key=SHA256(语言+prompt+模型链+图片身份哈希)；URL 图片短 TTL(120s)、data URI 长 TTL(900s)；只缓存派生文本与不可逆哈希，不缓存图片字节。

**配置项草案**（internal/conf）：
```yaml
vision_bridge:
  enabled: false
  vision_model: "glm-4v-flash"
  vision_base_url: ""
  vision_api_key_env: "OCTOPUS_VISION_API_KEY"
  vision_fallback_models: []
  language: "auto"                 # zh/en/auto
  request_timeout_seconds: 120
  max_inflight_vision_requests: 4
  max_images_per_request: 8
  max_request_bytes: 20971520
  max_image_reference_bytes: 15728640
  max_result_chars: 20000
  analysis_cache_size: 128
  analysis_cache_ttl_seconds: 900
  analysis_url_cache_ttl_seconds: 120
  target_channel_ids: []           # 空 = 全部；指定则仅这些 channel 走 bridge
```

**新增文件**：
- `internal/relay/visionbridge/`：discover.go（图片发现）、rewrite.go（替换）、vlm.go（OpenAI 兼容 client + fallback）、prompt.go（描述模板，参照插件的固定安全模板：指令先行、拒绝图片内指令注入、focus hint 截断 2000 rune 且包 `---` 分隔符）、cache.go（LRU）、safety.go（ValidateImageReference）、limits.go（并发信号量 4 + 超时）。
- 修改：`internal/relay/relay.go`（通道循环内接入 + 含图请求路由排序）、`internal/conf`（配置结构）、`internal/transformer/model`（加 HasImages() helper）。

### 待定/后续
- VLM 后端最终选型（免费云端 vs 本地 Ollama）与 API key 来源；
- 失败策略按上述推荐实现，如倾向纯 fail-closed 可改；
- 是否将含图请求的通道排序纳入 balancer 策略（v1 先做插入点内线性扫描）。

### Step 0a 价值链验证结果（2026-08-12，PASS）

经 NAS 生产网关（v1.4.3）实测 20 场景 × 121 次调用，三项决策阈值全部通过（详见 `docs/reviews/vision-bridge-step0a-report.md`）：

- ① 纯文本模型收图不可用率 **100%**（阈值 ≥70%）——失败形态全为静默降质：deepseek `200+空choices` 18/20 + 挂起 2/20；glm-5.2 拒答/幻觉 20/20。**无一例 400**，bridge 定位从「修复报错」修正为「修复静默降质」。
- ② VLM 描述成功率 **95%**（阈值 ≥80%，kimi-k3 代打——GLM 视觉系经网关 429/404 全不可用）。
- ③ 替换后可用率 **80%**（保守口径，阈值 ≥70%；剔除通道层故障后 97%）。

**决策：进入 Step 1**，并对 v0.2 方案追加两条必改项：
1. rewrite 前校验 VLM 描述非空且 ≥30 字符，空描述视为 VLM 失败（实测空描述会诱发下游自信幻觉）；
2. bridge 触发/成败判定不得依赖上游状态码（主要失败形态是 200 空响应）；relay 对「200+空 choices」的通用检测建议独立立项。

另确认：VLM 阶段中位延迟 38s（vs 原生视觉通道 9s，约 6 倍）→「含图请求优先路由视觉通道、bridge 仅兜底」升级为核心策略；默认 VLM 不硬编码 glm-4.6v-flash，改为部署环境实测 + fallback 链。
