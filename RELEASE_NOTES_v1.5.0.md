# Octopus v1.5.0 Release Notes

> English first, 中文见下方. — v1.4.3 → v1.5.0.

## ✨ Features

- **Vision Bridge**: when an image-bearing request is routed to a model *proven to lack vision*, a configured VLM first converts the images into text descriptions before forwarding — text-only upstreams don't error on images, they silently degrade (measured 100%: empty content / hang / refusal), and the bridge covers exactly that path. Validated end-to-end with 121 real gateway calls before implementation.
  - **Routing**: vision-capable / unknown-capability channels are always routed first (zero added latency); the VLM fallback only runs when no vision channel is available. Unknown models pass through untouched (never mis-replaced). Fallback path measures ~57s median end-to-end vs ~9s native — it is a fallback, not a replacement.
  - **Activation requires all three**: global toggle ∧ per-key toggle ∧ proven non-vision model. Off by default — no existing key changes routing behavior.
  - **Management**: dedicated settings page (DB-backed, changes apply immediately without restart) with VLM model / base URL / masked API key (explicit clear action) / fallback model chain, plus a **Test button** that probes the full chain with a built-in image (per-model availability / latency / description preview).
  - **Per-key inline toggle** on the API key card with a badge that grays out when the global switch is off.
  - **Fail-closed contract**: if the VLM chain fails, proven text-only channels are skipped entirely (the original image never reaches them) and the client gets a distinct `502 vision_fallback_exhausted` without internal details. The `/responses/compact` passthrough endpoint skips text-only channels for image inputs (best-effort detection). Known boundary: WebSocket inbound bypasses the bridge (noted in UI).
- **Relay log base64 redaction**: oversized base64 payloads (OpenAI data URIs *and* Anthropic `source.data`) in logged request content are replaced with placeholders, plus a 256KB hard cap — multi-MB images no longer bloat `relay_logs`. Log-layer only; forwarding is untouched. Applies to all requests, independent of bridge toggles.

## 🐛 Fixes

- **Inline API key toggles no longer clobber each other**: rapid sequential clicks on the enable / tools-only / vision-bridge switches used to spread a stale object and silently revert earlier toggles; the update hook now applies optimistic cache updates with rollback.
- **VLM/backup hardening from adversarial review**: `vision_bridge_api_key` excluded from backup exports (incl. WebDAV auto-upload); bridged request copies fully isolated from in-place outbound transforms (original request corruption reproduced and locked by regression tests); canonical-model capability evidence no longer contaminates differently-named upstream models; un-rewritten `previous_response_id` continuations keep their pinned channel order.

## 🧹 Housekeeping

- Handler-level integration tests locking the bridge invariants (image replacement, vision-channel priority, fail-closed, opt-out baseline); bilingual README usage notes (privacy / latency / test-first model selection / known boundaries); Step 0a validation report committed.

---

## 🐙 v1.5.0 发布说明（中文）

> 自 v1.4.3 起。English above.

## ✨ 新功能

- **视觉桥（Vision Bridge）**：含图请求被路由到*已证实无视觉能力*的纯文本模型时，先由配置的 VLM 把图片转成文字描述再转发——纯文本上游收到图片不报错而是静默降质（实测 100%：空内容/挂起/拒答），视觉桥专门兜住这条路径。实施前经 121 次真实网关调用完成价值链验证。
  - **路由**：视觉可用/能力未知的通道始终优先（零额外延迟），全部视觉通道不可用才走 VLM 兜底；能力未知的模型保守直通，绝不误替换。兜底链路端到端中位约 57s vs 原生约 9s——它是兜底，不是替代。
  - **生效条件（三者缺一不可）**：全局开关 ∧ key 级开关 ∧ 模型已证实无视觉。默认关闭，不改变任何现有 Key 的路由行为。
  - **管理面**：独立设置页（DB 存储，改完即生效免重启）——VLM 模型/Base URL/打码密钥（带显式清除）/备选模型链，以及**测试按钮**（内置测试图真调完整模型链，逐模型返回可用性/延迟/描述预览）。
  - **Key 卡片内联开关** + 徽章（全局未开时灰化提示）。
  - **fail-closed 契约**：VLM 链失败时纯文本通道整体跳过（原图绝不到达），客户端收到独立的 `502 vision_fallback_exhausted` 且不含内部细节；`/responses/compact` 直通入口对含图输入跳过纯文本通道（best-effort 检测）。已知边界：WebSocket 入站不经过视觉桥（UI 已标注）。
- **relay 日志 base64 脱敏**：日志请求内容中的超长 base64（OpenAI data URI 与 Anthropic `source.data` 均覆盖）替换为占位符 + 256KB 硬上限——MB 级图片不再撑爆 `relay_logs`。仅日志层，不影响转发；对所有请求生效，与视觉桥开关无关。

## 🐛 修复

- **Key 卡片内联开关不再互相清零**：启用/仅 tools/视觉桥三个开关连续快速点击时，旧实现会基于过期对象整体覆盖、静默回退先前的改动；更新 hook 现在做乐观缓存更新 + 失败回滚。
- **对抗审查驱动的加固**：`vision_bridge_api_key` 从备份导出剔除（含 WebDAV 自动上传）；bridged 请求副本与出站就地转换完全隔离（原请求被改坏的问题已实测复现并加回归锁）；canonical 模型能力位不再传染到不同名的上游模型；未改写的 `previous_response_id` 续接保持原通道顺序。

## 🧹 杂项

- Handler 级集成测试锁定桥核心不变量（替换/视觉优先/fail-closed/未开启基线）；双语 README 使用说明（隐私/延迟/先测后用/已知边界）；Step 0a 验证报告随库提交。
