'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useChannelList, type Channel } from '@/api/endpoints/channel';
import { Card } from './Card';
import { ChannelsTable } from './ChannelsTable';
import { ChannelToolbar } from './ChannelToolbar';
import { useSearchStore, useToolbarViewOptionsStore } from '@/components/modules/toolbar';
import { SiteChannelSection } from '@/components/modules/site-channel';
import { cn } from '@/lib/utils';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import { AnimatePresence, motion } from 'motion/react';
import { useTranslations } from 'next-intl';
import {
    isChannelJumpTarget,
    type ChannelJumpTarget,
    type PendingJump,
    useJumpStore,
} from '@/stores/jump';
import { useChannelTabStore } from './tab-store';
import { useChannelFiltersStore } from './filter-store';

type ChannelPendingJump = PendingJump & { target: ChannelJumpTarget };

const JUMP_LOCATE_RETRY_INTERVAL_MS = 50;
// ~6s：覆盖 tab 切换退场动画（180ms）、列表数据加载与编程式滚动后虚拟行的挂载
const JUMP_LOCATE_MAX_ATTEMPTS = 120;

// 搜索命中范围：名称 / 模型（含自定义）/ 接口地址 / Key 备注
function matchesChannelSearch(channel: Channel, term: string): boolean {
    if (channel.name.toLowerCase().includes(term)) return true;
    if (channel.model.toLowerCase().includes(term)) return true;
    if (channel.custom_model.toLowerCase().includes(term)) return true;
    if (channel.base_urls.some((u) => u.url.toLowerCase().includes(term))) return true;
    if (channel.keys.some((k) => k.remark.toLowerCase().includes(term))) return true;
    return false;
}

