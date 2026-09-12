/**
 * 实时日志概览列表的合并语义（F07）。
 *
 * 纯函数、无外部依赖：SSE 重连时客户端先重置概览列表，再逐一合并服务端
 * 内存快照事件——快照成为唯一权威。离线期间完成且被服务端完成窗口逐出的
 * 旧 running 条目因此被清除，不会长期显示幽灵 running 卡片。
 */

// live 列表在内存中按 id 降序保留固定条数：面板常被整天挂着，无上限会让
// 数万条日志对象常驻并让每条新日志触发全量排序。
export const LIVE_LOGS_MAX_ENTRIES = 500;

// 合并一条概览事件（快照或增量）：同 id 替换（running→终态迁移），
// 按 id 降序排列并截断到上限。
export function applyLiveLogEvent<T extends { id: number }>(current: T[], next: T): T[] {
    const rest = current.filter((item) => item.id !== next.id);
    const merged = [...rest, next].sort((a, b) => b.id - a.id);
    return merged.length > LIVE_LOGS_MAX_ENTRIES
        ? merged.slice(0, LIVE_LOGS_MAX_ENTRIES)
        : merged;
}
