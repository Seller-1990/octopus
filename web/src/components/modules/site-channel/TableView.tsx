"use client";

import { HistorySummary } from './HistorySummary';
import { SITE_ROUTE_COLUMN_ORDER, getRouteSourceTone, getRouteTypeTone, isSupportedRouteType } from './constants';
import { getGuessedRouteReason, getModelHistoryCount, getModelLastRequestAt, getUnknownRouteReason, makeModelKey, modelNeedsAttention } from './model-utils';
import { formatHistoryTime, routeSourceLabel, routeTypeLabel, type SiteModelView } from './utils';
import type { SiteChannelTableSort, SiteChannelTableSortField } from './ui-store';
import type { SiteModelRouteType } from '@/api/endpoints/site-channel';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import { CapabilityBadges } from '@/components/modules/model/CapabilityBadges';
import { Badge } from '@/components/ui/badge';
import { HoverCard, HoverCardContent, HoverCardTrigger } from '@/components/ui/hover-card';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { getModelIcon } from '@/lib/model-icons';
import { cn } from '@/lib/utils';
import { useVirtualizer } from '@tanstack/react-virtual';
import { ArrowUpDown, Check, CircleOff, History, MoreHorizontal, Trash2 } from 'lucide-react';
import { memo, useEffect, useRef, useState } from 'react';

function SelectionCheckbox({
    checked,
    onCheckedChange,
    disabled,
    ariaLabel,
    className,
}: {
    checked: boolean;
    onCheckedChange: (checked: boolean) => void;
    disabled?: boolean;
    ariaLabel: string;
    className?: string;
}) {
    return (
        <input
            type="checkbox"
            checked={checked}
            disabled={disabled}
            aria-label={ariaLabel}
            onChange={(event) => onCheckedChange(event.target.checked)}
            className={cn(
                'size-4 rounded border-border bg-background align-middle accent-primary disabled:cursor-not-allowed disabled:opacity-50',
                className,
            )}
        />
    );
}

function MoveRoutePopover({
    currentRouteType,
    disabled,
    buttonClassName,
    onMove,
}: {
    currentRouteType: SiteModelRouteType;
    disabled?: boolean;
    buttonClassName?: string;
    onMove: (routeType: SiteModelRouteType) => void;
}) {
    const [open, setOpen] = useState(false);

    return (
        <Popover open={open} onOpenChange={setOpen}>
            <PopoverTrigger asChild>
                <button
                    type="button"
                    disabled={disabled}
                    className={cn(
                        'rounded-lg p-1 text-muted-foreground transition hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50',
                        buttonClassName,
                    )}
                >
                    <MoreHorizontal className="size-4" />
                </button>
            </PopoverTrigger>
            <PopoverContent align="end" className="w-56 rounded-2xl border border-border/70 bg-card p-2 shadow-xl">
                <div className="space-y-2">
                    <div className="px-2 pt-1 text-xs font-medium text-muted-foreground">移动至...</div>
                    <div className="grid gap-1">
                        {SITE_ROUTE_COLUMN_ORDER.map((routeType) => (
                            <button
                                key={routeType}
                                type="button"
                                disabled={disabled || routeType === currentRouteType}
                                onClick={() => {
                                    onMove(routeType);
                                    setOpen(false);
                                }}
                                className={cn(
                                    'flex items-center justify-between rounded-xl px-2 py-2 text-left text-sm transition',
                                    routeType === currentRouteType
                                        ? 'bg-muted/60 text-muted-foreground'
                                        : 'hover:bg-muted',
                                )}
                            >
                                <span>{routeTypeLabel(routeType)}</span>
                                {routeType === currentRouteType ? <Check className="size-4" /> : null}
                            </button>
                        ))}
                    </div>
                </div>
            </PopoverContent>
        </Popover>
    );
}

// 10 columns: checkbox / 模型 / 分组 / 端点格式 / 来源 / Key / 状态 / 最近请求 / 渠道 / 操作.
// Shared by the sticky header row and every body row so columns stay aligned, and
// the explicit widths drive horizontal scroll inside the min-w-[74rem] block.
const SITE_CHANNEL_GRID_TEMPLATE =
    '3rem minmax(13rem,1.4fr) minmax(10rem,1fr) 9rem 6rem 5.5rem 7.5rem 9rem 6rem 7.5rem';

