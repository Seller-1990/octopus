# Octopus 全面审查报告（多顾问对抗审查）

**日期：** 2026-09-12 ｜ **基线：** dev @ 补签对账修复后（651fcf7 + 15 commits）
**模式：** 首席审查官编排，10 位顾问并行真实派发（正面线 4 + 对抗线 6），全部返回
**定位：** 继 2026-09-08 全面审计（Codex 单线）之后的第二轮独立审查——重点攻击 09-08 之后的修复本身与历史未审面

---

## 最终判定

**有条件通过** —— 架构分层、认证边界与数据链路骨架经 6 位对抗者联合攻击后幸存；但 **09-12 新引入的「同步后补签对账」存在被 4 位顾问独立击穿的缺陷簇（无节流循环 + 陈旧快照覆写当日成功）**，且备份体系在凭据保护与恢复语义上有两个重伤级缺口。条件 = 完成 P1 四项修复；条件全部映射到文末行动清单。

---

## 审查构成

| 线 | 顾问 | 选型理由 |
|---|---|---|
| 正面线 | 反驳者、本质追问者、外行人、无情行者 | 全栈/完整产品评审分诊表标准构成 |
| 对抗线 | 前端对抗者、后端对抗者、数据对抗者、性能对抗者、安全对抗者、逻辑对抗者 | 分诊表标准构成；逻辑对抗者专攻「修复决策的推理链」 |
| 未派发 | 商业对抗者 | 内网自用、无商业模型，议题不适用 |
| 未派发 | 产品对抗者 | 需求真实性由外行人覆盖；若需深挖留存/需求漂移可补派 |

派发过程：首轮 10 并发中 2 个成功；因平台限流/验证码失败 8 个，分批重试后全部真实返回，无代拟。

## 双重复核通过的高风险项（对抗组与反驳者/逻辑对抗者独立命中）

1. **补签对账缺陷簇** —— 逻辑对抗者、后端对抗者、数据对抗者、性能对抗者、本质追问者、反驳者 6 位独立命中（本轮最高置信发现）
2. **F03 statsSaveMu × 停机/热路径边界** —— 后端、数据、性能、本质追问者、反驳者、逻辑对抗者 6 位独立命中
3. **F08 停机路径具体坏法已构造** —— 后端、反驳者、逻辑对抗者 3 位独立命中
4. **F01 残余缺口（token 数上限不对称 / 截断 zip 静默下载）** —— 后端、反驳者、安全对抗者 3 位独立命中
5. **备份恢复语义（合并≠回滚）** —— 反驳者、数据对抗者 2 位独立命中

---

## 风险清单（统一分级 P0-P3，已合并去重）

### P1 重伤（4 项）

| # | 风险 | 发现者 | 证据 | 缓解措施 |
|---|---|---|---|---|
| R1 | **补签对账缺陷簇**：①`shouldReconcileCheckinAfterSync` 不查 `NextAutoCheckinAt` 退避也不限频 → 签到永久失败的账号按同步频率无限重签（上游风控风险）；②补签用同步开始时的陈旧快照判断，失败写回可**覆盖并发的手动签到成功**（updateAccountCheckinState 仅 credential_revision CAS）；③串行补签（60s 预算）使 SyncAll 批量时长最坏 10 账号 20+ 分钟；④非随机账号失败分支无 `checkin_fail_streak++`（storage.go:781-790 既有问题被补签放大） | 逻辑/后端/数据/性能/本质/反驳 | internal/sitesync/core.go:87-93,397-403；storage.go:763-790（else-if 无 streak++）；对照 core.go:314-320 防重签护栏 | 补签前查 DB 当前状态（当日已成功则跳过）；尊重退避（NextAutoCheckinAt 未到期不触发）；写回失败前比对同日 success；给非随机账号失败分支补 streak++；数据对抗者已给出验证 SQL |
| R2 | **备份 zip 全量明文凭据**：api_keys/channel_keys/site_tokens/账号密码明文导出（sanitize 只剔 3 个设置键）；WebDAV 自动备份允许 `http://` 明文外传且自动触发无审计日志 | 安全 | backup_extended_export.go:93-104；backup.go:1483-1518；model/site.go:223-227；webdav/backup.go:54；setting.go:220-234 | 敏感列 KEK 加密或「脱敏导出」选项；拒绝/警告 http scheme；自动备份补审计日志 |
| R3 | **F01 残余：token 数上限不对称** —— 导出 writeRecord 只查 4MiB 字节，导入还有 100k JSON token 上限；token 密集的 LLM 日志（~300KB 紧凑 JSON）可导出成功、导入被拒，违反 F01 自身「导出必可导入」不变式 | 后端 | backup_zip_export.go:89-111 vs backup_zip_stream_import.go:546-552（maxBackupZipRecordTokens=100_000） | 导出端 writeRecord 增加同款 token 数校验 |
| R4 | **F03 锁 × 停机/热路径组合**：statsSaveMu 是普通 Mutex 不感知 ctx——停机 SaveCache（10s）可能等锁耗尽预算，三路 flush 全败且阻塞后续 relay-log flush hook（shutdown hooks 无总超时）；日切首请求在热路径同步等锁（最坏分钟级尾延迟）；hourly 按 persist 时刻 todayDate 过滤 → 日切尾窗增量永久丢失 | 后端/性能/数据/反驳/逻辑 | stats.go:45,104-107,165-171,220-222,276-292；cache.go:49-59；shutdown.go:65-75；relay/metrics.go:401 | 锁等待改可取消（TryLock 轮询 + ctx）或停机专用预算；hourly 的 todayDate 改为快照时刻取；Close→Shutdown(8s) 见 R8 |

