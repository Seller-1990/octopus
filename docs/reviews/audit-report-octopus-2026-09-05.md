# Fuck My Shit Mountain Audit Report

**Project:** Octopus
**Audit mode:** full，第一性原则与对抗性审查
**Date:** 2026-09-05
**Reviewer:** Codex 主审 + 6 个并行只读子审查员
**Baseline:** `e8c788977c93ef886dbf37c6af544025825fde1b`，分支 `dev`
**Delivery:** 简体中文；全面风险审查 + F30 局部修复 + 用户批准的 F01–F04 安全修复 + 2026-09-05 复审收口（8 位顾问对抗复审、合入门禁实测、路由注册错误传播与代理信任启动告警补丁、CI 接入扩展/Python/race 测试），不等于全部风险已修复

## 1. Executive Summary

原始审计发现 **31 项风险：High 13、Medium 17、Low 1**。其中 **30 项已确认**（包含源码闭合调用链与动态反例两种证据），**1 项需要进一步核验运行条件**。首次审计修复 F30；用户随后明确批准并完成 **F01–F04**，当前累计修复 **5 项**，仍有 **26 项未修复（High 9、Medium 16、Low 1）**，其中包含 F27 的待核验风险。未扩展实施 F05+；F30 的价格脚本修复原样保留。

**安全修复结论：** 首次扩展信任改为 popup 手动建立，撤销同时使在途自动配对失效；TLS 指纹验证证书及主机名；反代来源信任默认关闭、显式配置并校验；登录预算在凭据校验前原子占用，状态有界且可全局过期回收。全套 Go 及相关包 race 回归通过；扩展 Node VM 不是浏览器端到端证明，未部署或修改真实配置、凭据、数据。

最重要的问题不是代码格式或文件长度，而是边界契约不一致：浏览器验证桥首次信任可被网页建立；TLS 指纹路径关闭证书验证；缓存配置刷新能覆盖已累计费用；配额重置不是幂等操作；异常模型目录被当成权威空列表；协议转换的两条实现保真程度不同；前端认证切换没有隔离查询与异步响应。

现有 Go 全套测试、TypeScript 检查通过，前端 Lint 只有一条 warning；但本轮新增的隔离反例直接复现了多个错误与登录限流 data race。**基线绿灯不能作为这些边界已正确的证明，也不建议仅凭现有 CI 判断可以安全发布。**本轮未运行生产压测，没有测得或宣称 P95、吞吐率、内存占用的优化百分比。

### Score Dashboard

以下保留原始审计判断分，未重算 F01–F04 修复后的评分；不是缺陷率、测试覆盖率或全仓安全认证。10 分最好，总体分为七维均值。

| Dimension | Score | Grade | 最强证据与判断依据 |
|---|---:|---|---|
| Security | 4.5 | C | F01/F02/F03 分别涉及首次信任、证书验证、来源地址信任，已有隔离反例。 |
| Stability | 5.0 | B | F06/F07 已复现费用归零；流终态、恢复失败还有独立契约缺口。 |
| Performance | 5.5 | B | F22 的集合增长与 F28 的重复扫描已确认；没有生产负载基准。 |
| Testing | 6.0 | B | 后端有大量有效测试，但新增组合/交错反例失败；前端缺认证与组件生命周期覆盖。 |
| Maintainability | 5.5 | B | 同一缓存状态、转换语义、异步生命周期存在多个不一致写入/处理入口。 |
| Design | 6.0 | B | 领域划分与 ADR 合理，但实现偏离终态、价格来源、严格转换等约定。 |
| Release | 5.0 | B | F30 已局部修复；F31 的构建输入遗漏及 CI 关键门禁缺失仍在。 |
| **Overall** | **5.4** | **B** | **需要定向修复，不支持全面重写，也不能据此认定未深读区域无风险。** |

### Finding Statistics

| Severity | Count | Confirmed | Suspected |
|---|---:|---:|---:|
| Critical | 0 | 0 | 0 |
| High | 13 | 12 | 1 |
| Medium | 17 | 17 | 0 |
| Low | 1 | 1 | 0 |
| Info | 0 | 0 | 0 |
| **Total** | **31** | **30** | **1** |

证据约定：
- **动态**：主审执行实际函数、Go overlay 测试、已安装依赖实验或隔离 SQLite 查询；不是生产环境利用。
- **源码**：主审抽查子审查员给出的关键出处，并核对调用链；未专门执行该项反例。
- **Suspected**：代码机制有风险，但真实运行触发条件或端到端影响尚待验证。
- 高风险项的等级考虑影响与条件；未把管理员主动配置上游的功能一律认定为 SSRF，也未把本地身份缓存串用称作服务端鉴权绕过。

## 2. Project Map

### 结构与不变量

| 领域 | 当前职责与所有者 | 本轮检查的不变量 |
|---|---|---|
| 启动 | `cmd` → 配置 → DB → op cache → HTTP → task | 返回启动成功应意味着配置有效且监听已建立。 |
| 管理/API 接口 | Gin + JWT/API Key + handlers → op | 认证前资源有界；来源地址不可由不可信请求决定。 |
| 转发 | relay/balancer → outbound/inbound transformer | choice/tool 索引不混用；不丢指令/媒体；业务终态不等于连接关闭。 |
| 费用与持久化 | DB 配置 + 内存运行态 + 周期 flush | 配置编辑不能回滚费用；重置幂等；失败批次不能无声丢弃。 |
| 同步与价格 | sitesync/task/helper/client | 上游异常不是权威空状态；取消/证书校验在所有传输路径一致。 |
| 验证桥 | 浏览器扩展 + 配对服务器 + 短期 broker | 浏览器高权限能力必须来自明确用户信任，不能来自任意网页。 |
| 前端 | Zustand 身份/本地状态 + React Query 服务端缓存 | 身份切换隔离查询和迟到响应；每次操作都有独立终止与清理。 |
| 发布 | pnpm/Go/scripts/CI/static embed | 产物与输入快照一致；生成失败不能包装成成功。 |

读取并用于判断的基础材料：`CLAUDE.md:1`、`CONTEXT.md:1`、`docs/adr/0001-separate-request-outcome-from-transport-termination.md:1`、`docs/adr/0008-run-cloudflare-verification-in-the-admin-browser.md:1`、`docs/adr/0009-prefer-passthrough-with-overridable-auto-transform.md:1`、`docs/adr/0011-resolve-prices-per-route-candidate.md:1`、`docs/adr/0012-default-to-strict-protocol-compatibility.md:1`。实现与文档冲突时，以源码和复现为事实，文档作为预期契约。

### 覆盖边界

仓库有 823 个跟踪文件；Go/TS/TSX/JS/Shell 跟踪文件合计 714 个、约 181,382 行，含测试和生成内容。六路审查均完成各自生产文件索引与风险模式扫描，重点深读高风险路径，**不是 18 万行逐行精读**。

| 审查分工 | 范围 | 深入重点 | 明确盲区 |
|---|---|---|---|
| 子审查 A | relay、transformer | SSE、重试/终态、事件 IR、Gemini、balancer | WS pool/session/replay、完整大适配器部分分支仅定向扫描。 |
| 子审查 B | op | 渠道/API Key、统计、日志/用量持久化 | catalog/group 全策略、全量 analytics 查询计划未逐一验证。 |
| 子审查 C | sitesync/task/client/helper 等 | 模型抓取、同步删除、调度、指纹传输、价格链 | 各平台恢复/投影全状态机、账户级并发、全部健康算法未深验。 |
| 子审查 D | server/update/webdav/extensions | 认证、限流、输入、恢复、启动、首次配对 | 浏览器端到端、二进制自更新/回滚、完整 broker 授权状态机未运行。 |
| 子审查 E | db/model/conf/utils | 迁移、缓存、ID、配置落盘 | MySQL/PostgreSQL 实例、完整历史升级与并发迁移未运行。 |
| 子审查 F | web | 身份/查询、日志 SSE、行级 mutation | 真实组件挂载、Profiler、bundle 分析、大型表单全分支未验证。 |
| 主审 | cmd/scripts/CI/static、跨层抽查及反例 | 构建价格、根因复现、风险校准、局部修复 | 跨平台安装包、发布流水线、生产配置没有实际执行。 |

