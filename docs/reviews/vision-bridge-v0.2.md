# Vision Bridge 集成方案

> 版本：v0.2（已按 9 顾问评审修订）
> 状态：待实现
> 关联：task_plan.md「Vision Bridge 调研与集成方案（2026-08-12）」段落

## 0. v0.1 → v0.2 修订记录

| # | v0.1 问题 | 来源 | v0.2 修订 |
| --- | --- | --- | --- |
| 1 | passthrough 模式 bridge 白跑、原图仍透传，「绝不透传」被击穿 | 后端/逻辑/反驳者 | §3.2：bridge 触发时强制该 attempt 走 transform |
| 2 | 就地改写 internalRequest 污染 failover，视觉通道拿不到原图 | 后端/反驳者/本质追问者 | §3.2：改写作用域=快照改写（ctx 存原图，只改当前 attempt） |
| 3 | tool_result 图片声称与代码不符（inbound 已丢弃） | 反驳者/本质追问者 | §2.2/§5.1：v1 明确不支持，列入非目标 |
| 4 | 「视觉优先」排序与 balancer/sticky 冲突、位置未定义 | 逻辑/性能/无情行者 | §5.3：relay 循环前对迭代器内部切片 stable sort |
| 5 | VLM 失败分支死逻辑 + 逐通道重试风暴 | 逻辑/性能 | §5.3：VLM 失败=请求级标记，非视觉通道跳过 |
| 6 | 缓存 key 缺 focus/base_url 维度、模型链漂移 | 数据对抗者/后端 | §5.5：key 补维度；只缓存链首模型 |
| 7 | 「谁拉图」未定义，SSRF 防线在云端路径无效 | 安全对抗者 | §5.6：本地端点自拉图（dial 层拒绝），云端 URL 直传不校验 |
| 8 | 数值口径矛盾（8图×600词 vs 输出上限 vs 20000 chars） | 数据/性能/反驳者 | §5.1/§5.7：动态预算 + max_tokens + 8000 码点 |
| 9 | 非流式 VLM 阶段零字节零保护，120s 超 CF 100s | 性能/反驳者 | §5.4：非流式 VLM 预算 60s |
| 10 | VLM 后端独立 client 重复造轮子 | 机会发现者 | §5.2：复用 channel HTTP client 基建 |
| 11 | 价值链前提零数据支撑 | 本质追问者 | §1.1/§8.2：新增 Step 0a/0b 分阶段验证 |
| 12 | !ok 模型 bridge 永不生效（自定义别名场景） | 反驳者/逻辑 | §5.3/§5.7：通道级视觉能力覆盖配置 |
| 13 | VLM 空/拒答输出无检测 → 静默垃圾 | 反驳者 | §5.2：最小检测（长度阈值+拒答特征串） |
| 14 | 流式 502 用 JSON 会污染 SSE 流 | 后端 | §5.3：流式走 SSE error 事件 |
| 15 | 图数超限行为未定义 | 逻辑 | §5.3：超限拒绝 413 |

## 1. 背景与目标

### 1.1 要解决的问题

用户的 API Key 可能指向**不支持视觉输入（multimodal）的纯文本模型**（如 DeepSeek）。当客户端发送含图片的请求时，这类模型无法理解图片，上游通常直接 400 或产生无意义输出。

**目标**：让 Octopus 在「目标模型不支持视觉」时，自动把图片转成高质量文字描述再喂给模型，实现「纯文本模型也能识图」，对客户端完全透明。

**前提验证（Step 0，编码前必做，分两阶段）**：
- **Step 0a（价值链验证，~1h）**：用云端 VLM（GLM-4V-Flash API，免费额度）× 真实目标模型（DeepSeek/Qwen 等纯文本模型）× 20 个代表场景（截图 OCR、图表、UI、含图指令），记录：① 上游对 image_url 的真实响应（400 / 忽略 / 垃圾输出）；② VLM 描述在任务上的成功率；③ 文本模型对替换文本的响应质量。若 ① 显示上游只是忽略而非 400，需重新评估 bridge 的必要性（可能是负优化）。产出：验证报告 markdown（附 20 个场景的响应样本）。
- **Step 0b（本地环境可选，~30min）**：本地 Ollama + moondream2/llava 环境搭建与单次冒烟（验证本地路径可行性）。若无本地隐私需求可跳过，Step 1 用云端 VLM 即可。

