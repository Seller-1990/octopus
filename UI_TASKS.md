# 前端 UI 优化任务清单

> 创建日期：2026-08-06
> 状态：需求梳理中

---

## 已确认任务

### 1. 分组卡片：倍率显示改为图标+数字

**参考**：日志明细中 key multiplier 的展示方式（小图标 + 倍率数字 badge）
**目标**：分组列表/卡片中的倍率字段，由纯文字改为与日志明细一致的紧凑图标样式
**状态**：已确认

### 2. 分组卡片：余额文字标签改为小图标

**参考**：减少视觉噪音，用直观图标代替"余额"文字
**目标**：分组卡片中「余额」标签替换为钱包/硬币等语义明确的小图标
**状态**：已确认

### 3. 分组排序：多方案可选

**目标**：分组卡片提供下拉选择器，支持多种排序预设
**预设方案**：
- 非中转优先 + 余额倒序（当前默认）
- 非中转优先 + 倍率正序
- 倍率正序 + 余额倒序
- 纯余额倒序

**继承模式**：全局设置提供默认值，每个分组可单独覆盖（显示"继承全局" + 覆盖按钮）
**状态**：已确认

### 4. 分组倍率上限管理

**目标**：设置倍率上限，同步时超过阈值的 key 自动从分组移除/禁用
**默认值**：0（不限制）
**继承模式**：全局设置提供默认值，每个分组可单独覆盖
**状态**：已确认

### 5. 慢失败 key 自动暂停

**目标**：单次耗时超限且最终失败的 key，累计达到阈值后临时暂停
**配置维度**：
- 超时阈值（如 60s）
- 连续慢失败次数（如 3 次）
- 暂停恢复时间（如 30 分钟）

**继承模式**：全局设置提供默认值，每个分组可单独覆盖
**状态**：已确认

### 6. 日志明细：倍率显示缺失排查

**现象**：部分调用记录不带倍率 badge（包括非中转站点）
**目标**：排查原因并修复，确保所有调用记录均正确展示倍率
**状态**：已确认（待代码排查）

### 7. 日志明细：实时刷新机制确认

**目标**：确认当前日志明细的数据获取方式（polling/SSE/WS）及刷新频率
**状态**：已确认（待代码排查）

### 8. 站点页：账号区域空白优化

**现象**：账号行与同步日志/签到状态之间存在较大空白，之前移走的操作行空间未回收
**目标**：收紧间距或重新编排，提升信息密度
**状态**：已确认

### 9. 分组页：负载均衡默认方案

**目标**：全局设置中提供默认负载均衡策略（轮训/随机/故障转移/加权）
**继承模式**：全局默认 + 每个分组可单独覆盖（显示"继承全局" + 覆盖按钮）
**状态**：已确认

### 10. 分组页：厂商筛选 filter chips

**目标**：分组页顶部工具栏增加厂商筛选，点击只显示包含该厂商 key 的分组
**实现**：从模型名前缀自动推断厂商标签，显示为带厂商 logo 的可点击 chip，支持多选
**状态**：已确认

### 11. 全局厂商图标体系

**目标**：统一在分组页、模型列表页、日志明细等处展示厂商 logo 图标
**参考**：日志明细已有的厂商图标样式
**状态**：已确认

### 12. 模型页精简：移除 Global Prices tab

**原因**：定价由站点同步自动获取（/api/pricing），无需手动编辑
**状态**：已确认

### 13. 模型页精简：Headers tab 移入系统设置

**目标**：Header 策略从模型页移入系统设置/高级配置
**精简为**：一个开关 + 规则列表（动作 + header 名 + 值 + 作用范围）
**状态**：已确认

### 14. 模型页精简：Catalog 隐藏高级配置

**隐藏项**：Protocol Policy、Route Candidate 编辑、Pricing 手动覆盖
**保留别名**：移入模型详情折叠面板（只读展示 + 简单增删）
**原因**：协议转换默认 auto 全自动，路由候选全自动生成，无需手动干预
**状态**：已确认

### 15. 模型列表页：UI 重设计为卡片网格

**方案**：放弃 sidebar+detail 双栏，改为全宽响应式卡片网格
**每张卡片**：
- 厂商图标（小 logo）
- 模型名
- 可用路由数（如"3 channels"）
- 自动同步的单价（input price/M）
- 启用/禁用开关
- 点击展开/弹出轻量详情（别名列表 + 路由来源渠道，只读）

**顶部工具栏**：搜索框 + 厂商筛选 chips + 同步按钮
**风格**：与分组页/站点页统一
**状态**：已确认

