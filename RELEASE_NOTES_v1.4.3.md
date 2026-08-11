# Octopus v1.4.3 Release Notes

> English first, 中文见下方. — v1.4.2 → v1.4.3.

## 🐛 Fixes

- **Model capability badges now populate**: the models.dev fetch (capability + price index) previously forced a proxy — environments without `proxy_url` config (where direct connection works) failed with "proxy url is empty", leaving capability badges permanently empty. Now it uses the proxy only when configured, otherwise falls back to direct. (Also fixes NAS where direct times out but proxy reaches models.dev.)
- **Migration 027 no longer freezes multimodal correction**: rows with only a reasoning-suffix name (e.g. `gemini-2.5-flash-thinking`) were written `vision_capable=false`, blocking models.dev from later correcting the multimodal bit (migration only writes `true`, never `false`).
- **`make build` now injects the backend version** (was hard-coded default, causing frontend/backend version mismatch and a spurious "cache mismatch" banner).
- **One-click update over flaky proxies** (from v1.4.2, kept): release download uses HTTP/1.1; version check keeps HTTP/2.

## ✨ Features

- **Inline API key toggles on the card**: enable/disable and tools-only switches directly on each key card (no need to open the edit dialog). Backend preserves the API secret and quota state on partial updates.

## 🧹 Housekeeping

- (records)

---

## 🐙 v1.4.3 发布说明（中文）

> 自 v1.4.2 起。English above.

## 🐛 修复

- **模型能力徽标现在能正常填充**：models.dev 拉取（能力 + 价格索引）此前强制走代理——未配置 `proxy_url` 的环境（直连可用）会报 "proxy url is empty" 失败，能力徽标永久为空。现改为：配置了代理才走代理，否则回退直连。（同时修复 NAS 直连超时但代理可达的场景。）
- **迁移 027 不再冻结多模态纠错**：仅名字含推理后缀的行（如 `gemini-2.5-flash-thinking`）此前被写入 `vision_capable=false`，阻塞 models.dev 后续纠正多模态位（迁移只写 `true`、从不写 `false`）。
- **`make build` 现在注入后端版本**（此前停在硬编码默认值，导致前后端版本不一致与误报「缓存不一致」横幅）。
- **一键更新经不稳定代理**（自 v1.4.2 保留）：release 下载用 HTTP/1.1；版本检查保留 HTTP/2。

## ✨ 新功能

- **API Key 卡片内联开关**：每个 Key 卡片直接可切换启用/禁用与仅 tools（无需进编辑弹窗）。后端在部分更新时保留密钥与配额状态。

## 🧹 维护

- （记录）