### 1.2 调研结论摘要（详见 task_plan.md）

- 生态通用原理：网关层拦截 `image_url` 内容块 → 调独立 VLM 把图转文字描述 → 替换图片块喂给纯文本模型。
- 最值得借鉴的实现：`Zesuy/Plugin-Deepseek-Vision`（Go/MIT/102⭐，CLIProxyAPI v7 预处理插件）——三协议图片发现/重写算法、fail-closed 契约、SSRF 防护、LRU 文本缓存、多图联合分析、模型回退链；其 cgo 插件 ABI 深度耦合 CLIProxyAPI，只能提取算法层。
- **本方案形态：Octopus 网关内建中间件**，复用其算法设计，按 Octopus 的通道语义调整失败策略。

### 1.3 非目标（v1 明确不做，防止范围膨胀）

- 不做前端 UI 配置界面（配置文件 + 环境变量）。
- 不做分布式/跨实例缓存（进程内 LRU 足够）。
- **不做图片本地下载管线**（图片由 VLM 侧拉取或网关临时拉取转 base64，见 §5.6 双路径；不落盘、不做缩放/OCR 管线）。
- 不做 agent 重分析（`view_image` 工具调用场景，v2 预留接口）。
- 不做 WebSocket 通道的 bridge（WS 是 OpenAI Responses 直通场景，v1 仅覆盖 HTTP transform 路径）。
- **不做 Anthropic tool_result 内图片**（inbound `messages.go` 当前丢弃 image 块，改 inbound 属范围变更，v2 再议）。
- 不做图片内容审查 / VLM 输出二次消毒 / 审计日志（见 §5.6「不做清单」）。

## 2. 需求范围

### 2.1 触发条件（全部满足才走 bridge）

1. 请求含 `image_url` 内容块（URL 或 data URI 两种形式；各协议 inbound 已归一化为内部 `MessageContentPart{Type:"image_url"}`）。
2. 目标通道的模型**确认无** multimodal 能力（`modelvendor.LookupCapabilities` 位图判 `CapMultimodal`，非旧布尔 `LookupVision`），且无通道级能力覆盖配置（`vision_capability_override`，见 §5.7）。
3. 全局开关 `vision_bridge.enabled == true`。
4. 通道不在 `vision_bridge.disabled_channel_ids` 黑名单（黑名单优先于一切）。

**能力判定三态语义**（全方案统一）：
- `ok=true, vision=true` → 视觉通道：原图透传，不 bridge。
- `ok=true, vision=false` → known-no-vision：fail-closed，bridge 或 502。
- `ok=false`（索引未知，如自定义模型名/别名/新模型）→ fail-open：**排位在视觉通道之后、known-no-vision 之前**，优先透传原图让上游决定；可用 `vision_capability_override` 显式修正。
- 索引未加载（`IndexSize()==0`）时：全部 `!ok`，bridge 静默不触发——启动日志 WARN 提示（复用 `internal/modelvendor` 现有日志点）。

### 2.2 支持面

- 入站协议：OpenAI Chat / OpenAI Responses / Anthropic（均已在 inbound 层归一化为 `InternalLLMRequest`）。**注意：当前代码库无 Gemini inbound 适配器**（Gemini 是出站协议），方案只覆盖现有三种入站。
- 图片位置：**仅 user 消息 content 数组**（v1 范围）。`file`/`document`/`input_audio` 块不处理；其中 Responses 的 `input_file` 图片块（`File` 结构）在 bridge 判定时应识别并走「跳过该通道/502」而非静默透传（§5.3 边界）。
- 出站协议：所有 transform 出站；**bridge 触发时强制该 attempt 走 transform 模式**（passthrough 与 bridge 互斥，见 §3.2）。

## 3. 总体设计

### 3.1 数据流