// Match VirtualizedGrid: measure layout height (offsetHeight) rather than the
// transformed visual height, so animations/scale don't shrink row measurements.
const measureRowHeight = (element: Element) =>
    element instanceof HTMLElement
        ? element.offsetHeight
        : element.getBoundingClientRect().height;

type SiteChannelTableViewProps = {
    models: SiteModelView[];
    resetKey: string;
    allVisibleSelected: boolean;
    pendingModelKeys: Set<string>;
    selectedModelKeys: Set<string>;
    compactMode: boolean;
    tableSort: SiteChannelTableSort;
    onToggleModelSelection: (modelKey: string, checked: boolean) => void;
    onToggleAllVisible: (checked: boolean) => void;
    onSortChange: (field: SiteChannelTableSortField) => void;
    onMoveModel: (model: SiteModelView, routeType: SiteModelRouteType) => void;
    onToggleDisabled: (model: SiteModelView) => void;
    onDeleteManualModel: (model: SiteModelView) => void;
    onNavigateToChannel: (channelId: number) => void;
};

export const SiteChannelTableView = ({
    models,
    resetKey,
    allVisibleSelected,
    pendingModelKeys,
    selectedModelKeys,
    compactMode,
    tableSort,
    onToggleModelSelection,
    onToggleAllVisible,
    onSortChange,
    onMoveModel,
    onToggleDisabled,
    onDeleteManualModel,
    onNavigateToChannel,
}: SiteChannelTableViewProps) => {
    'use no memo';

    const scrollRef = useRef<HTMLDivElement | null>(null);

    // eslint-disable-next-line react-hooks/incompatible-library
    const rowVirtualizer = useVirtualizer({
        count: models.length,
        getScrollElement: () => scrollRef.current,
        getItemKey: (index) => makeModelKey(models[index].group_key, models[index].model_name),
        estimateSize: () => (compactMode ? 44 : 64),
        measureElement: measureRowHeight,
        overscan: 8,
    });

    // Scroll back to the top whenever the filter / search / quick-filter scope changes.
    useEffect(() => {
        rowVirtualizer.scrollToIndex(0);
    }, [resetKey, rowVirtualizer]);

    // Re-estimate off-screen row heights when compact mode toggles; visible rows are
    // re-measured automatically via measureElement.
    useEffect(() => {
        rowVirtualizer.measure();
    }, [compactMode, rowVirtualizer]);

    const renderSortHead = (field: SiteChannelTableSortField, label: string) => (
        <button
            type="button"
            onClick={() => onSortChange(field)}
            className="inline-flex items-center gap-1 text-xs font-medium text-muted-foreground transition hover:text-foreground"
        >
            <span>{label}</span>
            <ArrowUpDown className={cn('size-3.5', tableSort.field === field && 'text-foreground')} />
        </button>
    );

    const cellPaddingClass = compactMode ? 'py-2' : 'py-3';

    return (
        <div
            ref={scrollRef}
            role="table"
            className="h-full w-full overflow-auto overscroll-contain"
        >
            <div className="min-w-[74rem]">
                <div
                    role="row"
                    className="sticky top-0 z-10 grid items-center gap-2 border-b border-border/70 bg-card px-4 py-2.5"
                    style={{ gridTemplateColumns: SITE_CHANNEL_GRID_TEMPLATE }}
                >
                    <div role="columnheader">
                        <SelectionCheckbox
                            checked={allVisibleSelected}
                            disabled={models.length === 0}
                            ariaLabel="选择当前可见模型"
                            onCheckedChange={onToggleAllVisible}
                        />
                    </div>
                    <div role="columnheader">{renderSortHead('model_name', '模型')}</div>
                    <div role="columnheader">{renderSortHead('group_name', '分组')}</div>
                    <div role="columnheader">{renderSortHead('route_type', '端点格式')}</div>
                    <div role="columnheader" className="text-xs font-medium text-muted-foreground">来源</div>
                    <div role="columnheader" className="text-xs font-medium text-muted-foreground">Key</div>
                    <div role="columnheader" className="text-xs font-medium text-muted-foreground">状态</div>
                    <div role="columnheader">{renderSortHead('last_request_at', '最近请求')}</div>
                    <div role="columnheader" className="text-xs font-medium text-muted-foreground">渠道</div>
                    <div role="columnheader" className="text-right text-xs font-medium text-muted-foreground">操作</div>
                </div>
                <div className="relative w-full" style={{ height: `${rowVirtualizer.getTotalSize()}px` }}>
                    {rowVirtualizer.getVirtualItems().map((virtualRow) => {
                        const model = models[virtualRow.index];
                        const modelKey = makeModelKey(model.group_key, model.model_name);
                        const { Avatar: ModelAvatar } = getModelIcon(model.model_name);
                        const isPending = pendingModelKeys.has(modelKey);
                        const isSelected = selectedModelKeys.has(modelKey);
                        const historyCount = getModelHistoryCount(model);

                        return (
                            <div
                                key={modelKey}
                                data-index={virtualRow.index}
                                ref={rowVirtualizer.measureElement}
                                role="row"
                                data-state={isSelected ? 'selected' : undefined}
                                className={cn(
                                    'absolute left-0 grid w-full items-center gap-2 border-b border-border/60 px-4',
                                    cellPaddingClass,
                                    isSelected && 'bg-muted/40',
                                    model.disabled && 'opacity-60',
                                    isPending && 'opacity-70',
                                )}
                                style={{
                                    top: `${virtualRow.start}px`,
                                    gridTemplateColumns: SITE_CHANNEL_GRID_TEMPLATE,
                                }}
                            >
                                <div role="cell" className="min-w-0">
                                    <SelectionCheckbox
                                        checked={isSelected}
                                        disabled={isPending}
                                        ariaLabel={`选择模型 ${model.model_name}`}
                                        onCheckedChange={(checked) => onToggleModelSelection(modelKey, checked)}
                                    />
                                </div>
                                <div role="cell" className="min-w-0">
                                    <div className="flex min-w-0 items-center gap-2">
                                        <ModelAvatar size={18} />
                                        <div className="min-w-0 flex-1">
                                            <div className="flex min-w-0 items-center gap-1.5">
                                                <span className="min-w-0 truncate text-sm font-medium">{model.model_name}</span>
                                                <CapabilityBadges capabilities={model.capabilities} size="xs" />
                                                {model.source === 'manual' ? (
                                                    <Badge variant="outline" className="h-5 shrink-0 px-1.5 text-[10px] border-primary/30 bg-primary/10 text-primary">自定义</Badge>
                                                ) : null}
                                            </div>
                                            {!compactMode ? (
                                                <div className="text-[11px] text-muted-foreground">
                                                    {model.manual_override ? '手动覆盖' : '自动映射'}
                                                </div>
                                            ) : null}
                                        </div>
                                    </div>
                                </div>
                                <div role="cell" className="min-w-0">
                                    <div className="max-w-[14rem] truncate text-sm">{model.group_name || model.group_key}</div>
                                </div>
                                <div role="cell" className="min-w-0">
                                    <div className="flex flex-wrap gap-1.5">
                                        <Badge variant="outline" className={cn('h-6 px-2 text-[11px]', getRouteTypeTone(model.route_type))}>
                                            {routeTypeLabel(model.route_type)}
                                        </Badge>
                                        {!isSupportedRouteType(model.route_type) ? (
                                            <Badge
                                                variant="outline"
                                                className="h-6 px-2 text-[11px] border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300"
                                                title={getUnknownRouteReason(model) ?? undefined}
                                            >
                                                待人工指定
                                            </Badge>
                                        ) : null}
                                        {isSupportedRouteType(model.route_type) && model.route_metadata?.route_guessed ? (
                                            <Badge
                                                variant="outline"
                                                className="h-6 px-2 text-[11px] border-sky-500/30 bg-sky-500/10 text-sky-700 dark:text-sky-300"
                                                title={getGuessedRouteReason(model) ?? undefined}
                                            >
                                                名称猜测
                                            </Badge>
                                        ) : null}
                                    </div>
                                </div>
                                <div role="cell" className="min-w-0">
                                    <Badge variant="outline" className={cn('h-6 px-2 text-[11px]', getRouteSourceTone(model.route_source))}>
                                        {routeSourceLabel(model.route_source)}
                                    </Badge>
                                </div>
                                <div role="cell" className="min-w-0">
                                    <div className="text-sm">
                                        {model.enabled_key_count}/{model.key_count}
                                    </div>
                                    {!model.has_keys ? (
                                        <div className="text-[11px] text-amber-700 dark:text-amber-300">缺少 Key</div>
                                    ) : null}
                                </div>
                                <div role="cell" className="min-w-0">
                                    <div className="flex flex-wrap gap-1.5">
                                        {model.disabled ? (
                                            <Badge variant="outline" className="h-6 px-2 text-[11px] border-destructive/30 bg-destructive/10 text-destructive">
                                                已禁用
                                            </Badge>
                                        ) : (
                                            <Badge variant="outline" className="h-6 px-2 text-[11px] border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300">
                                                已启用
                                            </Badge>
                                        )}
                                        {modelNeedsAttention(model) ? (
                                            <Badge variant="outline" className="h-6 px-2 text-[11px] border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300">
                                                待处理
                                            </Badge>
                                        ) : null}
                                    </div>
                                </div>
                                <div role="cell" className="min-w-0">
                                    <div className="text-sm">{formatHistoryTime(getModelLastRequestAt(model))}</div>
                                    <div className="text-[11px] text-muted-foreground">{historyCount} 次记录</div>
                                </div>
                                <div role="cell" className="min-w-0">
                                    {model.projected_channel_id ? (
                                        <button
                                            type="button"
                                            onClick={() => onNavigateToChannel(model.projected_channel_id!)}
                                            className="inline-flex rounded-full border border-border px-2 py-1 text-xs transition hover:border-primary/30 hover:bg-primary/5"
                                        >
                                            #{model.projected_channel_id}
                                        </button>
                                    ) : (
                                        <span className="text-sm text-muted-foreground">-</span>
                                    )}
                                </div>
                                <div role="cell" className="min-w-0">
                                    <div className="flex justify-end gap-1">
                                        <MoveRoutePopover
                                            currentRouteType={model.route_type}
                                            disabled={isPending || model.disabled}
                                            onMove={(routeType) => onMoveModel(model, routeType)}
                                        />
                                        <HoverCard>
                                            <HoverCardTrigger asChild>
                                                <button
                                                    type="button"
                                                    className="rounded-lg p-1 text-muted-foreground transition hover:bg-muted hover:text-foreground"
                                                >
                                                    <History className="size-4" />
                                                </button>
                                            </HoverCardTrigger>
                                            <HoverCardContent
                                                side="top"
                                                align="end"
                                                className="w-auto max-w-none rounded-2xl border border-border/70 bg-card p-0 shadow-xl"
                                            >
                                                <HistorySummary model={model} />
                                            </HoverCardContent>
                                        </HoverCard>
                                        {model.source === 'manual' ? (
                                            <button
                                                type="button"
                                                onClick={() => onDeleteManualModel(model)}
                                                disabled={isPending}
                                                className="rounded-lg p-1 text-muted-foreground transition hover:bg-destructive/10 hover:text-destructive disabled:opacity-50"
                                                title="删除自定义模型"
                                            >
                                                <Trash2 className="size-4" />
                                            </button>
                                        ) : null}
                                        <button
                                            type="button"
                                            onClick={() => onToggleDisabled(model)}
                                            disabled={isPending}
                                            className={cn(
                                                'rounded-lg p-1 transition',
                                                model.disabled
                                                    ? 'text-destructive hover:bg-destructive/10'
                                                    : 'text-muted-foreground hover:bg-muted hover:text-foreground',
                                            )}
                                        >
                                            <CircleOff className="size-4" />
                                        </button>
                                    </div>
                                </div>
                            </div>
                        );
                    })}
                </div>
            </div>
        </div>
    );
};
