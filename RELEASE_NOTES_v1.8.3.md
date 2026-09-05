# Octopus v1.8.3 Release Notes

> English first, 中文见下方. — v1.8.2 → v1.8.3.

## ⚠️ Breaking / Action Required

- **Verification bridge first trust is now manual (extension 0.4.0)**: a webpage can no longer establish the first pairing trust via URL fragment. First-time pairing (and re-pairing after removing all pairings, or when the server creates a *new* pairing for the same address) must be confirmed in the extension popup by entering the address and token. Automatic fragment pairing now only reconnects a saved pairing whose address (including path) **and** pairing id match exactly; everything else is rejected — rejections and reconnections are now visible at the top of the popup instead of failing silently. **Reload the unpacked extension after upgrading** (the extension self-replaces on next popup open) and remove any unrecognized saved pairings.
- **Upstream TLS certificate verification is enforced**: the Chrome/Firefox fingerprint clients no longer skip certificate and hostname verification. Self-signed or private-CA upstreams that worked silently will start failing with x509 errors — fix the upstream certificate or install the CA into the deployment trust store. There is deliberately no insecure bypass; if you are blocked by this, `OCTOPUS_PRICE_ALLOW_MISSING`-style escape hatches do not exist here by design.
- **Reverse-proxy source trust is opt-in**: `server.trusted_proxies` defaults to empty. Direct deployments need no action. If Octopus runs behind a reverse proxy, configure the proxy's real addresses/CIDRs (JSON array in config, or comma-separated `OCTOPUS_SERVER_TRUSTED_PROXIES` without spaces) — otherwise all users behind the proxy share one login budget and audit logs record the proxy IP. Invalid addresses/CIDRs **stop HTTP startup** (fail closed). A startup warning is logged when the list is empty.
- **Login budget is charged before credential verification**: 5 attempts per IP per 10 minutes (in-flight requests included); the 5th admission starts a 15-minute lockout; any admitted successful login resets the budget. State is process-local, capped at 10,000 IPs; at capacity, unknown IPs receive `429` even with correct credentials (logged at most once per minute). Direct deployments are unaffected as long as clients are not all sharing one proxy IP.
- **Price generation is fail-loud**: `scripts/updatePrice.py` no longer swallows network/JSON errors or silently generates an empty/partial price table. Upstream provider or schema drift now fails the build; if you must ship through a transient upstream outage, set `OCTOPUS_PRICE_ALLOW_MISSING=<provider>` to skip it explicitly — the generated file header records the skip, so it can't get lost.

## ✨ Highlights

- **Data-correctness batch (all previously dynamically reproduced, now fixed with red→green regressions)**:
  - *Channel refresh no longer erases pending costs* — updating a channel config used to rebuild the cache from the DB snapshot, wiping usage recorded since the last flush (cost 5 → rename → 0). Refresh now runs under the same channel lock as usage recording, reads the DB first, and keeps cached runtime fields (cost/status/last-use) as the authority, re-dirtying only real differences. A failed refresh no longer evicts the cache at all.
  - *Quota reset is now idempotent* — two requests racing on an expired quota snapshot could zero out usage already billed in the new cycle, and a stale period snapshot could overwrite an admin's period change. Reset is now conditional: it re-reads the DB under the quota lock and only resets when `reset_at` still equals the caller's snapshot; the period always comes from the DB's current value, and the middleware consumes the actual post-check state instead of assuming `used=0`.
  - *Malformed upstream responses no longer trigger full model removal* — a 200 response with an empty body, `{}`, `null`, or an error object used to decode into an empty model list, and the AutoSync task would delete every model, route, and zero-price entry for the channel. The protocol boundary now rejects all of these; an explicit `data: []` decodes fine, and the sync task logs a warning instead of removing anything when the list is empty (partial model removal with a non-empty list is unaffected).
- **Price pipeline hardening**: explicit prices (including free models) are distinguished from missing fields; if fewer than 50% of generated entries carry an explicit price (upstream schema drift such as a renamed `cost` field), generation fails instead of publishing an all-zero table. Model ids/aliases are whitelisted before being written into generated Go source (quote/brace/backslash injection rejected); deduplication now works on raw ids so colon-style names (`model:1` / `model:2`) no longer collapse; `presets.go` is written atomically (tmp + rename).

## 🐛 Fixes

