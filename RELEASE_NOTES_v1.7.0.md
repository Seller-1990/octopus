# Octopus v1.7.0 Release Notes

> English first, 中文见下方. — v1.6.0 → v1.7.0.

## ✨ Features

- **One-click browser sync**: a new 🌐 button on each site account card creates a verification pairing + session in one step and opens the target site in your browser. The Verification Bridge extension (v0.3.1) auto-detects the pairing from the URL fragment, strips it immediately, and takes over the sync task — no manual token copy-paste needed. The extension can be downloaded directly from the management UI via a 🧩 "Install Extension" button.

- **Capability badges everywhere**: model capability icons (multimodal / reasoning / voice / image-gen / video-gen) now appear in all model lists — group member lists and site-channel model tables — not just group card titles and the model catalog. Ungrouped models in the discovery list also get capability badges via a `resolveCapabilities` fallback against the models.dev index.

## 🐛 Fixes

- **Pairing orphan prevention**: when the one-click browser sync API creates a pairing but session creation fails, the pairing is now automatically revoked instead of being left as a 30-day orphan.
- **Extension zip download reliability**: the zip archive is now fully buffered in memory before writing the HTTP response, so a mid-zip error no longer leaves a corrupt 200 response with JSON appended to a half-written zip.
- **Browser popup false alarm**: `window.open` with `noopener` always returns `null` per the HTML spec; the previous check incorrectly reported "popup blocked" on every successful browser sync. The check has been removed.
- **Extension auto-pair tab leak**: closed tab IDs were never removed from the `autoPairedTabs` set, potentially causing silent skips when Chrome reused a tab ID. A `tabs.onRemoved` listener now cleans up.
- **Log detail site name**: log details always use the historical log card; site names are highlighted in both real-time and historical log lists.
- **Orphan group member cleanup**: group members referencing deleted channels are now automatically cleaned up.

## 🚀 Performance

- **Tools probe serial execution**: automatic tools capability probing for new group members now runs serially with a 5-second interval between each probe, instead of launching parallel goroutines. The global probe semaphore is reduced from 4 to 1, preventing burst requests to upstream channels.

## 🔧 Other

- Extension version bumped to 0.3.1 with auto-pairing via URL fragment, origin allowlist, and `pendingUrl` compatibility.
- `Operation` parameter in the one-click sync API is now validated against a whitelist (sync / checkin only).
- `nas_origin` is automatically derived from the request when `api_base_url` is not configured — no manual setup required.

---

## 中文更新说明

### ✨ 新功能

- **一键浏览器同步**：每个站点账号卡片新增 🌐 浏览器同步按钮，点击后自动创建配对和验证会话，并在浏览器中打开目标站点。验证桥扩展（v0.3.1）自动检测 URL fragment 中的配对信息并接管同步任务，无需手动复制令牌。扩展可直接在管理界面通过 🧩"安装扩展"按钮下载。

- **全位置能力图标**：模型能力图标（多模态 / 推理 / 语音 / 图片生成 / 视频生成）现在在所有模型列表中显示——分组成员列表和站点渠道模型表——不再仅限于分组卡片标题和模型目录。发现列表中未分组的模型也通过 `resolveCapabilities` 兜底显示能力图标。

### 🐛 修复

- **配对孤儿防护**：一键浏览器同步 API 创建配对后若会话创建失败，配对会自动撤销，不再残留 30 天孤儿。
- **扩展 zip 下载可靠性**：zip 归档现在先完整缓冲在内存中再写 HTTP 响应，避免写入中途出错时返回损坏的 200 响应。
- **浏览器弹窗误报**：`window.open` 使用 `noopener` 时按 HTML 规范始终返回 null，之前的检查在每次成功同步时都误报"弹窗被拦截"。已移除该检查。
- **扩展自动配对标签页泄漏**：关闭的标签页 ID 未从 `autoPairedTabs` 集合中移除，可能导致 Chrome 复用 ID 时静默跳过配对。新增 `tabs.onRemoved` 监听器清理。
- **日志明细站点名**：日志明细始终使用历史日志卡片展示；实时日志和历史日志列表均突出显示站点名。
- **孤儿组成员清理**：引用已删除渠道的组成员现在会被自动清理。

### 🚀 性能

- **Tools 探测串行执行**：新增分组成员的自动 tools 能力探测改为串行执行，每条探测间隔 5 秒，不再并行启动多个 goroutine。全局探测信号量从 4 降到 1，避免对渠道造成突发请求压力。

### 🔧 其他

- 扩展版本升级至 0.3.1，支持 URL fragment 自动配对、origin 白名单校验、`pendingUrl` 兼容。
- 一键同步 API 的 `Operation` 参数增加白名单校验（仅允许 sync / checkin）。
- `nas_origin` 在未配置 `api_base_url` 时自动从请求推导，无需手动设置。
