'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useChannelList, type Channel } from '@/api/endpoints/channel';
import { Card } from './Card';
import { ChannelsTable } from './ChannelsTable';
import { ChannelFilters } from './ChannelFilters';
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
        window.setTimeout(() => {
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
                if (filters.type !== 'all' && channel.raw.type !== filters.type) return false;
                if (filters.status === 'enabled' && !channel.raw.enabled) return false;
                if (filters.status === 'disabled' && channel.raw.enabled) return false;
                if (filters.reserve === 'transit' && !channel.raw.is_reserve) return false;
                if (filters.reserve === 'charity' && channel.raw.is_reserve) return false;
                return true;
            }),
        [visibleChannels, filters.type, filters.status, filters.reserve],
    );

    const targetedManagedChannel = useMemo(
        () => visibleChannels.find((channel) => channel.raw.id === targetedChannelId && channel.raw.managed) ?? null,
        [visibleChannels, targetedChannelId],
    );

    useEffect(() => {
        if (!pendingChannelJump) return;
        if (activeTab !== 'manual') return;

        const channelId = pendingChannelJump.target.channelId;
        const node = channelRowRefs.current.get(channelId);
        if (!node) return;

        const timer = window.setTimeout(() => {
            node.scrollIntoView({ behavior: 'smooth', block: 'center' });
            flashChannelRow(channelId);
            clearPending(pendingChannelJump.requestId);
        }, 80);

        return () => window.clearTimeout(timer);
    }, [pendingChannelJump, clearPending, flashChannelRow, visibleManualChannels.length, targetedManagedChannel, activeTab]);

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
    ) : error ? (
        <div className="rounded-3xl border border-destructive/30 bg-destructive/10 px-4 py-6 text-sm text-destructive">
            {t('loadFailed', { message: error.message })}
        </div>
    ) : null;

    const showTableView = layout === 'table';
    const manualEmpty = !isLoading && !error && visibleManualChannels.length === 0 && !targetedManagedChannel;

    const manualContent = showTableView ? (
        manualEmpty ? (
            <div className="rounded-3xl border border-border/70 bg-card/70 px-4 py-8 text-center text-sm text-muted-foreground">
                {t('empty')}
            </div>
        ) : (
            <ChannelsTable
                items={visibleManualChannels}
                highlightedId={highlightedChannelId}
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
            footer={manualFooter ?? (manualEmpty ? (
                <div className="rounded-3xl border border-border/70 bg-card/70 px-4 py-8 text-center text-sm text-muted-foreground">
                    {t('empty')}
                </div>
            ) : null)}
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
                                <ChannelFilters />
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
