import { keepPreviousData, useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient, API_BASE_URL } from '../client';
import { logger } from '@/lib/logger';
import { LIVE_WINDOW_CLOCK_BUFFER_SECONDS, resolveLogDateRange } from '@/lib/log-range';
import { useCallback, useEffect, useMemo, useState } from 'react';

/**
 * 尝试状态
 */
export type AttemptStatus = 'success' | 'failed' | 'client_canceled' | 'circuit_break' | 'skipped';
export type RequestOutcome = 'success' | 'failed' | 'client_canceled' | 'indeterminate';

export type RelayLogWSMode = 'fresh' | 'continuation' | 'replay';

export type RelayLogWSExecMode = 'passthrough' | 'transform';

export type RelayLogWSRecovery = 'reconnect' | 'replay' | 'downgrade';

/**
 * 单次渠道尝试信息
 */
export interface ChannelAttempt {
    channel_id: number;
    channel_key_id?: number;
    channel_key_remark?: string;
    channel_name: string;
    model_name: string;
    attempt_num: number;    // 第几次尝试
    status: AttemptStatus;
    status_code?: number;
    outcome?: RequestOutcome;
    attribution?: 'upstream' | 'client' | 'relay' | 'policy' | '';
    completion_evidence?: string;
    duration: number;       // 耗时(毫秒)
    sticky?: boolean;
    msg?: string;
    /** 后端AttemptUsageSnapshot的轻量投影：这里只消费倍率字段 */
    usage?: {
        price_multiplier?: number;
    } | null;
}

/**
 * 日志数据
 */
export interface LogSiteActionTarget {
    site_id: number;
    site_name: string;
    account_id: number;
    account_name: string;
    group_key: string;
    group_name: string;
    model_name: string;
    model_disabled: boolean;
    can_disable_model: boolean;
    channel_id: number;
    channel_name: string;
}

export interface LogSiteActionTargets {
    attempt_targets: Array<LogSiteActionTarget | null>;
    legacy_error_target?: LogSiteActionTarget | null;
}

export interface RelayLog {
    id: number;
    time: number;                // 时间戳
    request_model_name: string;  // 请求模型名称
    request_api_key_id?: number;
    request_api_key_name?: string; // 请求使用的 API Key 名称
    channel: number;             // 实际使用的渠道ID
    channel_name: string;        // 渠道名称
    actual_model_name: string;   // 实际使用模型名称
    canonical_model_name?: string;
    route_candidate_id?: number;
    inbound_protocol?: string;
    outbound_protocol?: string;
    protocol_mode?: string;
    protocol_policy?: string;
    protocol_allow_lossy?: boolean;
    protocol_warnings?: string[];
    protocol_failure_stage?: string;
    input_tokens: number;        // 输入Token
    transport_input_tokens?: number | null; // 实际发送到上游请求体的 Token 估算
    bill_input_tokens?: number | null; // 按常规输入价格计费的 Token
    cache_read_tokens?: number | null; // 从缓存读取的 Token
    cache_write_tokens?: number | null; // 写入缓存的 Token
    output_tokens: number;       // 输出Token
    ftut: number;                // 首字时间(毫秒)
    use_time: number;            // 总用时(毫秒)
    cost: number;                // 消耗费用
    price_quote_id?: number;
    price_source?: string;
    price_unit?: string;
    price_currency?: string;
    price_input?: number;
    price_output?: number;
    price_cache_read?: number;
    price_cache_write?: number;
    price_per_request?: number;
    price_group_multiplier?: number;
    price_exchange_rate_usd?: number;
    price_observed_at?: string | null;
    price_stale?: boolean;
    price_convertible?: boolean;
    price_original_cost?: number;
    price_match_reason?: string;
    token_source?: 'reported' | 'estimated' | 'unknown';
    request_content: string;     // 请求内容
    response_content: string;    // 响应内容
    error: string;               // 错误信息
    success?: boolean;
    outcome?: RequestOutcome;
    transport_termination?: string;
    completion_evidence?: string;
    terminal_event?: string;
    header_policy_trace?: string;
    attempts?: ChannelAttempt[]; // 所有尝试记录
    total_attempts?: number;     // 总尝试次数
    used_ws?: boolean;           // 是否使用了上游WebSocket
    ws_mode?: RelayLogWSMode | null; // 上游 WebSocket 会话模式
    ws_exec_mode?: RelayLogWSExecMode | null; // 上游 WebSocket 事件处理方式
    ws_recovery?: RelayLogWSRecovery | null; // 本次请求触发的恢复动作
}

