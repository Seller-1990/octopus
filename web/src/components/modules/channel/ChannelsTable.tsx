'use client';

import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { ChannelType, useFetchModel, type Channel, useEnableChannel, useDeleteChannel } from '@/api/endpoints/channel';
import { MorphingDialog, MorphingDialogTrigger, MorphingDialogContainer, MorphingDialogContent } from '@/components/ui/morphing-dialog';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Switch } from '@/components/ui/switch';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import {
    AlertDialog,
    AlertDialogAction,
    AlertDialogCancel,
    AlertDialogContent,
    AlertDialogDescription,
    AlertDialogFooter,
    AlertDialogHeader,
    AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import {
    Tooltip,
    TooltipTrigger,
    TooltipContent,
} from '@/components/animate-ui/components/animate/tooltip';
import { toast } from '@/components/common/Toast';
import { CardContent } from './CardContent';
import { typeLabel } from './ChannelToolbar';
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from '@/components/ui/table';
import { cn } from '@/lib/utils';
import type { StatsMetricsFormatted } from '@/api/endpoints/stats';
import { ChevronLeft, ChevronRight, TestTube2, Trash2 } from 'lucide-react';

const BATCH_CHUNK_SIZE = 5;
const PAGE_SIZE_OPTIONS = [10, 20, 50, 100] as const;

export type ChannelListItem = { raw: Channel; formatted: StatsMetricsFormatted };

interface ChannelsTableProps {
    items: ChannelListItem[];
    highlightedId: number | null;
    registerRow: (id: number, node: HTMLElement | null) => void;
}

// 类型徽章配色（对齐 AxonHub 的彩色 provider 徽章）
const TYPE_BADGE_CLASS: Record<ChannelType, string> = {
    [ChannelType.OpenAIChat]: 'border-emerald-500/20 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400',
    [ChannelType.OpenAIResponse]: 'border-sky-500/20 bg-sky-500/10 text-sky-700 dark:text-sky-400',
    [ChannelType.Anthropic]: 'border-orange-500/20 bg-orange-500/10 text-orange-700 dark:text-orange-400',
    [ChannelType.Gemini]: 'border-blue-500/20 bg-blue-500/10 text-blue-700 dark:text-blue-400',
    [ChannelType.Volcengine]: 'border-violet-500/20 bg-violet-500/10 text-violet-700 dark:text-violet-400',
    [ChannelType.OpenAIEmbedding]: 'border-slate-500/20 bg-slate-500/10 text-slate-700 dark:text-slate-400',
};

export function ChannelsTable({ items, highlightedId, registerRow }: ChannelsTableProps) {
    const t = useTranslations('channel.table');
    const tPage = useTranslations('channel.page');
    const tCard = useTranslations('channel.card');
    const tFilters = useTranslations('channel.filters');
    const tForm = useTranslations('channel.form');

    const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set());
    const [isBatchBusy, setIsBatchBusy] = useState(false);
    const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);
    const [pageSize, setPageSize] = useState<number>(20);
    const [pageIndex, setPageIndex] = useState(0);

    const enableChannel = useEnableChannel();
    const deleteChannel = useDeleteChannel();

    const pageCount = Math.max(1, Math.ceil(items.length / pageSize));
    const safePageIndex = Math.min(pageIndex, pageCount - 1);
    const pageItems = useMemo(
        () => items.slice(safePageIndex * pageSize, (safePageIndex + 1) * pageSize),
        [items, safePageIndex, pageSize],
    );

    // 筛选/同步导致行集合变化后，清掉已不存在的选中项；批量执行期间冻结，
    // 否则第一条成功触发列表重取就会把操作条从用户眼前抽走
    useEffect(() => {
        if (isBatchBusy) return;
        setSelectedIds((prev) => {
            const valid = new Set(items.map((item) => item.raw.id));
            const next = new Set([...prev].filter((id) => valid.has(id)));
            return next.size === prev.size ? prev : next;
        });
    }, [items, isBatchBusy]);

    // 数据收缩后页码越界回退
    useEffect(() => {
        setPageIndex((prev) => Math.min(prev, pageCount - 1));
    }, [pageCount]);

    // 跳转定位目标不在当前页时自动翻页，让行挂载供定位重试命中
    useEffect(() => {
        if (!highlightedId) return;
        const index = items.findIndex((item) => item.raw.id === highlightedId);
        if (index < 0) return;
        setPageIndex(Math.floor(index / pageSize));
    }, [highlightedId, items, pageSize]);

    // managed 渠道只读，不可选中、不参与批量操作
    const selectableIds = items.filter((item) => !item.raw.managed).map((item) => item.raw.id);
    const allSelected = selectableIds.length > 0 && selectableIds.every((id) => selectedIds.has(id));
    const someSelected = selectableIds.some((id) => selectedIds.has(id));
    const headerCheckboxRef = useRef<HTMLInputElement>(null);

    useEffect(() => {
        if (headerCheckboxRef.current) {
            headerCheckboxRef.current.indeterminate = !allSelected && someSelected;
        }
    }, [allSelected, someSelected]);

    const toggleAll = () => {
        setSelectedIds(allSelected ? new Set() : new Set(selectableIds));
    };

    const toggleOne = (id: number) => {
        setSelectedIds((prev) => {
            const next = new Set(prev);
            if (next.has(id)) {
                next.delete(id);
            } else {
                next.add(id);
            }
            return next;
        });
    };

    // 分批并发：几十条选中时一口气打满后端只会放大失败数
    const runBatch = async (action: (id: number) => Promise<unknown>) => {
        const ids = [...selectedIds].filter((id) => selectableIds.includes(id));
        if (ids.length === 0) return;

        setIsBatchBusy(true);
        try {
            const results: PromiseSettledResult<unknown>[] = [];
            for (let i = 0; i < ids.length; i += BATCH_CHUNK_SIZE) {
                const chunk = ids.slice(i, i + BATCH_CHUNK_SIZE);
                results.push(...(await Promise.allSettled(chunk.map((id) => action(id)))));
            }
            const success = results.filter((r) => r.status === 'fulfilled').length;
            const failed = results.length - success;
            if (failed === 0) {
                toast.success(t('batchAllSuccess', { count: success }));
            } else {
                const firstError = results.find((r): r is PromiseRejectedResult => r.status === 'rejected');
                const reason = firstError?.reason instanceof Error ? firstError.reason.message : String(firstError?.reason ?? '');
                const message = failed === results.length
                    ? t('batchAllFailed', { count: failed })
                    : t('batchPartial', { success, failed });
                toast.error(message, { description: reason });
            }
            // 只清本次操作过的项：期间新勾选的属于用户后续意图，不该无提示吞掉
            setSelectedIds((prev) => {
                const next = new Set(prev);
                for (const id of ids) next.delete(id);
                return next;
            });
        } finally {
            setIsBatchBusy(false);
        }
    };

    const handleBatchSetEnabled = (enabled: boolean) => {
        void runBatch((id) => enableChannel.mutateAsync({ id, enabled }));
    };

    const handleBatchDelete = () => {
        setDeleteConfirmOpen(false);
        void runBatch((id) => deleteChannel.mutateAsync(id));
    };

    return (
        <div className="flex min-h-0 flex-1 flex-col gap-2">
            {(selectedIds.size > 0 || isBatchBusy) && (
                <div className="flex flex-wrap items-center gap-2 rounded-2xl border border-primary/30 bg-primary/5 px-3 py-2">
                    <span className="text-sm font-medium">{t('selectedCount', { count: selectedIds.size })}</span>
                    <div className="flex flex-wrap items-center gap-1.5">
                        <Button type="button" variant="outline" size="sm" className="h-7 rounded-lg text-xs" disabled={isBatchBusy} onClick={() => handleBatchSetEnabled(true)}>
                            {t('batchEnable')}
                        </Button>
                        <Button type="button" variant="outline" size="sm" className="h-7 rounded-lg text-xs" disabled={isBatchBusy} onClick={() => handleBatchSetEnabled(false)}>
                            {t('batchDisable')}
                        </Button>
                        <Button type="button" variant="destructive" size="sm" className="h-7 rounded-lg text-xs" disabled={isBatchBusy} onClick={() => setDeleteConfirmOpen(true)}>
                            <Trash2 className="size-3.5" />
                            {t('batchDelete')}
                        </Button>
                        <Button type="button" variant="ghost" size="sm" className="h-7 rounded-lg text-xs" disabled={isBatchBusy} onClick={() => setSelectedIds(new Set())}>
                            {t('cancelSelection')}
                        </Button>
                    </div>
                </div>
            )}

            <div className="min-h-0 flex-1 overflow-auto rounded-2xl border border-border/70 bg-card/50">
                <Table>
                    <TableHeader className="[&_th]:sticky [&_th]:top-0 [&_th]:z-10 [&_th]:bg-muted/60 [&_th]:backdrop-blur">
                        <TableRow className="hover:bg-transparent border-b">
                            <TableHead className="w-10 pr-0">
                                <input
                                    ref={headerCheckboxRef}
                                    type="checkbox"
                                    className="size-3.5 cursor-pointer accent-primary"
                                    checked={allSelected}
                                    onChange={toggleAll}
                                    aria-label={t('selectAll')}
                                    disabled={selectableIds.length === 0 || isBatchBusy}
                                />
                            </TableHead>
                            <TableHead className="min-w-40">{t('name')}</TableHead>
                            <TableHead>{t('type')}</TableHead>
                            <TableHead>{t('status')}</TableHead>
                            <TableHead>{t('reserve')}</TableHead>
                            <TableHead className="text-right">{t('models')}</TableHead>
                            <TableHead className="text-right">{t('keys')}</TableHead>
                            <TableHead>{t('proxy')}</TableHead>
                            <TableHead className="text-right">{t('requests')}</TableHead>
                            <TableHead className="text-right">{t('cost')}</TableHead>
                            <TableHead className="text-right">{t('actions')}</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {pageItems.map(({ raw: channel, formatted }) => (
                            <TableRow
                                key={channel.id}
                                ref={(node: HTMLTableRowElement | null) => registerRow(channel.id, node)}
                                className={cn(
                                    'transition-colors',
                                    (selectedIds.has(channel.id) || highlightedId === channel.id) && 'bg-primary/10',
                                )}
                            >
                                <TableCell className="pr-0">
                                    {!channel.managed && (
                                        <input
                                            type="checkbox"
                                            className="size-3.5 cursor-pointer accent-primary"
                                            checked={selectedIds.has(channel.id)}
                                            onChange={() => toggleOne(channel.id)}
                                            disabled={isBatchBusy}
                                            aria-label={t('selectOne', { name: channel.name })}
                                        />
                                    )}
                                </TableCell>
                                <NameCell
                                    channel={channel}
                                    formatted={formatted}
                                    viewDetailsLabel={tCard('viewDetails', { name: channel.name })}
                                    managedBadge={tCard('managedBadge')}
                                />
                                <TableCell>
                                    <Badge variant="outline" className={cn('rounded-lg font-normal', TYPE_BADGE_CLASS[channel.type])}>
                                        {typeLabel(tForm, channel.type)}
                                    </Badge>
                                </TableCell>
                                <TableCell>
                                    <EnableSwitch
                                        channel={channel}
                                        enabledToast={tCard('toast.enabled')}
                                        disabledToast={tCard('toast.disabled')}
                                        enableAria={tCard('enableChannel', { name: channel.name })}
                                        disableAria={tCard('disableChannel', { name: channel.name })}
                                    />
                                </TableCell>
                                <TableCell>
                                    <span className="text-xs text-muted-foreground">
                                        {channel.is_reserve ? tFilters('transit') : tFilters('charity')}
                                    </span>
                                </TableCell>
                                <TableCell className="text-right tabular-nums">{modelCount(channel)}</TableCell>
                                <TableCell className="text-right tabular-nums">
                                    {channel.keys.filter((k) => k.enabled).length}/{channel.keys.length}
                                </TableCell>
                                <TableCell>
                                    <span className="text-xs text-muted-foreground">
                                        {channel.proxy_mode === 'pool' ? t('proxyPool') : t('proxyDirect')}
                                    </span>
                                </TableCell>
                                <TableCell className="text-right tabular-nums">
                                    {formatted.request_count.formatted.value}
                                    <span className="ml-1 text-xs text-muted-foreground">{formatted.request_count.formatted.unit}</span>
                                </TableCell>
                                <TableCell className="text-right tabular-nums">
                                    {formatted.total_cost.formatted.value}
                                    <span className="ml-1 text-xs text-muted-foreground">{formatted.total_cost.formatted.unit}</span>
                                </TableCell>
                                <TableCell className="text-right">
                                    {!channel.managed && <TestButton channel={channel} labels={t} />}
                                </TableCell>
                            </TableRow>
                        ))}
                    </TableBody>
                </Table>
            </div>

            <div className="flex flex-wrap items-center justify-between gap-2 px-1 text-xs text-muted-foreground">
                <div className="flex items-center gap-3">
                    <span>{tPage('total', { count: items.length })}</span>
                    {selectedIds.size > 0 && <span>{t('selectedCount', { count: selectedIds.size })}</span>}
                </div>
                <div className="flex items-center gap-2">
                    <span>{tPage('rowsPerPage')}</span>
                    <Select
                        value={String(pageSize)}
                        onValueChange={(value) => {
                            setPageSize(Number(value));
                            setPageIndex(0);
                        }}
                    >
                        <SelectTrigger size="sm" aria-label={tPage('rowsPerPage')} className="h-7 w-16 rounded-lg text-xs">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent className="rounded-lg">
                            {PAGE_SIZE_OPTIONS.map((size) => (
                                <SelectItem key={size} className="rounded-lg text-xs" value={String(size)}>
                                    {size}
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                    <span className="tabular-nums">{tPage('pageStatus', { page: safePageIndex + 1, total: pageCount })}</span>
                    <div className="flex items-center gap-1">
                        <Button
                            type="button"
                            variant="outline"
                            size="icon"
                            className="size-7 rounded-lg"
                            disabled={safePageIndex === 0}
                            onClick={() => setPageIndex((prev) => Math.max(0, prev - 1))}
                            aria-label={tPage('prevPage')}
                        >
                            <ChevronLeft className="size-3.5" />
                        </Button>
                        <Button
                            type="button"
                            variant="outline"
                            size="icon"
                            className="size-7 rounded-lg"
                            disabled={safePageIndex >= pageCount - 1}
                            onClick={() => setPageIndex((prev) => Math.min(pageCount - 1, prev + 1))}
                            aria-label={tPage('nextPage')}
                        >
                            <ChevronRight className="size-3.5" />
                        </Button>
                    </div>
                </div>
            </div>

            <AlertDialog open={deleteConfirmOpen} onOpenChange={setDeleteConfirmOpen}>
                <AlertDialogContent className="rounded-3xl">
                    <AlertDialogHeader>
                        <AlertDialogTitle>{t('batchDeleteTitle')}</AlertDialogTitle>
                        <AlertDialogDescription>
                            {t('batchDeleteHint', { count: selectedIds.size })}
                        </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                        <AlertDialogCancel className="rounded-xl">{t('batchDeleteCancel')}</AlertDialogCancel>
                        <AlertDialogAction
                            className="rounded-xl bg-destructive text-white hover:bg-destructive/90"
                            disabled={isBatchBusy}
                            onClick={(event) => {
                                event.preventDefault();
                                handleBatchDelete();
                            }}
                        >
                            {t('batchDeleteConfirm')}
                        </AlertDialogAction>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>
        </div>
    );
}

function modelCount(channel: Channel): number {
    const split = (models: string) =>
        models.split(',').map((m) => m.trim()).filter(Boolean);
    return new Set([...split(channel.model), ...split(channel.custom_model)]).size;
}

// next-intl 的 t 支持携带插值参数调用，宽松签名便于向子组件传递
type TableT = (key: string, values?: Record<string, string | number>) => string;

function TestButton({ channel, labels }: { channel: Channel; labels: TableT }) {
    const fetchModel = useFetchModel();
    const effectiveKey = channel.keys.find((k) => k.enabled && k.channel_key.trim())?.channel_key.trim() ?? '';
    const canTest = Boolean(channel.base_urls?.[0]?.url) && Boolean(effectiveKey);

    const handleTest = () => {
        fetchModel.mutate(
            {
                type: channel.type,
                base_urls: channel.base_urls,
                keys: channel.keys
                    .filter((k) => k.channel_key.trim())
                    .map((k) => ({ enabled: k.enabled, channel_key: k.channel_key.trim() })),
                proxy_mode: channel.proxy_mode,
                proxy_config_id: channel.proxy_mode === 'pool' ? channel.proxy_config_id : null,
                match_regex: channel.match_regex || null,
                custom_header: channel.custom_header.filter((h) => h.header_key.trim()),
            },
            {
                onSuccess: (data) => {
                    toast.success(labels('testSuccess', { count: data?.length ?? 0 }));
                },
                onError: (error) => {
                    toast.error(labels('testFailed'), { description: error.message });
                },
            }
        );
    };

    return (
        <Tooltip>
            {/* disabled 按钮不派发 pointer 事件，tooltip 挂在 span 上才能解释禁用原因 */}
            <TooltipTrigger asChild>
                <span className="inline-flex">
                    <Button
                        type="button"
                        variant="outline"
                        size="icon"
                        className="size-7 rounded-full border-border/70 bg-card text-muted-foreground hover:text-primary"
                        disabled={!canTest || fetchModel.isPending}
                        onClick={handleTest}
                        aria-label={labels('test')}
                    >
                        <TestTube2 className={cn('size-3.5', fetchModel.isPending && 'animate-pulse')} />
                    </Button>
                </span>
            </TooltipTrigger>
            <TooltipContent>{canTest ? labels('test') : labels('testDisabled')}</TooltipContent>
        </Tooltip>
    );
}

function NameCell({
    channel,
    formatted,
    viewDetailsLabel,
    managedBadge,
}: {
    channel: Channel;
    formatted: StatsMetricsFormatted;
    viewDetailsLabel: string;
    managedBadge: string;
}) {
    return (
        <TableCell className="max-w-56">
            <MorphingDialog>
                <MorphingDialogTrigger className="max-w-full" aria-label={viewDetailsLabel}>
                    <span className="flex min-w-0 items-center gap-1.5">
                        <span className="truncate text-sm font-medium transition-colors hover:text-primary">
                            {channel.name}
                        </span>
                        {channel.managed && (
                            <Badge
                                variant="outline"
                                className="shrink-0 rounded-full border-amber-500/30 bg-amber-500/10 px-1.5 text-[10px] font-medium text-amber-700 dark:text-amber-300"
                            >
                                {managedBadge}
                            </Badge>
                        )}
                    </span>
                </MorphingDialogTrigger>
                <MorphingDialogContainer>
                    <MorphingDialogContent className="w-full md:max-w-xl bg-card text-card-foreground px-4 py-2 rounded-3xl max-h-[90dvh] overflow-y-auto">
                        <CardContent channel={channel} stats={formatted} />
                    </MorphingDialogContent>
                </MorphingDialogContainer>
            </MorphingDialog>
        </TableCell>
    );
}

function EnableSwitch({
    channel,
    enabledToast,
    disabledToast,
    enableAria,
    disableAria,
}: {
    channel: Channel;
    enabledToast: string;
    disabledToast: string;
    enableAria: string;
    disableAria: string;
}) {
    const enableChannel = useEnableChannel();

    const handleEnableChange = (checked: boolean) => {
        enableChannel.mutate(
            { id: channel.id, enabled: checked },
            {
                onSuccess: () => {
                    toast.success(checked ? enabledToast : disabledToast);
                },
                onError: (error) => {
                    toast.error(error.message);
                },
            }
        );
    };

    return (
        <Switch
            checked={channel.enabled}
            onCheckedChange={handleEnableChange}
            disabled={enableChannel.isPending || channel.managed}
            aria-label={channel.enabled ? disableAria : enableAria}
        />
    );
}
