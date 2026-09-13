import { useState } from 'react';
import {
    MorphingDialogClose,
    MorphingDialogTitle,
    MorphingDialogDescription,
    useMorphingDialog,
} from '@/components/ui/morphing-dialog';
import { useCreateChannel, ChannelType, AutoGroupType } from '@/api/endpoints/channel';
import { useTranslations } from 'next-intl';
import { toast } from '@/components/common/Toast';
import { ChannelForm, type ChannelFormData } from './Form';
import { PRESET_GROUP_ORDER, PROVIDER_PRESETS, type ProviderPresetGroup } from './provider-presets';
import {
    Select,
    SelectContent,
    SelectGroup,
    SelectItem,
    SelectLabel,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';

const GROUP_LABEL_KEYS: Record<ProviderPresetGroup, string> = {
    official: 'presetGroupOfficial',
    china: 'presetGroupChina',
    global: 'presetGroupGlobal',
    local: 'presetGroupLocal',
};

const EMPTY_FORM: ChannelFormData = {
    name: '',
    type: ChannelType.OpenAIChat,
    base_urls: [{ url: '', delay: 0 }],
    custom_header: [],
    ws_mode: 'inherit',
    protocol_policy: 'auto',
    tls_fingerprint: '',
    allow_lossy: false,
    proxy_mode: 'direct',
    proxy_config_id: null,
    param_override: '',
    keys: [{ enabled: true, channel_key: '', remark: '' }],
    model: '',
    custom_model: '',
    auto_sync: false,
    auto_group: AutoGroupType.None,
    enabled: true,
    match_regex: '',
};

export function CreateDialogContent() {
    const { setIsOpen } = useMorphingDialog();
    const createChannel = useCreateChannel();
    const [formData, setFormData] = useState<ChannelFormData>({ ...EMPTY_FORM });
    const t = useTranslations('channel.create');
    const tForm = useTranslations('channel.form');
    const tProxy = useTranslations('proxyPool');

    // 预设填充：名称留空才覆盖（尊重用户已输入的名称），URL/类型以预设为准
    const applyPreset = (presetId: string) => {
        const preset = PROVIDER_PRESETS.find((p) => p.id === presetId);
        if (!preset) return;
        setFormData((prev) => ({
            ...prev,
            name: prev.name.trim() === '' ? preset.name : prev.name,
            type: preset.type,
            base_urls: [{ url: preset.baseUrl, delay: 0 }],
        }));
    };

    const handleSubmit = (event: React.FormEvent<HTMLFormElement>) => {
        event.preventDefault();
        const normalizedBaseUrls = (formData.base_urls ?? []).filter((u) => u.url.trim()).map((u) => ({
            url: u.url.trim(),
            delay: Number(u.delay || 0),
        }));
        const normalizedKeys = formData.keys
            .filter((k) => k.channel_key.trim())
            .map((k) => ({ enabled: k.enabled, channel_key: k.channel_key, remark: k.remark ?? '' }));
        const normalizedHeaders = (formData.custom_header ?? [])
            .map((h) => ({ header_key: h.header_key.trim(), header_value: h.header_value }))
            .filter((h) => h.header_key && h.header_value !== '');

        const paramOverride = formData.param_override.trim();
        if (formData.proxy_mode === 'pool' && !formData.proxy_config_id) {
            toast.error(tProxy('selectRequired'));
            return;
        }
        createChannel.mutate(
            {
                name: formData.name,
                type: formData.type,
                enabled: formData.enabled,
                base_urls: normalizedBaseUrls,
                keys: normalizedKeys,
                model: formData.model,
                custom_model: formData.custom_model,
                proxy_mode: formData.proxy_mode,
                proxy_config_id: formData.proxy_mode === 'pool' ? formData.proxy_config_id : null,
                auto_sync: formData.auto_sync,
                auto_group: formData.auto_group,
                custom_header: normalizedHeaders,
                ws_mode: formData.ws_mode,
                protocol_policy: formData.protocol_policy,
                tls_fingerprint: formData.tls_fingerprint,
                allow_lossy: formData.allow_lossy,
                param_override: paramOverride,
                match_regex: formData.match_regex.trim(),
            },
            {
                onSuccess: () => {
                    setFormData({ ...EMPTY_FORM });
                    setIsOpen(false);
                },
                onError: (error) => {
                    toast.error(error.message);
                }
            });
    };

    return (
        <div className="w-screen max-w-full md:max-w-xl h-full min-h-0 flex flex-col">
            <MorphingDialogTitle className="shrink-0">
                <header className="mb-6 flex items-center justify-between">
                    <h2 className="text-2xl font-bold text-card-foreground">{t('dialogTitle')}</h2>
                    <MorphingDialogClose
                        className="relative right-0 top-0"
                        variants={{
                            initial: { opacity: 0, scale: 0.8 },
                            animate: { opacity: 1, scale: 1 },
                            exit: { opacity: 0, scale: 0.8 }
                        }}
                    />
                </header>
            </MorphingDialogTitle>
            <MorphingDialogDescription disableLayoutAnimation className="flex-1 min-h-0 overflow-auto">
                <div className="space-y-3">
                    <div className="space-y-2">
                        <label className="text-sm font-medium text-card-foreground">
                            {tForm('presetPlaceholder')}
                        </label>
                        <Select onValueChange={applyPreset}>
                            <SelectTrigger className="rounded-xl w-full border border-border px-4 py-2 text-foreground">
                                <SelectValue placeholder={tForm('presetPlaceholder')} />
                            </SelectTrigger>
                            <SelectContent className="rounded-xl">
                                {PRESET_GROUP_ORDER.map((group) => (
                                    <SelectGroup key={group}>
                                        <SelectLabel className="text-xs text-muted-foreground">
                                            {tForm(GROUP_LABEL_KEYS[group])}
                                        </SelectLabel>
                                        {PROVIDER_PRESETS.filter((p) => p.group === group).map((preset) => (
                                            <SelectItem key={preset.id} className="rounded-xl" value={preset.id}>
                                                {preset.name}
                                            </SelectItem>
                                        ))}
                                    </SelectGroup>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>
                    <ChannelForm
                        formData={formData}
                        onFormDataChange={setFormData}
                        onSubmit={handleSubmit}
                        isPending={createChannel.isPending}
                        submitText={t('submit')}
                        pendingText={t('submitting')}
                        idPrefix="new-channel"
                    />
                </div>
            </MorphingDialogDescription>
        </div>
    );
}