export type LogStatusFilter = 'all' | 'success' | 'failed' | 'error' | 'client_canceled' | 'indeterminate';

/**
 * 日志列表查询参数
 */
export type LogKeywordScope = 'default' | 'content';
export type LogKeywordMode = 'default' | 'prefix' | 'exact' | 'contains';
export type LogPaginationMode = 'cursor' | 'page';

export interface LogCursor {
    time: number;
    id: number;
}

export interface LogListParams {
    page?: number;
    page_size?: number;
    limit?: number;
    before_time?: number;
    before_id?: number;
    start_time?: number;
    end_time?: number;
    channel_ids?: number[];
    site_ids?: number[];
    site_account_ids?: number[];
    api_key_ids?: number[];
    request_models?: string[];
    actual_models?: string[];
    canonical_models?: string[];
    status?: LogStatusFilter;
    keyword?: string;
    keyword_scope?: LogKeywordScope;
    keyword_mode?: LogKeywordMode;
    pagination?: LogPaginationMode;
    include_content?: boolean;
    with_total?: boolean;
    enabled?: boolean;
}

export interface UseLogsOptions {
    pageSize?: number;
    filters?: Omit<LogListParams, 'page' | 'page_size' | 'start_time' | 'end_time'>;
    /** 时间窗口：live 为滚动谓词（查询时求值），fixed 为冻结区间；缺省不限时间 */
    range?: LogRangeQuery;
    /** live 窗口求值所用时区（"今天零点"等按此时区切分） */
    rangeTimezone?: string;
    enabled?: boolean;
    /** live 窗口下头部轮询间隔（毫秒）。fixed 窗口恒不轮询。缺省不轮询。 */
    refetchInterval?: number | false;
}

/**
 * 时间窗口的两种语义（与 lib/log-range 的 LogDateRange 结构兼容）：
 * - live：每次请求时求值当前窗口，新日志永远落在窗口内；
 * - fixed：冻结区间（用户显式选定的历史范围）。
 */
export type LogRangeQuery =
    | { mode: 'live'; preset: 'today' | '7d' | '30d' | 'month' }
    | { mode: 'fixed'; start?: number; end?: number };

// live 窗口的查询上界缓冲改由 lib/log-range 统一提供（分析查询共用同一口径）。
const LIVE_CLOCK_BUFFER_SECONDS = LIVE_WINDOW_CLOCK_BUFFER_SECONDS;
void LIVE_CLOCK_BUFFER_SECONDS;

const logFiltersKey = (filters?: UseLogsOptions['filters']) => ({
    channel_ids: filters?.channel_ids?.filter((id) => id > 0).sort((a, b) => a - b) ?? [],
    site_ids: filters?.site_ids?.filter((id) => id > 0).sort((a, b) => a - b) ?? [],
    site_account_ids: filters?.site_account_ids?.filter((id) => id > 0).sort((a, b) => a - b) ?? [],
    api_key_ids: filters?.api_key_ids?.filter((id) => id > 0).sort((a, b) => a - b) ?? [],
    request_models: [...(filters?.request_models ?? [])].sort(),
    actual_models: [...(filters?.actual_models ?? [])].sort(),
    canonical_models: [...(filters?.canonical_models ?? [])].sort(),
    status: filters?.status && filters.status !== 'all' ? filters.status : 'all',
    keyword: filters?.keyword?.trim() ?? '',
    keyword_scope: filters?.keyword_scope ?? 'default',
    keyword_mode: filters?.keyword_mode ?? 'default',
});

const logRangeKey = (range?: LogRangeQuery, timezone?: string) => {
    if (!range || range.mode === 'fixed') {
        return ['fixed', range?.start ?? null, range?.end ?? null] as const;
    }
    return ['live', range.preset, timezone ?? null] as const;
};

