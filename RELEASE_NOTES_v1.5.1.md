# Octopus v1.5.1 Release Notes

> English first, 中文见下方. — v1.5.0 → v1.5.1.

## ✨ Features

- **Account-level Cloudflare Cookie support**: site accounts can now carry Cloudflare clearance cookies per account, so syncing and check-in keep working for upstream sites behind Cloudflare protection.
- **TLS fingerprint & UA disguise presets**: the account edit page supports TLS fingerprint and User-Agent disguise presets, and channels gain TLS fingerprint disguise — making upstream requests look like normal browser traffic.
- **UA disguise presets + external check-in URL wiring + check-in history**: site/channel management now wires external check-in URLs and keeps a visible check-in history.
- **Real-time request logs**: live relay request logs are available in the management panel.

## 🐛 Fixes

Code-review driven fixes (2026-08-21 review round):

- **sub2api token refresh can no longer get stuck permanently**: if the refresh leader panics, the shared call entry is now always cleaned up and waiters are released instead of timing out on every later refresh.
- **HTTP error paths close response bodies**: `sitesync` now closes the response body even when `http.Client.Do` returns both a response and a transport error, avoiding file-descriptor leaks in long-running syncs.
- **First-token timeout uses `errors.Is`**: the stream processor now wraps the shared sentinel error and the relay layer detects it via `errors.Is` instead of string matching.
- **Router rejects unknown HTTP methods at startup**: a typo like `"PTACH"` now fails route registration immediately instead of silently exposing the endpoint as `GET`.
- **Text deltas append instead of overwrite**: `InternalResponseFromStreamEvents` now appends repeated `TextDelta`/`Refusal` deltas for the same choice, matching the stream aggregator semantics.
- **Responses stream always terminates with `[DONE]`**: the event-based path now appends `data: [DONE]` even when earlier events are already buffered.

## 🧹 Housekeeping

- Full `go test ./...` and `go vet ./...` pass; affected packages pass `go test -race`.

---

## 🐙 v1.5.1 发布说明（中文）

> 自 v1.5.0 起。English above.

## ✨ 新功能

- **账号级 Cloudflare Cookie 支持**：站点账号可独立配置 Cloudflare 过盾 Cookie，使同步与签到在 Cloudflare 防护的上游站点保持可用。
- **TLS 指纹与 UA 伪装预设**：账号编辑页支持 TLS 指纹与 UA 伪装预设；渠道侧接入 TLS 指纹伪装，让上游请求更接近真实浏览器流量。
- **UA 伪装预设 + 外部签到 URL 接线 + 签到历史**：站点/渠道管理接入外部签到 URL，并提供签到历史查看。
- **实时请求日志**：管理面板支持实时查看请求日志。

## 🐛 修复

2026-08-21 代码审查轮次驱动修复：

- **sub2api token 刷新不再可能永久卡死**：刷新 leader 发生 panic 时，共享调用条目保证清理并释放等待方，后续刷新不再拖满超时。
- **HTTP 错误路径关闭响应体**：`sitesync` 在 `http.Client.Do` 同时返回响应与传输错误时也关闭响应体，避免长跑同步的文件描述符泄漏。
- **首字超时改用 `errors.Is`**：流处理器包装共享哨兵错误，relay 层用 `errors.Is` 识别，替代脆弱的字符串匹配。
- **路由注册拒绝未知 HTTP method**：`"PTACH"` 之类的拼写错误现在启动期直接报错，不再静默退化为 `GET` 暴露。
- **文本增量改为追加**：`InternalResponseFromStreamEvents` 对同 choice 的重复 `TextDelta`/`Refusal` 改为追加，与流聚合器语义一致。
- **Responses 流始终以 `[DONE]` 收尾**：事件路径在已有缓冲事件时也追加 `data: [DONE]`。

## 🧹 杂项

- `go test ./...` 与 `go vet ./...` 全量通过；受影响包通过 `go test -race`。