---

## 待确认任务

（等待用户补充）

---

## 实施方案

### 代码审查结论

| 现有系统 | 发现 |
|----------|------|
| 日志倍率 badge | `log/Item.tsx:922` — Coins 图标 + amber 色 Badge，tooltip 包裹 |
| 分组倍率 | `group/ItemList.tsx:149` — 纯文字 span，sky-blue pill，无图标 |
| 分组余额 | `group/ItemList.tsx:149` — 纯文字 span，emerald pill，含"余额"文字标签 |
| 分组排序 | `group/member-sort.ts` — 固定策略：tier0(余额倒序) + tier1(倍率正序) |
| 负载均衡 | `group/Card.tsx:354` — 4 按钮直接切换，无全局默认 |
| 日志刷新 | `api/endpoints/log.ts:317` — SSE 实时流（无 filter 时），有 filter 时 cursor 分页无 polling |
| 厂商图标 | `model-icons.tsx` 用 @lobehub/icons SVG；`VendorBadge.tsx` 仅文字色 Badge |
| 站点账号间距 | `site/index.tsx:1775` — space-y-3 (12px) 导致 header 与状态行之间空白 |
| 模型页 tabs | `model/index.tsx:14` — 4 项 VIEWS 数组 |
| 全局设置 | `setting/index.tsx` — 9 个 SettingCard，masonry 双列 |

---

### 分批实施计划

#### 第一批：分组卡片优化（任务 1-5）

**涉及文件**：
- `web/src/components/modules/group/ItemList.tsx` — 倍率/余额展示
- `web/src/components/modules/group/member-sort.ts` — 排序策略
- `web/src/components/modules/group/Card.tsx` — 排序选择器 UI
- `web/src/components/modules/group/index.tsx` — 全局/分组排序设置
- `web/src/api/endpoints/group.ts` — 排序/倍率上限字段
- `web/src/api/endpoints/setting.ts` — 新 SettingKey
- `web/src/components/modules/setting/index.tsx` — 新 SettingCard（路由默认值）
- 后端 `internal/model/` + `internal/op/` — 新设置字段、倍率上限逻辑
- 后端 `internal/outlierwindow/window.go` — 扩展 sample 增加 durationMS
- 后端 `internal/relay/relay.go` + `compact.go` — Report() 传入 span.Duration()
- 后端 `internal/task/site_outlier.go` — 慢失败候选检测逻辑

**实施步骤**：
1. 倍率 badge 改为 Coins 图标 + amber 色（复用日志样式）
2. 余额标签改为 Wallet 图标 + emerald 色
3. `member-sort.ts` 重构为多策略，增加 sortStrategy 参数
4. Card 顶部添加排序方案下拉（4 种预设）
5. 后端增加 Setting: `default_group_sort_strategy` + `default_multiplier_cap`
6. 前端设置页新增 SettingCard "路由默认值"
7. 后端站点同步时检查倍率上限并自动移除
8. 慢失败暂停（扩展现有 POR 而非独立系统）：
   - 扩展 `outlierwindow.sample` 增加 `durationMS int64`
   - `Report()` 增加 duration 参数，relay 层传入 `span.Duration()`
   - 新增慢失败评估模式：failure + duration > threshold 联合检测
   - 复用 POR 现有退役/恢复/探测基础设施
   - 新 Setting keys: `outlier_slow_fail_duration_ms`, `outlier_slow_fail_count`

#### 第二批：日志 + 站点页（任务 6-8）

**涉及文件**：
- `web/src/components/modules/log/Item.tsx` — 倍率缺失排查
- `internal/relay/relay.go:276-289` — EffectivePrice 设置逻辑
- `internal/relay/metrics.go:489` — PriceGroupMultiplier 写入
- `internal/model/log.go:176` — `omitempty` 导致 0 值不输出
- `web/src/components/modules/site/index.tsx:1775` — 账号区域间距

**排查结论（倍率缺失原因）**：
- 前端 badge 在 multiplier == 1 时故意隐藏（设计如此，1x 无信息量）
- multiplier == 0 的情况：模型未进入 Catalog（CatalogResolveIdentity 失败），或无任何价格数据
- JSON `omitempty` 使 0 值字段不出现在 API 响应中
- 日志是 SSE 实时流，无需修改刷新机制