export function Channel() {
    const { data: channelsData, isLoading, error } = useChannelList();
    const t = useTranslations('channel.table');
    const pendingJump = useJumpStore((state) => state.pending);
    const clearPending = useJumpStore((state) => state.clearPending);
    const pageKey = 'channel' as const;
    const searchTerm = useSearchStore((s) => s.getSearchTerm(pageKey));
    const layout = useToolbarViewOptionsStore((s) => s.getLayout(pageKey));
    const sortField = useToolbarViewOptionsStore((s) => s.getSortField(pageKey));
    const sortOrder = useToolbarViewOptionsStore((s) => s.getSortOrder(pageKey));
    const filters = useChannelFiltersStore();
    const [highlightedChannelId, setHighlightedChannelId] = useState<number | null>(null);
    const activeTab = useChannelTabStore((s) => s.activeTab);
    // 卡片与表格统一按 id 注册行元素，跳转定位时滚动并高亮
    const channelRowRefs = useRef<Map<number, HTMLElement>>(new Map());
    const highlightTimerRef = useRef<number | null>(null);

    const pendingChannelJump = pendingJump && isChannelJumpTarget(pendingJump.target)
        ? pendingJump as ChannelPendingJump
        : null;
    const targetedChannelId = pendingChannelJump?.target.channelId ?? null;

    const setChannelRowRef = useCallback((channelId: number, node: HTMLElement | null) => {
        const refs = channelRowRefs.current;
        if (node) {
            refs.set(channelId, node);
            return;
        }
        refs.delete(channelId);
    }, []);

    const flashChannelRow = useCallback((channelId: number) => {
        setHighlightedChannelId(channelId);
        if (highlightTimerRef.current !== null) {
            window.clearTimeout(highlightTimerRef.current);
        }
        highlightTimerRef.current = window.setTimeout(() => {
            highlightTimerRef.current = null;
            setHighlightedChannelId((current) => (current === channelId ? null : current));
        }, 1800);
    }, []);

    const sortedChannels = useMemo(() => {
        if (!channelsData) return [];
        return [...channelsData].sort((a, b) => {
            const diff = sortField === 'name'
                ? a.raw.name.localeCompare(b.raw.name)
                : a.raw.id - b.raw.id;
            return sortOrder === 'asc' ? diff : -diff;
        });
    }, [channelsData, sortField, sortOrder]);

    const visibleChannels = useMemo(() => {
        const term = searchTerm.toLowerCase().trim();

        return sortedChannels.filter((channel) => {
            if (channel.raw.id === targetedChannelId) {
                return true;
            }

            if (term && !matchesChannelSearch(channel.raw, term)) {
                return false;
            }

            return true;
        });
    }, [sortedChannels, searchTerm, targetedChannelId]);

    const visibleManualChannels = useMemo(
        () =>
            visibleChannels.filter((channel) => {
                if (channel.raw.managed) return false;
                // 跳转目标不被持久化筛选吃掉：筛选是跨会话残留状态，
                // 把定位目标滤掉会让跳转静默失败
                if (channel.raw.id === targetedChannelId) return true;
                if (filters.type !== 'all' && channel.raw.type !== filters.type) return false;
                if (filters.status === 'enabled' && !channel.raw.enabled) return false;
                if (filters.status === 'disabled' && channel.raw.enabled) return false;
                if (filters.reserve === 'transit' && !channel.raw.is_reserve) return false;
                if (filters.reserve === 'charity' && channel.raw.is_reserve) return false;
                return true;
            }),
        [visibleChannels, filters.type, filters.status, filters.reserve, targetedChannelId],
    );

    const targetedManagedChannel = useMemo(
        () => visibleChannels.find((channel) => channel.raw.id === targetedChannelId && channel.raw.managed) ?? null,
        [visibleChannels, targetedChannelId],
    );

    // 跳转定位：目标行可能因 tab 切换动画（AnimatePresence mode="wait" 延迟挂载）、
    // 数据未加载或虚拟化窗口外在首次检查时不存在，必须轮询重试；放弃时也要清掉
    // pending，否则跳转意图永久悬挂并钉住搜索豁免
    useEffect(() => {
        if (!pendingChannelJump) return;
        if (activeTab !== 'manual') return;

        const channelId = pendingChannelJump.target.channelId;
        const requestId = pendingChannelJump.requestId;
        let cancelled = false;
        let attempts = 0;
        let timer: number | undefined;

        const tryLocate = () => {
            if (cancelled) return;
            const node = channelRowRefs.current.get(channelId);
            if (node) {
                node.scrollIntoView({ behavior: 'smooth', block: 'center' });
                flashChannelRow(channelId);
                clearPending(requestId);
                return;
            }
            attempts += 1;
            if (attempts > JUMP_LOCATE_MAX_ATTEMPTS) {
                clearPending(requestId);
                return;
            }
            timer = window.setTimeout(tryLocate, JUMP_LOCATE_RETRY_INTERVAL_MS);
        };

        timer = window.setTimeout(tryLocate, JUMP_LOCATE_RETRY_INTERVAL_MS);
        return () => {
            cancelled = true;
            if (timer !== undefined) window.clearTimeout(timer);
        };
    }, [pendingChannelJump, clearPending, flashChannelRow, activeTab]);

    // 类型 chips 计数基于全部普通渠道（不随筛选/搜索变化，保证各 chip 数稳定）
    const toolbarChannels = useMemo(
        () => (channelsData ?? []).map((item) => item.raw),
        [channelsData],
    );

    // 网格视图是虚拟化渲染：目标在窗口外时节点不存在，需要先编程式滚动到该行。
    // requestId 作为 token 保证对同一目标的重复跳转也能再次触发滚动。
    const gridScrollTarget = useMemo(() => {
        if (!pendingChannelJump || activeTab !== 'manual' || layout === 'table') return null;
        if (pendingChannelJump.target.channelId === targetedManagedChannel?.raw.id) return null;
        const index = visibleManualChannels.findIndex(
            (item) => item.raw.id === pendingChannelJump.target.channelId,
        );
        return index >= 0 ? { index, token: pendingChannelJump.requestId } : null;
    }, [pendingChannelJump, activeTab, layout, visibleManualChannels, targetedManagedChannel]);

    const renderChannelCard = useCallback((item: NonNullable<typeof channelsData>[number]) => (
        <div
            ref={(node) => setChannelRowRef(item.raw.id, node)}
            className={cn(
                'rounded-[1.75rem] transition-all',
                highlightedChannelId === item.raw.id && 'ring-2 ring-primary/35 ring-offset-2 ring-offset-background',
            )}
        >
            <Card channel={item.raw} stats={item.formatted} layout={layout === 'table' ? 'grid' : layout} />
        </div>
    ), [highlightedChannelId, layout, setChannelRowRef]);

    const manualColumnCompute = useCallback((width: number) => {
        if (layout === 'list') return 1;
        const MIN_CARD_WIDTH = 320;
        const GUTTER = 16;
        const cols = Math.floor((width + GUTTER) / (MIN_CARD_WIDTH + GUTTER));
        return Math.max(1, Math.min(6, cols));
    }, [layout]);

    const targetedSection = targetedManagedChannel ? (
        <section className="space-y-3 px-1 pb-4">
            <div>
                <div className="text-sm font-semibold">{t('targetedManagedTitle')}</div>
                <div className="text-xs text-muted-foreground">{t('targetedManagedHint')}</div>
            </div>
            {renderChannelCard(targetedManagedChannel)}
        </section>
    ) : undefined;

    const manualFooter = isLoading ? (
        <div className={cn('grid gap-4', layout === 'list' ? 'grid-cols-1' : 'md:grid-cols-2 lg:grid-cols-3')}>
            {Array.from({ length: layout === 'list' ? 2 : 3 }).map((_, index) => (
                <div key={index} className="h-56 animate-pulse rounded-3xl border border-border/70 bg-muted/40" />
            ))}
        </div>
    ) : null;

    const showTableView = layout === 'table';
    const manualEmpty = !isLoading && !error && visibleManualChannels.length === 0 && !targetedManagedChannel;

    const emptyBox = (
        <div className="rounded-3xl border border-border/70 bg-card/70 px-4 py-8 text-center text-sm text-muted-foreground">
            {t('empty')}
        </div>
    );
    const loadingBox = (
        <div className="rounded-3xl border border-border/70 bg-card/70 px-4 py-8 text-center text-sm text-muted-foreground">
            {t('loading')}
        </div>
    );

    // 错误态优先于视图分支，避免表格视图把加载失败误报成「没有渠道」
    const manualContent = error ? (
        <div className="rounded-3xl border border-destructive/30 bg-destructive/10 px-4 py-6 text-sm text-destructive">
            {t('loadFailed', { message: error.message })}
        </div>
    ) : showTableView ? (
        manualEmpty ? emptyBox : isLoading ? loadingBox : visibleManualChannels.length === 0 ? null : (
            <ChannelsTable
                items={visibleManualChannels}
                highlightedId={highlightedChannelId}
                // 跳转目标可能不在当前页：focusId 驱动表格自动翻页后，
                // 定位重试才能找到行节点（highlightedId 此时尚未设置）
                focusId={activeTab === 'manual' ? targetedChannelId : null}
                focusToken={pendingChannelJump?.requestId ?? null}
                registerRow={setChannelRowRef}
            />
        )
    ) : (
        <VirtualizedGrid
            items={visibleManualChannels}
            layout={layout}
            columns={manualColumnCompute}
            estimateItemHeight={216}
            header={targetedSection}
            footer={manualFooter ?? (manualEmpty ? emptyBox : null)}
            scrollToItem={gridScrollTarget}
            getItemKey={(item) => `channel-${item.raw.id}`}
            renderItem={renderChannelCard}
        />
    );

    return (
        <div className="flex h-full min-h-0 flex-col">
            <div className="relative flex-1 min-h-0">
                <AnimatePresence mode="wait" initial={false}>
                    <motion.div
                        key={activeTab}
                        initial={{ opacity: 0, y: 6 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: -4 }}
                        transition={{ duration: 0.18, ease: [0.4, 0, 0.2, 1] }}
                        className="absolute inset-0 flex flex-col min-h-0"
                    >
                        {activeTab === 'site' ? (
                            <SiteChannelSection
                                searchTerm={searchTerm}
                                sortField={sortField}
                                sortOrder={sortOrder}
                                // 表格布局仅普通渠道消费，站点 tab 回退卡片网格
                                layout={layout === 'table' ? 'grid' : layout}
                            />
                        ) : (
                            <div className="flex min-h-0 flex-1 flex-col">
                                <ChannelToolbar channels={toolbarChannels} />
                                {showTableView && targetedSection ? (
                                    <div className="px-1 pb-2">{targetedSection}</div>
                                ) : null}
                                {manualContent}
                            </div>
                        )}
                    </motion.div>
                </AnimatePresence>
            </div>
        </div>
    );
}
