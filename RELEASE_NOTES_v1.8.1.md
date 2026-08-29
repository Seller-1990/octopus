# Octopus v1.8.1 Release Notes

> English first, 中文见下方. — v1.7.1 → v1.8.1 (includes v1.8.0).

## ✨ Highlights

- **Log detail is now push-driven real-time**: the log detail list subscribes to the server-sent event stream — a log shows up within milliseconds of being written. Polling is demoted to a fallback (30 s while the stream is connected, 5 s after disconnection), and a "live stream" freshness badge replaces the fixed 5-second promise.
- **Root-cause fix for "new logs never appear"**: the date-range `end` used to freeze at the moment the page was opened (and at every preset click), so backend `time <= end_time` filtering silently excluded every new log — polling and manual refresh both queried a dead window. Today/7d/30d/month presets are now rolling predicates evaluated per query; only explicitly picked historical ranges stay frozen.
- **Check-in no longer flips to failures after a restart**: the check-in task runs on startup (`runOnStart`), and accounts without random scheduling were re-checked unconditionally — duplicate requests rejected by sites overwrote the day's status into `failed` (e.g. 1 failure in the morning, 11 after an update). A same-day guard now skips already-successful accounts on scheduled/startup runs (manual "check in all" still forces a re-check), and "already checked in" response matching covers more variants.

## 🐛 Fixes

- **Log detail real-time chain**: rolling-predicate date presets with a 60 s client-clock buffer; head + pages query split so polling only refreshes the latest page (no full-page re-fetch storms, no cursor-gap drift); automatic rebuild when the head page and cached pages stop overlapping (burst traffic) or when a rolling window crosses midnight; frozen-view banner ("N new logs — click to view") so prepend no longer yanks the reading position; manual refresh rebuilds the page cache instead of re-fetching every loaded page.
- **Live log panel wired up**: the previously dead SSE code (overview stream, attempt-level details, stop-attempt button) is now reachable as the third "Live" tab on the log page.
- **Check-in status integrity**: same-day already-succeeded guard for scheduled/startup check-in runs (new skip reason `already_checked_in_today`); extended already-checked-in message matching (repeat/duplicate/今日已签/请勿重复 etc.); Go test suite green.
- **CI frontend lint**: refs are no longer written during render (stream callback/log-list/head-refetch refs sync via effects); hook dependency warnings on the log page resolved.

## 🚀 UX Improvements

- **Navigation slimmed from 10 to 8 items**: the Circuit page moved out of the nav (entry point in Settings → Reliability, plus a home-page banner that only appears when channels are actually tripped); the Vision Bridge page became a card inside Settings (it was already a settings form wearing a full-page costume).
- **Data freshness badge**: "data as of HH:MM:SS · live stream / auto-refreshing" with a degraded badge when auto-refresh fails.
- **Filter hygiene**: the filter badge now counts every active filter (date/channel/keyword mode + the six business dimensions); one "clear all" that really clears everything; log-page keywords persist across reloads (no more "mode remembered, term forgotten" ghost filtering); drilldown shows removable filter chips and no longer silently clears the account filter.
- **API key card**: the enable / tools-only / vision-bridge switches moved to a dedicated row at the card bottom; the action buttons stand alone on the right.
- **Site rows**: metric cluster and controls separated by a divider; the auto-sync / relay / enable switches now carry short text labels.
- **Global layout**: content width capped at 1440 px (was 1600 px); log cards drop the meaningless "0ms /" prefix when first-token time is unknown; the home activity heatmap mutes empty cells.
- **Misc**: manual refresh no longer fakes a 500 ms spinner; "load more" failures offer a retry button; empty states explain the current range; "Canonical Model" unified to 规范模型 across locales.

---

## 中文更新说明

> 自 v1.7.1 起（包含 v1.8.0）。English above.

## ✨ 亮点

- **日志明细升级为推送式实时**：明细列表订阅服务端 SSE 事件流，日志写入后毫秒级出现。轮询降级为兜底（连接在线 30 秒一次，断线后 5 秒一次），新鲜度标识会显示「实时推送中」。
- **"新日志永远不出现"的根因修复**：日期范围的 end 此前冻结在页面打开时刻（以及每次点击预设按钮的时刻），后端 `time <= end_time` 过滤把所有新日志挡在窗外——轮询与手动刷新查的都是死窗口。今天/近 7 天/近 30 天/本月预设改为每次查询时求值的滚动谓词，只有显式选定的历史区间才冻结。
- **重启后签到失败数突变修复**：签到任务 `runOnStart=true` 会在启动时立即重跑，无随机调度的账号被无条件重签——重复请求被站点拒绝后把当天状态覆写为 failed（早上 1 个失败、更新后 11 个）。现在计划/启动触发的签到会跳过当天已成功的账号（手动"全量签到"仍可强制重签），"已签到"响应识别也覆盖了更多变体。

## 🐛 修复

- **日志明细实时链路**：滚动谓词日期预设（含 60 秒时钟缓冲）；head + pages 查询分离，轮询只刷最新一页（不再全页重取风暴、不再游标错位缺缝）；头部与缓存页无重叠（突发流量）或滚动窗口跨天时自动重建；冻结视图 + "有 N 条新日志"横幅，顶部插入不再拽走阅读位置；手动刷新改为重建分页缓存。
- **实时日志面板接线**：此前 190 行从未接线的 SSE 代码（概览流、尝试级详情、停止尝试按钮）以日志页第三个「实时」tab 的形式启用。
- **签到状态完整性**：计划/启动触发的签到跳过当天已成功账号（新跳过原因 `already_checked_in_today`）；已签到消息识别扩展（repeat/duplicate/今日已签/请勿重复等）；Go 测试全绿。
- **CI 前端 lint**：渲染期不再写 ref（事件回调/日志列表/头部刷新 ref 均改为 effect 同步）；日志页 hook 依赖告警清零。

## 🚀 体验优化

- **导航从 10 项精简到 8 项**：熔断页移出主导航（设置 → 可靠性有入口，首页仅在实际发生熔断时显示告警条）；视觉桥独立页迁移为设置页卡片（它本来就是设置表单的形态）。
- **数据新鲜度标识**：「数据截至 HH:MM:SS · 实时推送中/自动刷新中」，自动刷新失败时显示降级徽标。
- **筛选卫生**：筛选角标统计全部生效筛选（日期/渠道/关键词 + 六个业务维度）；一个真正清全部的"清除"按钮；日志页关键词跨刷新持久化（不再出现"模式在、词不在"的幻影筛选）；下钻显示可移除的筛选 chips，不再静默清空账号筛选。
- **密钥卡片**：启用/仅 tools/视觉桥三个开关移到卡片底部独立一行；右侧只留操作按钮组。
- **站点行**：信息与操作区之间加分隔线；同步/中转/启用开关补上简短文字标签。
- **全局布局**：内容宽度上限 1600px → 1440px；日志卡在无首字时间时不再显示无意义的 "0ms /" 前缀；首页活动热力图空格子降噪。
- **杂项**：手动刷新不再假装转 500ms 圈；加载更多失败提供重试按钮；空态说明当前时间范围；三语言包统一「规范模型」术语。