### P2 可修复（12 项）

| # | 风险 | 发现者 | 证据 | 缓解 |
|---|---|---|---|---|
| R5 | F08 具体坏法：`httpSrv.Close()` 硬断在途 LLM 流/SSE（费用记 indeterminate）；SaveCache 与锁竞争时 docker stop 10s 宽限被吃满后 SIGKILL | 后端/反驳/逻辑 | server.go:97-99；cache.go:50；cmd/start.go:57-71 | Close 改 `Shutdown(ctx 10s)` 超时再 Close；F08 正式立项 |
| R6 | 截断 ZIP 静默下载成功：导出中途中止时 200 头已发，前端 downloadApiFile 不校验完整性直接落盘（DR 假象） | 后端/反驳 | setting.go:140-156；client.ts:169-188 | 前端下载后 `unzip` 校验或尾记录标记；或导出改为先落临时文件 |
| R7 | 恢复语义：导入是 upsert 合并（备份后新增行存活、settings 却被回滚成半合并态），UI 文案称「覆盖」；WebDAV 自动备份不含日志（恢复非完整时点） | 反驳/数据 | backup.go:1339-1372；log.go:1282-1287 注释自相矛盾；webdav/backup.go:54 | UI 文案改「合并导入」并明示行为；评估「覆盖式恢复」选项；自动备份补日志开关 |
| R8 | SSRF 防护不一致：site detect / channel fetch-model 抓任意 URL 无内网校验、跟随重定向（visionbridge 有 isForbiddenHost 对照） | 安全 | sitesync/detect.go:70-137；helper/fetch.go:57-86；对照 relay/visionbridge/safety.go:93-103 | 复用 isForbiddenHost + CheckRedirect 逐跳校验 |
| R9 | F07 客户端 500 截断可丢 running 条目（服务端 running 无上限、finished 200；>300 running 时低 id running 被裁，StopAttempt 不可达）+ 重连重置整块闪烁 | 逻辑/前端 | live-logs-merge.ts:11-21；livelog.go:74-78,368-394；log.ts:569-577 | 截断时 running 豁免；重连保留旧列表至快照首条到达 |
| R10 | checkAuth 把一切失败当会话失效（断网/502 强制登出）；logout 不清 React Query 缓存（换账号闪现上一会话数据）；401 非 JSON 体绕过 handleError | 前端/逻辑 | user.ts:114-130；query.tsx:12；client.ts:53-92 | 仅 401/403 登出；logout 时 queryClient.clear()；401 登出条件加 error_code 校验 |
| R11 | 任务启动同相齐跑：runOnStart 任务在 phase 等待之前同步触发，启动 burst 打在 SQLite 最脆弱窗口 | 后端 | task.go:124-136；init.go:99-109 | runOnStart 也纳入相位调度 |
| R12 | gemini thought signature 缓存无界增长（cache.New(64) 是分片数不是容量） | 后端 | transformer/compat/gemini_signature_cache.go:10-34 | 加 TTL 扫描或容量上限 |
| R13 | 验证桥 7 端点无认证无速率限制（identify 每次触发 DB 写）；配对令牌 TTL 最长一年 | 安全 | site_recovery.go:59-67；verification_bridge.go:26-27 | IP 级限流；TTL 收敛 ≤7 天 |
| R14 | 三套日切口径并存：usage_aggregates 用 UTC、stats_daily/hourly 用本地、site_model_hourly 混用 | 数据 | usage_aggregate.go:364-370；stats.go:277,322；stats_site_model.go:55-58 | 统一口径（建议全部 UTC 存储、展示层换算） |
| R15 | 价格数据质量（价格对比视图的地基）：stale 报价无限期参与路由打分；0 价被吞（真免费模型无报价行）；消失模型报价永存；估算价无 estimated 标记 | 数据 | site_pricing.go:518-553；catalog.go:1539-1552；pricing.go:195-197,14 | 路由打分排除超龄 stale；refresh 补 absent-sweep；估算价打标 |
| R16 | EnsureRotated TOCTOU（先查后建无事务无唯一索引，并发双击可堆积重复配对——F04 的复发路径） | 后端 | verification_browser.go:162-183；(site_account_id,name) 无唯一索引 | 补唯一索引 + 事务内 upsert |
| R17 | CI 质量门禁缺口：-race 只跑 internal/server（F03/F06 改动的 op/relay 并发代码不在内）；release.yaml 无测试前置；本地产物缺 -tags=jsoniter 与发布产物不一致 | 无情行者 | ci.yml:36-37；release.yaml:3-6；Makefile:34-37 vs build.sh:346 | race 扩到 op/relay；release 加 needs:ci；Makefile 补 tags |
| R18 | **价格对比计划需修订**（击穿昨日方案）：ModelName 列未归一化大小写，`WHERE model_name=?` 查空/漏行，`LOWER()` 包列会废掉拟新增索引 | 性能 | site_pricing.go:44,79-89 vs PLAN_PRICE_COMPARE:19,37-44 | 落地前先归一化存量列（迁移写回小写）再建索引 |