- **Verification bridge hardening**: the automatic pairing entry no longer accepts a path-carrying variant of a trusted address (which could redirect `identify` and subsequent task traffic through an attacker-controlled subpath), and no longer creates records for a different pairing id on an already-trusted address (another account's token on the same server could silently inject itself). The two branches were previously uncovered because the test mock hardcoded `pairing:{id:1}` — the mock now scopes custom responses to the fragment token.
- **Login throttle audit logging**: capacity rejections are logged at most once per sweep interval with a running drop counter (previously an attacker could scale 429 audit lines with attack rate at ~17 req/s); lockout establishment logs once per lockout.
- **Dev build versioning**: `build.sh` derived dev versions from a hardcoded `v1.1.0-dev+<hash>` fallback; it now derives from the latest tag (`v1.8.3-dev+<hash>`), so dev deployments no longer report stale versions.
- **CI gates**: extension Node tests, Python price-script tests, and `-race` on the server packages are now part of CI (previously all of the security work above shipped with its regression evidence outside the pipeline).

## 🔧 Upgrade Notes

- **No database migrations in this release** — rollback to v1.8.2 is schema-safe. Take your usual data backup anyway.
- **Docker**: after pulling the new image, make sure `/app/data/octopus-updated` does not exist in the data volume (a stale self-update binary silently shadows the image binary, on upgrade and rollback alike) and verify the version on the Settings → info panel.
- All 9 tests added for the data-correctness batch reproduce the original findings first (red) and pass after the fix (green); full suite, `-race` on op/server/helper, extension tests, and Python tests are green.

---

## 中文更新说明

> 自 v1.8.2 起。English above.

### ⚠️ 破坏性 / 需要操作

- **验证桥首次信任改为手动(扩展 0.4.0)**:网页 fragment 不再能建立首次信任;首次配对、清空后重配、同地址换号配对都必须在扩展 popup 手动确认。自动配对只重连「地址(含路径)与配对 id 都完全一致」的已存配对,其余一律拒绝,拒绝/重连结果在 popup 顶部可见。**升级后请在扩展管理页重新加载扩展**,并删除不认识的历史配对。
- **上游 TLS 证书验证强制开启**:指纹客户端不再跳过证书与主机名校验,自签名/私有 CA 上游将开始报 x509 错误——修复上游证书或将 CA 装入部署环境信任库,无 insecure 逃生开关(有意设计)。
- **反代来源信任默认关闭**:`server.trusted_proxies` 默认为空。直连部署无需操作;反代部署必须配置真实代理地址/CIDR,否则代理后所有用户共享一个登录预算、审计日志记录的是代理 IP。非法 CIDR 会阻止启动(fail closed),空名单启动时有告警日志。
- **登录预算改为校验前预扣**:每 IP 十分钟 5 次(含在途),第五次准入锁定 15 分钟,已准入成功登录清空预算;进程内存储,上限 10,000 IP,容量满时未知 IP 即使密码正确也收 429(每分钟最多记一条审计日志)。
- **价格生成 fail-loud**:网络/JSON/上游名单与 schema 漂移都会让构建失败;确需跳过缺失厂商时用 `OCTOPUS_PRICE_ALLOW_MISSING=<provider>` 显式逃生舱,产物头部会留痕。

### ✨ 修复

- **数据正确性批次(均先红测复现再修)**:渠道配置刷新不再抹掉未落库成本(记 5 → 改名 → 归零的交错已闭合);配额重置幂等(并发双请求不再清零新周期用量,旧快照不再覆盖管理员周期设置);上游畸形 200 响应(空 body/`{}`/`null`/错误对象)不再被当作权威空目录撤掉全部模型与路由,明确空数组仅告警不删除。
- **价格管线加固**:显式价格与字段缺失可区分(有价占比 <50% 判定 schema 漂移并拒绝生成);model id 白名单防注入;冒号命名不再被去重吞并;presets.go 原子写入。
- **验证桥收紧**:已信任地址的带路径变体与不同 pairing id 注入路径关闭;429 审计日志全局限频;dev 构建版本号改为基于最近 tag 自动派生(不再显示过时的 v1.1.0);扩展测试/Python 测试/server 包 race 检测接入 CI。

### 🔧 升级说明

- 本次无数据库迁移,回滚 v1.8.2 schema 安全;升级前照常备份数据。
- Docker 拉新镜像后,确认数据卷内不存在 `octopus-updated` 旧二进制(它会静默遮蔽镜像内的新版本),并在设置页核对版本号。