未读取真实 `data/`、凭据、生产服务；没有访问真实上游、安装依赖、修改全局配置、提交或回滚用户改动。已有未跟踪报告/计划原样保留。暴露工具中没有 code-review-graph/Playwright 专用调用，本轮使用本地源码、测试及 Node 隔离实验，不宣称完成浏览器 E2E。未检索外部 CVE，因此没有“依赖不存在漏洞”的结论。

## 3. Top Risks

| 优先级 | ID | 风险 | 核心问题 | 状态 |
|---|---|---|---|---|
| 已修复 | F01 | High | 任意网页可在首次状态建立扩展配对信任 | 手动首次信任及在途撤销回归通过 |
| 已修复 | F02 | High | TLS 指纹请求接受不可信证书 | 不可信证书/错主机名拒绝，可信证书通过 |
| 已修复 | F03/F04 | High/Medium | 登录限流可伪造 IP 绕过，且存在实际 data race | 代理配置、并发预算、容量和 race 回归通过 |
| 数据正确性 | F06/F07 | High | 渠道编辑与重复配额重置都能令已计费用归零 | 动态确认，未修 |
| 数据正确性 | F09 | High | 畸形成功响应被当空模型目录并触发路由删除 | 解码动态确认、删除链源码确认，未修 |
| 协议正确性 | F13/F14/F15 | High | Gemini 丢系统指令、拆错工具 choice、丢流式图片 | 动态确认，未修 |
| 身份隔离 | F19 | High | 退出/切换身份后复用旧查询结果 | 依赖实验+源码确认，未修 |
| 密钥处理 | F26 | High | bootstrap 密码先落盘，再尽力删除 | 源码确认，未修 |
| 条件性可靠性 | F27 | High | ID 生成缺少重启/跨实例唯一性保证 | 待核验触发条件 |
| 构建正确性 | F30 | High | 获取失败后覆盖空价格表并返回成功 | **本轮已修复** |

## 4. Detailed Findings

以下 Evidence/Problem 保留原始快照的缺陷事实与行号。F01–F04 的当前落点、已执行验证及部署边界见每项 Remediation 和第 8 节；未将历史反例误标为当前仍存在的问题。

### F01 — 网页可建立验证桥首次配对信任
- **Severity / Status / Confidence / Category:** High / Confirmed / 高 / Security；**已修复**。
- **Evidence:** `extensions/verification-bridge/background.js:51` 从标签页 fragment 读 token/origin；`:106` 的空配对分支允许任意 HTTP(S) origin；`:307` 配对后启动自动化。`extensions/verification-bridge/manifest.json:13` 授权所有 HTTP(S) 主机。相同源码被镜像到 `static/extensions/verification-bridge/background.js`，本轮比较两目录一致。
- **Problem / Scenario:** 新装或清空配对后，无用户手势的网页 URL 即可触发 `addPairing`。主审运行原监听器与检查函数片段，空状态下记录到恶意模拟 origin 的一次配对调用。后续 MAIN-world 携 cookie 请求及响应回传链见 `extensions/verification-bridge/background.js:717`、`extensions/verification-bridge/background.js:680`。
- **Impact:** 攻击者控制配对端后可能借管理员浏览器访问已登录站点。首次自动配对已动态确认；完整浏览器数据外传链未端到端执行。已有可信配对会拒绝新 origin。
- **Minimal fix / Long term:** fragment 仅产生待确认信息；首次信任须在 popup 明示确认 origin/范围，再启动自动化。扩展源与嵌入副本统一生成，不能只修一份。
- **Regression:** 空配对+任意 fragment 不产生配对/任务；用户确认后的合法流程成功；伪服务端不能操作未授权站点。
- **Effort:** 4–8 小时。
- **Remediation:** `extensions/verification-bridge/background.js:246` 复用手动配对入口建立首次信任；自动入口仅接受已经保存的同 origin。网络返回后还要求原授权记录仍存在，阻止撤销后恢复配对或覆盖新手动令牌。没有增加待确认队列或重复 origin helper。源和 static 镜像同步；`extensions/verification-bridge/background.test.js:105` 起的 12 项生产脚本 VM 回归通过（两份脚本各 6 项）。首次安装/升级须核对地址、重新加载扩展并清理不明旧配对；未验证真实 popup 点击或完整外传链。

### F02 — TLS 指纹无条件关闭证书验证
- **Severity / Status / Confidence / Category:** High / Confirmed / 高 / Security；**已修复**。
- **Evidence:** `internal/client/tls_fingerprint.go:45` 的 `NewFingerprintedClient` 在 `internal/client/tls_fingerprint.go:54` 添加 `WithInsecureSkipVerify()`；站点调用入口 `internal/sitesync/http.go:148`。
- **Problem / Scenario:** 主审用 `httptest.NewTLSServer` 的不受信任证书调用真实指纹客户端，收到 HTTP 200；预期的证书拒绝没有发生。
- **Impact:** 开启浏览器指纹也降低了服务端身份验证，链路/代理攻击者可冒充上游读取认证信息或篡改响应；并非所有普通 HTTP client 都受此影响。
- **Minimal fix / Long term:** 删除无条件 insecure 选项；自签名部署单独支持可信 CA，不能把指纹和跳过验证绑定。
- **Regression:** 不可信证书失败、可信证书成功、Chrome/Firefox 指纹仍生效；验证代理路径。
- **Effort:** 1–2 小时；自签名兼容范围需确认。
- **Remediation:** `internal/client/tls_fingerprint.go:50` 删除唯一无条件 insecure 选项，raw/adapter 共享工厂。`internal/client/tls_fingerprint_test.go:21` 证实 Chrome/Firefox 及 adapter 拒绝不可信证书，服务端收到的 HTTP 请求为零；`:66` 用隔离子进程和临时 CA 验证可信证书成功及错主机名失败，断言具体 x509 错误。未添加不安全开关或修改系统证书库；真实反代/私有 CA 部署组合仍需部署方验收。

### F03 — 可伪造来源 IP 绕过登录锁定
- **Severity / Status / Confidence / Category:** High / Confirmed / 高 / Security；**已修复**。
- **Evidence:** `internal/server/server.go:37` 使用 `gin.New()`，仓库未配置 `SetTrustedProxies`；`internal/server/handlers/user.go:48` 用 `ClientIP()` 作为限流键。本地 Gin v1.12.0 默认信任全部代理地址。
- **Problem / Scenario:** 主审固定 `RemoteAddr`、连续六次轮换 XFF，经过实际限流函数仍返回 401 而不是第六次 429。
- **Impact:** 直连或代理不清洗来源头时锁定失效，安全日志也可污染。条目仅按同 IP 复访/成功清理，轮换 IP 还可令表增长。正确清洗头的可信反代环境不能据此断言已被绕过。
- **Minimal fix / Long term:** 直连禁用代理头信任；部署显式配置可信代理 CIDR；限流表独立过期清理与容量界限。
- **Regression:** 固定 peer 轮换 XFF 仍锁定；可信代理解析正确；大量过期 IP 被清理。
- **Effort:** 2–4 小时。
- **Remediation:** `internal/conf/config.go:17` 增加默认空 `trusted_proxies`；`internal/server/server.go:55` 必须成功配置代理名单，否则在清理临时文件和监听前返回错误。真实应用 engine 验证默认拒绝 XFF/X-Real-IP、IPv4/IPv6、可信多跳链与非法 IP/CIDR。Viper 的生产 Load 路径验证文件数组及无空格逗号环境变量，空环境回退文件。限流表最多 10,000 IP，全局扫描最多每分钟一次；容量满时未知来源 fail closed，保留活跃锁及未过期预算，返回 429/Retry-After。合法新来源也可能暂时被拒绝；多实例限流仍需部署侧处理。

