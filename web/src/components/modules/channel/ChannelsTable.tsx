'use client';

import { useTranslations } from 'next-intl';
import { type Channel, useEnableChannel } from '@/api/endpoints/channel';
import { MorphingDialog, MorphingDialogTrigger, MorphingDialogContainer, MorphingDialogContent } from '@/components/ui/morphing-dialog';
import { Badge } from '@/components/ui/badge';
import { Switch } from '@/components/ui/switch';
import { toast } from '@/components/common/Toast';
import { CardContent } from './CardContent';
import { typeLabel } from './ChannelFilters';
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

export type ChannelListItem = { raw: Channel; formatted: StatsMetricsFormatted };

interface ChannelsTableProps {
    items: ChannelListItem[];
    highlightedId: number | null;
    registerRow: (id: number, node: HTMLElement | null) => void;
}

export function ChannelsTable({ items, highlightedId, registerRow }: ChannelsTableProps) {
    const t = useTranslations('channel.table');
    const tCard = useTranslations('channel.card');
    const tFilters = useTranslations('channel.filters');
    const tForm = useTranslations('channel.form');

    return (
        <div className="min-h-0 flex-1 overflow-y-auto rounded-2xl border border-border/70 bg-card/50">
            <Table>
                <TableHeader className="sticky top-0 z-10 bg-card/95 backdrop-blur">
                    <TableRow className="hover:bg-transparent">
                        <TableHead className="min-w-40">{t('name')}</TableHead>
                        <TableHead>{t('type')}</TableHead>
                        <TableHead>{t('status')}</TableHead>
                        <TableHead>{t('reserve')}</TableHead>
                        <TableHead className="text-right">{t('models')}</TableHead>
                        <TableHead className="text-right">{t('keys')}</TableHead>
                        <TableHead>{t('proxy')}</TableHead>
                        <TableHead className="text-right">{t('requests')}</TableHead>
                        <TableHead className="text-right">{t('cost')}</TableHead>
                    </TableRow>
                </TableHeader>
                <TableBody>
                    {items.map(({ raw: channel, formatted }) => (
                        <TableRow
                            key={channel.id}
                            ref={(node: HTMLTableRowElement | null) => registerRow(channel.id, node)}
                            className={cn(highlightedId === channel.id && 'bg-primary/10')}
                        >
                            <NameCell
                                channel={channel}
                                formatted={formatted}
                                viewDetailsLabel={tCard('viewDetails', { name: channel.name })}
                                managedBadge={tCard('managedBadge')}
                            />
                            <TableCell>
                                <Badge variant="outline" className="rounded-lg font-normal">
                                    {typeLabel(tForm, channel.type)}
                                </Badge>
                            </TableCell>
                            <TableCell>
                                <EnableSwitch
                                    channel={channel}
                                    enabledToast={tCard('toast.enabled')}
                                    disabledToast={tCard('toast.disabled')}
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
                        </TableRow>
                    ))}
                </TableBody>
            </Table>
        </div>
    );
}

function modelCount(channel: Channel): number {
    const split = (models: string) =>
        models.split(',').map((m) => m.trim()).filter(Boolean);
    return new Set([...split(channel.model), ...split(channel.custom_model)]).size;
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
}: {
    channel: Channel;
    enabledToast: string;
    disabledToast: string;
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
            aria-label={channel.enabled ? disabledToast : enabledToast}
        />
    );
}
