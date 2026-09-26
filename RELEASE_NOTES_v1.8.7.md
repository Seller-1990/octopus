# Octopus v1.8.7 Release Notes

> English first, 中文见下方. — v1.8.6 → v1.8.7.

## 🐛 Fixes (v1.8.6 user feedback)

- **Notification bell**: "Browser notifications" click now explains itself — on HTTP (non-localhost) the Notification API is always denied; the bell shows an explicit toast instead of failing silently, and the settings page treats insecure contexts as unsupported. "Mark all read" is meaningful again (the auto-mark-on-open behavior was removed; clicking now visibly clears the badge and row highlights).
- **Price compare panel**: scrolling restored (dedicated scroll region matching the catalog/discovery structure — content is no longer clipped), and the 9-column table scrolls horizontally instead of being cut off.
- **Group vendor filter**: models that match no known vendor (e.g. `jev-1.3.0`) now fall into an "Other" bucket (sorted last) so every group is reachable from the vendor chips.

## 🔧 Upgrade Notes

- No database migration. Rollback to v1.8.6 is a binary swap.

---

## 中文更新说明

> 自 v1.8.6 起。English above.

## 🐛 修复（v1.8.6 用户反馈）

- **通知铃铛**：「浏览器通知」在 HTTP（非 localhost）访问下原本被静默吞掉——现在明确 toast 提示需要 HTTPS 或 localhost；设置页同口径按不支持处理。「全部已读」恢复生效（移除打开面板即自动已读的行为，点击后徽章消失、行高亮解除）。
- **价格对比页**：滚动恢复（与目录/发现页同构的专属滚动区，内容不再被裁切）；9 列表格横向可滚，不再被切掉。
- **分组厂商过滤**：无法归入已知厂商的模型（如 `jev-1.3.0`）归入固定排最后的「Other」桶，所有分组都能被厂商 chip 选中。

## 🔧 升级注意

- 无数据库迁移。回滚到 v1.8.6 仅需换回二进制。
