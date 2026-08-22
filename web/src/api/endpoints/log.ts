import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient, API_BASE_URL } from '../client';
import { logger } from '@/lib/logger';
import { useCallback, useMemo, useState, useEffect } from 'react';

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
    filters?: Omit<LogListParams, 'page' | 'page_size'>;
    enabled?: boolean;
}

const logFiltersKey = (filters?: UseLogsOptions['filters']) => ({
    start_time: filters?.start_time ?? null,
    end_time: filters?.end_time ?? null,
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

function appendLogListParams(params: URLSearchParams, filters?: UseLogsOptions['filters']) {
    if (filters?.start_time) params.set('start_time', String(filters.start_time));
    if (filters?.end_time) params.set('end_time', String(filters.end_time));
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
            queryClient.invalidateQueries({ queryKey: ['logs'] });
        },
        onError: (error) => {
            logger.error('日志清空失败:', error);
        },
    });
}

const logsInfiniteQueryKey = (pageSize: number, filters?: UseLogsOptions['filters']) => ['logs', 'infinite', pageSize, logFiltersKey(filters)] as const;

/**
 * 日志管理 Hook
 * 整合初始加载与滚动加载更多；实时日志由 useLiveLogs 独立承担。
 *
 * @example
 * const { logs, hasMore, isLoadingMore, loadMore, clear } = useLogs();
 *
 * // logs 按时间倒序
 * logs.forEach(log => console.log(log.request_model_name));
 *
 * // 滚动到底部时加载更多
 * if (hasMore && !isLoadingMore) loadMore();
 */
export function useLogs(options: UseLogsOptions = {}) {
    const { pageSize = 20, filters, enabled = true } = options;

    const queryClient = useQueryClient();

    type CursorPage = { logs: RelayLog[]; next_cursor?: LogCursor | null; has_more: boolean; warning?: string; search_mode?: string };

    const logsQuery = useInfiniteQuery({
        queryKey: logsInfiniteQueryKey(pageSize, filters),
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
        refetchOnMount: 'always',
        refetchOnWindowFocus: false,
        enabled,
    });

    const logs = useMemo(() => {
        const pages = logsQuery.data?.pages ?? [];
        const seen = new Set<number>();
        const merged: RelayLog[] = [];

        for (const page of pages) {
            for (const log of page.logs) {
                if (seen.has(log.id)) continue;
                seen.add(log.id);
                merged.push(log);
            }
        }

        merged.sort((a, b) => b.time - a.time);
        return merged;
    }, [logsQuery.data]);

    // 解构出稳定字段再 memoize：函数体直接访问 logsQuery 对象会捕获整个
    // 每次渲染都变化的对象引用，无法保留 useCallback 的记忆化。
    const { hasNextPage, isFetchingNextPage, fetchNextPage } = logsQuery;
    const loadMore = useCallback(async () => {
        if (!hasNextPage) return;
        if (isFetchingNextPage) return;

        try {
            await fetchNextPage();
        } catch (e) {
            logger.error('加载更多日志失败:', e);
        }
    }, [hasNextPage, isFetchingNextPage, fetchNextPage]);

    const clear = useCallback(() => {
        queryClient.removeQueries({ queryKey: logsInfiniteQueryKey(pageSize, filters) });
    }, [pageSize, filters, queryClient]);

    return {
        logs,
        error: logsQuery.error,
        hasMore: !!logsQuery.hasNextPage,
        isLoading: logsQuery.isLoading,
        isLoadingMore: logsQuery.isFetchingNextPage,
        refetch: logsQuery.refetch,
        isRefetching: logsQuery.isRefetching,
        loadMore,
        clear,
        warning: logsQuery.data?.pages?.[0]?.warning ?? null,
        searchMode: logsQuery.data?.pages?.[0]?.search_mode ?? null,
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
