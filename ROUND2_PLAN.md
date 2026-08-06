# 第二轮实施方案

> 日期：2026-08-06
> 基于代码研究编写，实施前需审查确认

---

## 修改 16：Releases 格式根治

### 现状
`scripts/build.sh` 的 `create_archives()` 函数（line 486）会为 `build/bin/` 中**所有二进制**生成 zip。

### 方案
在 `create_archives()` 的循环中增加过滤，跳过已有专用安装包的平台：

```bash
# Skip platforms that have dedicated package formats
case "${basename_file}" in
    *-linux-*|*-windows-*|*-desktop-*) continue ;;
esac
```

保留 zip 的只有 macOS（darwin）——macOS 无签名的 .dmg 体验不比 zip 好。

### 影响
- release.yaml 的 upload pattern 需更新：移除 `build/archives/*` 中的 linux/windows zip
- 自更新客户端 `internal/update/core.go` 的 `getDownloadFilename()` 需要适配新格式：
  - Linux: 下载 .deb 或直接下载裸二进制（自更新场景不需要包管理器格式）
  - 方案：保留裸二进制在 release assets 中（不 zip），自更新客户端直接下载裸二进制

### 最终 release assets 清单
| 平台 | 格式 | 用途 |
|------|------|------|
| Linux x86_64/arm64/armv7/x86 | `.deb` + `.rpm` | 包管理器安装 |
| Linux x86_64/arm64/armv7/x86 | 裸二进制（无 zip） | 自更新 + 便携用户 |
| Windows x86_64 | `.exe` NSIS 安装器 | 安装 |
| Windows x86_64 | `octopus-desktop-x86_64.exe` | 便携桌面版 |
| macOS arm64/amd64 | `.zip` | 下载解压使用 |
| 全平台 | `sha256sums.txt` + `md5.txt` | 校验 |

### 风险评估
- 自更新客户端依赖 zip 格式下载——需同时修改 `getDownloadFilename()` 和 `UpdateCore()` 的解压逻辑
- **决策**：Linux/Windows 裸二进制不打包直接上传，自更新时直接下载裸二进制（无需解压）；macOS 保持 zip

---

## 修改 17：站点页账号区域布局重组

### 现状
`web/src/components/modules/site/index.tsx` lines 1764-1842
- 左侧：账号名 + 凭据类型 badge + 启用状态 badge
- 右侧：分组/模型/余额指标 + 同步/签到/代理模式文字 + 开关按钮
- 问题：右侧信息分多行显示，占用大量纵向空间

### 方案
将同步/签到/代理模式信息以 Badge 形式移入左侧名称行，与其他 badge 平级：

改后布局：
```
[账号名] [凭据类型] [启用] [自动同步] [随机签到] [代理: inherit]
                    [分组:3 | 模型:15 | 余额:$1.2k | 今日:$0.1] [开关] [按钮组]
```

### 改动范围
- 仅 `web/src/components/modules/site/index.tsx`
- 将 lines 1822-1841 的状态文字改为 Badge，移入 line 1778 的 `flex-wrap items-center gap-2` 容器中
- 删除右侧原有的第二行

---

## 修改 18：设置页补充排序策略+倍率上限

### 现状
`web/src/components/modules/setting/Routing.tsx` 只有一个「默认负载均衡策略」下拉

### 方案
在 Routing.tsx 中增加两个 SettingRow：
1. **默认排序策略** — Select 下拉（4 种预设同 member-sort.ts）
2. **默认倍率上限** — 数字输入框（0 = 不限制）

### 改动范围
- `web/src/api/endpoints/setting.ts` — 新增 2 个 SettingKey
- `web/src/components/modules/setting/Routing.tsx` — 增加 2 个 SettingRow
- locale 文件 — 增加对应翻译

---

## 修改 19：模型目录改为价格总览

### 现状
Catalog.tsx 已在第一轮重写为卡片网格，显示模型名+厂商+路由数+开关

### 方案
在卡片中增加价格信息展示：
- 每张卡片增加一行：显示该模型的最低价格来源站点 + 价格（如 "SiteA: $2.5/M"）
- CatalogModelDialog 中增加「价格对比」section：列出该模型在各站点的价格