function appendLogListParams(params: URLSearchParams, filters?: UseLogsOptions['filters']) {
    const channelIds = filters?.channel_ids?.filter((id) => id > 0) ?? [];
    if (channelIds.length > 0) params.set('channel_ids', channelIds.join(','));
    const siteIds = filters?.site_ids?.filter((id) => id > 0) ?? [];
    if (siteIds.length > 0) params.set('site_ids', siteIds.join(','));
    const accountIds = filters?.site_account_ids?.filter((id) => id > 0) ?? [];
    if (accountIds.length > 0) params.set('site_account_ids', accountIds.join(','));
    const apiKeyIds = filters?.api_key_ids?.filter((id) => id > 0) ?? [];
    if (apiKeyIds.length > 0) params.set('api_key_ids', apiKeyIds.join(','));
    if (filters?.request_models?.length) params.set('request_models', filters.request_models.join(','));
    if (filters?.actual_models?.length) params.set('actual_models', filters.actual_models.join(','));
    if (filters?.canonical_models?.length) params.set('canonical_models', filters.canonical_models.join(','));
    if (filters?.status && filters.status !== 'all') params.set('status', filters.status);
    const keyword = filters?.keyword?.trim();
    if (keyword) params.set('keyword', keyword);
    if (filters?.keyword_scope && filters.keyword_scope !== 'default') params.set('keyword_scope', filters.keyword_scope);
    if (filters?.keyword_mode && filters.keyword_mode !== 'default') params.set('keyword_mode', filters.keyword_mode);
}

/** 时间窗口入参：live 在请求时求值（end 额外加时钟缓冲），fixed 原样使用。 */
function appendRangeParams(params: URLSearchParams, range?: LogRangeQuery, timezone?: string) {
    const resolved = resolveLogDateRange(range, timezone);
    const endTime = range?.mode === 'live' && resolved.end != null
        ? resolved.end + LIVE_WINDOW_CLOCK_BUFFER_SECONDS
        : resolved.end;
    if (resolved.start) params.set('start_time', String(resolved.start));
    if (endTime) params.set('end_time', String(endTime));
}

/**
 * 清空日志 Hook
 *
 * @example
 * const clearLogs = useClearLogs();
 *
 * clearLogs.mutate();
 */
export async function getLogDetail(id: number): Promise<RelayLog> {
    return apiClient.get<RelayLog>(`/api/v1/log/${id}`);
}

export function useLogSiteActionTargets(ids: number[], enabled = true) {
    const stableIds = useMemo(() => Array.from(new Set(ids.filter((id) => id > 0))).sort((a, b) => a - b), [ids]);
    return useQuery({
        queryKey: ['logs', 'site-action-targets', stableIds],
        queryFn: async () => {
            if (stableIds.length === 0) return {} as Record<number, LogSiteActionTargets>;
            const chunkSize = 100;
            const chunks: number[][] = [];
            for (let i = 0; i < stableIds.length; i += chunkSize) {
                chunks.push(stableIds.slice(i, i + chunkSize));
            }
            const results = await Promise.all(
                chunks.map((chunk) =>
                    apiClient.get<Record<number, LogSiteActionTargets>>(
                        `/api/v1/log/site-action-targets?ids=${chunk.join(',')}`,
                    ),
                ),
            );
            return Object.assign({}, ...results) as Record<number, LogSiteActionTargets>;
        },
        enabled: enabled && stableIds.length > 0,
        staleTime: 30000,
        refetchOnWindowFocus: false,
    });
}

export function useClearLogs() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async () => {
            return apiClient.delete<null>('/api/v1/log/clear');
        },
        onSuccess: () => {
            logger.log('日志清空成功');
            // 不整体 invalidate ['logs']：那会让 infinite query 按序重取所有已加载页。
            // 分页缓存直接移除（挂载中的 observer 会自动重取第一页），其余按需失效。
            queryClient.removeQueries({ queryKey: ['logs', 'infinite'] });
            queryClient.invalidateQueries({ queryKey: ['logs', 'head'] });
            queryClient.invalidateQueries({ queryKey: ['logs', 'analytics'] });
            queryClient.invalidateQueries({ queryKey: ['logs', 'site-action-targets'] });
        },
        onError: (error) => {
            logger.error('日志清空失败:', error);
        },
    });
}

/**
 * 日志管理 Hook。
 *
 * 查询架构（head + pages 分离）：
 * - head：独立轮询最新一页（live 窗口下每 refetchInterval 毫秒一次，且后台 tab 不停），
 *   实时性由它承担。固定窗口不轮询（历史报表语义，无需实时）。
 * - pages：游标分页历史，不自动轮询。v5 的 infinite refetch 会按序重取所有已加载页，
 *   深翻页时轮询会放大成串行请求风暴，且 page 1 重取后与后续页的游标错位会产生缺缝；
 *   让轮询只打 head（恒定 1 个请求）可以从根上避开这两个问题。
 * - 合并：pages 先填、head 后写覆盖（运行中请求完成后，attempt/价格字段以 head 快照为准）。
 */
