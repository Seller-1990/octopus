# Octopus v1.8.8 Release Notes

> English first, 中文见下方. — v1.8.7 → v1.8.8.

## ✨ Improvements (v1.8.7 user feedback)

- **Circuit breaker shows what actually broke**: each entry now carries the channel name, site / account names and key remark (resolved server-side from the channel→site binding); the table gains a "Site / Account" column and the channel filter dropdown shows names instead of bare IDs.
- **Notification history can be cleared**: the bell panel gains a "Clear all" action — a local hide-watermark hides past events while new ones arrive normally (the server ring is shared across devices; clearing is per-browser by design).
- **Group card actions are always visible**: pin/delete previously lived in a hover-revealed overlay that was effectively undiscoverable (read as "groups can't be deleted" — deletion itself always existed); the action row now sits in the card's normal flow without blocking the member list.

## 🐛 Notes

- Browser notifications on plain HTTP remain a browser hard limit (Notification is always denied outside secure contexts); the UI already explains this. To enable them, serve the panel over HTTPS or localhost.

---

## 中文更新说明

> 自 v1.8.7 起。English above.

## ✨ 改进（v1.8.7 用户反馈）

- **熔断管理显示具体站点**：每条熔断记录补充渠道名、站点/账号名与 key 备注（服务端经渠道→站点绑定批量解析）；表格新增「站点 / 账号」列，渠道筛选下拉显示名称而非裸 ID。
- **通知记录可一键清空**：铃铛面板新增「清空记录」——本地隐藏水位以下的历史事件不再展示，新事件照常到达（服务端 ring 为多端共享内存态，清空按浏览器生效）。
- **分组卡片操作常显**：置顶/删除此前藏在悬停浮现层里（等于不可发现，被理解为"无法删除"——删除功能本身一直存在）；现在操作行固定显示在卡片布局流内，不遮挡成员列表。

## 🐛 说明

- HTTP 访问下浏览器通知不可用是浏览器硬限制（非安全上下文 Notification 一律拒绝），界面已有明确提示。需要该功能请通过 HTTPS 反代或 localhost 访问面板。
