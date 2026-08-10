# 最近两次提交反抗性审查报告

## 审查范围

- `6d7709f926ccc20ee3e9c648de75877a18213822`：channel-model tools 能力探测与 `tools_only` 路由。
- `e741a8ec1f9084be1d0bc04c145147d56d6e43e2`：v3.1 判别矩阵、批量探测、证据层级和前端操作。
- 审查基准：最终 `HEAD=e741a8e`。只报告最终代码仍存在的问题；已被第二次提交修复的问题不列为缺陷。

本次采用反抗性审查：先假定状态机、数据库守卫和异步反馈存在错误，再沿正常用户路径寻找反例。审查同时限制“防护冲动”：不因纯理论风险建议新增重试、兜底或状态层，只保留可触发、可验证、会改变业务结果的问题。

## 结论摘要

| ID | 严重性 | 问题 | 类型 |
|---|---|---|---|
| R1 | 高 | 历史行的 `NULL` 被 SQL 守卫排除，探测结果静默不落库 | 升级回归 |
| R2 | 高 | 失败的流式终态在判定失败前被计为 tools 成功 | 状态反馈错误 |
| R3 | 中高 | 一次 `executed` 永久压制后续真实失败 | 过度防护 |
| R4 | 中 | auto 探测把 5xx 白名单文本累计为“不支持” | 判别矩阵错误 |
| R5 | 中 | 编辑器可重复启动真实扣费批量任务并丢失首个任务跟踪 | 前端竞态/重复实现 |
| R6 | 中 | preset 重建丢失 reset 的 `nil + u7 + probed_at` | 状态继承错误 |
| R7 | 中 | 用字符串扫描解析 JSON，合法响应会误判 | 过度编码 |
| R8 | 中低 | 行级 tools 操作在触屏设备上不可达 | 交互回归 |
| R9 | 中低 | 批量轮询失败被静默吞掉 | 错误反馈缺失 |
| R10 | 低 | 单 vendor 时 tools 筛选入口消失 | 条件耦合 |
| R11 | 低 | 异步接口声明返回 202，实际返回 200 | API 契约漂移 |

## 详细发现与修复方案

### R1：历史 `group_items` 行被 `NULL` 三值逻辑卡住

证据：

- `internal/model/group.go:40-43` 新增的 tools 字段没有非空约束或历史数据回填。
- `internal/db/db.go:69-82,137` 通过 `AutoMigrate` 给既有 `group_items` 加列。
- `internal/op/tools_policy.go:299-300`、`320-321`、`395-396`、`431-432` 使用 `NOT (...)` 或 `NOT IN (...)` 保护 source。
- SQL 中 `NULL NOT IN (...)` 和 `NOT (NULL = ...)` 的结果是 `UNKNOWN`，不会命中 `UPDATE`。

后果示例：

1. 用户从旧版本升级，原有 `group_items` 的 `supports_tools_source` 为 `NULL`。
2. 某渠道连续两次返回 `tools not supported`。
3. `ReportToolsUnsupported` 执行成功但更新 0 行，条目仍是未探测状态。
4. `tools_only` 路由继续放行这个渠道，请求持续失败；UI 也不会显示“不支持”。

最小修复：

1. 增加显式迁移，将历史 `supports_tools_source IS NULL` 回填为 `''`。
2. 将守卫改成空值安全表达式，例如 `COALESCE(supports_tools_source, '') NOT IN (...)`。
3. 不要再增加一层 Go 侧预读判断；数据库谓词本身应是唯一真相，避免读写竞态。

回归测试：用原始 SQL 插入/构造 `supports_tools_source=NULL` 的历史行，分别验证 `executed`、`unsupported`、T9 failure 和 U7 success 能按预期更新。

### R2：失败终态被提前计为 tools 成功

证据：

