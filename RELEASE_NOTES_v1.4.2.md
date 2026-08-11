# Octopus v1.4.2 Release Notes

> English first, 中文见下方. — v1.4.1 → v1.4.2, 2 commits.

## 🐛 Fixes

- **One-click update now survives flaky proxies**: the release-binary download (60MB+) previously negotiated HTTP/2; when routed through a proxy (e.g. clash on NAS), the multiplexed stream could be interrupted (`PROTOCOL_ERROR` / `SSL_ERROR_SYSCALL`), making the one-click update hang for ~20 minutes and fail. The download now uses a dedicated HTTP/1.1 client (HTTP/2 disabled), honoring the system proxy (http/socks). Other API calls keep HTTP/2 unchanged.

## 🧹 Housekeeping

- Task-plan records for capability badges v2 and anyrouter research.

---

## 🐙 v1.4.2 发布说明（中文）

> 自 v1.4.1 起，2 个提交。English above.

## 🐛 修复

- **一键更新不再被不稳定代理打断**：release 二进制下载（60MB+）此前协商 HTTP/2；经代理（如 NAS 上的 clash）路由时，多路复用流可能中断（`PROTOCOL_ERROR` / `SSL_ERROR_SYSCALL`），导致一键更新挂起约 20 分钟并失败。现在下载改用专用 HTTP/1.1 客户端（禁用 HTTP/2），并沿用系统代理（http/socks）。其他 API 调用仍保持 HTTP/2 不变。

## 🧹 维护

- task_plan 记录能力徽标 v2 方案与 anyrouter 调研。