### P3 观察（择要）

- **UX 人话 10 条**（外行人）：首启无密码零引导（桌面版 crash 循环风险）；skipped 在总览被算成 failed；手动签到入口藏在三点菜单；base URL 接入说明难找；「结果不确定」无解释；导入按钮红色 destructive 像删数据；验证桥/视觉桥命名混淆。做得好的：一键导出到客户端、CF 报错三出路、缺 Key 提示给动作。
- **性能**：relay log 队列满载时同步 flush 把写延迟传导进请求 goroutine（log.go:135-165）；大 body per-attempt 全量 marshal + base64 计 token（计费口径失真）；WAL checkpoint 无运维干预点。
- **安全**：CORS 允许 "*"+credentials 组合；WS Origin 全放行（被 APIKeyAuth 恰好挡住）；升级链仅 SHA-256 无签名；jwt_secret 双重角色（文档化权衡）。
- **一致性细节**：relay_logs 无 fact 的行永不过期；liveAttempts 句柄泄漏边角；熔断 LastFailureTime 被慢失败顺延；多 tab 无 storage 事件同步；i18n 硬编码中文残留。
- **交付卫生**（无情行者 8 条阻断项）：新人 Docker 首启 crash 循环（compose 注释掉 bootstrap 密码）；CI 主门禁不含生产构建；F08/F09 无 owner 无时限；.planning/.zcode 等目录未进 .gitignore；产物命名两套体系。

## 分歧与裁决

| 分歧点 | 冲突双方 | 裁决 |
|---|---|---|
| F01 预检 NULL 行为 | 数据对抗者：NULL 行使 MAX 跳过该行，预检漏报；后端对抗者：COALESCE 已收口 | **数据对抗者正确**：`COALESCE(MAX(expr),0)` 只兜底空集，行内 NULL 使该行表达式为 NULL 被 MAX 忽略。P3 修复=每列包 COALESCE |
| F01 记录计数 | 逻辑对抗者：导出比导入严 1 条（manifest 计入），偏保守幸存；后端对抗者：token 数不对称是真缺口 | 两者不冲突且都成立：字节/条数边界一致（计数差偏保守），**token 数是真实缺口**（R3） |
| F03 核心论证 | 多位击穿边界 vs 逻辑对抗者判定「不回退」核心幸存 | 幸存判定成立（API 只读内存，内存单调）；被击穿的是边界外推（R4 的停机/日切尾窗）——修复保持，边界另修 |
| F05 会话守卫 | 逻辑对抗者构造长期 key 反例 vs 本质追问者确认「登录必换 token」 | 当前实现下守卫成立（JWT iat 必不同）；补一条属性测试锁定「登录必轮换 token」不变量 |
| 补签「不污染统计口径」 | 数据对抗者：source="sync" 口径幸存 vs 多位：状态覆写/风控击穿 | 口径本身幸存（无聚合消费者）；**状态正确性被击穿**（R1）——两者是不同维度，R1 成立 |

## 幸存清单（攻击后防线成立，择要）

- 认证边界（安全对抗者逐组核对 17 个 handler）：管理面全覆盖、stream-token 一次性原子吊销、登录限流 fail-closed、无默认凭据
- 注入面：全部 gorm Raw 参数化 + LIKE 转义；zip 导入多重上限防 bomb
- F01 核心（导出=导入字节契约）、F02（同步 bind）、F03 核心（不回退）、F04 错误分类、F05 当前形态、F07 原子快照订阅
- 加权采样、iterator copy、CAS 凭据链、livelog 多订阅者关闭、验证 cookie 白名单
- 数据骨架：facts 自包含、scope 隔离、Convertible 门槛（汇率缺失不会按 0 成本胜出）