- `internal/relay/relay.go:515-520` 在 `fwdErr == nil` 后立即调用 `ReportToolsSupported`。
- `internal/relay/relay.go:526-551` 随后才把 `response.failed` / `error` 判为失败。
- `internal/relay/relay.go:1723-1751` 明确把 `response.failed`、Anthropic `error` 视为可通过 HTTP 200 流返回的失败终态。

后果示例：

1. 某条目当前已正确标记 `supports_tools=false`。
2. 普通 API key 仍向该渠道发送带 tools 的请求，上游以 HTTP 200 SSE 返回 `response.failed`。
3. 代码先把这次请求计为一次 tools 成功，之后才把请求结果判为失败。
4. 两次类似失败后，U7 将 `false` 重置为 `nil`；`tools_only` 又会把该渠道加入候选，形成错误回流。

最小修复：先计算 `outcome := requestOutcomeForTerminalEvent(terminalEvent)`，仅在 `outcome == RequestOutcomeSuccess` 时调用 `ReportToolsSupported`。`incomplete`、`cancelled` 和失败终态都不应提供 capability 成功证据。

回归测试：从 `supports_tools=false` 开始，连续输入两个 `response.failed`/`error` 流，断言状态仍为 false；再输入两个真正成功终态，断言状态才转为 `nil/u7`。

### R3：`executed/manual` 的永久保护属于过度防护

证据：

- `internal/op/tools_policy.go:318-321` 和 `392-396` 禁止 `unsupported` 与真实 T9 失败覆盖 `source=manual`。
- `internal/model/group.go:41` 虽记录 `supports_tools_probe_key_id`，覆盖判断却不考虑 key 或证据时间。
- `internal/op/tools_policy_test.go:222-246` 把“一次 executed 永久高于后续失败”固化为测试契约。

后果示例：

1. 管理员使用 channel key A 手动探测一次，模型成功执行 tool call，写入 `true/manual`。
2. 后续切换到权限不同的 key B，或上游关闭该模型的 tools 能力。
3. 真实业务请求连续多次明确返回 `tools not supported`。
4. 状态仍永久保持 true，`tools_only` 会持续选中已失效渠道，除非管理员手动 reset。

最小修复：只让管理员显式强制状态 `manual-force` 不可覆盖。`manual` 是一次探测结果，不是永久配置；达到确认阈值的真实失败应允许覆盖它。先采用这一条简单优先级即可，不建议再引入衰减分数、置信度浮点值或更多 evidence class。

回归测试：`executed -> 两次 T9 tools unsupported -> false/t9`；同时保留 `manual-force -> 任意探测/反馈仍不变`。

### R4：auto 探测没有限制为 4xx

证据：

- `internal/toolsprobe/tools_probe.go:100-120` 对所有非 2xx 响应直接匹配“不支持 tools”白名单，没有检查状态码范围。
- 文件头判别矩阵和测试描述都声明只有 `auto 4xx` 可以累计为 unsupported。
- `internal/toolsprobe/matrix_test.go:144-149` 只覆盖 required 5xx，没有覆盖 auto 5xx。

后果示例：

上游网关临时返回 502，并在 body 中包裹原始错误文本 `model does not support tools`。24 小时内出现两次后，代码把一个网关故障当作模型能力结论写成 false，导致 `tools_only` 排除本来可用的渠道。

最小修复：在 auto 2xx 分支之后增加明确的 `statusCode < 400 || statusCode >= 500 => unknown`，只有 4xx 才进入白名单累计。无需针对 429、500、502 分别增加更多分支。

回归测试：auto 500/502 即使 body 命中白名单，重复两次也必须保持 unknown；auto 400 的既有 pending/unsupported 流程保持不变。

### R5：编辑器可重复启动付费批量任务

证据：

- `web/src/components/modules/group/Editor.tsx:527-548` 只检查 `batchRunning`，没有检查 mutation 正在启动。
- `batchRunning` 到 `onSuccess` 的 `Editor.tsx:541-543` 才置为 true。
- `Editor.tsx:509-523` 启动新轮询前会停止旧轮询。
- `Card.tsx` 和 `Editor.tsx` 各维护一套相似但不一致的轮询/互斥状态机。

