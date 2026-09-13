'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { useQueryClient } from '@tanstack/react-query';
import { ChannelType, useLastSyncTime, useSyncChannel, type Channel } from '@/api/endpoints/channel';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
    MorphingDialog,
    MorphingDialogTrigger,
    MorphingDialogContainer,
    MorphingDialogContent,
} from '@/components/ui/morphing-dialog';
import {
    Tooltip,
    TooltipTrigger,
    TooltipContent,
} from '@/components/animate-ui/components/animate/tooltip';
import { useSearchStore, useToolbarViewOptionsStore } from '@/components/modules/toolbar';
import { toast } from '@/components/common/Toast';
import { CreateDialogContent } from './Create';
import { cn } from '@/lib/utils';
import { FilterX, LayoutGrid, List, Plus, RefreshCw, Search, Table2 } from 'lucide-react';
import {
    useChannelFiltersStore,
    type ChannelReserveFilter,
    type ChannelStatusFilter,
} from './filter-store';

const CHANNEL_TYPES: ChannelType[] = [
    ChannelType.OpenAIChat,
    ChannelType.OpenAIResponse,
    ChannelType.Anthropic,
    ChannelType.Gemini,
    ChannelType.Volcengine,
    ChannelType.OpenAIEmbedding,
];

export function typeLabel(tForm: (key: string) => string, type: ChannelType): string {
    switch (type) {
        case ChannelType.OpenAIChat:
            return tForm('typeOpenAIChat');
        case ChannelType.OpenAIResponse:
            return tForm('typeOpenAIResponse');
        case ChannelType.Anthropic:
            return tForm('typeAnthropic');
        case ChannelType.Gemini:
            return tForm('typeGemini');
        case ChannelType.Volcengine:
            return tForm('typeVolcengine');
        case ChannelType.OpenAIEmbedding:
            return tForm('typeOpenAIEmbedding');
    }
}

interface ChannelToolbarProps {
    channels: Channel[];
}

// AxonHub 式整页工具栏：类型 chips（带计数）+ 内联搜索 + 状态筛选 + 视图切换 + 主操作
export function ChannelToolbar({ channels }: ChannelToolbarProps) {
    const t = useTranslations('channel.page');
    const tForm = useTranslations('channel.form');
    const tFilters = useTranslations('channel.filters');
    const { type, status, reserve, setType, setStatus, setReserve, reset } = useChannelFiltersStore();
    const searchTerm = useSearchStore((s) => s.getSearchTerm('channel'));
    const setSearchTerm = useSearchStore((s) => s.setSearchTerm);
    const layout = useToolbarViewOptionsStore((s) => s.getLayout('channel'));
    const setLayout = useToolbarViewOptionsStore((s) => s.setLayout);

    const hasActiveFilters = type !== 'all' || status !== 'all' || reserve !== 'all' || searchTerm.trim() !== '';

    const manualChannels = channels.filter((c) => !c.managed);
    const typeCounts = new Map<ChannelType, number>();
    for (const channel of manualChannels) {
        typeCounts.set(channel.type, (typeCounts.get(channel.type) ?? 0) + 1);
    }

    return (
        <div className="space-y-3 px-1 pb-4">
            <div className="flex flex-wrap items-center gap-2">
                <TypeChip
                    active={type === 'all'}
                    label={t('chipsAll')}
                    count={manualChannels.length}
                    onClick={() => setType('all')}
                />
                {CHANNEL_TYPES.map((channelType) => (
                    <TypeChip
                        key={channelType}
                        active={type === channelType}
                        label={typeLabel(tForm, channelType)}
                        count={typeCounts.get(channelType) ?? 0}
                        onClick={() => setType(type === channelType ? 'all' : channelType)}
                    />
                ))}
            </div>

            <div className="flex flex-wrap items-center gap-2">
                <div className="relative min-w-48 flex-1">
                    <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                    <Input
                        type="text"
                        value={searchTerm}
                        onChange={(e) => setSearchTerm('channel', e.target.value)}
                        placeholder={t('searchPlaceholder')}
                        aria-label={t('searchPlaceholder')}
                        className="h-9 rounded-xl pl-9"
                    />
                </div>

                <Select value={status} onValueChange={(value) => setStatus(value as ChannelStatusFilter)}>
                    <SelectTrigger size="sm" aria-label={tFilters('status')} className="h-9 w-28 rounded-xl text-xs">
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent className="rounded-xl">
                        <SelectItem className="rounded-xl text-xs" value="all">{tFilters('statusAll')}</SelectItem>
                        <SelectItem className="rounded-xl text-xs" value="enabled">{tFilters('statusEnabled')}</SelectItem>
                        <SelectItem className="rounded-xl text-xs" value="disabled">{tFilters('statusDisabled')}</SelectItem>
                    </SelectContent>
                </Select>

                <Select value={reserve} onValueChange={(value) => setReserve(value as ChannelReserveFilter)}>
                    <SelectTrigger size="sm" aria-label={tFilters('reserve')} className="h-9 w-28 rounded-xl text-xs">
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent className="rounded-xl">
                        <SelectItem className="rounded-xl text-xs" value="all">{tFilters('reserveAll')}</SelectItem>
                        <SelectItem className="rounded-xl text-xs" value="charity">{tFilters('charity')}</SelectItem>
                        <SelectItem className="rounded-xl text-xs" value="transit">{tFilters('transit')}</SelectItem>
                    </SelectContent>
                </Select>

                {hasActiveFilters && (
                    <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        onClick={() => {
                            reset();
                            setSearchTerm('channel', '');
                        }}
                        className="h-9 rounded-xl px-2 text-xs text-muted-foreground hover:text-foreground"
                    >
                        <FilterX className="size-3.5" />
                        {tFilters('reset')}
                    </Button>
                )}

                <div className="ml-auto flex flex-wrap items-center gap-2">
                    <ViewSwitcher layout={layout} onChange={setLayout} />

                    <SyncModelsButton />

                    <CreateChannelButton label={t('create')} />
                </div>
            </div>
        </div>
    );
}