export function useLogs(options: UseLogsOptions = {}) {
    const { pageSize = 20, filters, range, rangeTimezone, enabled = true, refetchInterval } = options;

    const queryClient = useQueryClient();

    type CursorPage = { logs: RelayLog[]; next_cursor?: LogCursor | null; has_more: boolean; warning?: string; search_mode?: string };

    const baseFiltersKey = useMemo(() => logFiltersKey(filters), [filters]);
    const rangeKey = useMemo(() => logRangeKey(range, rangeTimezone), [range, rangeTimezone]);
    const headQueryKey = useMemo(
        () => ['logs', 'head', pageSize, baseFiltersKey, rangeKey] as const,
        [pageSize, baseFiltersKey, rangeKey],
    );
    const infiniteQueryKey = useMemo(
        () => ['logs', 'infinite', pageSize, baseFiltersKey, rangeKey] as const,
        [pageSize, baseFiltersKey, rangeKey],
    );

    const headQuery = useQuery({
        queryKey: headQueryKey,
        queryFn: async () => {
            const params = new URLSearchParams();
            params.set('limit', String(pageSize));
            params.set('with_total', 'false');
            params.set('include_content', 'false');
            params.set('pagination', 'cursor');
            appendLogListParams(params, filters);
            appendRangeParams(params, range, rangeTimezone);
            const result = await apiClient.get<{ logs: RelayLog[] | null; has_more?: boolean; warning?: string; search_mode?: string } | null>(
                `/api/v1/log/list?${params.toString()}`,
            );
            return {
                logs: result?.logs ?? [],
                warning: result?.warning,
                search_mode: result?.search_mode,
            } satisfies Pick<CursorPage, 'logs' | 'warning' | 'search_mode'>;
        },
        enabled,
        staleTime: 0,
        refetchOnMount: 'always',
        refetchOnWindowFocus: false,
        // 固定窗口是历史报表语义，不轮询；live 窗口按传入间隔轮询且后台 tab 不停
        //（监控面板挂着就是要在后台持续收新日志）。
        refetchInterval: range?.mode === 'live' ? (refetchInterval ?? false) : false,
        refetchIntervalInBackground: true,
        placeholderData: keepPreviousData,
    });

    const logsQuery = useInfiniteQuery({
        queryKey: infiniteQueryKey,
        initialPageParam: null as LogCursor | null,
        queryFn: async ({ pageParam }) => {
            const params = new URLSearchParams();
            params.set('limit', String(pageSize));
            params.set('with_total', 'false');
            params.set('include_content', 'false');
            params.set('pagination', 'cursor');
            if (pageParam?.time && pageParam?.id) {
                params.set('before_time', String(pageParam.time));
                params.set('before_id', String(pageParam.id));
            }
            appendLogListParams(params, filters);
            appendRangeParams(params, range, rangeTimezone);
            const result = await apiClient.get<{ logs: RelayLog[] | null; has_more?: boolean; next_cursor?: LogCursor | null; warning?: string; search_mode?: string } | null>(
                `/api/v1/log/list?${params.toString()}`,
            );
            return {
                logs: result?.logs ?? [],
                has_more: result?.has_more ?? false,
                next_cursor: result?.next_cursor ?? null,
                warning: result?.warning,
                search_mode: result?.search_mode,
            } satisfies CursorPage;
        },
        getNextPageParam: (lastPage) => {
            if (!lastPage?.has_more) return undefined;
            return lastPage.next_cursor ?? undefined;
        },
        staleTime: 0,
        // 不用 'always'：重挂载触发 infinite refetch 会按序重取所有已加载页
        //（深翻页时是请求风暴）。顶部新鲜度由 head 承担，深度重建走 resetInfinite/refresh。
        refetchOnMount: false,
        refetchOnWindowFocus: false,
        placeholderData: keepPreviousData,
        enabled,
    });

    // head 与 page 1 无重叠时（两次轮询间隔内到达超过一页容量的突发流量），
    // 两段之间的日志既不在 head 也不在已缓存页，游标分页永远取不到。
    // 检测到断档就重置分页缓存，page 1 重取后自然补桥。
    const gapBridgeEffectEnabled = enabled;
    useEffect(() => {
        if (!gapBridgeEffectEnabled) return;
        const headLogs = headQuery.data?.logs ?? [];
        const page1 = logsQuery.data?.pages?.[0]?.logs ?? [];
        if (headLogs.length === 0 || page1.length === 0) return;
        const headOldest = headLogs[headLogs.length - 1];
        const page1Newest = page1[0];
        // 列表按 time DESC, id DESC：head 最旧一条严格新于 page 1 最新一条即断档
        const hasGap =
            headOldest.time > page1Newest.time ||
            (headOldest.time === page1Newest.time && headOldest.id > page1Newest.id);
        if (hasGap) {
            void queryClient.resetQueries({ queryKey: infiniteQueryKey });
        }
    }, [gapBridgeEffectEnabled, headQuery.data, logsQuery.data, queryClient, infiniteQueryKey]);

    const logs = useMemo(() => {
        const merged = new Map<number, RelayLog>();
        for (const page of logsQuery.data?.pages ?? []) {
            for (const log of page.logs) merged.set(log.id, log);
        }
        for (const log of headQuery.data?.logs ?? []) merged.set(log.id, log);
        return Array.from(merged.values()).sort((a, b) => b.time - a.time || b.id - a.id);
    }, [logsQuery.data, headQuery.data]);

    // 解构出稳定字段再 memoize：函数体直接访问 query 对象会捕获整个
    // 每次渲染都变化的对象引用，无法保留 useCallback 的记忆化。
    const { hasNextPage, isFetchingNextPage, fetchNextPage, isFetching } = logsQuery;
    const { refetch: refetchHead, dataUpdatedAt: headUpdatedAt } = headQuery;
    const [loadMoreError, setLoadMoreError] = useState<Error | null>(null);
    const loadMore = useCallback(async () => {
        if (!hasNextPage) return;
        // 同时挡 isFetching：fetchNextPage 默认 cancelRefetch: true 会丢弃在途请求，
        // 与游标加载交错时会造成页间缺缝（去重只能吃重复，补不回缝隙）。
        if (isFetchingNextPage || isFetching) return;

        setLoadMoreError(null);
        try {
            await fetchNextPage();
        } catch (e) {
            setLoadMoreError(e instanceof Error ? e : new Error(String(e)));
            logger.error('加载更多日志失败:', e);
        }
    }, [hasNextPage, isFetchingNextPage, isFetching, fetchNextPage]);

    // 手动刷新：先重建分页缓存（page 1 按当前窗口重取），完成后串行刷新 head。
    // 串行保证 head 的返回不早于 page 1，避免旧 head 快照覆盖新 page 1 数据
    //（fixed 窗口无轮询，覆盖后不会自愈）。不做 infinite refetch —— 全页重取会让
    // 旧游标链与新窗口错位产生缺缝；深翻页后刷新回到最新视图也是"刷新"的自然语义。
    const resetInfinite = useCallback(() => {
        void queryClient.resetQueries({ queryKey: infiniteQueryKey });
    }, [queryClient, infiniteQueryKey]);
    const refresh = useCallback(async () => {
        await queryClient.resetQueries({ queryKey: infiniteQueryKey });
        await refetchHead();
    }, [queryClient, infiniteQueryKey, refetchHead]);

    const clear = useCallback(() => {
        queryClient.removeQueries({ queryKey: infiniteQueryKey });
        queryClient.removeQueries({ queryKey: headQueryKey });
    }, [infiniteQueryKey, headQueryKey, queryClient]);

    return {
        logs,
        error: logsQuery.error,
        headError: headQuery.error,
        hasMore: !!logsQuery.hasNextPage,
        isLoading: logsQuery.isLoading,
        isLoadingMore: logsQuery.isFetchingNextPage,
        refetch: refresh,
        isRefetching: logsQuery.isRefetching || headQuery.isFetching,
        loadMore,
        loadMoreError,
        clear,
        /** 重置分页缓存并重取第一页（live 窗口跳变时由调用方触发补齐） */
        resetInfinite,
        /** 最近一次 head 成功拉取的时间戳（数据新鲜度标识用） */
        updatedAt: headUpdatedAt,
        warning: headQuery.data?.warning ?? logsQuery.data?.pages?.[0]?.warning ?? null,
        searchMode: headQuery.data?.search_mode ?? logsQuery.data?.pages?.[0]?.search_mode ?? null,
    };
}

