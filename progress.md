# Progress

## 2026-08-08

- 已完成四个问题的全仓代码审查，确认当前行为与设计目标不一致。
- 已修正 `docs/group-multiplier-policy-analysis.md`，加入第四个问题：倍率策略必须是最高优先级准入规则。
- 已读取父级 Trellis、Octopus backend/frontend spec 和跨层思考指南。
- 四个问题审查完成：可见性、删除语义、暂停状态隔离、最高优先级准入均有明确代码落点和验收标准。
- 已确认没有激活的 Octopus Trellis 任务，使用本文件作为唯一规划状态。
- 当前阶段：开始后端数据模型、迁移和路由准入实现。

## 2026-08-09

- 已完成后端修复：倍率策略派生字段、持续路由准入过滤、独立 `policy_blocked` 状态、设置保存/报价更新后的状态重算和 SQLite 迁移 `024`。
- 已完成 API 与前端修复：站点渠道、分组列表、待建 Key/投影 Key 和路由预览返回并展示分组倍率、有效倍率、上限、策略状态及原因；同步暂停与倍率阻断可并列展示。
- 已补充回归测试：超限条目保留且不进入路由、提高上限后恢复、设置倍率校验、同步成功不清除策略阻断、迁移补列。
- 已通过 `gofmt`、`git diff --check`、`CI=true pnpm lint`、`CI=true pnpm exec tsc --noEmit --incremental false` 和 `CI=true pnpm build`。
- 默认 `proxy.golang.org` 访问超时；改用 `GOPROXY=https://goproxy.cn,direct` 后定向与全量 Go 测试均通过。
- 质量规范中的重复验证也已通过：取消终态 `-count=100`、流式契约 `-count=20`、Usage 并发 `-count=100`。

## 2026-08-12

- Vision Bridge Step 0a 价值链验证完成（PASS）：经 NAS 生产网关 121 次真实调用，①上游不可用率 100%、②VLM 描述成功率 95%、③替换链路可用率 80%。报告：`docs/reviews/vision-bridge-step0a-report.md`，harness/结果留档 `docs/reviews/step0a/`（gitignore 内，仅本地）。
- 计划外发现：上游失败形态是「200+空 choices」静默降质而非 400；VLM 空输出会诱发下游幻觉（需最小长度防护）；GLM 视觉系经本网关不可用（429/404），Step 0a 用 kimi-k3 代打。两条必改项已写回 task_plan.md。
- 下一步：Step 1 骨架 + 核心路径（vision-bridge worktree，2 天）。
- 注意：同一时段有另一会话在改 multiplier/catalog 相关文件（internal/op/、internal/db/migrate/），本任务未触碰这些文件。

## 2026-08-13

- Vision Bridge Step 2 完成并提交（worktree feat/vision-bridge，commit 见分支）：DB Setting 五键 + 设置页（VLM 连通性测试按钮、密钥打码/显式清除）+ APIKey.vision_bridge 内联开关 + 管理端 /api/v1/vision-bridge/test。
- 恢复上一会话（0d53fe7f，429 中断）：回收 backend-adversary 完整审查报告（4 P1 + 7 P2 + 7 P3），补派前端对抗审查（1 P1 + 3 P2）。
- 审查修复落地（后端）：备份/WebDAV 导出剔除 vision_bridge_api_key；502 错误不再回显 VLM 内网地址与上游错误体；compact 入口对含图输入跳过已证实纯文本通道；rewrite 副本深拷贝（Messages 内容块 + Tools）并清空 RawInputItems/扩展 raw items（审查遗漏项，Responses 出站以 raw 为权威源会漏原图，已加回归测试）；canonicalVision 仅对同名上游模型作证据 + 能力判定请求内快照；未改写续接请求跳过 StablePartition；replay 状态改存 bridged 副本；HasImages/Discover 覆盖 Message.Images 旁路；成功 attempt 打 vision_bridged 标记；probe 错误截断 500 字符 + 测试端点校验 base_url/上限 8 备选；SSRF 名单补 CGNAT；cacheKey 长度前缀；base64 空白容错；VLM content 数组兼容；modelChain 去重。
- 审查修复落地（前端）：useUpdateAPIKey 乐观更新+回滚（三开关连点互相清零 P1）；VLM 密钥「清除已保存密钥」入口；Key 卡片视觉桥徽章感知全局开关（未启用灰化）+ 表单 hint 跳转设置页；配置变更即失效旧测试结果；错误 line-clamp；三语 locale 补 4 键并修正 test.hint/en 徽章大小写，description 标注 WS 入站边界。
- 验证：go test ./... 32 包 EXIT=0；gofmt/diff-check 干净；CI=true tsc --noEmit / pnpm lint / pnpm build 全绿。
- 已知边界（记录）：WS 入站不走 bridge（UI 已标注）；relay.Handler 级端到端测试仍缺（backend-adversary 测试缺口清单，后续补）；十进制/短格式 IP SSRF 形态未拦（内网自用接受）。
- Vision Bridge Step 3（收尾）完成：relay_logs 请求内容 base64 脱敏（无锚点长串模式 + 256KB 硬上限兜底，仅日志层，响应侧沿用既有 filterResponseForLog）；Handler 级集成测试 4 用例锁定核心不变量（bridged 替换/视觉通道优先压过 priority/VLM 失败 fail-closed 不回显/key 未开启基线不变）；handlers 校验与探测截断单测、safety（CGNAT/空 payload/折行 base64）与 vlm（数组 content/modelChain 去重）回归锁；README + README_zh「视觉桥使用说明」小节；Step 0a 报告随分支提交（修复引用悬空）。
- Step 3 对抗审查（1 agent，全部实测验证）：P1 = 首版脱敏对 Anthropic 入站（source.data 无 data URI 前缀）完全失效 → 改无锚点 base64 长串模式 + 硬上限，PHP \/ 转义、折行、大写 BASE64 等绕过一并关闭；P2 = -count=2 下集成测试因包级 analysis cache 复用必挂（已实跑复现）→ 加 ResetServiceForTest 钩子；README 延迟口径改端到端 57s vs 9s（约 6 倍）。记录项：脱敏同步扫描约 0.5s/15MB（内网接受，硬上限后自然收敛）；op channelRefreshCache 不清 channelCache（既有疑似问题，未触发）。全量 32 包 EXIT=0，-count=2 集成复跑通过。