### F04 — 登录限流共享条目存在 data race
- **Severity / Status / Confidence / Category:** Medium / Confirmed / 高 / Security、Stability；**已修复**。
- **Evidence:** `internal/server/handlers/login_throttle.go:25` 的 `sync.Map` 存指针；`internal/server/handlers/login_throttle.go:36` 读 `lockedUntil`，`:51`、`:56`、`:58` 并发改字段，没有条目锁。
- **Problem / Scenario:** 主审 8 个 goroutine 并发 check/failure，`go test -race` 实际报告上述读写竞争；现有顺序测试捕获不到。
- **Impact:** 丢失败计数、锁定时间不稳定。`sync.Map` 只保护映射操作，不保护指针指向的状态。
- **Minimal fix / Long term:** 在同一锁内完成窗口判断、计数、锁定/清除；若还限制并发验证，检查与预留尝试须成为一个原子操作。
- **Regression:** 并发 check/failure/success 通过 race，计数与时间窗口正确；保持正常解锁语义。
- **Effort:** 2–4 小时。
- **Remediation:** `internal/server/handlers/login_throttle.go:35` 用单 mutex + map 值保护窗口、清扫、计数和容量占用；删除 split check/failure 两阶段及可变指针。尝试在凭据校验前记账，包含在途请求，第五次准入锁定十五分钟；已准入的成功登录清空该 IP。避免晚到失败重新建项、丢计数或续锁，也不用后台 goroutine/in-flight registry。十分钟窗口、十五分钟锁定的精确边界、容量竞争和真实 handler 429 均有回归；`-race` 通过。

### F05 — 公开入口在认证前无界解码正文
- **Severity / Status / Confidence / Category:** Medium / Confirmed / 高 / Security、Performance；未修复，源码证据。
- **Evidence:** `internal/server/handlers/user.go:43` 先绑定正文再限流；`internal/server/handlers/site_recovery.go:381`、`internal/server/handlers/site_recovery.go:397`、`internal/server/handlers/site_recovery.go:419` 先解码 pairing token/cookies 后认证。对照 `internal/server/handlers/site_recovery.go:502` 仅 browser-result 入口设置 5 MiB 上限。
- **Problem / Scenario:** 无凭据请求发送超大字符串或 cookie 数组，拒绝身份前已经进行分配/解析。服务器也未配置读取头超时。
- **Impact:** 未认证的 CPU/内存消耗入口；实际影响受反代正文上限和部署资源约束，未执行 OOM 实验。
- **Minimal fix / Long term:** 公开小请求在解码前限制大小，登录先限流；设置读头超时，勿给长流接口机械套用短总超时。
- **Regression:** 超阈值返回 413、业务 op 未执行；正常输入及慢请求边界正确。
- **Effort:** 2–4 小时。

### F06 — 渠道配置刷新抹掉尚未落库的使用成本
- **Severity / Status / Confidence / Category:** High / Confirmed / 高 / Stability、Data；未修复。
- **Evidence:** `internal/op/channel.go:136` 的 `ChannelKeyRecordUse` 累加缓存并标脏；`internal/op/channel.go:425` 更新配置后刷新；`internal/op/channel.go:879` 删除 key cache 并从 DB 发布旧状态；`internal/op/channel.go:202` flush 读取当时缓存值。
- **Problem / Scenario:** 主审真实临时 SQLite：记成本 5 → 只修改渠道名 → flush，数据库 `TotalCost` 变成 0。没有并发也会发生。`ChannelGetByName` 的 DB 快照发布也需同类核查。
- **Impact:** 已完成调用的 key 成本、状态码、最后使用时间被回滚；刷新查询失败前先删缓存还会扩大丢数风险。
- **Minimal fix / Long term:** 完整读取成功后再发布；同一渠道锁保护刷新/运行态更新，保留脏运行字段。不能只在刷新前 flush，因为两步间仍会有新增量。明确配置字段与运行字段所有者。
- **Regression:** 重命名保留 5；SELECT 失败保留旧缓存；并发增量与刷新交错不丢数。
- **Effort:** 4–8 小时。

### F07 — 到期配额重置不是幂等操作
- **Severity / Status / Confidence / Category:** High / Confirmed / 高 / Stability、Data；未修复。
- **Evidence:** `internal/op/apikey.go:197` 的 `APIKeyResetQuota` 仅按 ID 无条件置零；`internal/server/middleware/auth.go:91` 根据锁外旧快照决定是否调用；`internal/op/apikey.go:220` 增量与重置虽共用锁，但没有重检周期。
- **Problem / Scenario:** 两请求都读到到期状态；A 重置，新周期计费 5，B 再重置。主审按该交错顺序调用真实 API，DB 用量从 5 变为 0。
- **Impact:** 周期边界漏计金额配额；旧参数还可能覆盖管理员新周期设置。
- **Minimal fix / Long term:** 锁内读取当前状态或按预期 reset-at 做条件更新，返回实际配额快照；middleware 不得在未重置时仍假设 used=0。
- **Regression:** 同一周期 reset→+5→重复 reset 仍为 5；双请求 barrier 与期间改周期案例。
- **Effort:** 2–4 小时。

### F08 — 统计 getter 写零值可覆盖并发累计
- **Severity / Status / Confidence / Category:** Medium / Confirmed / 高 / Stability、Maintainability；未修复，源码证据。
- **Evidence:** `internal/op/stats.go:290`、`internal/op/stats.go:324` updater 用实体锁；`internal/op/stats.go:379`、`internal/op/stats.go:394` getter cache miss 后不取该锁，直接 Set 零值并标脏。
- **Problem / Scenario:** getter 读 miss 后暂停，updater 写入成本，getter 恢复覆盖零值。分片 cache 的单次操作锁不能消除这个逻辑交错。
- **Impact:** 新实体/冷缓存窗口丢统计，可能低估 MaxCost。常规 `-race` 也不一定报告。
- **Minimal fix / Long term:** getter miss 只返回零值不写；或同实体锁下二次检查再初始化，避免查询隐含破坏性写入。
- **Regression:** 固定 miss→update→init 交错，两个实体类型的费用及持久化都不倒退。
- **Effort:** 1–3 小时。

### F09 — 畸形成功响应被当作权威撤模
- **Severity / Status / Confidence / Category:** High / Confirmed / 高 / Stability、Data；未修复。
- **Evidence:** `internal/helper/fetch.go:208` 的解码器接受空 body，JSON 缺模型列表不报错；`internal/task/sync.go:57` 计算全量删除，`:60` 写空渠道模型，`:75` 删除 GroupItem。
- **Problem / Scenario:** 主审验证空字符串、`null`、`{}`、200 错误对象均被解码为零模型且 nil error；同步到删除路由的影响通过当前调用链核对，未实际运行删除任务。
- **Impact:** 普通 AutoSync 渠道遇上游维护/异常代理响应后可能失去全部已知模型与路由。
- **Minimal fix / Long term:** 在协议边界要求合法列表结构，区分缺字段与明确空数组；空目录业务策略单独决定，不能一律拒绝真正撤模。
- **Regression:** 四类畸形响应保留历史；明确空数组按确认策略处理；有效列表正常增删。
- **Effort:** 2–4 小时。

### F10 — 回溯正则没有实际执行预算
- **Severity / Status / Confidence / Category:** Medium / Confirmed / 高 / Performance、Stability；未修复，源码证据。
- **Evidence:** `internal/helper/fetch.go:37` 编译 regexp2，`:42` 匹配前未设置 `MatchTimeout`；本地 regexp2 v1.11.5 默认接近无限超时。`internal/task/sync.go:25` 的 context 不会自动中止 CPU 匹配。
- **Problem / Scenario:** 管理员配置 `^(a+)+$`，上游给长串 `a` 加非匹配尾字符，产生灾难性回溯，调度 running 标记长期占用。
- **Impact:** 阻塞模型同步并消耗 CPU；需要不利正则与模型名组合，未当作任意匿名请求可触发的全站 DoS。
- **Minimal fix / Long term:** 有限匹配超时、循环间检查取消；是否改用标准 regexp 须先确认 lookaround 等兼容需求。
- **Regression:** 不利输入有界失败、取消生效、现有合法模式不回归；测试外层硬超时。
- **Effort:** 1–2 小时。