export type LiveRequestState = 'running' | 'success' | 'failed' | 'canceled';

export interface LiveAttempt {
    attempt_index: number;
    channel_name: string;
    model_name: string;
    error: string;
}

export interface LiveLogOverview {
    id: number;
    state: LiveRequestState;
    started_at: string;
    completed_at?: string;
    duration_ms: number;
    request_model_name: string;
    actual_model_name: string;
    channel_name: string;
    input_tokens: number;
    output_tokens: number;
    cache_read_tokens: number;
    cache_write_tokens: number;
    total_cost: number;
    error?: string;
    // 请求完成后的补充字段：与 DB 历史日志一致，用于实时列表展示
    // key 倍率与完整 key 切换记录。
    price_group_multiplier?: number;
    price_group_multiplier_known?: boolean;
    attempts?: ChannelAttempt[];
}

function isLiveFinished(state: LiveRequestState) {
    return state === 'success' || state === 'failed' || state === 'canceled';
}

async function fetchLogStreamToken() {
    const { token } = await apiClient.get<{ token: string }>('/api/v1/log/stream-token');
    return token;
}

// useLiveLogs 订阅实时日志概览流（初始历史快照 + 增量）。
// live 列表在内存中按 id 降序保留固定条数：面板常被整天挂着，无上限会让
// 数万条日志对象常驻并让每条新日志触发全量排序。
const LIVE_LOGS_MAX_ENTRIES = 500;
// 进入无过滤日志页时先拉取最近 N 条历史日志作为初始快照，随后由 SSE 增量更新。
// 否则页面只在收到下一条实时事件后才显示内容，给用户「没有日志」的错觉。
const LIVE_LOGS_SNAPSHOT_SIZE = 20;

