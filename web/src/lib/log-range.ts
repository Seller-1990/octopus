import dayjs from 'dayjs';
import timezonePlugin from 'dayjs/plugin/timezone';
import utc from 'dayjs/plugin/utc';

dayjs.extend(utc);
dayjs.extend(timezonePlugin);

export type LogDatePreset = 'today' | '7d' | '30d' | 'month';

/**
 * 日志日期范围的两种语义：
 * - live：滚动谓词（今天/近 7 天/近 30 天/本月）。只存预设名，不存时间戳，
 *   每次查询时按当前时刻重新求值，保证页面挂着不动时新日志始终落在窗口内。
 *   此前把 end 冻结在"打开页面/点击预设的时刻"并持久化，导致后端
 *   time <= end_time 过滤掉所有新日志，轮询与手动刷新都查同一个死窗口。
 * - fixed：用户显式选定的历史区间，窗口冻结（含"昨天"这类天然完整的历史时段）。
 */
export type LogDateRange =
    | { mode: 'live'; preset: LogDatePreset }
    | { mode: 'fixed'; start?: number; end?: number };

export const DEFAULT_LOG_DATE_RANGE: LogDateRange = { mode: 'live', preset: 'today' };

// live 窗口的查询上界缓冲：客户端时钟可能落后服务端，缓冲不足会漏掉刚写入的日志。
// 明细与分析查询共用同一缓冲，避免"明细有、统计没有"的窗口口径分裂。
export const LIVE_WINDOW_CLOCK_BUFFER_SECONDS = 60;

const LIVE_PRESETS: readonly LogDatePreset[] = ['today', '7d', '30d', 'month'];

export function isLogDateRange(value: unknown): value is LogDateRange {
    if (!value || typeof value !== 'object') return false;
    const v = value as { mode?: unknown; preset?: unknown; start?: unknown; end?: unknown };
    if (v.mode === 'live') {
        return typeof v.preset === 'string' && LIVE_PRESETS.includes(v.preset as LogDatePreset);
    }
    if (v.mode === 'fixed') {
        // 后端支持单边过滤，fixed 至少要有一个端点（用户可能只选开始或只选结束）
        return typeof v.start === 'number' || typeof v.end === 'number';
    }
    return false;
}

/** 求值语义时间窗口（秒级 unix）。fixed 原样返回；live 按当前时刻计算。 */
export function resolveLogDateRange(
    range: LogDateRange | undefined,
    timezone: string | undefined,
): { start?: number; end?: number } {
    if (range && range.mode === 'fixed') return { start: range.start, end: range.end };
    const now = dayjs().tz(timezone || 'UTC');
    switch (range && range.mode === 'live' ? range.preset : 'today') {
        case '7d':
            return { start: now.subtract(6, 'day').startOf('day').unix(), end: now.unix() };
        case '30d':
            return { start: now.subtract(29, 'day').startOf('day').unix(), end: now.unix() };
        case 'month':
            return { start: now.startOf('month').unix(), end: now.unix() };
        default:
            return { start: now.startOf('day').unix(), end: now.unix() };
    }
}