```
客户端含图请求
  → parseRequest()（inbound 归一化，图片块为 image_url）
  → 路由规划（group/iter 构造）
  → [NEW] 含图请求三档排序：视觉通道 > !ok 通道 > known-no-vision 通道
          （在 balancer 迭代器 sticky 应用之后做稳定分区，见 §5.3）
  → 通道循环内：
      internalRequest.Model = item.ModelName  (relay.go:278)
      → 判定该通道模型能力（三态）
      → known-no-vision 且 bridge 开启且请求含图？
          → 强制 protocolMode = Transform（§3.2）
          → 调 VLM 把图片联合分析为文本描述 → 在【当前尝试的副本】上替换图片块
          → 校验无残留 image_url
      → ra.attempt()  (relay.go:383，正常转发)
```

### 3.2 插入点与作用域（v0.2 关键修订）

- **主插入点**：`internal/relay/relay.go` 通道循环内、`internalRequest.Model = item.ModelName`（relay.go:278）之后、`ra.attempt()`（relay.go:383）之前。
- **与 passthrough 互斥**：`decision.ProtocolMode` 在该通道循环内已判定（relay.go:299-310）。若 bridge 判定成立（known-no-vision 含图），**强制该 attempt 的 protocolMode = Transform**——否则同协议通道默认走 passthrough 用 `rawBody` 把原图原样发给上游，bridge 白跑且违背「绝不透传」。实现上在 `ra` 构造时覆盖 protocolMode 即可（forward 的 passthrough 分支在 attempt 内可绕过）。
- **改写作用域**：**不在共享 `internalRequest` 上就地改写**。实现路径：在请求上下文（`gin.Context`）存原图快照（遍历 Messages 收集所有 image_url 块的 `[]MessageContentPart` 浅拷贝），替换只作用于 `internalRequest.Messages[i].MultipleContent`；进入视觉通道前从快照还原所有图片块位置。这消除「bridge 改写 → failover 到视觉通道 → 视觉模型拿到文本化请求」的静默降质（粘性路由场景尤其关键）。
- **一次请求一次 VLM**：VLM 分析结果按请求缓存（进程内），同通道重试与后续非视觉通道复用该结果，不重复调用（见 §5.5 缓存与 §5.3 失败标记）。

## 4. 模块设计（新增 `internal/relay/visionbridge/` 包）

| 文件 | 职责 |
| --- | --- |
| `bridge.go` | 入口：判定 + 调度，对外暴露 `DescribeImages(ctx, images, focus) ([]ImageDescription, error)` 与 `RewriteRequest(req) error`（接口按「可复用的描述能力」划分，v2 的 view_image 工具可复用同一调用） |
| `discover.go` | 遍历 user 消息 MultipleContent，收集 `image_url` 块，返回图片清单与替换点索引 |
| `rewrite.go` | 逐图替换为文本块 + 追加联合分析块；替换后 re-scan 校验无残留 |
| `vlm.go` | **复用现有 channel HTTP client 基建**（`helper.ChannelHTTPClientWithContext` + 代理语义），OpenAI 兼容调用；fallback 链 + 错误分类 + 429 短退避 |
| `prompt.go` | 描述 prompt 模板（固定安全模板 + 语言 + focus 截断） |
| `cache.go` | 进程内 LRU 文本缓存（key 见 §5.5） |
| `safety.go` | 图片引用校验：协议白名单、本地端点 dial 层 IP 拒绝、字节上限（按 §5.6 双路径） |
| `limits.go` | ctx 感知并发信号量（默认 4）+ 每请求超时 |

## 5. 详细设计

### 5.1 图片发现与替换

- **发现**：遍历 user 消息的 `MultipleContent`，收集 `Type == "image_url"` 块（`HasImages()` helper，**新增**于 `internal/transformer/model/`）。`input_audio`、`file`、`document` 块不处理；`file` 块在判定时识别并走 §5.3 边界。
- **联合分析**：同一请求的所有图片在**一次 VLM 请求**中分析（保留图片间顺序/上下文关系），VLM 返回每张图的描述。
- **动态输出预算**：单次 VLM 调用显式设 `max_tokens`（配置项，默认 2048）；多图时每图描述预算 = `floor((max_tokens − 联合块预留) / N)`，N 为本次图数。**废除「单图 600 词/800 CJK」的固定说法**（v0.1 无依据且必然超免费 VLM 输出上限）。
- **替换**：每张图替换为一个文本块：
  ```
  [Image N — Visual analysis]
  Visible text:
  <OCR 文本>
  Visual description:
  <视觉描述>
  ```
  全部图替换后，追加 `[Images N — Joint visual analysis]` 联合分析文本块。