### F11 — 模型分页没有进度与总量边界
- **Severity / Status / Confidence / Category:** Medium / Confirmed / 高 / Performance、Stability；未修复，源码证据。
- **Evidence:** `internal/helper/fetch.go:92` 的 Gemini 循环仅等空 token；`internal/helper/fetch.go:142` 的 Anthropic 循环仅等 has_more=false，复用 last_id，没有重复游标检测。
- **Problem / Scenario:** 服务端重复非空 token，或 has_more=true 搭配空/重复 last_id，持续抓同一页；重复模型还累积内存。
- **Impact:** 任务可能消耗完整 30 分钟窗口，手动入口只有请求 context，没有独立操作 deadline；不是所有调用都无限运行。
- **Minimal fix / Long term:** 检测无进展/循环游标，限制页数、总模型数及操作时间；集中到实际分页边界，不增加通用框架。
- **Regression:** 重复、循环、空页、正常多页，均有确定结果和边界。
- **Effort:** 1–2 小时。

### F12 — 站点指纹路径丢弃请求取消
- **Severity / Status / Confidence / Category:** Medium / Confirmed / 高 / Stability、Performance；未修复，源码证据。
- **Evidence:** `internal/sitesync/http.go:168` 调用无 context 包装；`internal/client/tls_fingerprint.go:73` 改用 Background，旁边 `internal/client/tls_fingerprint.go:78` 已有带 context 的实现。
- **Problem / Scenario:** 调用方短 deadline 到期，指纹网络请求仍等待甚至继续执行上游操作。
- **Impact:** 取消不及时、占用任务资源；客户端已有 30 秒 timeout，不应描述为无限挂起。
- **Minimal fix / Long term:** 直接复用已有 `DoFingerprintedRequestContext`；统一普通/指纹/浏览器传输的取消契约，避免再包一层同功能抽象。
- **Regression:** 阻塞本地上游配短 deadline、主动 cancel、成功请求，验证结束时间与副作用边界。
- **Effort:** 0.5–1 小时。

### F13 — Gemini 丢弃数组形式 system/developer 指令
- **Severity / Status / Confidence / Category:** High / Confirmed / 高 / Stability、Protocol；未修复。
- **Evidence:** `internal/transformer/outbound/gemini/messages.go:631` 的系统分支只读 `Content.Content`；`internal/transformer/model/model.go:987` 将数组写入 `MultipleContent`。
- **Problem / Scenario:** 主审执行 ChatInbound→Gemini.TransformRequest，system/developer 的 text 数组输入均变成 `systemInstruction.parts=[]`，用户消息保留。
- **Impact:** 同义字符串/数组输入产生不同语义，系统约束被静默丢弃，违背严格转换 ADR。
- **Minimal fix / Long term:** 转换支持的文本块，明确拒绝或报告不能表示的块；将等价输入测试放在组合边界。
- **Regression:** 字符串、单块、多块两种角色均保留内容和顺序。
- **Effort:** 1–2 小时。

### F14 — Gemini 并行工具被拆成多个 Chat choices
- **Severity / Status / Confidence / Category:** High / Confirmed / 高 / Stability、Protocol；未修复。
- **Evidence:** `internal/transformer/outbound/gemini/messages.go:193`、`:196`、`:197` 把 tool index 放入事件 Index；`internal/transformer/model/stream_event.go:180` 用事件 Index 分 choice。
- **Problem / Scenario:** 主审走 Gemini.TransformStreamEvent→ChatInbound.TransformStreamEvents，一个 candidate 的两个工具被输出到 choices[0] 和 choices[1]；第二个 choice 没有对应终态。
- **Impact:** 只处理首 choice 的客户端丢第二个调用，工具关联与结束语义错误。
- **Minimal fix / Long term:** 事件 Index 保持 candidate/choice 语义，工具索引保留 ToolCall.Index；先核验全部消费者契约，不能跨协议机械替换索引。
- **Regression:** 一 candidate 两工具、跨 chunk、非零 candidate、多个 candidate，验证完整下游 wire。
- **Effort:** 2–4 小时。

### F15 — Gemini 流式事件转换丢图片
- **Severity / Status / Confidence / Category:** High / Confirmed / 高 / Stability、Protocol；未修复。
- **Evidence:** `internal/transformer/outbound/gemini/messages.go:158` 事件循环只处理 thought/text/functionCall；非流分支 `internal/transformer/outbound/gemini/messages.go:1418` 支持 inlineData。relay 优先事件链见 `internal/relay/relay.go:1866`。
- **Problem / Scenario:** 同一合成 PNG payload 经 TransformResponse 保留 data URL，经事件链只留下 role/stop。主审已动态对照。请求转换也有 responseModalities 映射，不能认为流式媒体永远不可能到达。
- **Impact:** 客户端获得成功终态却没有生成图片；两套适配实现不等价。
- **Minimal fix / Long term:** 补事件 IR/编码的媒体表示与顺序；补齐前显式拒绝不支持组合，不静默成功。逐步统一规范化转换源，而非同时维护两份语义。
- **Regression:** 图片、文本+图片、多个媒体块，比较非流与真实流式组合链。
- **Effort:** 4–8 小时。

### F16 — 上游心跳被当作首 token
- **Severity / Status / Confidence / Category:** Medium / Confirmed / 高 / Stability、Observability；未修复，源码证据。
- **Evidence:** `internal/relay/relay.go:1716` 使用 RawSource；`internal/relay/stream/processor.go:288` 任意写出置 payloadWritten；`:232` 因而触发首 token 回调并停表。
- **Problem / Scenario:** 上游先发 SSE 注释心跳，随后只心跳无业务事件，首 token budget 已关闭，TTFT 记录为心跳时间。
- **Impact:** 无有效输出的上游占用连接，首 token 指标失真；Written 又影响切换策略。
- **Minimal fix / Long term:** 分离“HTTP 已提交”和“业务首 token”；首业务前可缓冲注释，按完整业务帧停计时。**不能在实际写出后仅把 Written 改回 false 来重新重试。**
- **Regression:** 心跳→阻塞、半帧→阻塞、跨读取边界；超时正确，已提交响应不发生不安全重试。
- **Effort:** 3–5 小时。

### F17 — passthrough 读失败丢已收到的 usage
- **Severity / Status / Confidence / Category:** Medium / Confirmed / 高 / Stability、Accounting；未修复，源码证据。
- **Evidence:** `internal/relay/stream/processor.go:213` 非 EOF 错误直接返回；`internal/relay/relay.go:1729` 在 OnFinish 才解析缓冲；`:1772` 补偿依赖该回调填入的副本且只处理 canceled。
- **Problem / Scenario:** Anthropic message_start 已带 input_tokens，随后连接重置/UnexpectedEOF；已观察到的 usage 没进入聚合器。
- **Impact:** 失败尝试的实际费用与内容观测缺失；不能把已收费的部分输出当作零使用。
- **Minimal fix / Long term:** 观测采集与成功 finalize 分离，每种退出都至多一次采集完整已读事件，同时保留失败 outcome。
- **Regression:** usage 帧后读错误、无终态写错误、取消，usage 保留且不重复计费。
- **Effort:** 2–4 小时。

### F18 — 无语义终态的 EOF 被记为成功
- **Severity / Status / Confidence / Category:** Medium / Confirmed / 高 / Stability、Observability；未修复，源码证据。
- **Evidence:** `internal/relay/stream/processor.go:205` EOF 进入 finalize；`:342` 仅要求写过 payload；`internal/relay/protocol_helpers.go:35` 空终态默认 success；`internal/relay/relay.go:698` 随后恢复熔断。
- **Problem / Scenario:** 上游只有内容 delta，无 finish/stop/completed 等语义终态便正常关闭 body，仍记成功并可能建立 sticky。
- **Impact:** 截断响应被隐藏，健康判断错误；与 ADR 的 outcome/transport 分离相悖。
- **Minimal fix / Long term:** 对有明确终态的协议区分完整与 indeterminate；保留已读 usage，不对已写出结果重试，也不把无证据 EOF 当恢复熔断依据。历史兼容策略须显式约定。
- **Regression:** Chat/Anthropic/Responses 的 delta+EOF 与有终态对照，验证 outcome、健康及 sticky。
- **Effort:** 2–4 小时。