function TypeChip({
    active,
    label,
    count,
    onClick,
}: {
    active: boolean;
    label: string;
    count: number;
    onClick: () => void;
}) {
    return (
        <button
            type="button"
            onClick={onClick}
            aria-pressed={active}
            className={cn(
                'inline-flex h-8 items-center gap-1.5 rounded-full border px-3 text-xs font-medium transition-colors',
                active
                    ? 'border-primary/30 bg-primary text-primary-foreground'
                    : 'border-border bg-card text-foreground hover:bg-muted/50'
            )}
        >
            <span>{label}</span>
            <span
                className={cn(
                    'inline-flex h-4 min-w-4 items-center justify-center rounded-full px-1 text-[10px] tabular-nums',
                    active ? 'bg-primary-foreground/20' : 'bg-muted text-muted-foreground'
                )}
            >
                {count}
            </span>
        </button>
    );
}

const VIEW_OPTIONS = [
    { value: 'grid', icon: LayoutGrid, labelKey: 'grid' },
    { value: 'list', icon: List, labelKey: 'list' },
    { value: 'table', icon: Table2, labelKey: 'table' },
] as const;

function ViewSwitcher({
    layout,
    onChange,
}: {
    layout: string;
    onChange: (page: 'channel', value: 'grid' | 'list' | 'table') => void;
}) {
    const t = useTranslations('toolbar.popover');

    return (
        <div
            role="group"
            aria-label={t('layout')}
            className="flex h-9 items-center gap-0.5 rounded-xl border border-border bg-card p-0.5"
        >
            {VIEW_OPTIONS.map(({ value, icon: Icon, labelKey }) => (
                <button
                    key={value}
                    type="button"
                    onClick={() => onChange('channel', value)}
                    aria-label={t(labelKey)}
                    aria-pressed={layout === value}
                    title={t(labelKey)}
                    className={cn(
                        'inline-flex size-7 items-center justify-center rounded-lg transition-colors',
                        layout === value
                            ? 'bg-primary text-primary-foreground'
                            : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground'
                    )}
                >
                    <Icon className="size-3.5" />
                </button>
            ))}
        </div>
    );
}

function SyncModelsButton() {
    const t = useTranslations('channel.filters');
    const queryClient = useQueryClient();
    const syncChannel = useSyncChannel();
    const { data: lastSyncTime } = useLastSyncTime();

    // 后端未同步过时返回零值时间（"0001-01-01..."），按从未同步处理
    const parsedSync = lastSyncTime ? new Date(lastSyncTime) : null;
    const hasSynced = parsedSync && !Number.isNaN(parsedSync.getTime()) && parsedSync.getFullYear() > 2000;
    const lastSyncLabel = hasSynced
        ? t('lastSyncAt', { time: parsedSync.toLocaleString() })
        : t('neverSynced');

    const handleSync = () => {
        syncChannel.mutate(undefined, {
            onSuccess: () => {
                // 同步为阻塞接口，返回即已完成；模型列表已在服务端更新
                queryClient.invalidateQueries({ queryKey: ['channels', 'list'] });
                toast.success(t('syncSuccess'));
            },
            onError: (error) => {
                toast.error(t('syncFailed'), { description: error.message });
            },
        });
    };

    return (
        <Tooltip>
            <TooltipTrigger asChild>
                <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="h-9 rounded-xl px-3 text-xs"
                    disabled={syncChannel.isPending}
                    onClick={handleSync}
                    aria-label={t('syncModels')}
                >
                    <RefreshCw className={`size-3.5 ${syncChannel.isPending ? 'animate-spin' : ''}`} />
                    {t('syncModels')}
                </Button>
            </TooltipTrigger>
            <TooltipContent>
                <span className="block">{lastSyncLabel}</span>
                <span className="block text-muted-foreground">{t('syncHint')}</span>
            </TooltipContent>
        </Tooltip>
    );
}

function CreateChannelButton({ label }: { label: string }) {
    const [open, setOpen] = useState(false);

    return (
        <MorphingDialog open={open} onOpenChange={setOpen}>
            <MorphingDialogTrigger
                aria-label={label}
                className="inline-flex h-9 items-center gap-1.5 rounded-xl bg-primary px-3.5 text-xs font-medium text-primary-foreground transition-colors hover:bg-primary/90"
            >
                <Plus className="size-3.5" />
                {label}
            </MorphingDialogTrigger>
            <MorphingDialogContainer>
                <MorphingDialogContent className="flex max-h-[calc(100vh-2rem)] w-fit max-w-full flex-col overflow-hidden rounded-3xl bg-card px-6 py-4 text-card-foreground custom-shadow">
                    <CreateDialogContent />
                </MorphingDialogContent>
            </MorphingDialogContainer>
        </MorphingDialog>
    );
}