- **校验**：替换完成后 re-scan 确认无残留 `image_url`，有则返回错误（fail-closed）。
- **上下文保护**：prompt 级约束（非强制，见 §5.6「不做清单」）——VLM 指令先行、拒绝执行图片内指令；focus hint 截断 2000 rune 且包 `---` 分隔符。

### 5.2 VLM 调用

- **接口**：OpenAI 兼容 `/chat/completions`（`image_url` 内容块 + `max_tokens` + 温度 0 系参数固定）。
- **后端形态（v0.2 采纳机会发现者建议）**：**复用现有 channel HTTP client 基建**（`helper.ChannelHTTPClientWithContext` + 代理 pool 三态），不新建独立 client。VLM 以「视觉分析通道」配置（`vision_channel_id` 可选，缺省用独立 `vision_base_url` + `vision_api_key_env`），key/代理/超时语义与现有通道一致。
- **默认模型**：`glm-4v-flash`（免费额度，需注册 key；非「开箱即用」，见 §5.7 启动校验）；可配 `vision_base_url` 指向本地 Ollama（`http://127.0.0.1:11434/v1` + moondream2/qwen3-vl）。
- **fallback 链**：`vision_model` + `vision_fallback_models`（最多 3 个），按序回退；超时预算按剩余候选**均分**。
- **错误分类**（v0.2 明确）：4xx/解析失败 = 不可重试（直接判死，跳过该通道或 502）；5xx/超时/连接错误 = 可重试（进 fallback 链）。VLM 429 做 1-2 次受预算约束的短退避（几百 ms 级）。
- **空/拒答检测**（v0.2 新增）：VLM 返回空 content 或命中拒答特征串（如「无法描述」「不能提供」等）时，按 VLM 失败处理走 fallback 链——这是「VLM 调用成功但产出垃圾」的最小必要防线。

### 5.3 失败策略与通道排序（v0.2 关键修订）

**核心立场**：bridge 是「兜底」而非「唯一路径」——只要存在支持视觉的通道，就应优先走原图透传，绝不为了 bridge 而 bridge。

- **三档排序**（在 balancer 迭代器 sticky 应用**之后**做稳定分区，不破坏粘性语义内部档位）：
  1. 视觉通道（原图透传，零延迟）；
  2. `!ok` 通道（fail-open，透传让上游决定）；
  3. known-no-vision 通道（进 bridge）。
  - 排序实现位置：`relay.go` 通道循环**前**、获取迭代器**后**，对迭代器内部 candidates 切片按三档做 stable sort（保留 sticky 前置效果，只在档位间移动）。不破坏 balancer 已产生的相对顺序，仅按视觉能力分层。
- **VLM 失败**（v0.2 修订）：
  - **VLM 失败 = 本次请求 bridge 失败**，在请求上打标记；剩余**非视觉通道直接跳过**（它们命中同一个 VLM），只保留尚未尝试的视觉通道继续。
  - 视觉通道全部失败且 VLM 失败 → 返回 502，`code: vision_fallback_exhausted`，附失败摘要与 attempts 明细（model/category/retryable）。
  - **原则**：对 known-no-vision 通道，绝不把原图透传（上游 400 且属静默降质）。
- **响应形态**（v0.2 修订）：流式请求的 502 用 SSE error 事件（复用 `hb.FlushOrError` 通道，避免污染已开启的 SSE 流）；非流式用 JSON 错误体。
- **429/503 优先级**（v0.2 明确）：上游 429/503 + Retry-After **优先透传**（让客户端 SDK 重试接管）；`vision_fallback_exhausted` 仅在无任何可重试信号时返回。
- **图数超限**（v0.2 明确）：超过 `max_images_per_request` → 拒绝，413 + 明确错误码（不截断不静默透传）。
- **bridge 关闭或 `!ok`**：按 fail-open 透传（现状语义），`!ok` 可用 `vision_capability_override` 修正（§5.7）。