### F19 — 前端身份切换复用旧私有缓存
- **Severity / Status / Confidence / Category:** High / Confirmed / 高 / Security、Frontend state；未修复。
- **Evidence:** `web/src/api/endpoints/user.ts:116` logout 仅改 store；`web/src/provider/query.tsx:7` QueryClient 长期保留且 fresh 60 秒；`web/src/api/endpoints/apikey.ts:68` 使用无身份维度的固定 stats key。
- **Problem / Scenario:** 有效 Key A 退出后立即用有效 Key B 登录。主审用本地 QueryClient 及相同 key 实验，结果 owner=A、B 查询执行次数=0；组件/认证生命周期源码已核对，未做浏览器挂载。
- **Impact:** 同浏览器身份之间串统计/账户信息；不等同服务端越权，网络失败时旧数据还可能继续展示。
- **Minimal fix / Long term:** 认证切换取消并移除私有查询，配合非凭据 session epoch；不要把完整密钥塞进 query key。
- **Regression:** A→退出→B、A 迟到成功、B 查询失败，均不可显示 A 的数据。
- **Effort:** 4–8 小时。

### F20 — 旧请求迟到 401 会登出新会话
- **Severity / Status / Confidence / Category:** Medium / Confirmed / 高 / Stability、Frontend state；未修复。
- **Evidence:** `web/src/api/client.ts:111` 发送时读 token；`web/src/api/client.ts:19` 错误处理重新读当前 store 后 logout，没有比较请求身份。子审查员对真实 client.ts 转译代码用 deferred fetch 复现，主审核对调用链。
- **Problem / Scenario:** A 请求等待→登录 B→A 返回401，B 被 logout。
- **Impact:** 弱网、多请求及切换身份时出现无关登出；取消旧请求与错误归属均未闭合。
- **Minimal fix / Long term:** 捕获认证 epoch，401 只改变同 epoch 状态；与 F19 合并设计身份边界，但分开回归。
- **Regression:** A 的迟到401不影响B，B自身401仍正确登出。
- **Effort:** 2–4 小时。

### F21 — 并发 mutation 遗留行级 pending
- **Severity / Status / Confidence / Category:** Medium / Confirmed / 高 / Stability、Frontend state；未修复。
- **Evidence:** `web/src/components/modules/site-channel/index.tsx:1314`、`web/src/components/modules/site-channel/index.tsx:1353` 在单次 mutate 回调清 pending/override；另一行不在 pending 集合可继续提交。本地 MutationObserver 每次 mutate 替换 observer/options。
- **Problem / Scenario:** 主审用已安装依赖和两个 deferred promise：A、B 并发，仅 B 回调执行，pending 剩 A。
- **Impact:** A 行永久转圈/禁用直到卸载；A 失败时回滚和错误提示也遗漏。
- **Minimal fix / Long term:** 每次 `mutateAsync` 在独立 try/catch/finally 清理该行，或每个 mutation 都执行的 hook 回调；不用最后一次 observer 回调承载全部生命周期。
- **Regression:** 双成功、A失败B成功、逆序完成，所有 pending清空，失败行反馈正确。
- **Effort:** 3–5 小时。

### F22 — trackedIds 随日志页会话无界增长
- **Severity / Status / Confidence / Category:** Medium / Confirmed / 高 / Performance、Frontend state；未修复。
- **Evidence:** `web/src/components/modules/log/Running.tsx:292` 每次复制 Set，仅 add、不回收；`web/src/api/endpoints/log.ts:534` 的 500 上限只限制日志数组。
- **Problem / Scenario:** 子审查员执行当前 effect 原文，滚动 1,001 个 ID 后 logs=500、trackedIds=1001；主审抽查实际 effect。最近修复的收敛条件存在，**不是重复报告无限渲染循环**。
- **Impact:** 已逐出日志的 ID 留存，每次更新仍复制全部历史 Set；只确认规模增长，没有浏览器内存/耗时实测。
- **Minimal fix / Long term:** 根据仍可能参与展示的日志集合回收 ID，保留“确曾看到 running”语义，避免简单改为全部完成态快照。
- **Regression:** 超500请求仍有界；初次完成快照不混入；running→finished→DB接管正确。
- **Effort:** 2–4 小时。

### F23 — 尝试详情 SSE 断线后静默停止
- **Severity / Status / Confidence / Category:** Medium / Confirmed / 高 / Stability、Observability；未修复，源码证据。
- **Evidence:** `web/src/api/endpoints/log.ts:741` onerror 关闭连接；`:746` token/连接失败直接返回；`:757` effect 只依赖 enabled/id/state，没有恢复驱动或错误返回。
- **Problem / Scenario:** 展开的 running 请求首次取token失败，或连接一次断开；网络恢复但依赖不变，不会重新连接。
- **Impact:** attempt信息和可中止目标陈旧，界面无断线提示，需手动折叠重开。
- **Minimal fix / Long term:** 有界退避、每次重连新token、显式连接状态；终态/卸载停止。复用现有流生命周期经验，不加万能SSE框架。
- **Regression:** 首次失败、连接后断线、重试中卸载、进入终态，均无遗留连接。
- **Effort:** 3–5 小时。

### F24 — 中止尝试失败没有用户反馈
- **Severity / Status / Confidence / Category:** Low / Confirmed / 高 / Observability；未修复，源码证据。
- **Evidence:** `web/src/api/endpoints/log.ts:763` mutation只有请求；`web/src/components/modules/log/Running.tsx:195` 直接 mutate，无 onError/行内错误展示。
- **Problem / Scenario:** stop 返回409/500或网络失败，转圈结束但无失败原因，公共处理只输出console（401除外）。
- **Impact:** 用户无法判断停止请求是否被接受，不应把操作失败当无动作。
- **Minimal fix / Long term:** 失败toast或行内状态；成功仅表示请求被接受，不伪称上游已经停止。
- **Regression:** 409/500/网络错误均有反馈，最终停止仍靠流状态确认。
- **Effort:** 0.5–1 小时。

### F25 — 损坏 ZIP 在验证前丢弃 pending 日志/用量
- **Severity / Status / Confidence / Category:** Medium / Confirmed / 高 / Stability、Data；未修复，源码证据。
- **Evidence:** `internal/server/handlers/setting.go:233`、`internal/server/handlers/setting.go:267` 先 discard 后 DBImportZip；`internal/webdav/backup.go:178` 同形；`internal/op/backup_zip_import.go:48` 才验证 ZIP；`internal/op/log.go:1285` 清空日志与usage pending。
- **Problem / Scenario:** 管理员上传 not-a-zip，或有效名称的备份已损坏，导入失败但此前 pending 已空；不是未授权攻击。JSON语法错误路径已改过，不重复报旧问题。
- **Impact:** 一次失败恢复也会丢未持久化事实，不能称原子失败。
- **Minimal fix / Long term:** 恢复协调顺序应是预校验→停写/栅栏→事务→按结果丢弃或恢复旧pending。不能只移动一行而忽略事务失败/在途flush。
- **Regression:** 坏ZIP、缺manifest、校验失败、事务失败保留pending；成功恢复防止旧状态回灌。
- **Effort:** 4–8 小时。

### F26 — bootstrap 密码先落盘再清理
- **Severity / Status / Confidence / Category:** High / Confirmed / 高 / Security；未修复，源码证据。
- **Evidence:** `internal/conf/config.go:148` 先 SafeWriteConfigAs(AllSettings)，`:155` 才 scrub；`internal/conf/config.go:122` 忽略清理写错误。源码自身注释确认 env密码会被首次序列化。
- **Problem / Scenario:** 首次落盘和清理间崩溃/终止，或文件备份/监控捕获中间状态，明文密码留存；清理失败也不阻止启动。
- **Impact:** 违反“密码不落普通配置”的承诺。权限受umask约束；未读取或发现真实泄露的密码。
- **Minimal fix / Long term:** 写入前从配置快照剔除秘密，不先泄露后尽力擦除；必要写入错误显式传播。
- **Regression:** 临时目录+虚构密码，截获首次写入，整个过程均无秘密字节；覆盖失败路径。
- **Effort:** 2–4 小时。

