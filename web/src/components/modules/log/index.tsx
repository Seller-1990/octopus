'use client';

import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import {
    useLogs,
    useLogSiteActionTargets,
    type LogKeywordMode,
    type LogKeywordScope,
    type RelayLog,
} from '@/api/endpoints/log';
import type { UsageAnalyticsFilters, UsageBreakdownItem } from '@/api/endpoints/log-analytics';
import { LIVE_WINDOW_CLOCK_BUFFER_SECONDS, resolveLogDateRange } from '@/lib/log-range';
import { useChannelList } from '@/api/endpoints/channel';
import { useSiteList } from '@/api/endpoints/site';
import { X } from 'lucide-react';
import { LogCard, type LogSiteActionTargets } from './Item';
import { Loader2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { useTranslations } from 'next-intl';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import { useSearchStore } from '@/components/modules/toolbar';
import { useToolbarViewOptionsStore } from '@/components/modules/toolbar/view-options-store';
import { useLogUIStore } from './ui-store';
import { useLogAnalyticsStore } from './analytics-store';
import { UsageAnalytics } from './Analytics';
import { LogControls, type LogViewTabIds } from './Controls';
import { LiveLogPanel } from './Live';

type LogFilters = {
    keyword: string;
    keywordMode: LogKeywordMode;
    keywordScope: LogKeywordScope;
    channelIds: number[];
    siteIds: number[];
    siteAccountIds: number[];
    apiKeyIds: number[];
    requestModels: string[];
    actualModels: string[];
    canonicalModels: string[];
};

const LOG_PAGE_SIZE = 10;

function useDebouncedValue<T>(value: T, delay = 200) {
    const [debounced, setDebounced] = useState(value);
    useEffect(() => {
        const handle = setTimeout(() => setDebounced(value), delay);
        return () => clearTimeout(handle);
    }, [value, delay]);
    return debounced;
}

function FreshnessBadge({ updatedAt, live, hasError }: { updatedAt: number; live: boolean; hasError: boolean }) {
    const t = useTranslations('log');
    if (!updatedAt) return null;
    const time = new Date(updatedAt).toLocaleTimeString([], {
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        hour12: false,
    });
    return (
        <div className="flex min-w-0 items-center gap-1.5 text-[11px] text-muted-foreground">
            <span
                className={live ? 'size-1.5 shrink-0 rounded-full bg-emerald-500' : 'size-1.5 shrink-0 rounded-full bg-muted-foreground/40'}
                aria-hidden
            />
            <span className="truncate tabular-nums">
                {t('list.updatedAt', { time })}
                {live ? t('list.autoRefresh') : ''}
            </span>
            {hasError ? <span className="shrink-0 text-destructive">{t('list.refreshFailed')}</span> : null}
        </div>
    );
}

/** 当前生效的业务筛选 chips：让下钻/筛选的结果可见、可撤销，而不是静默改状态。 */
function ActiveFilterChips() {
    const t = useTranslations('log');
    const siteIds = useLogAnalyticsStore((s) => s.siteIds);
    const siteAccountIds = useLogAnalyticsStore((s) => s.siteAccountIds);
    const apiKeyIds = useLogAnalyticsStore((s) => s.apiKeyIds);
    const requestModels = useLogAnalyticsStore((s) => s.requestModels);
    const actualModels = useLogAnalyticsStore((s) => s.actualModels);
    const canonicalModels = useLogAnalyticsStore((s) => s.canonicalModels);
    const logChannelIds = useToolbarViewOptionsStore((s) => s.logChannelIds);
    const setSiteIds = useLogAnalyticsStore((s) => s.setSiteIds);
    const setSiteAccountIds = useLogAnalyticsStore((s) => s.setSiteAccountIds);
    const setAPIKeyIds = useLogAnalyticsStore((s) => s.setAPIKeyIds);
    const setRequestModels = useLogAnalyticsStore((s) => s.setRequestModels);
    const setActualModels = useLogAnalyticsStore((s) => s.setActualModels);
    const setCanonicalModels = useLogAnalyticsStore((s) => s.setCanonicalModels);
    const setLogChannelIds = useToolbarViewOptionsStore((s) => s.setLogChannelIds);
    const { data: sites } = useSiteList();
    const { data: channels } = useChannelList();

    const siteName = useMemo(
        () => (sites ?? []).find((site) => site.id === siteIds[0])?.name,
        [sites, siteIds],
    );
    const accountName = useMemo(() => {
        if (siteAccountIds.length === 0) return undefined;
        for (const site of sites ?? []) {
            const match = site.accounts.find((account) => account.id === siteAccountIds[0]);
            if (match) return match.name;
        }
        return undefined;
    }, [sites, siteAccountIds]);
    const channelNames = useMemo(() => {
        if (logChannelIds.length === 0) return [];
        const nameById = new Map<number, string>();
        for (const item of channels ?? []) nameById.set(item.raw.id, item.raw.name);
        return logChannelIds.map((id) => nameById.get(id) ?? `#${id}`);
    }, [channels, logChannelIds]);

    const chips = useMemo(() => {
        const list: Array<{ key: string; label: string; onRemove: () => void }> = [];
        if (siteIds.length > 0) {
            list.push({
                key: 'site',
                label: `${t('analytics.filters.site')}: ${siteName ?? `#${siteIds[0]}`}`,
                onRemove: () => setSiteIds([]),
            });
        }
        if (siteAccountIds.length > 0) {
            list.push({
                key: 'site_account',
                label: `${t('analytics.filters.account')}: ${accountName ?? `#${siteAccountIds[0]}`}`,
                onRemove: () => setSiteAccountIds([]),
            });
        }
        if (logChannelIds.length > 0) {
            const label = channelNames.length === 1
                ? channelNames[0]
                : `${t('popover.channelCount', { count: logChannelIds.length })}`;
            list.push({ key: 'channel', label: `${t('analytics.filters.channel')}: ${label}`, onRemove: () => setLogChannelIds([]) });
        }
        if (apiKeyIds.length > 0) {
            list.push({
                key: 'api_key',
                label: `API Key: #${apiKeyIds[0]}`,
                onRemove: () => setAPIKeyIds([]),
            });
        }
        if (requestModels.length > 0) {
            list.push({
                key: 'request_model',
                label: `${t('analytics.dimension.request_model')}: ${requestModels[0]}`,
                onRemove: () => setRequestModels([]),
            });
        }
        if (actualModels.length > 0) {
            list.push({
                key: 'actual_model',
                label: `${t('analytics.dimension.actual_model')}: ${actualModels[0]}`,
                onRemove: () => setActualModels([]),
            });
        }
        if (canonicalModels.length > 0) {
            list.push({
                key: 'canonical_model',
                label: `${t('analytics.dimension.canonical_model')}: ${canonicalModels[0]}`,
                onRemove: () => setCanonicalModels([]),
            });
        }
        return list;
    }, [
        accountName,
        actualModels,
        apiKeyIds,
        canonicalModels,
        channelNames,
        logChannelIds,
        requestModels,
        siteAccountIds,
        siteIds,
        siteName,
        setAPIKeyIds,
        setActualModels,
        setCanonicalModels,
        setLogChannelIds,
        setRequestModels,
        setSiteAccountIds,
        setSiteIds,
        t,
    ]);

    if (chips.length === 0) return null;

    return (
        <div className="flex flex-wrap items-center gap-1.5">
            {chips.map((chip) => (
                <span
                    key={chip.key}
                    className="inline-flex max-w-56 items-center gap-1 rounded-full border border-border/70 bg-muted/30 py-0.5 pl-2.5 pr-1 text-[11px] text-foreground"
                >
                    <span className="truncate">{chip.label}</span>
                    <button
                        type="button"
                        onClick={chip.onRemove}
                        aria-label={`${t('list.removeFilter')}: ${chip.label}`}
                        className="flex size-4 shrink-0 items-center justify-center rounded-full text-muted-foreground hover:bg-muted hover:text-foreground"
                    >
                        <X className="size-3" />
                    </button>
                </span>
            ))}
        </div>
    );
}

/**
 * 日志明细列表：始终走历史日志查询并按当前时间范围展示；
 * live 预设是滚动谓词（每次查询求值当前窗口），只有显式选定的历史区间才冻结。
 */
function LogDetailList() {
    const t = useTranslations('log');
    const pageKey = 'log' as const;
    const searchTerm = useSearchStore((s) => s.getSearchTerm(pageKey));
    const refreshRequestId = useLogUIStore((s) => s.refreshRequestId);
    const setRefreshing = useLogUIStore((s) => s.setRefreshing);
    const lastHandledRefreshRequestIdRef = useRef(refreshRequestId);
    const logDateRange = useToolbarViewOptionsStore((s) => s.logDateRange);
    const logChannelIds = useToolbarViewOptionsStore((s) => s.logChannelIds);
    const logKeywordMode = useToolbarViewOptionsStore((s) => s.logKeywordMode);
    const logKeywordScope = useToolbarViewOptionsStore((s) => s.logKeywordScope);
    const timezone = useLogAnalyticsStore((s) => s.timezone);
    const siteIds = useLogAnalyticsStore((s) => s.siteIds);
    const siteAccountIds = useLogAnalyticsStore((s) => s.siteAccountIds);
    const apiKeyIds = useLogAnalyticsStore((s) => s.apiKeyIds);
    const requestModels = useLogAnalyticsStore((s) => s.requestModels);
    const actualModels = useLogAnalyticsStore((s) => s.actualModels);
    const canonicalModels = useLogAnalyticsStore((s) => s.canonicalModels);
    const filters = useMemo<LogFilters>(() => ({
        keyword: searchTerm,
        keywordMode: logKeywordMode,
        keywordScope: logKeywordScope,
        channelIds: logChannelIds,
        siteIds,
        siteAccountIds,
        apiKeyIds,
        requestModels,
        actualModels,
        canonicalModels,
    }), [
        actualModels,
        apiKeyIds,
        canonicalModels,
        logChannelIds,
        logKeywordMode,
        logKeywordScope,
        requestModels,
        searchTerm,
        siteAccountIds,
        siteIds,
    ]);
    const debouncedFilters = useDebouncedValue(filters, 200);
    const logFilters = useMemo(() => ({
        keyword: debouncedFilters.keyword.trim() || undefined,
        keyword_mode: debouncedFilters.keyword.trim() ? debouncedFilters.keywordMode : undefined,
        keyword_scope: debouncedFilters.keyword.trim() ? debouncedFilters.keywordScope : undefined,
        channel_ids: debouncedFilters.channelIds.length > 0 ? debouncedFilters.channelIds : undefined,
        site_ids: debouncedFilters.siteIds.length > 0 ? debouncedFilters.siteIds : undefined,
        site_account_ids: debouncedFilters.siteAccountIds.length > 0 ? debouncedFilters.siteAccountIds : undefined,
        api_key_ids: debouncedFilters.apiKeyIds.length > 0 ? debouncedFilters.apiKeyIds : undefined,
        request_models: debouncedFilters.requestModels.length > 0 ? debouncedFilters.requestModels : undefined,
        actual_models: debouncedFilters.actualModels.length > 0 ? debouncedFilters.actualModels : undefined,
        canonical_models: debouncedFilters.canonicalModels.length > 0 ? debouncedFilters.canonicalModels : undefined,
    }), [debouncedFilters]);
    const dbLogsQuery = useLogs({
        pageSize: LOG_PAGE_SIZE,
        filters: logFilters,
        range: logDateRange,
        rangeTimezone: timezone,
        enabled: true,
        refetchInterval: 5000,
    });
    const allLogs = dbLogsQuery.logs;
    const logsError = dbLogsQuery.error;

    // === 展示缓冲：滚离顶部后冻结视图，新日志只累计计数（横幅提示），回顶自动解冻 ===
    const [frozenLogs, setFrozenLogs] = useState<RelayLog[] | null>(null);
    const [pendingNewCount, setPendingNewCount] = useState(0);
    const logsRef = useRef(allLogs);
    logsRef.current = allLogs;
    const atTopRef = useRef(true);
    const scrollContainerRef = useRef<HTMLDivElement | null>(null);

    const handleGridScroll = useCallback((info: { scrollTop: number }) => {
        // 滞回防抖：越过冻结线才冻结、回到解冻线以内才解冻。
        // 触控板惯性滚动常在阈值附近抖动，单阈值会让冻结/跟随反复横跳；
        // 横幅点击的 smooth scrollTo 途中也不会中途解冻（只有接近顶部才解冻）。
        if (atTopRef.current && info.scrollTop > 100) {
            atTopRef.current = false;
            setFrozenLogs(logsRef.current);
            setPendingNewCount(0);
        } else if (!atTopRef.current && info.scrollTop < 16) {
            atTopRef.current = true;
            setFrozenLogs(null);
            setPendingNewCount(0);
        }
    }, []);

    useEffect(() => {
        if (atTopRef.current) return;
        if (allLogs.length === 0) {
            // 数据被清空（如清空日志）后冻结快照已无意义，自动解冻
            if (frozenLogs && frozenLogs.length > 0) {
                setFrozenLogs(null);
                setPendingNewCount(0);
            }
            return;
        }
        const headId = frozenLogs?.[0]?.id;
        if (headId == null) return;
        let count = 0;
        for (const log of allLogs) {
            if (log.id > headId) count += 1;
        }
        setPendingNewCount(count);
    }, [allLogs, frozenLogs]);

    // 查询条件（筛选/时间范围）变化后，冻结快照不再代表当前结果集，
    // 且新旧数据集的 id 比较没有意义——立即解冻跟随新数据。
    const filterSignature = useMemo(() => JSON.stringify(logFilters), [logFilters]);
    useEffect(() => {
        setFrozenLogs(null);
        setPendingNewCount(0);
    }, [filterSignature, logDateRange]);

    // live 预设是滚动谓词：跨过窗口起点跳变（如午夜后"今天"的零点前移）时，
    // 分页缓存的旧窗口数据与 head 拉取的新窗口数据会分裂，主动重建分页缓存。
    // 仅检测"同一 range 对象内的窗口滚动"；更换 range 对象时 queryKey 已变化，
    // React Query 会自然发起新查询，重复 reset 只会浪费一次请求。
    const rangeStart = resolveLogDateRange(logDateRange, timezone).start;
    const lastRangeRef = useRef<{ range: typeof logDateRange; start?: number }>({
        range: logDateRange,
        start: rangeStart,
    });
    const resetInfinite = dbLogsQuery.resetInfinite;
    useEffect(() => {
        const prev = lastRangeRef.current;
        lastRangeRef.current = { range: logDateRange, start: rangeStart };
        if (prev.range !== logDateRange) return;
        if (prev.start === rangeStart) return;
        resetInfinite();
    }, [logDateRange, rangeStart, resetInfinite]);

    const displayLogs = frozenLogs ?? allLogs;
    const isFrozen = frozenLogs !== null;

    const logIDs = useMemo(() => displayLogs.map((log) => log.id), [displayLogs]);
    const siteActionTargetsQuery = useLogSiteActionTargets(logIDs, displayLogs.length > 0);
    const siteActionTargets = useMemo(() => {
        const next = new Map<number, LogSiteActionTargets>();
        const data = siteActionTargetsQuery.data ?? {};
        for (const [id, targets] of Object.entries(data)) {
            next.set(Number(id), targets);
        }
        return next;
    }, [siteActionTargetsQuery.data]);

    const canLoadMore = !isFrozen && dbLogsQuery.hasMore && !dbLogsQuery.isLoading && !dbLogsQuery.isLoadingMore && displayLogs.length > 0;
    const handleReachEnd = useCallback(() => {
        if (!canLoadMore) return;
        void dbLogsQuery.loadMore();
    }, [canLoadMore, dbLogsQuery]);

    const { refetch: refreshLogs } = dbLogsQuery;
    const handleRefresh = useCallback(async () => {
        setRefreshing(true);
        try {
            await refreshLogs();
        } finally {
            setRefreshing(false);
        }
    }, [refreshLogs, setRefreshing]);

    useEffect(() => {
        if (refreshRequestId === lastHandledRefreshRequestIdRef.current) return;
        lastHandledRefreshRequestIdRef.current = refreshRequestId;
        void handleRefresh();
    }, [handleRefresh, refreshRequestId]);

    const freshnessRow = (
        <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex min-w-0 flex-wrap items-center gap-2">
                <FreshnessBadge
                    updatedAt={dbLogsQuery.updatedAt}
                    live={logDateRange.mode === 'live'}
                    hasError={!!dbLogsQuery.headError}
                />
                <ActiveFilterChips />
            </div>
            {isFrozen && pendingNewCount > 0 ? (
                <button
                    type="button"
                    onClick={() => scrollContainerRef.current?.scrollTo({ top: 0, behavior: 'smooth' })}
                    className="shrink-0 rounded-full border border-primary/40 bg-primary/10 px-3 py-1 text-[11px] font-medium text-primary hover:bg-primary/20"
                >
                    {t('list.newLogs', { count: pendingNewCount })}
                </button>
            ) : null}
        </div>
    );

    const footer = useMemo(() => {
        if (isFrozen) return null;
        if (dbLogsQuery.loadMoreError) {
            return (
                <div className="flex justify-center py-4">
                    <Button type="button" variant="outline" size="sm" onClick={() => void dbLogsQuery.loadMore()}>
                        {t('list.retry')}
                    </Button>
                </div>
            );
        }
        if (dbLogsQuery.hasMore) {
            return (
                <div className="flex justify-center py-4">
                    <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
                </div>
            );
        }
        if (displayLogs.length > 0) {
            return (
                <div className="flex justify-center py-4">
                    <span className="text-sm text-muted-foreground">{t('list.noMore')}</span>
                </div>
            );
        }
        return null;
    }, [dbLogsQuery.hasMore, dbLogsQuery.loadMoreError, dbLogsQuery.loadMore, displayLogs.length, isFrozen, t]);

    if (dbLogsQuery.isLoading && displayLogs.length === 0) {
        return (
            <div className="flex h-full flex-col gap-3">
                {freshnessRow}
                <div className="flex h-full items-center justify-center">
                    <Loader2 className="size-6 animate-spin text-muted-foreground" />
                </div>
            </div>
        );
    }

    if (displayLogs.length === 0 && !dbLogsQuery.isLoading && !logsError) {
        return (
            <div className="flex h-full flex-col gap-3">
                {freshnessRow}
                <div className="flex h-full flex-col items-center justify-center gap-1 pb-10 text-sm text-muted-foreground">
                    <span>{t('list.empty')}</span>
                    <span className="text-xs">{t('list.emptyHint')}</span>
                </div>
            </div>
        );
    }

    return (
        <div className="flex h-full min-h-0 flex-col gap-3">
            {freshnessRow}
            {dbLogsQuery.warning ? (
                <div className="rounded-lg border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-300">
                    {dbLogsQuery.warning}
                </div>
            ) : null}
            {logsError ? (
                <div
                    role="alert"
                    className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive"
                >
                    <span>{t('list.loadFailed', { message: logsError.message })}</span>
                    <Button type="button" variant="outline" size="sm" onClick={() => void dbLogsQuery.refetch()}>
                        {t('list.retry')}
                    </Button>
                </div>
            ) : null}
            <div className="relative min-h-0 flex-1">
                <VirtualizedGrid
                    items={displayLogs as RelayLog[]}
                    layout="list"
                    columns={{ default: 1 }}
                    estimateItemHeight={80}
                    overscan={8}
                    getItemKey={(log) => `log-${log.id}`}
                    renderItem={(log) => <LogCard log={log} siteTargets={siteActionTargets.get(log.id) ?? null} />}
                    footer={footer}
                    onScroll={handleGridScroll}
                    onScrollContainer={(el) => {
                        scrollContainerRef.current = el;
                    }}
                    onReachEnd={handleReachEnd}
                    reachEndEnabled={canLoadMore}
                    reachEndOffset={2}
                />
            </div>
        </div>
    );
}

export function Log() {
    const queryClient = useQueryClient();
    const tabsId = useId();
    const view = useLogAnalyticsStore((state) => state.view);
    const scope = useLogAnalyticsStore((state) => state.scope);
    const dimension = useLogAnalyticsStore((state) => state.dimension);
    const timezone = useLogAnalyticsStore((state) => state.timezone);
    const siteIds = useLogAnalyticsStore((state) => state.siteIds);
    const siteAccountIds = useLogAnalyticsStore((state) => state.siteAccountIds);
    const apiKeyIds = useLogAnalyticsStore((state) => state.apiKeyIds);
    const requestModels = useLogAnalyticsStore((state) => state.requestModels);
    const actualModels = useLogAnalyticsStore((state) => state.actualModels);
    const canonicalModels = useLogAnalyticsStore((state) => state.canonicalModels);
    const setView = useLogAnalyticsStore((state) => state.setView);
    const setSiteIds = useLogAnalyticsStore((state) => state.setSiteIds);
    const setSiteAccountIds = useLogAnalyticsStore((state) => state.setSiteAccountIds);
    const setAPIKeyIds = useLogAnalyticsStore((state) => state.setAPIKeyIds);
    const setRequestModels = useLogAnalyticsStore((state) => state.setRequestModels);
    const setActualModels = useLogAnalyticsStore((state) => state.setActualModels);
    const setCanonicalModels = useLogAnalyticsStore((state) => state.setCanonicalModels);
    const logDateRange = useToolbarViewOptionsStore((state) => state.logDateRange);
    const logChannelIds = useToolbarViewOptionsStore((state) => state.logChannelIds);
    const setLogChannelIds = useToolbarViewOptionsStore((state) => state.setLogChannelIds);
    const refreshRequestId = useLogUIStore((state) => state.refreshRequestId);
    const setRefreshing = useLogUIStore((state) => state.setRefreshing);
    const lastAnalyticsRefreshRef = useRef(refreshRequestId);
    const tabIds = useMemo<LogViewTabIds>(() => ({
        analytics: {
            trigger: `${tabsId}-tab-analytics`,
            panel: `${tabsId}-panel-analytics`,
        },
        detail: {
            trigger: `${tabsId}-tab-detail`,
            panel: `${tabsId}-panel-detail`,
        },
        live: {
            trigger: `${tabsId}-tab-live`,
            panel: `${tabsId}-panel-live`,
        },
    }), [tabsId]);

    // 每半分钟更新一次分钟刻度：live 预设（今天/近 7 天/本月等）是滚动谓词，
    // 分析查询的 key 需要周期性吸收新的窗口端点，否则 KPI 会冻结在首次计算时刻。
    const [minuteTick, setMinuteTick] = useState(() => Math.floor(Date.now() / 60_000));
    useEffect(() => {
        const timer = window.setInterval(() => setMinuteTick(Math.floor(Date.now() / 60_000)), 30_000);
        return () => window.clearInterval(timer);
    }, []);

    useEffect(() => {
        if (refreshRequestId === lastAnalyticsRefreshRef.current) return;
        lastAnalyticsRefreshRef.current = refreshRequestId;
        setRefreshing(true);
        void queryClient.invalidateQueries({ queryKey: ['logs', 'analytics'] }).finally(() => {
            setRefreshing(false);
        });
    }, [queryClient, refreshRequestId, setRefreshing]);

    const analyticsFilters = useMemo<UsageAnalyticsFilters>(() => {
        const resolved = resolveLogDateRange(logDateRange, timezone);
        // live 窗口与明细查询使用同一时钟缓冲，避免"明细有、统计没有"的口径分裂
        const endTime = logDateRange.mode === 'live' && resolved.end != null
            ? resolved.end + LIVE_WINDOW_CLOCK_BUFFER_SECONDS
            : resolved.end;
        return {
            start_time: resolved.start,
            end_time: endTime,
            timezone,
            metric_scope: scope,
            site_ids: siteIds.length > 0 ? siteIds : undefined,
            site_account_ids: siteAccountIds.length > 0 ? siteAccountIds : undefined,
            channel_ids: logChannelIds.length > 0 ? logChannelIds : undefined,
            api_key_ids: apiKeyIds.length > 0 ? apiKeyIds : undefined,
            request_models: requestModels.length > 0 ? requestModels : undefined,
            actual_models: actualModels.length > 0 ? actualModels : undefined,
            canonical_models: canonicalModels.length > 0 ? canonicalModels : undefined,
        };
    }, [
        actualModels,
        apiKeyIds,
        canonicalModels,
        logChannelIds,
        logDateRange,
        minuteTick,
        requestModels,
        scope,
        siteAccountIds,
        siteIds,
        timezone,
    ]);

    const handleDrilldown = useCallback((item: UsageBreakdownItem) => {
        switch (dimension) {
            case 'site':
                setSiteIds(item.id > 0 ? [item.id] : []);
                break;
            case 'site_account':
                setSiteAccountIds(item.id > 0 ? [item.id] : []);
                break;
            case 'channel':
                setLogChannelIds(item.id > 0 ? [item.id] : []);
                break;
            case 'api_key':
                setAPIKeyIds(item.id > 0 ? [item.id] : []);
                break;
            case 'request_model':
                setRequestModels([item.name]);
                break;
            case 'actual_model':
                setActualModels([item.name]);
                break;
            case 'canonical_model':
                setCanonicalModels([item.name]);
                break;
        }
        setView('detail');
    }, [
        dimension,
        setAPIKeyIds,
        setActualModels,
        setCanonicalModels,
        setLogChannelIds,
        setRequestModels,
        setSiteAccountIds,
        setSiteIds,
        setView,
    ]);

    return (
        <div className="flex h-full min-h-0 flex-col gap-3">
            <LogControls tabIds={tabIds} />
            <div
                id={tabIds.analytics.panel}
                role="tabpanel"
                aria-labelledby={tabIds.analytics.trigger}
                hidden={view !== 'analytics'}
                tabIndex={view === 'analytics' ? 0 : -1}
                className="flex min-h-0 flex-1 flex-col"
            >
                {view === 'analytics' ? (
                    <UsageAnalytics filters={analyticsFilters} dimension={dimension} onDrilldown={handleDrilldown} />
                ) : null}
            </div>
            <div
                id={tabIds.detail.panel}
                role="tabpanel"
                aria-labelledby={tabIds.detail.trigger}
                hidden={view !== 'detail'}
                tabIndex={view === 'detail' ? 0 : -1}
                className="flex min-h-0 flex-1 flex-col"
            >
                {view === 'detail' ? <LogDetailList /> : null}
            </div>
            <div
                id={tabIds.live.panel}
                role="tabpanel"
                aria-labelledby={tabIds.live.trigger}
                hidden={view !== 'live'}
                tabIndex={view === 'live' ? 0 : -1}
                className="flex min-h-0 flex-1 flex-col"
            >
                {view === 'live' ? <LiveLogPanel /> : null}
            </div>
        </div>
    );
}