**修复方案**：
1. 后端：当 `canonicalModel == nil` 时仍尝试 fallback 到 globalprice 获取基础倍率 1.0
2. 后端：relay `saveLog` 中即使 multiplier=1 也写入（移除 omitempty 或改用 pointer）
3. 前端：multiplier == 1 时也显示 badge（淡色样式区分，如灰色 `1x`），确保所有调用均可见倍率
4. 站点页：`space-y-3` 改为 `space-y-1.5` 或重编排为紧凑 grid

#### 第三批：分组筛选 + 厂商图标（任务 9-11）

**涉及文件**：
- `web/src/components/modules/group/index.tsx` — 厂商 filter chips
- `web/src/lib/model-icons.tsx` — 扩展为通用厂商图标组件
- `web/src/components/modules/model/VendorBadge.tsx` — 改用 SVG logo
- 新建 `web/src/components/shared/VendorIcon.tsx` — 统一厂商图标

**实施步骤**：
1. 提取 `model-icons.tsx` 中的 @lobehub/icons 为共享 VendorIcon 组件
2. 分组页工具栏增加厂商 chips（从组内模型名前缀推断）
3. 模型列表、分组、日志共用统一图标
4. 后端设置页增加默认负载均衡策略

#### 第四批：模型页精简 + 重设计（任务 12-15）

**涉及文件**：
- `web/src/components/modules/model/index.tsx` — 移除 tabs
- `web/src/components/modules/model/LegacyPrices.tsx` — 删除
- `web/src/components/modules/model/Item.tsx` — 删除
- `web/src/components/modules/model/ItemOverlays.tsx` — 删除
- `web/src/components/modules/model/Create.tsx` — 删除
- `web/src/api/endpoints/model.ts` — 移除 useModelList/useUpdateModel/useDeleteModel/useCreateModel
- `web/src/components/modules/model/HeaderPolicies.tsx` — 移入 setting
- `web/src/components/modules/model/Catalog.tsx` — 重写为卡片网格
- `web/src/components/modules/model/CatalogDetail.tsx` — 精简为轻量弹窗
- `web/src/components/modules/setting/index.tsx` — 新增 Header Policies card

**Global Prices 移除安全性确认**：
- 定价三层解析仍完整：site quote > DB table (llm_info) > globalprice (auto-fetch)
- auto-fetch (`internal/price/price.go`) 从 models.dev 定期拉取，不依赖 UI
- 设置页已有"同步模型价格"按钮保留手动触发能力
- 移除 UI tab 后，DB 中已有数据仍作为中间 fallback 层被 relay 读取
- 保留 `useModelChannelList`（group editor 使用）和 `useUpdateModelPrice`（设置页使用）

**实施步骤**：
1. VIEWS 数组删除 headers + global-prices
2. LegacyPrices + Item + ItemOverlays + Create 文件删除
3. model.ts 中移除对应 hooks（保留 useModelChannelList/useUpdateModelPrice）
4. HeaderPolicies 移入 setting 模块，简化为开关+规则列表
5. Catalog.tsx 重写：sidebar+detail -> VirtualizedGrid 卡片
6. 每张卡片：VendorIcon + 模型名 + 路由数 + 单价 + 开关
7. 点击卡片弹出 Dialog：别名列表 + 路由来源渠道（只读）
8. 顶部工具栏：搜索 + 厂商 chips + 同步按钮

---

### 技术约束

- 图标体系：Lucide（UI 操作图标）+ @lobehub/icons（厂商 logo）
- 状态管理：Zustand（本地）+ React Query（服务端）
- 继承模式后端：Setting 表存全局默认，Group/SiteAccount 字段 0/null 表示继承
- 日志实时：SSE 流模式已经是实时的，无需修改刷新机制
- 排序/倍率上限前端优先：仅涉及展示排序的可纯前端实现；涉及自动移除的需后端配合
- 慢失败暂停：扩展现有 POR 系统（outlierwindow），复用退役/恢复/探测基础设施
- Global Prices 移除：安全确认——三层定价解析不依赖 UI，auto-fetch 独立运行
- 倍率缺失修复：根因是 CatalogResolveIdentity 失败时跳过 price 设置 + omitempty 隐藏 0 值

---

## 第二轮修改点（2026-08-06 确认）

### 16. Releases 格式根治

**目标**：移除不必要的 zip，只保留各平台原生安装包
**Linux**：只保留 `.deb` + `.rpm`（移除 zip）
**Windows**：只保留 `.exe` NSIS 安装器（移除 zip）
**macOS**：暂保留 `.zip`（`.dmg` 需要 Apple 签名，无签名体验不比 zip 好）
**状态**：已确认

### 17. 站点页账号区域布局重组

