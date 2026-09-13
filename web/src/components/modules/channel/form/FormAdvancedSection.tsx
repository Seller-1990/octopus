'use client';

import {
    type ChannelProtocolPolicy,
    type ChannelWSMode,
    ChannelType,
} from '@/api/endpoints/channel';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { useTranslations } from 'next-intl';
import { CHANNEL_UA_PRESETS, currentUAHeader, setUAHeader } from '@/lib/ua-presets';
import { X, Plus } from 'lucide-react';
import {
    Accordion,
    AccordionContent,
    AccordionItem,
    AccordionTrigger,
} from '@/components/ui/accordion';
import type { ChannelFormData } from '../Form';

interface Props {
    formData: ChannelFormData;
    onFormDataChange: (data: ChannelFormData) => void;
    idPrefix: string;
}

export function FormAdvancedSection({ formData, onFormDataChange, idPrefix }: Props) {
    const t = useTranslations('channel.form');
    const currentUA = currentUAHeader(formData.custom_header)?.header_value ?? '';

    const handleAddHeader = () => {
        onFormDataChange({
            ...formData,
            custom_header: [...(formData.custom_header ?? []), { header_key: '', header_value: '' }],
        });
    };

    const handleUpdateHeader = (idx: number, patch: Partial<ChannelFormData['custom_header'][number]>) => {
        const next = (formData.custom_header ?? []).map((h, i) => (i === idx ? { ...h, ...patch } : h));
        onFormDataChange({ ...formData, custom_header: next });
    };

    const handleRemoveHeader = (idx: number) => {
        const curr = formData.custom_header ?? [];
        if (curr.length <= 1) return;
        onFormDataChange({ ...formData, custom_header: curr.filter((_, i) => i !== idx) });
    };

    return (
        <Accordion type="single" collapsible className="w-full border rounded-xl bg-card">
            <AccordionItem value="advanced" className="border-none">
                <AccordionTrigger className="text-sm font-medium text-card-foreground py-3 px-4 hover:no-underline hover:bg-muted/30 rounded-xl transition-colors">
                    {t('advanced')}
                </AccordionTrigger>
                <AccordionContent className="pt-4 px-4 pb-4 space-y-4 border-t">
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        <div className="space-y-2">
                            <label htmlFor={`${idPrefix}-protocol-policy`} className="text-sm font-medium text-card-foreground">
                                {t('protocolPolicy')}
                            </label>
                            <Select
                                value={formData.protocol_policy}
                                onValueChange={(value) =>
                                    onFormDataChange({
                                        ...formData,
                                        protocol_policy: value as ChannelProtocolPolicy,
                                    })
                                }
                            >
                                <SelectTrigger
                                    id={`${idPrefix}-protocol-policy`}
                                    className="w-full rounded-xl border border-border px-4 py-2 text-foreground"
                                >
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent className="rounded-xl">
                                    <SelectItem className="rounded-xl" value="auto">{t('protocolPolicyAuto')}</SelectItem>
                                    <SelectItem className="rounded-xl" value="passthrough-only">{t('protocolPolicyPassthrough')}</SelectItem>
                                    <SelectItem className="rounded-xl" value="transform-allowed">{t('protocolPolicyTransform')}</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>

                        <div className="space-y-2">
                            <label htmlFor={`${idPrefix}-tls-fingerprint`} className="text-sm font-medium text-card-foreground">
                                {t('tlsFingerprint')}
                            </label>
                            <Select
                                value={formData.tls_fingerprint || 'none'}
                                onValueChange={(value) =>
                                    onFormDataChange({
                                        ...formData,
                                        tls_fingerprint: value === 'none' ? '' : value as ChannelFormData['tls_fingerprint'],
                                    })
                                }
                            >
                                <SelectTrigger
                                    id={`${idPrefix}-tls-fingerprint`}
                                    className="w-full rounded-xl border border-border px-4 py-2 text-foreground"
                                >
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent className="rounded-xl">
                                    <SelectItem className="rounded-xl" value="none">{t('tlsFingerprintNone')}</SelectItem>
                                    <SelectItem className="rounded-xl" value="chrome">{t('tlsFingerprintChrome')}</SelectItem>
                                    <SelectItem className="rounded-xl" value="firefox">{t('tlsFingerprintFirefox')}</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>

                        <div className="flex min-h-16 items-center justify-between gap-4 rounded-xl border border-border px-4 py-3">
                            <label htmlFor={`${idPrefix}-allow-lossy`} className="text-sm font-medium text-card-foreground">
                                {t('allowLossy')}
                            </label>
                            <Switch
                                id={`${idPrefix}-allow-lossy`}
                                checked={formData.allow_lossy}
                                onCheckedChange={(checked) =>
                                    onFormDataChange({ ...formData, allow_lossy: checked })
                                }
                            />
                        </div>

                        {formData.type === ChannelType.OpenAIResponse ? (
                            <div className="space-y-2">
                                <label htmlFor={`${idPrefix}-ws-mode`} className="text-sm font-medium text-card-foreground">
                                    {t('wsMode')}
                                </label>
                                <Select
                                    value={formData.ws_mode ?? 'inherit'}
                                    onValueChange={(value) => onFormDataChange({ ...formData, ws_mode: value as ChannelWSMode })}
                                >
                                    <SelectTrigger id={`${idPrefix}-ws-mode`} className="rounded-xl w-full border border-border px-4 py-2 text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
                                        <SelectValue />
                                    </SelectTrigger>
                                    <SelectContent className='rounded-xl'>
                                        <SelectItem className='rounded-xl' value="inherit">{t('wsModeInherit')}</SelectItem>
                                        <SelectItem className='rounded-xl' value="passthrough">{t('wsModePassthrough')}</SelectItem>
                                        <SelectItem className='rounded-xl' value="transform">{t('wsModeTransform')}</SelectItem>
                                        <SelectItem className='rounded-xl' value="off">{t('wsModeOff')}</SelectItem>
                                    </SelectContent>
                                </Select>
                            </div>
                        ) : null}

                    </div>

                    <div className="space-y-2">
                        <div className="flex items-center justify-between">
                            <label className="text-sm font-medium text-card-foreground">
                                {t('uaPreset')}
                            </label>
                        </div>
                        <Select
                            value={CHANNEL_UA_PRESETS.includes(currentUA) ? currentUA : 'custom'}
                            onValueChange={(value) =>
                                onFormDataChange({
                                    ...formData,
                                    custom_header: setUAHeader(
                                        formData.custom_header,
                                        value === 'custom' ? '' : value,
                                    ),
                                })
                            }
                        >
                            <SelectTrigger
                                id={`${idPrefix}-ua-preset`}
                                className="rounded-xl w-full border border-border px-4 py-2 text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                            >
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent className='rounded-xl'>
                                <SelectItem className='rounded-xl' value="custom">{t('uaPresetCustom')}</SelectItem>
                                {CHANNEL_UA_PRESETS.map((preset) => (
                                    <SelectItem key={preset} className='rounded-xl' value={preset}>
                                        {preset}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>

                        <div className="flex items-center justify-between">
                            <label className="text-sm font-medium text-card-foreground">
                                {t('customHeader')} {formData.custom_header.length > 0 ? `(${formData.custom_header.length})` : ''}
                            </label>
                            <Button
                                type="button"
                                variant="ghost"
                                size="sm"
                                onClick={handleAddHeader}
                                className="h-6 px-2 text-xs text-muted-foreground/70 hover:text-muted-foreground hover:bg-transparent"
                            >
                                <Plus className="h-3 w-3 mr-1" />
                                {t('customHeaderAdd')}
                            </Button>
                        </div>
                        <div className="space-y-2">
                            {(formData.custom_header ?? []).map((h, idx) => (
                                <div key={`hdr-${idx}`} className="flex items-center gap-2">
                                    <Input
                                        type="text"
                                        value={h.header_key}
                                        onChange={(e) => handleUpdateHeader(idx, { header_key: e.target.value })}
                                        placeholder={t('customHeaderKey')}
                                        className="rounded-xl flex-1"
                                    />
                                    <Input
                                        type="text"
                                        value={h.header_value}
                                        onChange={(e) => handleUpdateHeader(idx, { header_value: e.target.value })}
                                        placeholder={t('customHeaderValue')}
                                        className="rounded-xl flex-1"
                                    />
                                    <Button
                                        type="button"
                                        variant="ghost"
                                        size="sm"
                                        onClick={() => handleRemoveHeader(idx)}
                                        disabled={(formData.custom_header ?? []).length <= 1}
                                        className="h-8 w-8 p-0 rounded-xl text-muted-foreground hover:text-destructive hover:bg-transparent disabled:opacity-40"
                                        title="Remove"
                                    >
                                        <X className="h-4 w-4" />
                                    </Button>
                                </div>
                            ))}
                        </div>
                    </div>

                    <div className="space-y-2">
                        <label htmlFor={`${idPrefix}-match-regex`} className="text-sm font-medium text-card-foreground">
                            {t('matchRegex')}
                        </label>
                        <Input
                            id={`${idPrefix}-match-regex`}
                            type="text"
                            value={formData.match_regex}
                            onChange={(e) => onFormDataChange({ ...formData, match_regex: e.target.value })}
                            placeholder={t('matchRegexPlaceholder')}
                            className="rounded-xl"
                        />
                    </div>

                    <div className="space-y-2">
                        <label htmlFor={`${idPrefix}-param-override`} className="text-sm font-medium text-card-foreground">
                            {t('paramOverride')}
                        </label>
                        <textarea
                            id={`${idPrefix}-param-override`}
                            value={formData.param_override}
                            onChange={(e) => onFormDataChange({ ...formData, param_override: e.target.value })}
                            placeholder={t('paramOverridePlaceholder')}
                            className="min-h-28 w-full rounded-xl border border-border bg-background px-3 py-2 text-sm text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                        />
                    </div>
                </AccordionContent>
            </AccordionItem>
        </Accordion>
    );
}