function relayLogToLiveOverview(log: RelayLog): LiveLogOverview {
    return {
        id: log.id,
        state: log.outcome === 'success'
            ? 'success'
            : log.outcome === 'client_canceled'
                ? 'canceled'
                : 'failed',
        started_at: new Date(log.time * 1000).toISOString(),
        completed_at: new Date((log.time + Math.max(0, Math.floor(log.use_time / 1000))) * 1000).toISOString(),
        duration_ms: log.use_time,
        request_model_name: log.request_model_name,
        actual_model_name: log.actual_model_name,
        channel_name: log.channel_name,
        input_tokens: log.input_tokens,
        output_tokens: log.output_tokens,
        cache_read_tokens: log.cache_read_tokens ?? 0,
        cache_write_tokens: log.cache_write_tokens ?? 0,
        total_cost: log.cost,
        error: log.error,
        price_group_multiplier: log.price_group_multiplier,
        attempts: log.attempts,
    };
}

export function useLiveLogs(enabled = true) {
    const [logs, setLogs] = useState<LiveLogOverview[]>([]);
    // 初始值随 enabled：禁用时保持非加载态。enabled 中途变化时本 hook 的
    // isLoading 不会被消费（调用方按 filterMode 切换加载来源），无需同步重置。
    const [isLoading, setIsLoading] = useState(enabled);
    const [error, setError] = useState<Error | null>(null);

    useEffect(() => {
        if (!enabled) return;
        let cancelled = false;
        let retryTimer: ReturnType<typeof setTimeout> | null = null;
        let eventSource: EventSource | null = null;

        const scheduleReconnect = () => {
            if (cancelled) return;
            retryTimer = setTimeout(() => {
                retryTimer = null;
                void connect(true);
            }, 2000);
        };

        const loadSnapshot = async () => {
            try {
                const params = new URLSearchParams();
                params.set('limit', String(LIVE_LOGS_SNAPSHOT_SIZE));
                params.set('with_total', 'false');
                params.set('include_content', 'false');
                params.set('pagination', 'cursor');
                const result = await apiClient.get<{ logs: RelayLog[] | null } | null>(
                    `/api/v1/log/list?${params.toString()}`,
                );
                if (cancelled) return;
                setLogs((result?.logs ?? []).map(relayLogToLiveOverview));
            } catch (e) {
                if (cancelled) return;
                setError(e instanceof Error ? e : new Error('Failed to load recent logs'));
            }
        };

        const connect = async (isReconnect = false) => {
            try {
                const token = await fetchLogStreamToken();
                if (cancelled) return;

                eventSource = new EventSource(`${API_BASE_URL}/api/v1/log/overview/stream?token=${token}`);
                eventSource.onopen = () => {
                    if (cancelled) return;
                    setError(null);
                    if (!isReconnect) setIsLoading(false);
                };
                eventSource.addEventListener('log', (event) => {
                    try {
                        const next = JSON.parse((event as MessageEvent<string>).data) as LiveLogOverview;
                        setLogs((current) => {
                            const rest = current.filter((item) => item.id !== next.id);
                            const merged = [...rest, next].sort((a, b) => b.id - a.id);
                            return merged.length > LIVE_LOGS_MAX_ENTRIES ? merged.slice(0, LIVE_LOGS_MAX_ENTRIES) : merged;
                        });
                        setIsLoading(false);
                        setError(null);
                    } catch {
                        setError(new Error('Invalid live log update'));
                    }
                });
                eventSource.onerror = () => {
                    if (cancelled) return;
                    setIsLoading(false);
                    setError(new Error('Live log stream disconnected'));
                    eventSource?.close();
                    eventSource = null;
                    scheduleReconnect();
                };
            } catch (e) {
                if (cancelled) return;
                setIsLoading(false);
                setError(e instanceof Error ? e : new Error('Failed to open live log stream'));
                scheduleReconnect();
            }
        };

        void loadSnapshot();
        void connect(false);

        return () => {
            cancelled = true;
            if (retryTimer) clearTimeout(retryTimer);
            eventSource?.close();
        };
    }, [enabled]);

    return { logs, isLoading, error };
}

