# Octopus v1.8.9 Release Notes

> English first, 中文见下方. — v1.8.8 → v1.8.9.

## 🐛 Fixes (v1.8.8 user feedback: sync works but check-in fails)

Diagnosis over the 2026-09-30 production run (39 check-in accounts): sync
succeeds wherever the session is valid, while check-in failures split into
five classes — site-disabled check-in (7), no usable check-in API (3: 405 or
the SPA page served for the API path), human-verification required (4:
Turnstile ×2 / PoW ×1 / signature header ×1), real Cloudflare or origin
outage (2), transient network (5, self-healed next run). Only the first two
classes are classification errors in Octopus — the site offers no check-in
benefit, so recording them as failures was wrong:

- Site-disabled check-in (fixed message「签到功能未启用」) now maps to
  **skipped** (neutral: no fail streak, neutral panel color) via an exact
  whitelist — non-whitelist business failures stay failed.
- HTTP 405 joins 404 as **skipped**, detected via the structured status code
  (not substring matching — a "balance 405" business message must not match).
- Same-day guard: check-in accounts skipped today are not re-POSTed on later
  scheduled runs the same day.
- Frontend: skipped renders neutral (idle bucket) instead of red.

Deliberately NOT changed: 200+HTML decode errors stay **failed** — a single
sample cannot distinguish a permanent SPA fallback from a transient error
page or a login redirect (invalid token); a wrong "skipped" is silent while a
wrong "failed" is visible. Human-verification / PoW / signature sites stay
failed with the site's own message (they genuinely need browser interaction).

## 🔧 Upgrade Notes

- No database migration. Rollback to v1.8.8 is a binary swap.

---

## 中文更新说明

> 自 v1.8.8 起。English above.

## 🐛 修复（v1.8.8 用户反馈：同步成功但签到失败）

对 2026-09-30 生产实跑（39 个签到账号）的诊断：会话有效即同步成功；签到失败分五类——站点关闭签到（7 个）、无可用签到接口（3 个：405 或 API 路径返回 SPA 网页）、需要人机验证（4 个：Turnstile×2 / PoW×1 / 签名头×1）、真实 Cloudflare 或源站故障（2 个）、瞬时网络（5 个，次日已自愈）。前两类是「站点侧本就没有可用的签到收益」，Octopus 把它们计为失败属于分类错误：

- 站点关闭签到（固定文案「签到功能未启用」）→ **skipped** 中性处理（不计连续失败、面板不再红），精确白名单匹配——非白名单的业务失败仍然如实显示失败。
- HTTP 405 与 404 同归 **skipped**，用结构化状态码判断（防止「余额 405」这类业务消息误命中）。
- 同日护栏：当日已被站点跳过的账号，当天后续定时轮次不再重复 POST。
- 前端：skipped 显示为中性（归入未执行桶），不再显示红色。

**刻意不改动**：200+HTML decode 错误保持 **failed**——单次样本无法区分「SPA 回退（永久）」与「瞬时错误页/登录跳转（token 失效）」；误标 skipped 是静默、误标 failed 是可见，代价不对称。需要人机验证/PoW/签名的站点保持 failed 并展示站点原始消息（它们确实需要浏览器交互）。

## 🔧 升级注意

- 无数据库迁移。回滚到 v1.8.8 仅需换回二进制。