### F27 — ID 生成的重启/跨实例唯一性未保证
- **Severity / Status / Confidence / Category:** High / Suspected / 中 / Stability、Data；未修复。
- **Evidence:** `internal/utils/snowflake/snowflake.go:15` 只以进程内毫秒水位递增；无节点位/持久化水位。`internal/model/log.go:142` ID为非自增主键；`internal/op/log.go:258` 重复主键使整个批次失败，成功后才出队。
- **Problem / Scenario:** 多实例同毫秒，或高频调用令水位超前后快速重启/时钟回拨，可能撞历史ID。唯一性不足可从代码确认，但真实部署是否满足条件、是否实际阻塞flush尚未执行验证，所以不列为已发生数据事故。
- **Impact:** 条件满足时整批日志/用量持续失败，不仅单条显示重复。
- **Minimal fix / Long term:** 先确认单/多实例契约，再选跨进程唯一ID；仅单实例也需恢复最大水位及排他写库约束，不能随意换ID格式破坏现有接口。
- **Regression:** 可注入时钟、重启水位、两生成器共享库、重复ID后的批次恢复。
- **Effort:** 4–8 小时。

### F28 — SQLite 回填分批但反复扫描已完成前缀
- **Severity / Status / Confidence / Category:** Medium / Confirmed / 高 / Performance、Release；未修复。
- **Evidence:** `internal/db/migrate/013.go:95`、`internal/db/migrate/018.go:33` 每批无主键游标地选择500条未处理记录。索引异步创建在迁移完成之后。
- **Problem / Scenario:** 主审内存SQLite、5000条合成记录、执行013同形SQL；计划包含 SCAN relay_logs，第1/5/10批 VM步骤为 **12527 / 18523 / 26023**，越往后前缀扫描越长。
- **Impact:** 无适用索引的大表升级最坏近似二次扫描；小事务限制WAL峰值不等于总工作量有界。以上是SQLite VM工作量，不是Go驱动或生产延时基准。
- **Minimal fix / Long term:** 主键递增游标分页，保留业务谓词和小事务；不建议为一次回填盲目加长期冗余索引。
- **Regression:** 匹配/不匹配混合、稀疏ID、幂等重跑，工作量近似线性，MySQL/PostgreSQL独立验证。
- **Effort:** 2–4 小时。

### F29 — 启动 API 返回成功但 HTTP 可能未监听
- **Severity / Status / Confidence / Category:** Medium / Confirmed / 高 / Stability、Release；未修复，源码证据。
- **Evidence:** `internal/server/server.go:57` 忽略路由注册错误，`:61` 异步 ListenAndServe只记录错误，`:66` 返回nil；`cmd/start.go:79` 因而继续。`cmd/start.go:35` 与 `cmd/desktop.go:71` 也忽略 `conf.Load` 的error。
- **Problem / Scenario:** 端口已占用仍继续启动后台任务；配置解析失败也不能在最靠近入口处正常返回错误。
- **Impact:** onReady/监督器看到的进程状态与真正服务状态不一致，出现“活着但不能响应”。
- **Minimal fix / Long term:** 同步验证配置/路由，先 net.Listen 成功再异步 Serve；按启动生命周期传播错误，不增加重复重试兜底。
- **Regression:** 端口冲突、非法注册、坏配置均返回错误；正常成功返回后立即可连接。
- **Effort:** 1–3 小时。

### F30 — 价格抓取失败生成空表并报告成功（已修复）
- **Severity / Status / Confidence / Category:** High / Confirmed / 高 / Release、Accounting；**本轮已修复局部根因**。
- **Evidence:** 修复前 `scripts/updatePrice.py` 捕获所有Exception并返回{}，main仍覆盖presets；`scripts/build.sh:300` 只按退出码判断成功。修复后见 `scripts/updatePrice.py:52`、`scripts/updatePrice.py:207`；回归见 `scripts/test_update_price.py:32`。
- **Problem / Scenario:** 主审先模拟网络失败并截获写入，确认原行为会生成空map且不报错；回归测试先出现5个失败子案例，修复后4个测试全部通过。
- **Impact:** 缺站点/DB价格的模型会失去全局兜底。`internal/relay/metrics.go:286` 缺价不计成本，金额配额不增长。后台虽有启动/周期刷新，但可能禁用/失败；`internal/helper/price.go:23` 生成的持久化零价还可能遮蔽后续全局价格。**不是所有已有有效报价的模型都会零计费。**
- **Applied fix:** 删除宽泛吞错fallback，网络/JSON错误传播使构建非零退出；没有可生成的支持模型时在写前报错。保留真实免费模型和原去重规则。没有改动真实预置、价格DB或联网抓价。
- **Remaining / Long term:** 持久化零价来源/刷新策略需另行核对；不能自动把全零价格当错误，因为免费模型合法。发布消费已验证快照、刷新与构建分离值得后续批准讨论。
- **Regression:** 网络异常、非法JSON、空/不支持目录均保留原文件；正常付费/免费模型及跨provider去重不变。
- **Effort:** 本轮完成；只修改脚本并增加标准库测试，不新增依赖。

### F31 — 构建“干净树”检查遗漏实际输入
- **Severity / Status / Confidence / Category:** Medium / Confirmed / 高 / Release、Maintainability；未修复，源码证据。
- **Evidence:** `scripts/build.sh:185` 仅检查 internal/cmd/main.go/scripts/web/src/static/go.mod/go.sum，遗漏 `web/package.json`、`web/pnpm-lock.yaml`、`web/next.config.ts`、`web/public`；git检查失败还被 `|| true` 转为空结果。
- **Problem / Scenario:** 未提交的依赖、前端构建配置或公共脚本进入产物，guard仍显示clean；入口随后又运行 go mod tidy 和实时价格生成，产物输入并非完整只读快照。
- **Impact:** “发布仅来自已提交输入”的门禁不能兑现，复现及归因困难；本轮没有改文件来触发真实发布，也没有运行安装/发布脚本。
- **Minimal fix / Long term:** 对全部构建输入进行检查，git错误fail closed；明确哪些生成阶段允许改变输入，避免用黑名单追赶遗漏。
- **Regression:** 每类输入在隔离测试仓库分别修改都拒绝发布；git失败不算clean；明确调试绕过仍可用。
- **Effort:** 1–2 小时。

## 5. Security Concerns

| 边界 | 问题 | 应保留的现有防御 |
|---|---|---|
| 浏览器首次信任 | F01 | 已有配对origin限制、单次broker token、同源执行检查。 |
| 网络身份/认证资源 | F02–F05 | JWT算法及版本校验、管理员与API Key权限分离。 |
| 身份/秘密生命周期 | F19/F26 | 不把秘密加入query key；不把普通备份的合法凭据需求泛化成泄漏。 |

未确认可利用的SQL注入或命令注入，不作无证据断言。更新checksum与解压路径限制已存在；WebDAV配置目标是管理员授权能力，不直接报SSRF。新增公开入口上限是信任边界校验，不属于要删除的过度防御。

## 6. Stability Concerns

最高收益是修复状态所有权，而非再加锁/再做一次flush：F06/F07/F08说明只有部分写入者遵守原子边界；F17/F18说明采集与成功终止被错误绑定；F25/F29说明调用返回值没有覆盖所有实际状态。建议每个修复先把观察到的反例固化在调用组合边界，再做局部改变。

## 7. Performance Concerns

| 优先改进 | 证据 | 验证方式 |
|---|---|---|
| 回填改主键游标 | F28 批次VM步骤增长 | 相同合成数据对照扫描工作量、正确性与幂等。 |
| 回收running tracking | F22 独立Set不受500日志上限约束 | 长会话集合大小、React Profiler、内存快照；本轮尚未做后两者。 |
| 指纹取消/分页/正则预算 | F10–F12 | 本地阻塞/重复游标/不利正则，验证退出时间与资源上界。 |
| 避免无业务流长期驻留 | F16 首token误判 | 心跳/半帧场景，首业务token预算及连接终止验证。 |

未获得用户流量分布、数据库规模、硬件、SLO，不能凭静态代码指定最终容量数字，也不能声称已提升响应速度。本轮优先给出可测量的优化方向。

## 8. Testing Gaps

### 8.1 第一轮验证（F30 价格脚本修复）