## 下一步行动

| # | 动作 | 责任人 | 时限 | 验收标准 |
|---|---|---|---|---|
| 1 | **修复补签对账（R1）**：当日已成功护栏（写回前查 DB）+ 尊重 NextAutoCheckinAt 退避 + 非随机账号失败分支补 streak++；补时序反例回归测试（并发手动签到 vs 补签） | 主席建议→用户确认 | 24h | 数据对抗者的验证 SQL 零命中；新回归测试 -race 通过 |
| 2 | **备份加固（R2/R3/R6/R7）**：writeRecord 补 token 数校验；UI 导入文案改「合并导入」并明示；评估凭据脱敏导出选项 | 同上 | 本周 | 构造 300KB 紧凑 JSON 日志导出→导入成功；文档明示备份含明文凭据 |
| 3 | **停机协议最小修复（R4/R5，F08 立项）**：Close→Shutdown(ctx 10s)+超时 Close；statsSaveMu 等待可取消或停机路径专用预算；hourly todayDate 快照时刻取 | 同上 | 本周 | SIGTERM 注入测试：在途请求 drain、SaveCache 三路成功、无 hook 超时被 SIGKILL |

## 纪律自查

- 对抗性审查：已完成——6 位专业对抗者全部输出非空击穿清单（前端 12/后端 14/数据 12/性能 10/安全 10/逻辑 9+幸存清单）
- 顾问真实性：**全部 10 位真实派发**（首轮 8 个因平台限流失败，分批重试后全部返回，无代拟）
- 分歧裁决：5 项分歧全部裁决并记录依据
- 闭环：评审模式完整（判定/取舍/风险/行动/自查）；无诊断模式议题

---

## 修复执行与对抗复审闭环（2026-09-12 晚）

行动 1-3 已实施并分四个 commit 落地：`0265791`（补签对账加固）、`5517a64`（备份加固）、`b789d71`（停机协议）、`71f457f`（复审闭环）。

复审派发：后端对抗者 + 逻辑对抗者，精确限定审查三个修复 commit 的 diff。命中 9 个有效问题（P1×1、P2×2、P3×6），全部裁决如下：

| # | 复审发现 | 严重度 | 处置 |
|---|---|---|---|
| 1 | 当日成功保护 SELECT-then-UPDATE 在 MySQL/PG 下 TOCTOU（SQLite 靠 WAL 意外成立） | P1 | **已修复**：守卫并入 UPDATE 谓词（`last_checkin_status<>success OR last_checkin_at<今日零点`），RowsAffected=0 时二次读区分「保护命中」与「revision 冲突」 |
| 2 | 停机预算自相矛盾：Shutdown(10s) drain 吃掉 docker 默认 10s 宽限，SaveCache 未跑即被 SIGKILL | P2 | **已缓解**：compose 显式 `stop_grace_period: 40s`（drain 10 + SaveCache 10 + relay flush 10 < 40）；全局停机 deadline 留作后续改造 |
| 3 | 日切翻转后 enqueue 是纯内存，崩溃窗口丢昨日尾窗 | P2 | **已修复**：翻转缓存**之前**单行幂等落库 prevDaily（PK=Date），失败才入重试队列；重量级 statsSaveDBWithDailyOverride 删除，热路径不再等锁 |
| 4 | token 计数 200KB 阈值算术错误（1 字节 token 存在） | P3 | **已修复**：阈值降为 100_000 字节（=token 上限×1 字节） |
| 5 | 导出响应被 gzip 包裹，Content-Length 被中间件重写，截断不可检测 + 双重压缩 | P3 | **已修复**：`/api/v1/setting/export` 加入 gzip 排除 |
| 6 | 导出 500 回传原始 err.Error()（内部信息泄露） | P3 | **已修复**：固定文案 + 详情入日志 |
| 7 | skipped（平台不支持）也 streak++ 并推进退避 | P3 | **已修复**：skipped 中性化——不累计失败、不推进退避 |
| 8 | WebDAV URL 解析失败时静默跳过告警 | P3 | **已修复**：解析失败/空 scheme 也留痕 |
| 9 | 补签双读窗口（门禁重读与 checkinAccount 内部再读之间仍可插入成功）、保护路径 CAS 失败吞检查日志、guard 跳过路径汇总口径分裂、测试断言力缺口 | P3 | **接受为残留**（影响=偶发重复签到请求被站点「已签到」幂等吸收/日志口径细节/测试增强），已在代码注释与本表登记 |

复审确认有效的部分：F03 串行化核心、F01 字节契约、Shutdown/Join 语义、channel 信号量无饥饿无双持、snapshotDate 跨日序列走查无累积错误、preflight COALESCE 修正生效。
