import { useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
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
    const queryClient = useQueryClient();
    const [formData, setFormData] = useState<ChannelFormData>({ ...EMPTY_FORM });
    // 预设选择器是「一次性命令」而非持久选中态：应用后立即清空显示，
    // 同一预设可重复选择，触发器也不会在用户手改后显示过期名称
    const [presetSelection, setPresetSelection] = useState('');
    const t = useTranslations('channel.create');
    const tForm = useTranslations('channel.form');
    const tProxy = useTranslations('proxyPool');

    // 名称溯源：预设填的名允许被下一个预设覆盖，用户手输名则尊重。
    // ChannelForm 的每次名称变更都会清掉该标志（applyPreset 例外，见下）
    const nameFromPresetRef = useRef(false);

    // 预设填充：名称为空或来自上一个预设时覆盖；地址仅替换第一行（保留
    // 用户已添加的其余行）；协议切换后与该协议无关的 ws_mode 脏值一并复位
    const applyPreset = (presetId: string) => {
        const preset = PROVIDER_PRESETS.find((p) => p.id === presetId);
        if (!preset) return;
        const fillName = formData.name.trim() === '' || nameFromPresetRef.current;
        setFormData((prev) => ({
            ...prev,
            name: fillName ? preset.name : prev.name,
            type: preset.type,
            ws_mode: preset.type === ChannelType.OpenAIResponse ? prev.ws_mode : 'inherit',
            base_urls: [
                { url: preset.baseUrl, delay: 0 },
                ...prev.base_urls.slice(1),
            ],
        }));
        nameFromPresetRef.current = fillName;
        setPresetSelection('');
    };

    const handleFormDataChange = (next: ChannelFormData) => {
        if (next.name !== formData.name) {
            nameFromPresetRef.current = false;
        }
        setFormData(next);
    };

    const handleSubmit = (event: React.FormEvent<HTMLFormElement>) => {
        event.preventDefault();
        // 统一口径：检查、提交、展示都以 trim 后的名称为准（HTML required 挡不住纯空格）
        const trimmedName = formData.name.trim();
        if (!trimmedName) {
            toast.error(t('nameRequired'));
            return;
        }
        const cachedChannels = queryClient.getQueryData<{ name?: string }[]>(['channels', 'list']);
        if (cachedChannels?.some((item) => item.name?.trim() === trimmedName)) {
            toast.error(t('duplicateName'));
            return;
        }
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
                name: trimmedName,
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
                <div className="space-y-4 px-1">
                    <div className="space-y-2">
                        <label htmlFor="channel-preset-select" className="text-sm font-medium text-card-foreground">
                            {tForm('presetPlaceholder')}
                        </label>
                        <Select value={presetSelection} onValueChange={applyPreset}>
                            <SelectTrigger id="channel-preset-select" className="rounded-xl w-full border border-border px-4 py-2 text-foreground">
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
                        <p className="text-xs text-muted-foreground">{tForm('presetHint')}</p>
                    </div>
                    <ChannelForm
                        formData={formData}
                        onFormDataChange={handleFormDataChange}
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
