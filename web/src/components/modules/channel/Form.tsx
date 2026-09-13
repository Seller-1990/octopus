'use client';

import {
    type AutoGroupType,
    type Channel,
    type ChannelProtocolPolicy,
    type ChannelWSMode,
} from '@/api/endpoints/channel';
import { Switch } from '@/components/ui/switch';
import { Button } from '@/components/ui/button';
import { useTranslations } from 'next-intl';
import { useEffect } from 'react';
import { FormBasicSection } from './form/FormBasicSection';
import { FormModelsSection } from './form/FormModelsSection';
import { FormNetworkSection } from './form/FormNetworkSection';
import { FormAdvancedSection } from './form/FormAdvancedSection';

export interface ChannelKeyFormItem {
    id?: number;
    enabled: boolean;
    channel_key: string;
    status_code?: number;
    last_use_time_stamp?: number;
    total_cost?: number;
    remark?: string;
}

export interface ChannelFormData {
    name: string;
    type: Channel['type'];
    base_urls: Channel['base_urls'];
    custom_header: Channel['custom_header'];
    ws_mode: ChannelWSMode;
    protocol_policy: ChannelProtocolPolicy;
    tls_fingerprint: Channel['tls_fingerprint'];
    allow_lossy: boolean;
    proxy_mode: Channel['proxy_mode'];
    proxy_config_id: number | null;
    param_override: string;
    keys: ChannelKeyFormItem[];
    model: string;
    custom_model: string;
    enabled: boolean;
    auto_sync: boolean;
    auto_group: AutoGroupType;
    match_regex: string;
}

export interface ChannelFormProps {
    formData: ChannelFormData;
    onFormDataChange: (data: ChannelFormData) => void;
    onSubmit: (event: React.FormEvent<HTMLFormElement>) => void;
    isPending: boolean;
    submitText: string;
    pendingText: string;
    onCancel?: () => void;
    cancelText?: string;
    idPrefix?: string;
}

// 表单壳：状态与提交按钮留在本文件，字段按分区拆在 form/ 下。
export function ChannelForm({
    formData,
    onFormDataChange,
    onSubmit,
    isPending,
    submitText,
    pendingText,
    onCancel,
    cancelText,
    idPrefix = 'channel',
}: ChannelFormProps) {
    const t = useTranslations('channel.form');

    // Ensure the form always shows at least 1 row for base_urls / keys / custom_header.
    // This avoids "empty list" UI and also keeps URL + APIKEY layout consistent.
    useEffect(() => {
        if (!formData.base_urls || formData.base_urls.length === 0) {
            onFormDataChange({ ...formData, base_urls: [{ url: '', delay: 0 }] });
            return;
        }
        if (!formData.keys || formData.keys.length === 0) {
            onFormDataChange({ ...formData, keys: [{ enabled: true, channel_key: '' }] });
            return;
        }
        if (!formData.custom_header || formData.custom_header.length === 0) {
            onFormDataChange({ ...formData, custom_header: [{ header_key: '', header_value: '' }] });
        }
    }, [formData, onFormDataChange]);

    return (
        <form onSubmit={onSubmit} className="space-y-4 px-1">
            <FormBasicSection formData={formData} onFormDataChange={onFormDataChange} idPrefix={idPrefix} />
            <FormModelsSection formData={formData} onFormDataChange={onFormDataChange} idPrefix={idPrefix} />
            <FormNetworkSection formData={formData} onFormDataChange={onFormDataChange} />
            <FormAdvancedSection formData={formData} onFormDataChange={onFormDataChange} idPrefix={idPrefix} />

            <div className="flex flex-wrap items-center justify-between gap-4 p-4 rounded-xl bg-muted/20 border border-border/50">
                <label className="flex items-center gap-2 cursor-pointer">
                    <Switch
                        checked={formData.enabled}
                        onCheckedChange={(checked) => onFormDataChange({ ...formData, enabled: checked })}
                    />
                    <span className="text-sm font-medium text-card-foreground">{t('enabled')}</span>
                </label>
                <div className="flex items-center gap-6">
                    <label className="flex items-center gap-2 cursor-pointer">
                        <Switch
                            checked={formData.auto_sync}
                            onCheckedChange={(checked) => onFormDataChange({ ...formData, auto_sync: checked })}
                        />
                        <span className="text-sm text-card-foreground">{t('autoSync')}</span>
                    </label>
                </div>
            </div>

            <div className={`flex flex-col gap-3 pt-2 ${onCancel ? 'sm:flex-row' : ''}`}>
                {onCancel && cancelText && (
                    <Button
                        type="button"
                        variant="secondary"
                        onClick={onCancel}
                        className="w-full sm:flex-1 rounded-2xl h-12"
                    >
                        {cancelText}
                    </Button>
                )}
                <Button
                    type="submit"
                    disabled={isPending}
                    className="w-full sm:flex-1 rounded-2xl h-12"
                >
                    {isPending ? pendingText : submitText}
                </Button>
            </div>
        </form>
    );
}