后果示例：

用户对 20 个条目快速双击“Test tools”。两个 POST 都在 UI 进入 running 前到达服务端，于是执行 40 次真实上游请求。第二个任务替换第一个任务的轮询，UI 只展示后一个结果；第一个任务继续后台运行并扣费。

最小修复：抽取一个共享 `useToolsBatchTask` hook，统一持有同步的 active-task ref、mutation、轮询和完成/失败清理。点击处理器必须在发 POST 前同步占用 active 状态。只修复前端正常路径即可；当前管理端场景不需要再给服务端叠加分布式锁或复杂幂等系统。

回归测试：组件测试中连续触发两次 click，断言只发送一次 POST；任务完成/启动失败后允许再次启动；新任务不能覆盖仍在跟踪的 task id。

### R6：preset 重建丢失 reset 元数据

证据：

- `internal/op/tools_policy.go:355-375` 的 reset 有意写入 `supports_tools=nil`、`source=u7` 和最近 `probed_at`。
- `internal/op/group_preset.go:212-223` 与 `448-458` 只保存 `SupportsTools != nil` 的旧行。
- `internal/op/group_preset.go:520-526` 重建后立即触发异步探测。

后果示例：

管理员刚点“恢复自动”，系统进入六小时冷却期。随后切换 preset，旧行被删除，新行没有继承 `u7/probed_at`，系统把它当成从未探测并立即再次发起付费请求；刚刚的 reset 语义因此失效。

最小修复：按 `(channel_id, model_name)` 保存并继承所有旧行的四个 tools 元数据字段，不要用 `SupportsTools != nil` 作为是否继承 provenance 的条件。对完全空的旧状态执行复制没有副作用，因此不需要再设计额外状态枚举。

回归测试：`ResetToolsState -> GroupPresetActivate/active preset update` 后断言 `nil/u7/probed_at` 保持，且探测 hook 在冷却期内未被调用。

### R7：手写字符串扫描 JSON 是过度编码且会误判

证据：

- `internal/toolsprobe/tools_probe.go:127-162` 用 `strings.Contains` 和自制 token 判断检测 tool call。
- Anthropic 与 Responses 只匹配无空格的固定文本；Gemini 只要 body 任意位置出现 `functionCall` 就返回 true。
- `internal/toolsprobe/matrix_test.go:212-240` 仅覆盖紧凑 JSON，没有覆盖格式化 JSON、错误字段或文本中出现关键字。

后果示例：

- 合法响应 `{ "type": "tool_use" }` 因冒号后有空格而被判为没有 tool call，手动测试显示 `required_ignored`。
- Gemini 的普通文本字段若包含单词 `functionCall`，即使没有 function call 对象，也可能被判为 `executed=true`。

最小修复：优先复用现有 transformer 的响应结构；若复用会引入反向依赖，则用 `encoding/json` 定义最小局部结构体，只读取协议规定的数组/对象路径。删除 `isJSONIdentByte` 和协议关键词全文扫描，不要继续给字符串解析器补更多例外。

回归测试：覆盖 pretty JSON、`null`、空数组、文本中出现关键词、错误对象出现同名字段，以及真正的多 tool call。

### R8：触屏设备无法使用行级 tools 操作

证据：`web/src/components/modules/group/ItemList.tsx:190-228` 将按钮容器设为 `opacity-0 pointer-events-none`，只有 `group-hover` 或 `focus-within` 时恢复；触屏设备没有稳定 hover，隐藏按钮也无法先获得焦点。

后果示例：手机或平板用户能看到条目，却无法对单条记录执行测试、强制不支持或恢复自动，只能退回整组批量测试，增加不必要的付费请求。

最小修复：在不支持 hover 的设备上始终显示操作按钮；桌面端可继续 hover 显示。不要新增一套移动端菜单状态机。

回归测试：Playwright 使用 touch/mobile viewport，断言三个操作按钮可见、可聚焦、可点击。