### 5.4 流式与延迟

- bridge 在**请求转发前**完成（预处理），随后照常流式转发；响应流不做改造。
- **流式**：VLM 阶段有早期心跳覆盖（心跳在通道循环前启动，bridge 在循环内执行，期间持续发 SSE 注释字节，`hb.Hand()` 交接）——已验证不会被 CF 零字节判死。
- **非流式**（v0.2 修订）：非流式在 VLM 阶段零字节发送、无心跳保护，且 120s 预算可能超 CF 100s 读超时。**非流式 bridge 请求的 VLM 预算压到 ≤60s**（CF 100s 留余量给上游），超时快速返回 5xx 而非死等；配合 §5.1 的 max_tokens 收紧，单次 VLM 生成控制在秒级。
- 文档（USAGE）如实写明：非流式含图请求走 bridge 且部署在 CF 之后时，VLM 阶段超 100s 会 524。

### 5.5 缓存（v0.2 修订）

- 进程内 LRU；**key = SHA256(语言 + prompt 版本 + focus 截断后内容 + vision_base_url + 模型 + 图片身份)**。
  - focus 维度（v0.2 新增）：同一图不同提问不串结果——修正 v0.1「主场景错误复用」P1。
  - base_url 维度（v0.2 新增）：云端/本地端点切换不串。
  - **只缓存链首模型（主模型）成功结果**：fallback 模型产出不写缓存，避免质量漂移。
- **图片身份**：
  - data URI：**规范化后**取纯 base64 payload（补 padding、去 MIME 大小写差异）做内容哈希，长 TTL 900s——修正 v0.1「同图不同 key」。
  - URL：URL 本身，短 TTL 120s（仅对静态资源有效，签名 URL/动态图命中率低属预期）。
- **联合分析块不进缓存**（v0.2 明确）：单图描述按图缓存（最常见截图场景）；多图联合块每次重算（多图场景少，避免集合语义复杂化）。
- 只缓存派生文本与不可逆哈希，**不缓存图片字节**。

### 5.6 安全与隐私（做「必要的防线」，不做「安全堡垒」）

**谁拉图（v0.2 明确，双路径）**：
- **本地 VLM 端点**（`vision_base_url` 指向本地/内网，如 Ollama）：**网关自拉图**——复用 `internal/op/proxy_pool.go` 现有 IP 判定，下载时用自定义 `Transport.DialContext` 做 dial 层 IP 拒绝（杜绝 URL 重定向/rebinding 绕过），再以 base64 data URI 发给本地 VLM。此路径下：SSRF 防护真实生效、15MB 单图字节上限可执行、URL 隐私泄漏消除（URL 不出网）。
- **云 VLM 端点**（默认 GLM）：图片 URL 由 VLM 服务商服务器拉取，**网关不做 IP 校验**（校验的是第三方要拉的东西，无效防线）——此路径的 SSRF 属 VLM 厂商责任，非网关责任。文档写明「图片与图片 URL 会发往 VLM 服务商并被其拉取」。

**必要防线**：
- 图片引用校验：仅接受 `http(s)` / `data:image` URI；拒绝 `file://`；本地端点路径做 dial 层内网/loopback/link-local IP 拒绝；单图字节上限（本地路径 15MB 可执行；云端路径对 data URI 有效，URL 形式只做协议/长度检查）；单请求字节上限 20MB。
- 失败响应不回显上游文本/URL/凭证。

**顺手修（低成本）**：`relay_log` 落库时对 bridge 触发过的请求省略 `image_url` 内容块（data URI 原图可达 20MB，参考现有 `images.go` 日志省略先例）——避免 base64 原图落库。

