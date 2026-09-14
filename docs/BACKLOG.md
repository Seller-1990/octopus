# Octopus 缺陷台账（唯一事实源）

> 规则：ID 全局唯一、永不复用，前缀带来源日期（A250913=09-13 早轮报告，B250913=09-13 晚间报告，C250913=09-13/14 第三轮十顾问审查，LEGACY=历史遗留）。每轮审查当天由产出会话追加，报告本身只做 append-only 归档，不再承载活状态。
> 状态：`fixed@<hash>` 已修复并提交 / `open` 待修 / `skipped` 有意跳过（内网自用定调）/ `refuted` 经复核不成立（含否决依据，防止后续轮次重报）/ `obsolete` 已被其他修复覆盖。

## 一、open（按建议优先级）

| ID | 来源 | 位置 | 等级 | 问题与下一步 |
|---|---|---|---|---|
| C250913-01 | 第三轮·性能 | `internal/utils/tokenizer` + `relay.go:1445` + `inbound/anthropic/messages.go` | P1 | 同一文本每请求过两遍 BPE 计数（结果仅在上游不回 usage 时被消费）。改为惰性：仅 usage 缺失时计算 + 按 body 哈希缓存 |
| C250913-02 | 第三轮·反驳 | `catalog_provision_tx.go:114`、`client/http.go` | P1 | 流式请求对上游静默挂死无超时兜底（自动建组 FirstTokenTimeOut 默认 0、无读超时）。给自动建组非零默认 + StreamProcessor 加流内不活跃上限 |
| C250913-03 | 第三轮·反驳 | `op/apikey.go:238`、`op/log.go:159` | P1 | 成功路径同步配额写 + 队列满同步回压在 SQLite 单写者下放大写拥塞。配额改内存脏缓存+周期落库（对齐 ChannelKeyRecordUse 模式）；回压刷库改有界重试 |
| B250913-07/08 | 晚间 | `db/migrate/003/004/006/007.go` | P2 | MySQL TEXT DEFAULT 与 PG DATETIME（004.go:45 在 PG 必炸、启动失败）。**仅多数据库部署相关；未来迁 PG 前必须先修** |
| C250913-04 | 第三轮·数据 | `op/usage_aggregate.go:194-211` | P3 | 聚合 identity 行名称固化：实体改名后聚合快照永久挂旧名。identity 行 OnConflict 时追加 DoUpdates 更新名称列 |
| C250913-05 | 第三轮·逻辑 | `op/site_pricing.go:325` | P2 | F01 修复只堵新增：存量污染行（manual_override=true 且 known=true 且 multiplier=0）持续生效并可经备份传播。一次性迁移或管理面待复核清单 |
| C250913-06 | 第三轮·数据 | `sitesync/pricing.go:56-63` | P3 | 错误信封（200+success:false）会误清报价 last_error（观测性）。行 56 前校验 success 信封（复用 balance.go 的 isValidUserSelfPayload 模式） |
| B250913-13 | 晚间 | `sitesync/project.go:796-814` | P3 | rewriteManagedGroupItemsForAccount 逐条 Count+Update，大账号数百次单条 SQL。批量查重 + IN 更新 |
| C250913-07 | 第三轮·前端 | `site-channel/index.tsx`（3191 行）、`site/index.tsx`（2683 行） | P2 | 巨型组件拆分（前端对抗者已给出具体边界：site-channel 拆 UnifiedCompletionDialog/HistorySummary/TableView/4 个对话框 + useModelEditing hook；site 拆 Import/Archived/Delete 对话框 + useSiteActions/useSiteInventory；jump 编排与 ref 缓存不要拆） |
| C250913-08 | 第三轮·外行 | 多处 | P3 | 重复实现收敛：模型名拆分三套、TLS 指纹校验两份、Retry-After 解析两份、crud_errors 三胞胎、渠道后处理 goroutine 双份、27 处手写 parseIDParam；`model`/`dbmodel` 包别名同包分裂（relay/op）一次性统一 |
| C250913-09 | 第三轮·外行 | `handlers/channel.go:101-120` vs `op/channel.go:75-91` | P2 | 渠道代理模式校验 handler/op 各抄一遍（改一处不生效的静默 bug 源）。删 handler 侧，靠 op 层 apperror 带状态码 |
| C250913-10 | 第三轮·本质 | `op/` 27.9k 行 | P3 | op 为自救型上帝包。只拆交互面最窄的两个子域：backup（~5.2k）与 verification（~2.3k），其余不动 |
| C250913-11 | 第三轮·前端 | `site-channel/index.tsx:1150-1252` | P3 | `site-channel-model` 跳转全套死代码（无 requestJump 调用者）或接上或删除；内含「弹窗打开期间分组筛选被锁死」隐患 |
| C250913-12 | 第三轮·性能 | `site/index.tsx:2229`、`site.ts:564-575` | P3 | 站点页无虚拟化 + 每展开账号一个 30s checkin-logs 轮询。改为进入视口才 enabled 或降频 |
| B250913-15 | 晚间 | `update/update.go:131-148` | P3 | zip 自更新失败可致进程半挂且更新通道永久闭锁（仅用自更新功能时相关） |
| C250913-13 | 第三轮·性能 | `site_pricing.go:478`、`catalog.go:1122` | P3 | `LOWER(model_name)` 包列索引失效。归一化小写生成列或 COLLATE NOCASE |
| C250913-14 | 第三轮·性能 | `processor.go:274-276` | P3 | transform 模式每 chunk 重跑 SSE 解析做终态检测（inbound 适配器本知情）。廉价子串预筛或适配器打终态标记 |
| LEGACY-250905 | 09-05 报告 | `docs/reviews/audit-report-octopus-2026-09-05.md` | - | 该报告 23 条「未修」项自 09-06 后零更新、被后续三轮遗忘。**待办：合并去重入本台账** |

