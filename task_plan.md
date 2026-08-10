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