**明确不做（避免过度防御）**：
- 不做图片内容审查/敏感内容过滤（VLM 是可信第三方，职责是描述不是审查）。
- 不做 VLM 输出二次消毒（自由文本可靠消毒在技术上不可行；prompt 约束 + 分隔符是务实防线）。
- 不做审计日志（复用现有 relay metrics 的通道/模型/失败统计）。
- 不做跨用户缓存隔离（key 加 `api_key_id` 维度）——个人网关单用户 A=B，缓存投毒=自我注入，属可接受剩余风险；v2 若分享 key 再加。

**隐私提示**：bridge 会把图片、图片 URL 及提示文本发往 VLM 端点；使用云端免费模型意味着出网，敏感数据请配置本地 Ollama。写入 USAGE 文档，不设强制拦截。

### 5.7 配置

配置格式：JSON（与现有 `config.json` 一致），结构归属 `internal/conf`。

```json
{
  "vision_bridge": {
    "enabled": false,
    "vision_model": "glm-4v-flash",
    "vision_base_url": "",
    "vision_api_key_env": "OCTOPUS_VISION_API_KEY",
    "vision_channel_id": 0,
    "vision_fallback_models": [],
    "language": "auto",
    "max_tokens": 2048,
    "request_timeout_seconds": 60,
    "max_inflight_vision_requests": 4,
    "max_images_per_request": 8,
    "max_request_bytes": 20971520,
    "max_image_reference_bytes": 15728640,
    "max_result_chars": 8000,
    "analysis_cache_size": 128,
    "analysis_cache_ttl_seconds": 900,
    "analysis_url_cache_ttl_seconds": 120,
    "disabled_channel_ids": [],
    "vision_capability_override": {}
  }
}
```

- 默认 `enabled: false`——零影响上线。
- **启动校验**（v0.2 新增）：`enabled=true` 时校验 VLM 配置（base_url 或 channel_id 至少其一；key 缺失时 WARN 而非报错，本地 Ollama 无需 key）；`IndexSize()==0` 时 WARN「models.dev 不可达，bridge 静默停用」。
- `vision_capability_override`：模型名 → `"vision" | "no_vision" | "auto"` 的通道级覆盖表，修正自定义模型名/别名/索引误判两个方向。
- 配置项已按无情行者建议瘦身（v0.1 的 focus/language 细节、TTL 细分等收敛为上述最小集）。

## 6. 文件变更清单

新增：
- `internal/relay/visionbridge/`（bridge.go / discover.go / rewrite.go / vlm.go / prompt.go / cache.go / safety.go / limits.go）
- `internal/relay/visionbridge/visionbridge_test.go`

修改：
- `internal/relay/relay.go`：通道循环内接入 bridge（强制 transform + 快照改写）；含图请求三档排序（循环前对迭代器内部切片 stable sort）。
- `internal/conf/`：`VisionBridge` 配置结构 + 启动校验 + 加载。
- `internal/transformer/model/`：`HasImages()` helper（新增）。
- `internal/modelvendor/`：暴露 `LookupCapabilities`（位图）+ `IsModelVisionCapable`。
- `internal/relay/metrics.go`：relay_log 省略 bridge 请求的 image_url 块（顺手修）。

## 7. 测试计划

- 单元测试（visionbridge 包）：
  - discover：URL / data URI / 无图 / file 块边界。
  - rewrite：单图 / 多图联合 / 动态预算 / 替换后无残留。
  - safety：file:// 拒绝、本地端点 dial 层内网拒绝（含重定向绕过用例）、data URI 超限。
  - cache：focus/base_url 维度区分、data URI 规范化、链首模型限定、TTL。
  - vlm：错误分类（4xx 判死 / 5xx 重试）、429 退避、空/拒答检测、fallback 均分预算。
- 集成（relay 层，mock VLM HTTP server）：
  - 含图 + 视觉通道 → 原图透传（不调 VLM）。
  - 含图 + known-no-vision + bridge 开 → 强制 transform + 图片替换为文本后转发（**必须覆盖 Chat→Chat 同协议通道**，防 passthrough 回归）。
  - 含图 + 非视觉通道 + bridge 关 → 原样转发（现状语义）。
  - VLM 失败 → 非视觉通道跳过、只试视觉通道；全失败 → 502（流式走 SSE error / 非流式 JSON）。
  - 三档排序断言：含图 + random balancer 下视觉通道必须最先被尝试。
  - 粘性场景：sticky 非视觉通道 + 存在视觉通道时，视觉通道仍被优先尝试。
  - 流式请求：预处理后正常流式。