**问题**：`space-y` 缩小后仍有空间浪费，根因是布局结构不合理
**目标**：将同步/签到状态信息移到账号行同一行内（右侧），而非另起一行
**效果**：整个账号卡片高度大幅缩减，信息密度提升
**状态**：已确认

### 18. 设置页补充全局默认排序策略和倍率上限

**问题**：SettingRouting 只加了负载均衡默认，缺少排序和倍率上限
**目标**：SettingRouting 卡片增加"默认排序策略"下拉 + "默认倍率上限"数字输入
**状态**：已确认

### 19. 模型目录改为"模型价格总览"

**目标**：每个模型显示它在各站点的价格对比，展示哪个站点最便宜
**定位**：查询工具，非日常操作入口，默认折叠/低优先级展示
**状态**：已确认

### 20. 站点统计数字异常排查

**现象**：用户有 61 个站点，但全部=30、成功=12、部分成功=16、失败=2、中转=5
**目标**：排查统计逻辑是否正确（数字不一致问题）
**状态**：排查中

### 21. 站点统计面板 UX 改进

**问题**：CheckinPanel 的统计让用户误以为是"全部站点"统计，实际只统计签到相关账号
**根因**：
- "全部"计数的是签到功能账号数，非站点总数
- 中转/Reserve 是叠加标签，与状态分类不互斥，但平级展示造成误解
- 未开启签到或平台不支持签到的账号被静默排除

**改进方案**：
1. 面板标题明确标注为"签到状态统计"（而非泛化的站点统计）
2. 增加真正的"全部站点"总数展示（如页面顶部）
3. 中转/Reserve 标签从状态分类中分离，改为独立筛选条件或角标
4. 考虑显示"未纳入签到统计"的账号数量，让用户知道为什么总数不等于站点数

**相关文件**：
- `web/src/components/modules/site/checkin-status.ts:155-177` - buildCheckinSummary()
- `web/src/components/modules/site/CheckinPanel.tsx:285-307` - filter tabs UI

**状态**：已确认

### 22. API 密钥独立为单独页面

**问题**：API 密钥管理放在设置页中，与低频配置项混在一起，不便日常使用
**目标**：将 API 密钥管理提升为独立页面（侧边栏/导航新增入口）
**改进**：
1. 从设置页移除 SettingAPIKey 卡片
2. 导航栏新增 "API 密钥" 页面入口
3. 独立页面可做更丰富的展示（列表+搜索+用量统计），不受卡片尺寸限制

**状态**：已确认

### 23. API 密钥页面：per-key 用量统计 + 配额管理

**目标**：独立 API 密钥页面展示每个 key 的详细统计，并支持配额限制
**展示内容（per-key）**：
- 总 Token 消耗（input/output/cache）
- 总成本
- 请求次数（成功/失败/总计）
- 最后使用时间
- 今日/本周/本月用量趋势

**配额管理**：
- 增加 per-key 额度上限设置（如每月最大 token 数或金额）
- relay 层请求前检查是否超限，超限返回 429
- 支持额度周期重置（每日/每周/每月）

**后端改动**：
- 新增 API 查询接口：per-key 统计聚合（基于已有 UsageAggregate/RelayLog）
- APIKey model 增加 quota 相关字段（quota_limit, quota_period, quota_used）
- relay 层增加 quota 检查逻辑

**前端**：
- 独立页面列表视图，每行展示 key 名称 + 统计摘要
- 点击展开/弹窗显示详细统计图表
- 配额设置编辑表单

**状态**：已确认

### 24. 恢复手动测活按钮（禁用自动测活）

**目标**：在主页和分组卡片中恢复手动测活按钮，但不进行自动定时测活
**实现**：
- 分组卡片中增加手动测活触发按钮（点击即对该分组执行一次探测）
- 主页健康总览中保留手动触发入口
- 关闭自动定时测活任务（或将默认设为关闭）
- 保留 GroupHealthEnabled 设置开关作为全局开关（关闭时手动按钮也隐藏）

**状态**：已确认

### 25. 自定义测活 Prompt

**问题**：硬编码的 probe prompt（"Hi", "hello" 等）可能被站点识别为异常行为并封禁
**目标**：允许用户自定义测活对话内容
**实现**：
- 设置页 Reliability 或 Routing 卡片中增加 "自定义测活 Prompt" 文本输入
- 后端 Setting 新增 key: `group_health_probe_prompt`
- `internal/grouphealth/probe.go` 中优先使用自定义 prompt，未设置时 fallback 到随机池
- 支持多条（换行分隔），随机选取

**状态**：已确认