### R9：批量轮询失败被静默吞掉

证据：`web/src/components/modules/group/Card.tsx:109-123` 和 `Editor.tsx:509-522` 的 `catch` 只停止轮询并清空 running 状态，没有错误提示。

后果示例：服务重启后内存任务消失，status 接口返回 404。UI 只是停止动画，用户会误认为任务完成并再次点击，产生第二轮费用；第一轮究竟执行了多少条也不可见。

最小修复：共享 batch hook 在轮询错误时显示一次明确 toast，并保留“结果未知”的状态。不要自动重启任务，也不要无限重试；一次可见失败比隐藏重试更安全。

回归测试：模拟 status 401/404/500，断言停止轮询、显示错误且不会自动发起新的 batch。

### R10：tools 筛选与 vendor 数量错误耦合

证据：`web/src/components/modules/group/index.tsx:119-162` 把 tools 筛选按钮放在 `allVendors.length > 1` 的整个 vendor chips 条件块内。

后果示例：只有一个 vendor 的个人部署中，“仅看 tools 条目”完全不显示；增加第二个 vendor 后按钮才突然出现。

最小修复：把 tools 筛选作为独立控制渲染；只有 vendor chips 自身依赖 `allVendors.length > 1`。

回归测试：0、1、2 个 vendor 三种数据下 tools 筛选均存在，vendor chips 只在需要时出现。

### R11：批量异步接口的 202 契约未兑现

证据：

- `internal/server/handlers/tools_probe.go:46-59` 注释承诺 `202 + task_id`，实际调用 `resp.Success`。
- `internal/server/resp/resp.go:18-23` 的 `Success` 固定返回 HTTP 200。
- 同类异步入口 `internal/server/handlers/group_health.go:147-154` 明确返回 202。

后果示例：外部调用方按 HTTP 状态区分“任务已完成”和“任务已接受异步执行”时，会把 200 误认为结果已经完成；生成的 API 文档也会与实现不一致。

最小修复：与 group-health 保持一致，在 batch handler 明确返回 `http.StatusAccepted`。不要为此新增通用响应抽象，除非仓库随后出现第三个相同需求。

回归测试：handler 测试断言 POST `/batch` 返回 202，status 查询仍返回 200。

## 过度防护与过度编码判断

本次最明显的过度防护是 R3：把一次自动化 `required` 成功当成永久不可推翻的事实。真正应永久保护的是管理员显式配置 `manual-force`，不是一次观测结果。

最明显的过度编码有两处：

1. R5 中 Card/Editor 各自实现批量任务状态机，重复代码产生了不同互斥语义。
2. R7 中为 JSON 写半个字符串解析器，补丁越多越难覆盖合法格式。

建议修复时遵循一个约束：减少状态与实现数量，而不是继续补 guard。最终应只有一套证据优先级、一套批量任务前端 hook，以及结构化协议解析。

## 建议修复顺序

1. 先修 R1、R2：它们会直接污染或阻断核心路由状态。
2. 再简化 R3、修正 R4：让证据层级与判别矩阵恢复一致。
3. 处理 R5、R6：阻止重复付费和状态重建回归。
4. 用结构化解析替换 R7，再完成 R8-R11 的前端与契约修复。

## 验证记录与测试缺口

已执行并通过：

```text
go test -count=1 ./internal/op ./internal/toolsprobe ./internal/relay/...
npm run lint
./node_modules/.bin/tsc --noEmit
git diff --check HEAD~2..HEAD
```

现有测试通过不代表上述问题不存在，原因是测试集没有覆盖：旧库 `NULL` 行、HTTP 200 失败终态、跨 key/跨时间证据变化、auto 5xx、前端快速双击、reset 与 preset 组合、pretty JSON、触屏交互及轮询错误。

审查期间工作区存在与这两次提交无关的未提交改动；本报告只以 `HEAD~2..HEAD` 的提交内容为审查对象，没有把这些并发改动计入 findings。