## 二、skipped（内网自用，有意跳过——勿在后续轮次重复登记）

| ID | 来源 | 位置 | 问题 | 跳过理由 |
|---|---|---|---|---|
| C250913-S1 | 第三轮·安全 | `op/backup.go:104-1516`、`apikey.go:70` | 备份明文导出全部 sk-octopus key 与站点凭据 | 用户定调：内网自用不过度防御。若未来公网部署需重开 |
| C250913-S2 | 第三轮·前端 | `site.ts:87-133` | 站点账号明文凭据随 30s 轮询全量下发前端 | 同上 |
| B250913-14 | 晚间 | `handlers/channel.go:195-207`、`sitesync/detect.go` | fetchModel/detect SSRF（管理员自控面） | 触发者=管理员，内网自用无实质攻击者。visionbridge 的 isForbiddenHost 可供未来复用 |
| A250913-F12 | 早轮 | `op/verification_session.go:35` | verificationSessionEnsureLocks sync.Map 只增不删 | 按 accountID 数封顶，内网规模量级可忽略 |
| A250913-F17/F18auth | 早轮 | `validate.go:21`、`auth.go:96` | RequireJSON 子串匹配 / 配额错误回传 err.Error() | 无攻击路径的卫生项 |

## 三、refuted（复核不成立——勿再修）

| ID | 原登记 | 否决依据 |
|---|---|---|
| C250913-R1 | A250913-F10「错误信封清空分组倍率」 | `site_pricing.go:27-29`/`:84-86` 空参 no-op 守卫，构造不出清空路径；残留仅 last_error 观测性（见 C250913-06） |
| C250913-R2 | B250913-「RelayLogAdd 每请求三表 JOIN」 | `enrichUsageDimensions` 仅在 flush 批次调用（usage_facts.go:41），非每请求 |
| C250913-R3 | A250913-F13「WS 续接自旋不回退」 | GetPreferred 有回退（ws_pool.go:132-160）；残余仅池满全忙时等归还，P3 |
| C250913-R4 | A250913-F14「熔断热路径读设置」 | SettingGetInt 读 16 分片内存缓存非 DB，可忽略 |
| C250913-R5 | A250913-F18(site_channel)「1.0 误判未知」 | 注释自述的设计取舍（非 1 视为真值），1.0=默认值无资金影响 |

## 四、fixed（本日全部闭环项）

| ID | 描述 | 提交 |
|---|---|---|
| A250913-F01..F05 | 早轮五修复（倍率零值/thinking 累加/流式终结兜底/WS 亲和节流/死代码清理） | c934946 |
| B250913-fixed×16 | 晚间 16 修复（Gemini max_completion_tokens P0、max_tokens=1 截断、nil 防护、SIGHUP、分页死循环、数据竞争原子化、错误体限读、WS 适配器重建、volcengine thinking、passthrough 零拷贝等） | 43c9000 |
| A250913-F06 | 熔断 Open 态在途失败不顺延冷却起点 | 63042ff |
| C250913-P0-1 | 选路表现聚合 30s TTL 快照（热路径脱离 24h 全表聚合） | 73eb2db |
| B250913-04 | 选路价格评分批量预取（消除每候选每请求 2 次查询） | 274bb52 |
| B250913-01/02/03 + 同类孪生 | WS 四修：defer 顺序/字段 atomic+在途读弃用/FirstTokenTimeout break/会话 store 节流 | 83a2dd1 |
| C250913-D1 | 备份导入前 flush 内存缓存（key 计费账本不再蒸发） | 9e494d7 |
| A250913-F16 | runOnStart 移到相位等待之后 | bfd8373 |
| A250913-F15 | 静态中间件跳过 /v1 | ef9aa4b |
| B250913-09 | SQLite _txlock=immediate | 9b3e6b3 |
| B250913-12/C250913-N1 | 代理 client 按 URL 缓存 + SOCKS 拨号超时 | ef8fdb2 |
| C250913-P1-5 | 429/503 软失败计数并熔断 | cce89b3 |
| C250913-P1-8 | ResponseContent 256KB 上限 + base64 消隐 | 6551816 |
| A250913-F09 | 余额观测位（失败保留上一份） | d84c60e |
| C250913-P1-6 | 流式聚合 strings.Builder（O(N²)→O(N)） | f16339b |
| A250913-F11 | 路由探测三连发短路 | 337519c |
| B250913-11 | 批量同步 CatalogSync 收敛为末尾一次 | 3ab1882 |
| C250913-P1-9/前端 | request 超时+网络错误归一、三处静默 mutate 补 toast、幻影 setQueryData 清理 | 46dc946 |
