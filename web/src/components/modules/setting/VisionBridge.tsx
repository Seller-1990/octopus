'use client';

import { useState, type ChangeEvent } from 'react';
import { useTranslations } from 'next-intl';
import { CheckCircle2, Eye, EyeOff, FlaskConical, Key, Link, ScanEye, Sparkles, XCircle } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { toast } from '@/components/common/Toast';
import { SettingKey, useSetSetting } from '@/api/endpoints/setting';
import { useVisionBridgeTest, type VisionBridgeProbeResult } from '@/api/endpoints/visionbridge';
import { SettingCard, SettingRow, SettingSection, useSettingField, useSettingToggle } from './shared';
import type { ApiError } from '@/api/types';

/**
 * 视觉桥配置卡（原独立页降级）：给无视觉能力的模型补图像理解，
 * 属于"配置一次、之后不碰"的低频设置，嵌入设置页瀑布流而非占据主导航位。
 */
export function SettingVisionBridge() {
    const t = useTranslations('visionBridge');
    const tSetting = useTranslations('setting');

    const enabled = useSettingToggle(SettingKey.VisionBridgeEnabled);
    const model = useSettingField(SettingKey.VisionBridgeModel);
    const baseUrl = useSettingField(SettingKey.VisionBridgeBaseURL);
    const apiKey = useSettingField(SettingKey.VisionBridgeAPIKey);
    const fallbackModels = useSettingField(SettingKey.VisionBridgeFallbackModels);
    const setSetting = useSetSetting();

    const [showKey, setShowKey] = useState(false);
    const [results, setResults] = useState<VisionBridgeProbeResult[] | null>(null);
    const testMutation = useVisionBridgeTest();

    // 配置一变旧测试结果就过期，留在屏幕上会被误读成当前配置的结论
    const editField = (field: { setValue: (v: string) => void }) => (e: ChangeEvent<HTMLInputElement>) => {
        field.setValue(e.target.value);
        setResults(null);
    };

    // 已保存的 key 在列表接口里打码为空串，与「未编辑」不可区分，常规 onBlur
    // 提交会被 no-op 短路——切换到本地 Ollama 等无鉴权服务需要这个显式清除入口。
    const handleClearKey = () => {
        setSetting.mutate(
            { key: SettingKey.VisionBridgeAPIKey, value: '' },
            {
                onSuccess: () => {
                    apiKey.setValue('');
                    setResults(null);
                    toast.success(t('apiKey.cleared'));
                },
                onError: (error) => {
                    toast.error(tSetting('saveFailed'), { description: (error as ApiError)?.message });
                },
            }
        );
    };

    const handleTest = async () => {
        if (!model.value.trim() || !baseUrl.value.trim()) {
            toast.error(t('test.missingConfig'));
            return;
        }
        setResults(null);
        try {
            const res = await testMutation.mutateAsync({
                model: model.value,
                base_url: baseUrl.value,
                api_key: apiKey.value,
                fallback_models: fallbackModels.value,
            });
            setResults(res);
            if (res.every((r) => r.ok)) {
                toast.success(t('test.allPassed'));
            } else if (res.some((r) => r.ok)) {
                toast.warning(t('test.partialPassed'));
            } else {
                toast.error(t('test.allFailed'));
            }
        } catch (error) {
            toast.error(t('test.requestFailed'), { description: (error as ApiError)?.message });
        }
    };

    return (
        <SettingCard icon={ScanEye} title={t('title')} tooltip={t('description')}>
            <SettingRow icon={ScanEye} label={t('enabled.label')} tooltip={t('enabled.description')} htmlFor="vb-enabled">
            <Switch id="vb-enabled" checked={enabled.enabled} onCheckedChange={enabled.toggle} />
            </SettingRow>

            <SettingRow icon={Sparkles} label={t('model.label')} tooltip={t('model.description')} htmlFor="vb-model">
            <Input
                id="vb-model"
                value={model.value}
                onChange={editField(model)}
                onBlur={model.save}
                placeholder={t('model.placeholder')}
                className="w-64 rounded-xl"
            />
            </SettingRow>

            <SettingRow icon={Link} label={t('baseUrl.label')} tooltip={t('baseUrl.description')} htmlFor="vb-base-url">
            <Input
                id="vb-base-url"
                value={baseUrl.value}
                onChange={editField(baseUrl)}
                onBlur={baseUrl.save}
                placeholder={t('baseUrl.placeholder')}
                className="w-64 rounded-xl"
            />
            </SettingRow>

            <SettingRow icon={Key} label={t('apiKey.label')} tooltip={t('apiKey.description')} htmlFor="vb-api-key">
            <div className="flex w-64 flex-col items-end gap-1">
                <div className="relative w-full">
                    <Input
                        id="vb-api-key"
                        type={showKey ? 'text' : 'password'}
                        value={apiKey.value}
                        onChange={editField(apiKey)}
                        onBlur={apiKey.save}
                        placeholder={t('apiKey.placeholder')}
                        className="rounded-xl pr-9"
                        autoComplete="new-password"
                    />
                    <button
                        type="button"
                        onClick={() => setShowKey((v) => !v)}
                        className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                        aria-label={showKey ? t('apiKey.hide') : t('apiKey.show')}
                    >
                        {showKey ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                    </button>
                </div>
                <button
                    type="button"
                    onClick={handleClearKey}
                    disabled={setSetting.isPending}
                    className="text-[11px] text-muted-foreground underline-offset-2 hover:text-foreground hover:underline disabled:opacity-50"
                >
                    {t('apiKey.clear')}
                </button>
            </div>
            </SettingRow>

            <SettingRow icon={Sparkles} label={t('fallbackModels.label')} tooltip={t('fallbackModels.description')} htmlFor="vb-fallback">
            <Input
                id="vb-fallback"
                value={fallbackModels.value}
                onChange={editField(fallbackModels)}
                onBlur={fallbackModels.save}
                placeholder={t('fallbackModels.placeholder')}
                className="w-64 rounded-xl"
            />
            </SettingRow>

            <SettingSection title={t('test.title')} tooltip={t('test.description')} />

            <div className="flex items-center justify-between gap-4">
            <span className="text-xs text-muted-foreground">{t('test.hint')}</span>
            <Button
                onClick={handleTest}
                disabled={testMutation.isPending}
                className="rounded-xl"
                variant="outline"
            >
                <FlaskConical className="size-4" />
                {testMutation.isPending ? t('test.testing') : t('test.button')}
            </Button>
            </div>

            {results && (
            <div className="space-y-2">
                {results.map((r) => (
                    <div key={r.model} className="rounded-xl border border-border bg-muted/40 p-3 text-sm">
                        <div className="flex items-center gap-2">
                            {r.ok
                                ? <CheckCircle2 className="size-4 shrink-0 text-emerald-600 dark:text-emerald-400" />
                                : <XCircle className="size-4 shrink-0 text-red-600 dark:text-red-400" />}
                            <span className="font-medium">{r.model}</span>
                            <span className="ml-auto shrink-0 text-xs text-muted-foreground">{(r.latency_ms / 1000).toFixed(1)}s</span>
                        </div>
                        {r.ok && r.preview && (
                            <p className="mt-1.5 line-clamp-3 text-xs text-muted-foreground">{r.preview}</p>
                        )}
                        {!r.ok && r.error && (
                            <p className="mt-1.5 line-clamp-4 break-all text-xs text-red-600/80 dark:text-red-400/80">{r.error}</p>
                        )}
                    </div>
                ))}
            </div>
            )}
        </SettingCard>
    );
}