- 回归：现有 31 包测试全绿（bridge 默认关闭）。

## 8. 实现计划与分支策略

### 8.1 分支 / worktree

- 新分支：`feature/vision-bridge`（从 `dev` 拉出）。
- 新 worktree：`/Users/seller1990/Documents/Software Development/Octopus-vision-bridge`。
- 主 worktree 保持 `dev`；规划文档（task_plan.md / 本方案）随 dev 提交，功能代码全在 worktree 内。

### 8.2 实施步骤

0. **Step 0a（价值链验证，~1h，编码前）**：云端 VLM（GLM-4V-Flash）× 真实目标模型 × 20 代表场景，记录上游响应/VLM 成功率/文本模型响应质量。产出：验证报告 markdown。若上游仅忽略图片，评估是否降级方案。
0. **Step 0b（本地环境可选，~30min）**：Ollama + moondream2 环境搭建与单次冒烟。无隐私需求可跳过。
1. **Step 1（骨架 + 核心路径，~2 天）**：conf 配置 + 启动校验、`HasImages()`、visionbridge 包最小骨架（discover/rewrite/vlm/prompt/bridge，单图、无缓存/fallback/safety/limits）、relay 接入（强制 transform + 快照改写）。验收：`go test` 相关包全绿；集成测试覆盖「Chat→Chat 含图 bridge 开」用例；云端 VLM 端点冒烟一次「含图→非视觉通道收到文字描述」。
2. **Step 2（健壮性，~2 天）**：多图联合 + 动态预算、三档排序（relay 循环前 stable sort）、fallback 链 + 错误分类 + 429 退避、缓存（focus/base_url/规范化）、safety（双路径）、limits（ctx 信号量）。可选：本地 Ollama 路径验证。
3. **Step 3（收尾，~1 天）**：测试补全、relay_log 省略图片、USAGE 文档（隐私提示 + 配置 + 延迟/CF 说明）、全量回归、release notes。

## 9. 风险与开放问题

| 风险 | 等级 | 缓解 |
| --- | --- | --- |
| 价值链前提未验证（Step 0） | P1 | Step 0 前置实验；若上游忽略图片则降级方案 |
| 描述→文本模型链路降质不可观测 | P2 | bridge 生效时响应头标注（复用 X-Octopus-Warning 机制）；metrics 区分 bridge 成功/原生成功 |
| VLM 描述质量不均，强视觉任务仍弱于原生 | P2 | 视觉通道优先兜底；文档标注能力边界 |
| 图片出网隐私 | P2 | 双路径（本地 Ollama 可不出网）；USAGE 明示 |
| 非流式含图请求延迟（CF 524 风险） | P2 | 60s 预算 + max_tokens 收紧；文档披露 |
| 模型能力索引不准（!ok / 误标） | P2 | 三态语义 + `vision_capability_override` 覆盖 |
| VLM 免费额度 rate limit（429） | P2 | 短退避 + fallback；文档标注实测额度 |
| 多图 token 成本与上下文侵占 | P3 | max_tokens 硬上限 + 动态预算 + max_result_chars 8000 |

**v2 预留**（v1 不做，设计已留口）：
1. `!ok` 模型视觉能力**负反馈学习**（借鉴现有 `ReportToolsUnsupported` 机制：`!ok` 首次含图透传，若上游返回明确图像不支持错误则记住该模型无视觉，后续自动 bridge）。
2. 含图请求三档排序**并入路由规划层**（`CatalogPlanGroup`/feature 评估管线），v1 在 balancer 迭代器内实现。
3. Anthropic `tool_result` 图片支持（需改 inbound 归一化）。
4. agent 重分析（`view_image` 工具）复用 `DescribeImages` 接口。
5. 分享 key 场景：缓存加 `api_key_id` 维度 + VLM 用量计费折算。