| 验证 | 结果与边界 |
|---|---|
| `go test ./... -count=1 -timeout=60s` | 修复前通过，外层60秒硬超时，实际27.5秒。 |
| `pnpm exec tsc --noEmit --incremental false` | 通过，11.9秒；没有做生产前端构建。 |
| `pnpm lint` | 0 error、1 warning：`web/src/components/modules/log/Running.tsx:306` 缺trackedIds依赖。未机械加依赖/删除豁免。 |
| Node现有测试 | web 11 + verification bridge（仅 bridge-common.test.js）13，共24通过。**第一轮快照；F01 修复后 background.test.js 新增 12 项，见 8.2。** |
| `bash scripts/build_archive_test.sh` | 通过；只使用脚本自建临时夹具。 |
| 临时Go overlay反例 | Gemini三类、op两类、登录两类、TLS一类、模型解码一类，均捕获目标错误；不把这些预期红测说成项目基线失败。 |
| 登录定向 `-race` | 实际报告 `login_throttle.go` 多字段竞争（**第一轮未修时的红测**，F04 修复后见 8.2）。 |
| 扩大范围 `-race` | 60秒硬超时，终止时无有效结果；**没有全仓race通过结论**。 |
| Node隔离实验 | 首次自动配对、QueryClient身份缓存、MutationObserver单次回调行为由主审复验。 |
| SQLite隔离实验 | 5000条合成记录，同形回填SQL出现全扫描及递增VM步骤。 |
| `git diff --check` | 通过。 |

### 8.2 第二轮验证（F01–F04 安全修复）

| 验证 | 结果与边界 |
|---|---|
| `go test ./... -count=1 -timeout=55s` | 修复后全量通过。 |
| `go test -race ./internal/server/handlers -count=1` | 通过；8.1 中登录定向 race 红测消除。 |
| `internal/server/server_test.go` | 真实 `newEngine()` 覆盖 9 个 ClientIP 场景（直连忽略 XFF/X-Real-IP、IPv6、可信多跳链、非法头）+ 6 个非法 CIDR 拒绝场景。 |
| `internal/conf/config_test.go` | 5 个 Viper Load 场景（文件数组、无空格逗号环境变量、空环境回退等）。 |
| `internal/server/handlers/login_throttle_http_test.go` | 真实 handler 429 + Retry-After + 固定 peer 轮换 XFF 仍锁定。 |
| `extensions/verification-bridge/background.test.js` | 12 项生产脚本 VM 回归通过（源目录与 static 镜像各 6 项）；覆盖 fragment 自动配对被拒、手动配对成功、撤销使在途自动配对失效三主路径。 |
| F01 首配信任 | 空配对/未知 origin 在任何网络请求前拒绝；popup 手动路径无法被网页触达（无 externally_connectable）；两份镜像逐行一致。 |
| F02 证书验证 | `tls_fingerprint_test.go` 不可信证书/错主机名拒绝、可信证书（临时 CA + 隔离子进程）通过。 |

### 8.3 第三轮验证（2026-09-05 复审收口，实测）

用户在原 Codex 会话要求的「gpt-6-astra xhigh 子代理复审」**未执行**：运行时不支持该模型，降级尝试被用户中止，后续请求因 429 限流无输出。复审改由 ZCode 首席审查官流程（8 位顾问并行对抗审查）完成，结论「有条件通过」。以下为本轮实际执行的合入门禁，全部通过：

| 验证 | 结果 |
|---|---|
| `go build ./...` | 通过。 |
| `go test ./... -count=1 -timeout 120s` | 通过。 |
| `go test -race ./internal/server/... -count=1 -timeout 300s` | 通过（server、auth、handlers、resp 全绿）。 |
| `python3 -B -m unittest discover -s scripts -p 'test_update_price.py' -v` | 4 项通过。 |
| `node --test` 扩展测试 | background 12 + bridge-common 13 = 25 项通过（**修正 8.1 的 24：F01 新增 12 项后总数为 web 11 + bridge 25 = 36**）。 |
| `pnpm exec tsc --noEmit --incremental false` / `pnpm lint` | 通过 / 0 error、1 warning（同 8.1 已知项）。 |
| 复审新增改动 | `server.go` 路由注册错误传播（原 `RegisterAll` 返回值被吞）、`trusted_proxies` 为空时启动告警日志、CI 接入扩展测试/Python 测试/server 包 race。 |

复审残留已于同日收口（commit 42b9b66 / 5cdef39 / 6ae50e7）：
- F01 自动入口从同 origin 比较收紧为 baseURL 严格相等 + 配对 id 一致，新建分支对自动入口关闭；自动配对成功/拒绝写入 `lastAutoPairEvent` 并在 popup 顶部展示；扩展版本 bump 0.4.0。扩展测试增至 31 项（含带路径变体拒绝、不同 pairing id 拒绝、事件可见三个新用例）。
- F30 增加显式价/缺失价区分守卫（有价条目占比 < 50% 判定 schema 漂移并拒绝生成）、provider 缺失显式报错、model_id 白名单（拒绝注入字符）、按原始 id 去重（冒号命名不再吞并）、presets.go 原子写入。Python 测试 4→9 项。
- 登录限流：容量拒绝审计日志按清扫间隔全局限频并携带累计丢弃数（`atCapacity` 返回标记），锁定建立输出一次性日志；`-race` 通过。
- 仍披露的边界：反代共享登录预算的攻击面有界存在（约 17 rps 撑满容量），已由启动告警、容量日志限频与 README 部署说明三重披露；批次 B 的 F06/F07/F09 未修前不发 stable。

局部复现文件在 `/tmp/octopus-audit-20260905.1FPsgu`，是明确标识的临时夹具，不是待合并的失败测试；后续可转成正式回归。临时目录可能被系统清理，报告中的行为与测试设计是持久记录；已修项的持久回归均已入库（8.2/8.3）。

复跑示例（未修问题预期退出非零）：

```sh
perl -e 'alarm 60; exec @ARGV' go test -overlay /tmp/octopus-audit-20260905.1FPsgu/overlay.json ./internal/transformer/outbound/gemini ./internal/op -run '^TestAudit' -count=1 -timeout=55s
perl -e 'alarm 60; exec @ARGV' go test -race -overlay /tmp/octopus-audit-20260905.1FPsgu/handlers-overlay.json ./internal/server/handlers -run '^TestAuditLogin' -count=1 -timeout=50s
perl -e 'alarm 60; exec @ARGV' go test -overlay /tmp/octopus-audit-20260905.1FPsgu/network-overlay.json ./internal/client ./internal/helper -run '^TestAudit' -count=1 -timeout=50s
python3 -B -m unittest discover -s scripts -p 'test_update_price.py' -v
```

## 9. Maintainability Concerns

不把大型文件本身算缺陷：`site-channel/index.tsx` 和 `site/index.tsx` 很大，但本轮报告的是行级生命周期具体失败。优先形成以下可局部落地的边界：同一实体的统一状态发布、每次异步操作的清理、单一协议语义转换源、明确的传输预算。不建议引入通用Repository/事件总线/多层工厂来掩盖这些问题。

## 10. Type Safety Concerns

静态类型通过并不能证明外部JSON结构正确（F09）、同一整数索引的语义正确（F14）或SSE生命周期完整（F23）。优先在真实信任边界校验形状；内部已有类型和上游约束保证的不变量，不应重复层层验证。没有完整统计全仓断言/any用量，也没有以类型转换次数直接推断缺陷。

## 11. Release Concerns

`.github/workflows/ci.yml:33` 只做普通Go测试，前端在 `.github/workflows/ci.yml:61` 仅lint；没有这些新交错的race门禁，也未执行现有前端Node测试、明确的tsc检查、新增Python测试。标签发布工作流不依赖此处测试job成功。建议逐项接入已有可复现命令，而不是只增加检查数量。F30局部修复不会自动处理历史缺价记录；也没有实际重新构建/部署产品。

## 12. Principles Compliance

### Principles Violated