### 数据来源
- 已有 `EffectivePriceForCandidate` 逻辑和 `SiteModelPriceQuote` 表
- 前端需要新增一个 API hook 查询 per-model 的价格列表
- 或者复用 catalog 数据中已有的 route_candidates 信息（每个 candidate 可能关联价格）

### 改动范围
- `web/src/components/modules/model/Catalog.tsx` — 卡片中增加价格行
- `web/src/components/modules/model/CatalogModelDialog.tsx` — 增加价格对比 section
- 可能需要后端增加一个 per-model 价格聚合 API

---

## 修改 21：站点统计面板 UX 改进

### 现状
`CheckinPanel.tsx` lines 232-246 标题只写 "overview"，filter tabs 中 "all" 计数的是签到账号数

### 方案
1. 标题区域增加总站点数标注：`{sites.length} 站点 / {summary.total} 签到账号`
2. 翻译 key `overview` 改为更明确的 "签到总览"
3. Reserve/中转 filter 从状态分类分离，改为独立 toggle 筛选

### 改动范围
- `web/src/components/modules/site/CheckinPanel.tsx` — 标题区+filter 逻辑
- locale 文件 — 更新翻译

---

## 修改 22 + 23：API 密钥独立页面 + 统计 + 配额

### 现状
- API Key UI 在 `setting/APIKey.tsx` 中，已有完整 CRUD + 简单统计
- 后端 model: id, name, api_key, enabled, expire_at, max_cost, max_rpm, supported_models
- 已有 stats 接口返回 token/cost/request 统计

### 方案

#### 前端
1. 新建 `web/src/components/modules/apikey/index.tsx` — 独立页面
2. `web/src/route/config.tsx` — 在 `log` 和 `setting` 之间新增 `apikey` 路由
3. 页面内容：
   - 顶部：创建按钮 + 搜索
   - 主体：表格/列表视图，每行显示：名称、key（脱敏）、状态、已用额度/上限、今日用量、最后使用时间
   - 点击行展开或弹窗：详细统计（token 分布、成本趋势、请求成功率）
   - 编辑/创建表单：沿用现有 APIKeyForm 逻辑，增加配额字段
4. 从 `setting/index.tsx` 移除 SettingAPIKey 卡片

#### 后端
1. `internal/model/apikey.go` — 增加配额字段：
   ```go
   QuotaLimit    float64 `json:"quota_limit,omitempty"`    // 0 = unlimited
   QuotaPeriod   string  `json:"quota_period,omitempty"`   // daily/weekly/monthly
   QuotaUsed     float64 `json:"quota_used"`
   QuotaResetAt  int64   `json:"quota_reset_at,omitempty"`
   ```
2. `internal/relay/` — 请求前检查配额：
   - 在 relay 入口处（认证通过后），检查 `quota_limit > 0 && quota_used >= quota_limit`
   - 超限返回 HTTP 429 + 明确错误信息
3. `internal/task/` — 增加配额重置定时任务（按周期重置 quota_used）
4. 增加 per-key 统计聚合 API（基于已有 UsageAggregate 数据）

### 风险评估
- 配额检查在热路径上（每个请求），需要高效实现（内存缓存 quota 状态）
- 配额重置需要原子性（避免并发问题）

---

## 实施顺序

| 序号 | 修改 | 依赖 | 复杂度 |
|------|------|------|--------|
| 1 | #21 站点统计面板 UX | 无 | 低 |
| 2 | #17 站点页账号布局 | 无 | 低 |
| 3 | #18 设置页排序+倍率上限 | 无 | 低 |
| 4 | #16 Releases 格式 | 无 | 中（需改自更新客户端） |
| 5 | #19 模型价格总览 | 可能需后端 API | 中 |
| 6 | #22+23 API 密钥页面 | 需后端改动 | 高 |

---

## 审查要点

1. **#16 自更新兼容性**：裸二进制直接下载 vs zip 解压——需确保 `UpdateCore()` 能处理两种情况（新版直接下载、旧版 zip）
2. **#22+23 配额热路径性能**：quota 检查不能每次查 DB，需内存缓存 + 事件驱动更新
3. **#19 价格数据来源**：确认前端能否从现有 catalog API 获取价格信息，还是需要新接口
