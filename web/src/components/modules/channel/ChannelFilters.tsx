'use client';

import { useTranslations } from 'next-intl';
import { ChannelType } from '@/api/endpoints/channel';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { Button } from '@/components/ui/button';
import { FilterX } from 'lucide-react';
import {
    useChannelFiltersStore,
    type ChannelReserveFilter,
    type ChannelStatusFilter,
    type ChannelTypeFilter,
} from './filter-store';

const CHANNEL_TYPES: ChannelType[] = [
    ChannelType.OpenAIChat,
    ChannelType.OpenAIResponse,
    ChannelType.Anthropic,
    ChannelType.Gemini,
    ChannelType.Volcengine,
    ChannelType.OpenAIEmbedding,
];

export function ChannelFilters() {
    const t = useTranslations('channel.filters');
    const tForm = useTranslations('channel.form');
    const { type, status, reserve, setType, setStatus, setReserve, reset } = useChannelFiltersStore();

    const hasActiveFilters = type !== 'all' || status !== 'all' || reserve !== 'all';

    return (
        <div className="flex flex-wrap items-center gap-2 px-1 pb-3">
            <Select value={String(type)} onValueChange={(value) => setType((value === 'all' ? 'all' : Number(value)) as ChannelTypeFilter)}>
                <SelectTrigger size="sm" aria-label={t('type')} className="h-8 w-36 rounded-xl text-xs">
                    <SelectValue />
                </SelectTrigger>
                <SelectContent className="rounded-xl">
                    <SelectItem className="rounded-xl text-xs" value="all">{t('typeAll')}</SelectItem>
                    {CHANNEL_TYPES.map((channelType) => (
                        <SelectItem key={channelType} className="rounded-xl text-xs" value={String(channelType)}>
                            {typeLabel(tForm, channelType)}
                        </SelectItem>
                    ))}
                </SelectContent>
            </Select>

            <Select value={status} onValueChange={(value) => setStatus(value as ChannelStatusFilter)}>
                <SelectTrigger size="sm" aria-label={t('status')} className="h-8 w-32 rounded-xl text-xs">
                    <SelectValue />
                </SelectTrigger>
                <SelectContent className="rounded-xl">
                    <SelectItem className="rounded-xl text-xs" value="all">{t('statusAll')}</SelectItem>
                    <SelectItem className="rounded-xl text-xs" value="enabled">{t('statusEnabled')}</SelectItem>
                    <SelectItem className="rounded-xl text-xs" value="disabled">{t('statusDisabled')}</SelectItem>
                </SelectContent>
            </Select>

            <Select value={reserve} onValueChange={(value) => setReserve(value as ChannelReserveFilter)}>
                <SelectTrigger size="sm" aria-label={t('reserve')} className="h-8 w-32 rounded-xl text-xs">
                    <SelectValue />
                </SelectTrigger>
                <SelectContent className="rounded-xl">
                    <SelectItem className="rounded-xl text-xs" value="all">{t('reserveAll')}</SelectItem>
                    <SelectItem className="rounded-xl text-xs" value="charity">{t('charity')}</SelectItem>
                    <SelectItem className="rounded-xl text-xs" value="transit">{t('transit')}</SelectItem>
                </SelectContent>
            </Select>

            {hasActiveFilters && (
                <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={reset}
                    className="h-8 rounded-xl px-2 text-xs text-muted-foreground hover:text-foreground"
                >
                    <FilterX className="size-3.5" />
                    {t('reset')}
                </Button>
            )}
        </div>
    );
}

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