| Principle | 具体证据 | Severity | Affected Areas |
|---|---|---|---|
| 单一状态所有权/原子性 | F06/F07/F08 | High/Medium | op/cache/配额 |
| 不可信输入先验证 | F01/F05/F09 | High/Medium | bridge/API/models |
| DRY，语义单一来源 | F13–F15，普通/指纹取消差异F12 | High/Medium | transformer/client |
| Fail-fast，错误不伪成功 | F23/F25/F29/F30 | High/Medium | SSE/恢复/启动/构建 |
| Command-query separation | F08 getter隐式写零值 | Medium | stats |
| 有界资源/执行预算 | F10/F11/F22/F28 | Medium | 同步/前端/迁移 |

### Principles Respected

- ADR把Client Request、Upstream Attempt、Request Outcome、Transport Termination分开，方向正确；本轮要求实现遵守，不推翻领域模型。
- API handlers→op→DB/cache 的主分层可以保留，不需要拆微服务。
- 已有日志与usage事务、失败批次恢复、取消重试退避、快照复制等有真实用途。
- 站点级、账户级代理偏好及严格/宽松协议策略有明确业务差异，不能因分支多就删除。

## 13. Fallback / Defensive Code Analysis

### Fallback Summary

| 处理 | 模式/证据 | 建议 |
|---|---|---|
| **已删除** | F30 broad catch→空数据→成功写表 | 保留原异常与原预置；写前拒绝无法生成的目录。 |
| 应简化 | F26 先泄露再尽力清理 | 秘密写前剔除，删除事后修补流程的依赖。 |
| 应纠正 | F29 忽略错误后继续启动 | 同步建立就绪契约，不层层补重试。 |
| 应显式化 | F23 无反馈的catch/close | 状态+有限重连，不静默装作仍实时。 |
| 应复用 | F12 已有context版本却调用Background包装 | 复用已有接口，不再增加适配包装。 |
| 应统一 | F13–F15 双转换链漂移 | 组合测试保护后逐步统一语义。 |

**明确不删除的合理防御：** SSE Source.Close 的sync.Once；passthrough metrics的CAS去重；已写出后的禁止重试；可取消退避；日志/usage事务与失败恢复；SQLite大表AddColumn-only；tools批量上限与探测预算；输入大小/ZIP路径/校验和限制。这些位于真实信任或并发边界，不属于冗余校验。

没有为了“精简”而批量删除未知调用的函数、compat shim、注释或防护，也没有新增通用抽象。

## 14. Testing Authenticity Analysis

### Confidence Assessment

当前测试对单层功能有价值，但对跨层组合、失败提交顺序、会话切换、并发读改写覆盖不足；一次绿测无法反证这些风险。

### Valuable Tests

实际数据库临时夹具、日志与usage事务失败恢复、配额并发自增、stream terminal测试、归档内容测试都应保留。新增价格测试只替换外部网络边界，真实运行生成器并检查临时文件内容，不是单纯验证mock被调用。

### Suspicious Tests

旧TransformStream直接单层测试不能覆盖relay优先使用的事件链；顺序loginThrottle测试不能证明共享指针线程安全；只测纯helper的前端Node用例不能证明身份和组件生命周期正确。以上是**覆盖范围不足**，不直接给现有测试贴“伪测试”标签或删除。

### Missing Tests

按F01–F31各卡片落地针对性回归；最高优先是登录race、模型同步坏输入、配置刷新与费用、配额reset交错、Gemini组合wire、身份切换与并发mutation。CI接入这些命令属于待批准批次。

## 15. Type Safety Analysis

### Summary

优先用独立类型/字段表达choice index与tool index语义；对外部列表区分缺失/null/空数组；请求认证epoch不等于token字符串。是否新增类型须服从最小改动，不建议泛化所有DTO或重写API client。

## 16. Frontend State Analysis

### Summary

F19/F20需要一个认证生命周期所有者；F21需要每请求而非最后observer的生命周期；F22的派生集合需要有界回收；F23/F24需要真实错误反馈。组件拆分优先围绕这些可验证职责，不按行数机械切文件。真实浏览器交互验证仍是后续门槛。

## 17. Backend API Analysis

### Summary

区分认证前资源预算、身份验证、业务校验、持久化发布四个阶段。F03/F05/F09是边界问题；F06/F07/F25是提交顺序问题。合理的修复应减少需要调用方记住的隐式步骤，而非增加更多重复校验或调用方flush。

## 18. Dependency Weight Analysis

### Dependency Scoreboard

| 依赖/工具 | 本轮证据 | 处理 |
|---|---|---|
| Gin v1.12.0 | 实际默认代理信任与项目部署假设不一致 | 显式配置，不建议盲目换框架。 |
| TanStack query-core v5.90.11 | 实际cache fresh和单observer回调语义 | 修调用生命周期，不指控库BUG。 |
| regexp2 v1.11.5 | 默认执行预算不满足同步操作需要 | 设置有限预算；兼容性确认后才考虑替换。 |
| TLS指纹客户端 | 项目显式传insecure选项 | 修配置边界，不是已核实的依赖CVE。 |
| Next/React/UI库 | 未测bundle贡献、未查外部漏洞库 | 不作删除/升级结论，不凭package体积猜性能收益。 |

## 19. Recommended Fix Order

### Fix Immediately

| 批次 | 问题 | 范围与回归门槛 | 审批 |
|---|---|---|---|
| A 安全信任 | F01–F05、F26 | 扩展源/嵌入副本、client、server/conf及测试；首次确认、证书、可信代理、race、正文限额 | **L2，逐项确认后实施** |
| B 费用/目录正确性 | F06–F09、F25；F30历史影响核验 | op/middleware/helper/task/恢复；临时DB交错、坏输入、不丢pending | **L2，逐项确认后实施** |
| C 协议保真 | F13–F18 | transformer事件IR/消费者/relay；组合wire、终态、usage和重试矩阵 | **L2，先确认兼容策略** |

### Fix Before Stable Release

| 批次 | 问题 | 门槛 |
|---|---|---|
| D 前端身份与操作 | F19–F24 | 身份隔离、迟到响应、并发行操作、SSE恢复、长会话、真实浏览器测试。 |
| E 预算/启动/发布 | F10–F12、F28/F29/F31、CI测试接入 | 本地故障注入、扫描工作量对比、端口失败、完整构建输入检测。 |

### Schedule Later

F27先确认单实例/多实例及ID合同，再做可注入时钟和重启复现；不得仅凭名称“snowflake”就认定支持分布式。多数据库完整升级链、生产样本query plan、前端Profiler与bundle分析在安全/正确性修复之后推进。

### Ignore for Now

没有实际收益证据的全仓抽象替换、微服务拆分、批量变量/文件改名、删除必要并发防护、为了零warning机械调整effect。本轮不需要全局安装、不应改全局Codex/MCP配置，也不执行真实数据恢复。

## 20. Quick Wins

- **已完成：F30**，实际生产代码净减2行；错误路径更直接，4项标准库回归通过。新测试不接触真实价格文件。
- F12复用已有context API；F24补失败反馈；F29同步监听并传播错误，均是范围容易界定的后续小任务。
- F28主键游标和F22集合回收有可量化验证路径，优先于没有profile支持的微优化。
- 先将本轮隔离反例转成各批准批次的正式回归，避免再次只修顺序正常路径。

## 21. Long-term Refactor Plan

| 建议 | 强度 | 价值与约束 |
|---|---|---|
| 统一同实体状态发布/字段所有权 | Strong | 解决F06/F08多个入口不同锁契约；先用现有锁/函数，不引入全仓cache框架。 |
| 统一协议规范化语义源 | Strong | 两条转换链已有保真差异；先组合测试再迁移，保留有业务意义的协议策略。 |
| 认证会话成为前端私有数据边界 | Strong | 一处负责取消、清cache、epoch，减少各页面自行补救。 |
| 恢复/启动提供明确成功与失败边界 | Strong | 调用方不再手工记住discard/启动后台步骤；保持小接口和可故障注入。 |
| 扩展单一来源构建镜像、价格刷新与发布分离 | Worth exploring | 降低副本漂移和非确定构建输入，实施前确认现有发布流程。 |

**最终交付边界：**本轮仓库变更仅 `scripts/updatePrice.py`、`scripts/test_update_price.py` 和本报告。没有批量修复30项剩余风险，没有修改已有规划/历史报告，没有提交或部署。跨模块修改与安全策略改变须按上述批次确认；本报告并非“全部问题已修好”的声明。