// useLiveLogDetail 订阅运行中请求的尝试级详情流。
export function useLiveLogDetail(id: number, state: LiveRequestState, enabled: boolean) {
    const [attempts, setAttempts] = useState<LiveAttempt[]>([]);
    const [runningAttempt, setRunningAttempt] = useState<LiveAttempt | null>(null);

    // id 变化时在渲染期清空上一个请求的尝试列表（derived-state-reset 模式，
    // 替代原先 effect 内的同步重置）。重连/重展开时保留已收到的尝试，
    // 新事件按 attempt_index 去重合并，避免折叠再展开时列表闪空。
    const [prevId, setPrevId] = useState(id);
    if (prevId !== id) {
        setPrevId(id);
        setAttempts([]);
        setRunningAttempt(null);
    }

    useEffect(() => {
        if (!enabled || isLiveFinished(state)) return;
        let cancelled = false;
        let eventSource: EventSource | null = null;

        const connect = async () => {
            try {
                const token = await fetchLogStreamToken();
                if (cancelled) return;

                eventSource = new EventSource(`${API_BASE_URL}/api/v1/log/${id}/stream?token=${token}`);
                eventSource.addEventListener('attempt.started', (event) => {
                    try {
                        const next = JSON.parse((event as MessageEvent<string>).data) as LiveAttempt;
                        setRunningAttempt(next);
                        setAttempts((current) => {
                            const rest = current.filter((item) => item.attempt_index !== next.attempt_index);
                            return [...rest, next].slice(-50);
                        });
                    } catch {
                        return;
                    }
                });
                eventSource.addEventListener('attempt.finished', (event) => {
                    try {
                        const next = JSON.parse((event as MessageEvent<string>).data) as LiveAttempt;
                        setRunningAttempt((current) =>
                            current?.attempt_index === next.attempt_index ? null : current
                        );
                        setAttempts((current) => {
                            const rest = current.filter((item) => item.attempt_index !== next.attempt_index);
                            return [...rest, next].slice(-50);
                        });
                    } catch {
                        return;
                    }
                });
                eventSource.onerror = () => {
                    if (cancelled) return;
                    eventSource?.close();
                    eventSource = null;
                };
            } catch {
                return;
            }
        };

        void connect();

        return () => {
            cancelled = true;
            eventSource?.close();
        };
    }, [enabled, id, state]);

    return { attempts, runningAttempt };
}

// useStopAttempt 中止指定请求当前序号匹配的上游尝试。
export function useStopAttempt() {
    return useMutation({
        mutationFn: ({ requestId, attemptIndex }: { requestId: number; attemptIndex: number }) =>
            apiClient.post<unknown>(`/api/v1/log/${requestId}/${attemptIndex}/stop`),
    });
}
