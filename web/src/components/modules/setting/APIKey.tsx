'use client';

import { useCallback, useId, useMemo, useState } from 'react';
import { useTranslations } from 'next-intl';
import { KeyRound, Plus, Loader, Trash2, Check, X, Info, CalendarDays, Pencil, Maximize2, Share2, Search } from 'lucide-react';
import { motion, AnimatePresence } from 'motion/react';
import { Input } from '@/components/ui/input';
import { Calendar } from '@/components/ui/calendar';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Switch } from '@/components/ui/switch';
import { Badge } from '@/components/ui/badge';
import {
    MorphingDialog,
    MorphingDialogContainer,
    MorphingDialogContent,
    MorphingDialogDescription,
    MorphingDialogTitle,
    MorphingDialogTrigger,
    useMorphingDialog,
} from '@/components/ui/morphing-dialog';
import {
    useAPIKeyList,
    useCreateAPIKey,
    useUpdateAPIKey,
    useDeleteAPIKey,
    type APIKey,
} from '@/api/endpoints/apikey';
import { useGroupList } from '@/api/endpoints/group';
import { useStatsAPIKey } from '@/api/endpoints/stats';
import type { StatsAPIKeyFormatted } from '@/api/endpoints/stats';
import { useSettingValue, SettingKey } from '@/api/endpoints/setting';
import { useNavStore } from '@/components/modules/navbar/nav-store';
import { APIKeyExportOverlay } from './APIKeyExport';
import { OverlayPortal } from './OverlayPortal';
import { cn } from '@/lib/utils';
import { toast } from '@/components/common/Toast';
import { CopyIconButton } from '@/components/common/CopyButton';
import type { ApiError } from '@/api/types';

function toExpireAt(date: Date, time: string): number {
    const t = /^\d{2}:\d{2}$/.test(time) ? time : '00:00';
    const [hh, mm] = t.split(':').map(Number);
    const d = new Date(Date.UTC(date.getFullYear(), date.getMonth(), date.getDate(), hh, mm, 0));
    // 返回 Unix 时间戳（秒）
    return Math.floor(d.getTime() / 1000);
}

function parseExpireDate(expireAt?: number): Date | undefined {
    if (!expireAt) return undefined;
    // 从 Unix 时间戳（秒）转换为 Date
    const d = new Date(expireAt * 1000);
    return isNaN(d.getTime()) ? undefined : d;
}

function normalizeHHmm(input: string): string {
    const cleaned = input.replace(/[^\d:]/g, '');
    const parts = cleaned.includes(':') ? cleaned.split(':') : [cleaned.slice(0, 2), cleaned.slice(2, 4)];
    const hh = Math.min(23, Math.max(0, parseInt(parts[0] || '0', 10)));
    const mm = Math.min(59, Math.max(0, parseInt(parts[1] || '0', 10)));
    return `${hh.toString().padStart(2, '0')}:${mm.toString().padStart(2, '0')}`;
}

function normalizeMoneyInput(input: string): string {
    const cleaned = input.replace(/[^\d.]/g, '');
    const [intPart, ...rest] = cleaned.split('.');
    return rest.length > 0 ? `${intPart}.${rest.join('').slice(0, 6)}` : intPart;
}

function toggleModel(current: string | undefined, model: string): string | undefined {
    const models = current ? current.split(',').filter(Boolean) : [];
    const next = models.includes(model)
        ? models.filter((m) => m !== model)
        : [...models, model];
    return next.length ? next.join(',') : undefined;
}

function hasModel(supported: string | undefined, model: string): boolean {
    return supported ? supported.split(',').includes(model) : false;
}

interface APIKeyFormProps {
    apiKey?: APIKey;
    isPending: boolean;
    submitLabel: string;
    onSubmit: (data: Omit<APIKey, 'id' | 'api_key'>) => void;
    onClose: () => void;
}

function APIKeyForm({ apiKey, isPending, submitLabel, onSubmit, onClose }: APIKeyFormProps) {
    const t = useTranslations('setting');
    const { data: groups = [] } = useGroupList();

    const [form, setForm] = useState<Omit<APIKey, 'id' | 'api_key'>>(() => ({
        name: apiKey?.name ?? '',
        enabled: apiKey?.enabled ?? true,
        expire_at: apiKey?.expire_at,
        max_cost: apiKey?.max_cost,
        max_rpm: apiKey?.max_rpm,
        supported_models: apiKey?.supported_models,
        tools_only: apiKey?.tools_only ?? false,
        vision_bridge: apiKey?.vision_bridge ?? false,
        quota_limit: apiKey?.quota_limit ?? 0,
        quota_period: apiKey?.quota_period ?? 'monthly',
        quota_used: apiKey?.quota_used ?? 0,
        quota_reset_at: apiKey?.quota_reset_at,
    }));
    const [maxCostInput, setMaxCostInput] = useState(() =>
        apiKey?.max_cost != null ? String(apiKey.max_cost) : ''
    );
    const [maxRPMInput, setMaxRPMInput] = useState(() =>
        apiKey?.max_rpm != null ? String(apiKey.max_rpm) : ''
    );
    const [quotaLimitInput, setQuotaLimitInput] = useState(() =>
        apiKey?.quota_limit ? String(apiKey.quota_limit) : ''
    );
    const [expireTime, setExpireTime] = useState(() => {
        if (apiKey?.expire_at) {
            const d = new Date(apiKey.expire_at * 1000);
            if (!isNaN(d.getTime())) {
                return `${d.getUTCHours().toString().padStart(2, '0')}:${d.getUTCMinutes().toString().padStart(2, '0')}`;
            }
        }
        return '00:00';
    });
    const [expireOpen, setExpireOpen] = useState(false);

    const availableModels = useMemo(() => {
        const names = groups.map((g) => g.name).filter(Boolean);
        return Array.from(new Set(names)).sort((a, b) => a.localeCompare(b));
    }, [groups]);

    const expireDate = parseExpireDate(form.expire_at);
    const neverExpire = !form.expire_at;
    const isUnlimitedCost = maxCostInput.trim() === '';
    const isUnlimitedRPM = maxRPMInput.trim() === '';

    const expireLabel = neverExpire
        ? t('apiKey.form.neverExpire')
        : expireDate
            ? expireDate.toLocaleDateString()
            : t('apiKey.form.selectDate');

    const updateForm = useCallback((updater: Partial<Omit<APIKey, 'id' | 'api_key'>>) => {
        setForm((prev) => ({ ...prev, ...updater }));
    }, []);

    const handleSelectDate = useCallback((d: Date | undefined) => {
        if (d) {
            updateForm({ expire_at: toExpireAt(d, expireTime) });
            setExpireOpen(false);
        } else {
            updateForm({ expire_at: undefined });
        }
    }, [updateForm, expireTime]);

    const handleTimeBlur = useCallback(() => {
        if (!expireDate) return;
        const normalized = normalizeHHmm(expireTime);
        setExpireTime(normalized);
        updateForm({ expire_at: toExpireAt(expireDate, normalized) });
    }, [expireDate, expireTime, updateForm]);

    const handleToggleNeverExpire = useCallback(() => {
        if (neverExpire) {
            updateForm({ expire_at: toExpireAt(new Date(), expireTime) });
        } else {
            updateForm({ expire_at: undefined });
            setExpireOpen(false);
        }
    }, [neverExpire, expireTime, updateForm]);

    const handleMaxCostChange = useCallback((val: string) => {
        const normalized = normalizeMoneyInput(val);
        setMaxCostInput(normalized);
        const num = parseFloat(normalized);
        updateForm({ max_cost: Number.isFinite(num) ? num : undefined });
    }, [updateForm]);

    const handleClearMaxCost = useCallback(() => {
        setMaxCostInput('');
        updateForm({ max_cost: undefined });
    }, [updateForm]);

    const handleMaxRPMChange = useCallback((val: string) => {
        const cleaned = val.replace(/[^\d]/g, '');
        const num = parseInt(cleaned, 10);
        if (Number.isFinite(num) && num > 0) {
            setMaxRPMInput(cleaned);
            updateForm({ max_rpm: num });
        } else {
            setMaxRPMInput('');
            updateForm({ max_rpm: undefined });
        }
    }, [updateForm]);

    const handleClearMaxRPM = useCallback(() => {
        setMaxRPMInput('');
        updateForm({ max_rpm: undefined });
    }, [updateForm]);

    const handleQuotaLimitChange = useCallback((val: string) => {
        const normalized = normalizeMoneyInput(val);
        setQuotaLimitInput(normalized);
        const num = parseFloat(normalized);
        updateForm({ quota_limit: Number.isFinite(num) ? num : 0 });
    }, [updateForm]);

    const handleClearQuotaLimit = useCallback(() => {
        setQuotaLimitInput('');
        updateForm({ quota_limit: 0 });
    }, [updateForm]);

    const handleSubmit = useCallback((e: React.FormEvent) => {
        e.preventDefault();
        if (!form.name.trim()) return;
        onSubmit(form);
    }, [form, onSubmit]);

    return (
        <form onSubmit={handleSubmit} className="grid gap-2">
            <label className="grid gap-1 text-xs text-muted-foreground">
                {t('apiKey.form.name')}
                <Input
                    type="text"
                    value={form.name}
                    onChange={(e) => updateForm({ name: e.target.value })}
                    className="h-9 text-sm rounded-xl"
                    disabled={isPending}
                    required
                />
            </label>

            <div className="grid gap-1 text-xs text-muted-foreground">
                {t('apiKey.form.maxCost')}
                <div className="flex items-center gap-2">
                    <div className="relative flex-1">
                        <span className="absolute left-3 top-1/2 -translate-y-1/2 text-sm text-muted-foreground">$</span>
                        <Input
                            type="text"
                            inputMode="decimal"
                            placeholder={t('apiKey.form.maxCostPlaceholder')}
                            value={maxCostInput}
                            onChange={(e) => handleMaxCostChange(e.target.value)}
                            className="h-9 text-sm rounded-xl pl-7"
                            disabled={isPending}
                        />
                    </div>
                    <button
                        type="button"
                        onClick={handleClearMaxCost}
                        disabled={isPending}
                        aria-pressed={isUnlimitedCost}
                        className={cn(
                            'h-9 px-3 rounded-xl border text-sm transition-colors shrink-0',
                            isUnlimitedCost
                                ? 'bg-primary text-primary-foreground border-primary/30'
                                : 'border-border bg-muted/20 text-foreground hover:bg-muted/30',
                            isPending && 'opacity-50 cursor-not-allowed'
                        )}
                    >
                        {t('apiKey.form.unlimited')}
                    </button>
                </div>
            </div>

            <div className="grid gap-1 text-xs text-muted-foreground">
                {t('apiKey.form.maxRPM')}
                <div className="flex items-center gap-2">
                    <Input
                        type="text"
                        inputMode="numeric"
                        placeholder={t('apiKey.form.maxRPMPlaceholder')}
                        value={maxRPMInput}
                        onChange={(e) => handleMaxRPMChange(e.target.value)}
                        className="h-9 text-sm rounded-xl flex-1"
                        disabled={isPending}
                    />
                    <button
                        type="button"
                        onClick={handleClearMaxRPM}
                        disabled={isPending}
                        aria-pressed={isUnlimitedRPM}
                        className={cn(
                            'h-9 px-3 rounded-xl border text-sm transition-colors shrink-0',
                            isUnlimitedRPM
                                ? 'bg-primary text-primary-foreground border-primary/30'
                                : 'border-border bg-muted/20 text-foreground hover:bg-muted/30',
                            isPending && 'opacity-50 cursor-not-allowed'
                        )}
                    >
                        {t('apiKey.form.unlimited')}
                    </button>
                </div>
            </div>

            <div className="grid gap-1 text-xs text-muted-foreground">
                {t('apiKey.form.quotaLimit')}
                <div className="flex items-center gap-2">
                    <div className="relative flex-1">
                        <span className="absolute left-3 top-1/2 -translate-y-1/2 text-sm text-muted-foreground">$</span>
                        <Input
                            type="text"
                            inputMode="decimal"
                            placeholder={t('apiKey.form.quotaLimitPlaceholder')}
                            value={quotaLimitInput}
                            onChange={(e) => handleQuotaLimitChange(e.target.value)}
                            className="h-9 text-sm rounded-xl pl-7"
                            disabled={isPending}
                        />
                    </div>
                    <button
                        type="button"
                        onClick={handleClearQuotaLimit}
                        disabled={isPending}
                        aria-pressed={!quotaLimitInput.trim()}
                        className={cn(
                            'h-9 px-3 rounded-xl border text-sm transition-colors shrink-0',
                            !quotaLimitInput.trim()
                                ? 'bg-primary text-primary-foreground border-primary/30'
                                : 'border-border bg-muted/20 text-foreground hover:bg-muted/30',
                            isPending && 'opacity-50 cursor-not-allowed'
                        )}
                    >
                        {t('apiKey.form.unlimited')}
                    </button>
                </div>
                <div className="flex items-center gap-2">
                    <span className="shrink-0">{t('apiKey.form.quotaPeriod')}</span>
                    <select
                        value={form.quota_period}
                        onChange={(e) => updateForm({ quota_period: e.target.value as APIKey['quota_period'] })}
                        disabled={isPending}
                        className="h-9 flex-1 rounded-xl border border-border bg-muted/20 px-3 text-sm text-foreground"
                    >
                        <option value="daily">{t('apiKey.form.period.daily')}</option>
                        <option value="weekly">{t('apiKey.form.period.weekly')}</option>
                        <option value="monthly">{t('apiKey.form.period.monthly')}</option>
                    </select>
                </div>
                <div className="text-[11px] text-muted-foreground/80">{t('apiKey.form.quotaHint')}</div>
            </div>

            <div className="grid gap-1 text-xs text-muted-foreground">
                {t('apiKey.form.expireAt')}
                <div className="flex items-center gap-2 relative">
                    <Popover
                        open={expireOpen && !neverExpire}
                        onOpenChange={setExpireOpen}
                    >
                        <PopoverTrigger asChild>
                            <button
                                type="button"
                                disabled={isPending || neverExpire}
                                className="h-9 flex-1 flex items-center justify-between gap-2 rounded-xl border border-border bg-muted/20 px-3 text-sm text-foreground transition-colors hover:bg-muted/30 disabled:opacity-50"
                            >
                                <span className="truncate">{expireLabel}</span>
                                <CalendarDays className="size-4 text-muted-foreground" />
                            </button>
                        </PopoverTrigger>
                        <PopoverContent
                            align="start"
                            side="bottom"
                            sideOffset={8}
                            className="w-fit rounded-2xl border border-border/60 shadow-xl overflow-hidden bg-card p-0"
                        >
                            <Calendar
                                mode="single"
                                selected={expireDate}
                                onSelect={handleSelectDate}
                                disabled={isPending}
                                classNames={{ today: '' }}
                            />
                        </PopoverContent>
                    </Popover>

                    <Input
                        type="text"
                        value={expireTime}
                        onChange={(e) => setExpireTime(e.target.value.replace(/[^\d:]/g, '').slice(0, 5))}
                        onBlur={handleTimeBlur}
                        className="h-9 w-[92px] text-sm rounded-xl"
                        disabled={isPending || neverExpire || !expireDate}
                        inputMode="numeric"
                        placeholder="HH:mm"
                    />

                    <button
                        type="button"
                        onClick={handleToggleNeverExpire}
                        disabled={isPending}
                        aria-pressed={neverExpire}
                        className={cn(
                            'h-9 px-3 rounded-xl border text-sm transition-colors whitespace-nowrap shrink-0',
                            neverExpire
                                ? 'bg-primary text-primary-foreground border-primary/30'
                                : 'border-border bg-muted/20 text-foreground hover:bg-muted/30',
                            isPending && 'opacity-50 cursor-not-allowed'
                        )}
                    >
                        {t('apiKey.form.neverExpire')}
                    </button>
                </div>
            </div>

            <div className="grid gap-1">
                <div className="text-xs text-muted-foreground">{t('apiKey.form.supportedModels')}</div>
                <div className="max-h-40 overflow-auto rounded-xl p-2">
                    {availableModels.length === 0 ? (
                        <div className="text-xs text-muted-foreground py-2 text-center">
                            {t('apiKey.form.noModels')}
                        </div>
                    ) : (
                        <div className="flex flex-wrap gap-2">
                            {availableModels.map((m) => {
                                const checked = hasModel(form.supported_models, m);
                                return (
                                    <button
                                        key={m}
                                        type="button"
                                        disabled={isPending}
                                        onClick={() => updateForm({ supported_models: toggleModel(form.supported_models, m) })}
                                        className="text-left disabled:opacity-50"
                                    >
                                        <Badge
                                            variant={checked ? 'default' : 'outline'}
                                            className={cn(
                                                'cursor-pointer select-none',
                                                !checked && 'bg-background/40 hover:bg-background/70'
                                            )}
                                        >
                                            {m}
                                        </Badge>
                                    </button>
                                );
                            })}
                        </div>
                    )}
                </div>
                <div className="text-[11px] text-muted-foreground/80">{t('apiKey.form.modelsHint')}</div>
            </div>

            <div className="flex items-center justify-between pt-1">
                <span className="text-xs text-muted-foreground">{t('apiKey.form.enabled')}</span>
                <Switch
                    checked={form.enabled ?? true}
                    onCheckedChange={(checked) => updateForm({ enabled: checked })}
                    disabled={isPending}
                />
            </div>

            <div className="grid gap-1.5 pt-1">
                <div className="flex items-center justify-between">
                    <div className="flex items-center gap-1.5">
                        <span className="text-xs text-muted-foreground">{t('apiKey.form.toolsOnly')}</span>
                        <Info className="size-3.5 text-muted-foreground/70" />
                    </div>
                    <Switch
                        checked={form.tools_only ?? false}
                        onCheckedChange={(checked) => updateForm({ tools_only: checked })}
                        disabled={isPending}
                    />
                </div>
                <div className="text-[11px] text-muted-foreground/80">{t('apiKey.form.toolsOnlyHint')}</div>
            </div>

            <div className="grid gap-1.5 pt-1">
                <div className="flex items-center justify-between">
                    <div className="flex items-center gap-1.5">
                        <span className="text-xs text-muted-foreground">{t('apiKey.form.visionBridge')}</span>
                        <Info className="size-3.5 text-muted-foreground/70" />
                    </div>
                    <Switch
                        checked={form.vision_bridge ?? false}
                        onCheckedChange={(checked) => updateForm({ vision_bridge: checked })}
                        disabled={isPending}
                    />
                </div>
                <div className="text-[11px] text-muted-foreground/80">
                    {t('apiKey.form.visionBridgeHint')}{' '}
                    <button
                        type="button"
                        onClick={() => useNavStore.getState().setActiveItem('visionbridge')}
                        className="underline underline-offset-2 hover:text-foreground"
                    >
                        {t('apiKey.form.visionBridgeGoto')}
                    </button>
                </div>
            </div>

            <div className="flex gap-2 pt-2 mt-3">
                <button
                    type="button"
                    onClick={onClose}
                    disabled={isPending}
                    className="flex-1 h-9 flex items-center justify-center gap-1.5 rounded-xl bg-muted text-muted-foreground text-sm font-medium transition-all hover:bg-muted/80 active:scale-[0.98] disabled:opacity-50"
                >
                    <X className="size-4" />
                    {t('apiKey.form.cancel')}
                </button>
                <button
                    type="submit"
                    disabled={isPending || !form.name.trim()}
                    className="flex-1 h-9 flex items-center justify-center gap-1.5 rounded-xl bg-primary text-primary-foreground text-sm font-medium transition-all hover:bg-primary/90 active:scale-[0.98] disabled:opacity-50"
                >
                    {isPending ? <Loader className="size-4 animate-spin" /> : <Check className="size-4" />}
                    {submitLabel}
                </button>
            </div>
        </form>
    );
}

export function APIKeyFormOverlay({
    layoutId,
    apiKey,
    isPending,
    submitLabel,
    onSubmit,
    onClose,
}: {
    layoutId: string;
    apiKey?: APIKey;
    isPending: boolean;
    submitLabel: string;
    onSubmit: (data: Omit<APIKey, 'id' | 'api_key'>) => void;
    onClose: () => void;
}) {
    return (
        <OverlayPortal onClose={onClose}>
            <motion.div
                layoutId={layoutId}
                role="dialog"
                aria-modal="true"
                data-slot="dialog-content"
                className="fixed left-1/2 top-1/2 z-50 w-[min(420px,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 bg-card p-5 rounded-3xl border border-border max-h-[80vh] overflow-auto"
                transition={{ type: 'spring', stiffness: 400, damping: 30 }}
            >
                <APIKeyForm
                    apiKey={apiKey}
                    isPending={isPending}
                    submitLabel={submitLabel}
                    onSubmit={onSubmit}
                    onClose={onClose}
                />
            </motion.div>
        </OverlayPortal>
    );
}

export function APIKeyStatsCard({
    layoutId,
    apiKey,
    onClose,
}: {
    layoutId: string;
    apiKey: APIKey;
    onClose: () => void;
}) {
    const t = useTranslations('setting');
    const { data: statsList = [] } = useStatsAPIKey();
    const stats = useMemo(() => statsList.find((s) => s.api_key_id === apiKey.id), [statsList, apiKey.id]);
    const quotaPercent = apiKey.quota_limit > 0 ? Math.min(100, (apiKey.quota_used / apiKey.quota_limit) * 100) : 0;

    return (
        <OverlayPortal onClose={onClose}>
            <motion.div
                layoutId={layoutId}
                role="dialog"
                aria-modal="true"
                data-slot="dialog-content"
                className="fixed left-1/2 top-1/2 z-50 w-[min(320px,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 flex flex-col bg-card p-5 rounded-3xl border border-border max-h-[80vh] overflow-auto"
                transition={{ type: 'spring', stiffness: 400, damping: 30 }}
            >
                <div className="flex items-center justify-between gap-2 mb-3">
                    <h3 className="text-sm font-semibold text-card-foreground line-clamp-1">
                        {apiKey.name}
                    </h3>
                    <button
                        type="button"
                        onClick={onClose}
                        className="size-8 flex items-center justify-center rounded-lg bg-muted text-muted-foreground transition-colors hover:bg-muted/80"
                    >
                        <X className="size-4" />
                    </button>
                </div>

                {!stats ? (
                    <div className="text-sm text-muted-foreground">{t('apiKey.stats.noData')}</div>
                ) : (
                    <div className="space-y-3 text-sm">
                        <div className="rounded-lg border border-border/60 bg-muted/30 p-3">
                            <div className="flex items-center justify-between gap-2 text-xs text-muted-foreground">
                                <span>{t('apiKey.stats.quota')}</span>
                                <span>{apiKey.quota_limit > 0 ? `${apiKey.quota_used.toFixed(4)} / ${apiKey.quota_limit.toFixed(4)} $` : t('apiKey.stats.unlimited')}</span>
                            </div>
                            {apiKey.quota_limit > 0 ? (
                                <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-muted">
                                    <div className="h-full rounded-full bg-primary transition-[width]" style={{ width: `${quotaPercent}%` }} />
                                </div>
                            ) : null}
                        </div>
                        <div className="grid grid-cols-2 gap-3">
                            <div className="rounded-lg bg-muted/40 p-3">
                            <div className="text-xs text-muted-foreground">{t('apiKey.stats.inputToken')}</div>
                            <div className="font-medium tabular-nums">
                                {stats.input_token.formatted.value}
                                {stats.input_token.formatted.unit}
                            </div>
                            </div>
                            <div className="rounded-lg bg-muted/40 p-3">
                            <div className="text-xs text-muted-foreground">{t('apiKey.stats.outputToken')}</div>
                            <div className="font-medium tabular-nums">
                                {stats.output_token.formatted.value}
                                {stats.output_token.formatted.unit}
                            </div>
                            </div>
                            <div className="rounded-lg bg-muted/40 p-3">
                            <div className="text-xs text-muted-foreground">{t('apiKey.stats.inputCost')}</div>
                            <div className="font-medium tabular-nums">
                                {stats.input_cost.formatted.value}
                                {stats.input_cost.formatted.unit}
                            </div>
                            </div>
                            <div className="rounded-lg bg-muted/40 p-3">
                            <div className="text-xs text-muted-foreground">{t('apiKey.stats.outputCost')}</div>
                            <div className="font-medium tabular-nums">
                                {stats.output_cost.formatted.value}
                                {stats.output_cost.formatted.unit}
                            </div>
                            </div>
                            <div className="rounded-lg bg-muted/40 p-3">
                            <div className="text-xs text-muted-foreground">{t('apiKey.stats.requestSuccess')}</div>
                            <div className="font-medium tabular-nums">
                                {stats.request_success.formatted.value}
                                {stats.request_success.formatted.unit}
                            </div>
                            </div>
                            <div className="rounded-lg bg-muted/40 p-3">
                            <div className="text-xs text-muted-foreground">{t('apiKey.stats.requestFailed')}</div>
                            <div className="font-medium tabular-nums">
                                {stats.request_failed.formatted.value}
                                {stats.request_failed.formatted.unit}
                            </div>
                            </div>
                        </div>
                    </div>
                )}
            </motion.div>
        </OverlayPortal>
    );
}

function APIKeyKeyItem({
    apiKey,
    stats,
    statsLayoutId,
    editLayoutId,
    deleteLayoutId,
    exportLayoutId,
    onViewStats,
    onEdit,
    onDelete,
    onExport,
    isDeleting,
    onToggleEnabled,
    onToggleToolsOnly,
    onToggleVisionBridge,
}: {
    apiKey: APIKey;
    stats?: StatsAPIKeyFormatted;
    statsLayoutId: string;
    editLayoutId: string;
    deleteLayoutId: string;
    exportLayoutId: string;
    onViewStats: () => void;
    onEdit: () => void;
    onDelete: () => void;
    onExport?: () => void;
    isDeleting: boolean;
    /** 卡片内联开关：启用/禁用（不用进编辑）。 */
    onToggleEnabled: () => void;
    /** 卡片内联开关：仅 tools（不用进编辑）。 */
    onToggleToolsOnly: () => void;
    /** 卡片内联开关：视觉桥（不用进编辑）。 */
    onToggleVisionBridge: () => void;
}) {
    const t = useTranslations('setting');
    // key 开关只是三个生效条件之一（全局开关 ∧ key 开关 ∧ 模型已证实无视觉），
    // 全局未开时徽章灰化提示，避免暗示该 key 已具备视觉桥能力
    const { value: visionBridgeGlobal } = useSettingValue(SettingKey.VisionBridgeEnabled);
    const visionBridgeGloballyEnabled = visionBridgeGlobal === 'true';
    const [confirmDelete, setConfirmDelete] = useState(false);
    const quotaPercent = apiKey.quota_limit > 0
        ? Math.min(100, (apiKey.quota_used / apiKey.quota_limit) * 100)
        : 0;

    return (
        <motion.div
            layout
            initial={{ opacity: 0, scale: 0.95 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={{ opacity: 0, scale: 0.95, transition: { duration: 0.2 } }}
            transition={{ type: 'spring', stiffness: 500, damping: 30 }}
            className="group relative grid gap-3 rounded-xl bg-muted/50 p-3 overflow-hidden origin-top sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center"
        >
            <div className="min-w-0 space-y-2">
                <div className="flex min-w-0 items-center gap-2">
                    <span className="truncate text-sm font-medium">{apiKey.name}</span>
                    <Badge variant={apiKey.enabled ? 'default' : 'secondary'} className="shrink-0 text-[10px]">
                        {apiKey.enabled ? t('apiKey.status.enabled') : t('apiKey.status.disabled')}
                    </Badge>
                    {apiKey.tools_only && (
                        <Badge variant="outline" className="shrink-0 text-[10px] border-sky-500/40 text-sky-700 dark:text-sky-300">
                            {t('apiKey.badge.toolsOnly')}
                        </Badge>
                    )}
                    {apiKey.vision_bridge && (
                        <Badge
                            variant="outline"
                            className={cn(
                                'shrink-0 text-[10px]',
                                visionBridgeGloballyEnabled
                                    ? 'border-violet-500/40 text-violet-700 dark:text-violet-300'
                                    : 'border-border text-muted-foreground',
                            )}
                            title={visionBridgeGloballyEnabled ? undefined : t('apiKey.badge.visionBridgeInactive')}
                        >
                            {t('apiKey.badge.visionBridge')}
                        </Badge>
                    )}
                </div>
                <code className="block truncate text-xs text-muted-foreground">
                    {apiKey.api_key.slice(0, 12)}...{apiKey.api_key.slice(-4)}
                </code>
                <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
                    <span>{t('apiKey.stats.totalCost')}: {stats ? `${stats.total_cost.formatted.value}${stats.total_cost.formatted.unit}` : '-'}</span>
                    <span>{t('apiKey.stats.requestCount')}: {stats ? `${stats.request_count.formatted.value}${stats.request_count.formatted.unit}` : '-'}</span>
                    <span>
                        {t('apiKey.stats.quota')}: {apiKey.quota_limit > 0
                            ? `${apiKey.quota_used.toFixed(4)} / ${apiKey.quota_limit.toFixed(4)} $`
                            : t('apiKey.stats.unlimited')}
                    </span>
                </div>
                {apiKey.quota_limit > 0 ? (
                    <div className="h-1 overflow-hidden rounded-full bg-muted">
                        <div className="h-full rounded-full bg-primary transition-[width]" style={{ width: `${quotaPercent}%` }} />
                    </div>
                ) : null}
            </div>

            <div className="flex items-center justify-end gap-1.5">
                <div className="mr-1 flex shrink-0 flex-col items-end gap-1">
                    <label className="flex cursor-pointer items-center gap-1.5" title={t('apiKey.form.enabled')}>
                        <span className="text-[10px] text-muted-foreground">{t('apiKey.form.enabled')}</span>
                        <Switch checked={apiKey.enabled} onCheckedChange={onToggleEnabled} aria-label={t('apiKey.form.enabled')} />
                    </label>
                    <label className="flex cursor-pointer items-center gap-1.5" title={t('apiKey.form.toolsOnly')}>
                        <span className="text-[10px] text-muted-foreground">{t('apiKey.form.toolsOnly')}</span>
                        <Switch checked={apiKey.tools_only ?? false} onCheckedChange={onToggleToolsOnly} aria-label={t('apiKey.form.toolsOnly')} />
                    </label>
                    <label className="flex cursor-pointer items-center gap-1.5" title={t('apiKey.form.visionBridge')}>
                        <span className="text-[10px] text-muted-foreground">{t('apiKey.form.visionBridge')}</span>
                        <Switch checked={apiKey.vision_bridge ?? false} onCheckedChange={onToggleVisionBridge} aria-label={t('apiKey.form.visionBridge')} />
                    </label>
                </div>
                <motion.button
                    type="button"
                    layoutId={statsLayoutId}
                    onClick={onViewStats}
                    aria-label={t('apiKey.actions.stats', { name: apiKey.name })}
                    className="flex size-8 items-center justify-center rounded-lg bg-muted/60 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground active:scale-95"
                    title={t('apiKey.actions.stats', { name: apiKey.name })}
                >
                    <Info className="size-4" />
                </motion.button>
                <motion.button
                    type="button"
                    layoutId={editLayoutId}
                    onClick={onEdit}
                    aria-label={t('apiKey.actions.edit', { name: apiKey.name })}
                    className="flex size-8 items-center justify-center rounded-lg bg-muted/60 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground active:scale-95"
                    title={t('apiKey.actions.edit', { name: apiKey.name })}
                >
                    <Pencil className="size-4" />
                </motion.button>
                {onExport && (
                    <motion.button
                        type="button"
                        layoutId={exportLayoutId}
                        onClick={onExport}
                        aria-label={t('apiKey.actions.export', { name: apiKey.name })}
                        className="flex size-8 items-center justify-center rounded-lg bg-muted/60 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground active:scale-95"
                        title={t('apiKey.actions.export', { name: apiKey.name })}
                    >
                        <Share2 className="size-4" />
                    </motion.button>
                )}
                <CopyIconButton
                    text={apiKey.api_key}
                    className="flex size-8 items-center justify-center rounded-lg bg-primary/10 text-primary transition-all hover:bg-primary hover:text-primary-foreground active:scale-95"
                    copyIconClassName="size-4"
                    checkIconClassName="size-4"
                />

                {!confirmDelete && (
                    <motion.button
                        type="button"
                        layoutId={deleteLayoutId}
                        onClick={() => setConfirmDelete(true)}
                        aria-label={t('apiKey.actions.delete', { name: apiKey.name })}
                        className="flex size-8 items-center justify-center rounded-lg bg-destructive/10 text-destructive transition-colors hover:bg-destructive hover:text-destructive-foreground"
                    >
                        <Trash2 className="size-4" />
                    </motion.button>
                )}
            </div>

            <AnimatePresence>
                {confirmDelete && (
                    <motion.div
                        layoutId={deleteLayoutId}
                        className="absolute inset-0 flex items-center justify-center gap-2 bg-destructive p-3 rounded-xl"
                        transition={{ type: 'spring', stiffness: 400, damping: 30 }}
                    >
                        <button
                            type="button"
                            onClick={() => setConfirmDelete(false)}
                            aria-label={t('apiKey.actions.cancelDelete', { name: apiKey.name })}
                            className="flex size-8 items-center justify-center rounded-lg bg-destructive-foreground/20 text-destructive-foreground transition-all hover:bg-destructive-foreground/30 active:scale-95"
                        >
                            <X className="size-4" />
                        </button>
                        <button
                            type="button"
                            onClick={onDelete}
                            disabled={isDeleting}
                            className="flex-1 h-8 flex items-center justify-center gap-1.5 rounded-lg bg-destructive-foreground text-destructive text-sm font-medium transition-all hover:bg-destructive-foreground/90 active:scale-[0.98] disabled:opacity-50"
                        >
                            <Trash2 className="size-3.5" />
                            {isDeleting ? '...' : t('apiKey.form.confirm')}
                        </button>
                    </motion.div>
                )}
            </AnimatePresence>
        </motion.div>
    );
}

export function APIKeyPanelBase({
    idPrefix,
    containerClassName,
    listClassName,
    showSearch = false,
    renderHeaderExtra,
}: {
    idPrefix: string;
    containerClassName: string;
    listClassName: string;
    showSearch?: boolean;
    renderHeaderExtra?: (ctx: {
        disabled: boolean;
        onCloseAllOverlays: () => void;
    }) => React.ReactNode;
}) {
    const t = useTranslations('setting');
    const { data: apiKeys, isLoading: apiKeysLoading, error: apiKeysError } = useAPIKeyList();
    const { data: statsList = [] } = useStatsAPIKey();
    const createAPIKey = useCreateAPIKey();
    const updateAPIKey = useUpdateAPIKey();
    const deleteAPIKey = useDeleteAPIKey();
    const { value: apiBaseUrl } = useSettingValue(SettingKey.ApiBaseUrl);
    const effectiveBaseUrl = apiBaseUrl.trim() || (typeof window !== 'undefined' ? window.location.origin : '');
    const canExport = effectiveBaseUrl !== '';

    const instanceId = useId();
    const addLayoutId = `add-btn-${idPrefix}-${instanceId}`;
    const statsPrefix = `${idPrefix}-stats-${instanceId}`;
    const editPrefix = `${idPrefix}-edit-${instanceId}`;
    const exportPrefix = `${idPrefix}-export-${instanceId}`;
    const deletePrefix = `${idPrefix}-delete-`;

    const [isAdding, setIsAdding] = useState(false);
    const [viewingStats, setViewingStats] = useState<{ apiKey: APIKey; layoutId: string } | null>(null);
    const [editingKey, setEditingKey] = useState<{ apiKey: APIKey; layoutId: string } | null>(null);
    const [exportingKey, setExportingKey] = useState<{ apiKey: APIKey; layoutId: string } | null>(null);
    const [deletingId, setDeletingId] = useState<number | null>(null);
    const [search, setSearch] = useState('');

    const sortedApiKeys = useMemo(() => {
        if (!apiKeys) return [];
        const query = search.trim().toLowerCase();
        return [...apiKeys]
            .filter((key) => !query || key.name.toLowerCase().includes(query) || key.api_key.toLowerCase().includes(query))
            .sort((a, b) => a.id - b.id);
    }, [apiKeys, search]);

    const handleDelete = useCallback((id: number) => {
        setDeletingId(id);
        deleteAPIKey.mutate(id, {
            onSuccess: () => {
                toast.success(t('apiKey.toast.deleteSuccess'));
            },
            onError: (error) => {
                const msg = (error as unknown as ApiError)?.message;
                toast.error(t('apiKey.toast.deleteError'), { description: msg });
            },
            onSettled: () => setDeletingId((cur) => (cur === id ? null : cur)),
        });
    }, [deleteAPIKey, t]);

    const closeAllOverlays = useCallback(() => {
        setIsAdding(false);
        setViewingStats(null);
        setEditingKey(null);
        setExportingKey(null);
    }, []);

    const disabledHeaderActions = createAPIKey.isPending || isAdding || !!viewingStats || !!editingKey || !!exportingKey;

    const handleCreate = useCallback((data: Omit<APIKey, 'id' | 'api_key'>) => {
        createAPIKey.mutate(data, {
            onSuccess: () => {
                toast.success(t('apiKey.toast.createSuccess'));
                setIsAdding(false);
            },
            onError: (error) => {
                const msg = (error as unknown as ApiError)?.message;
                toast.error(t('apiKey.toast.createError'), { description: msg });
            },
        });
    }, [createAPIKey, t]);

    const handleUpdate = useCallback((apiKey: APIKey, data: Omit<APIKey, 'id' | 'api_key'>) => {
        updateAPIKey.mutate({ id: apiKey.id, ...data }, {
            onSuccess: () => {
                toast.success(t('apiKey.toast.updateSuccess'));
                setEditingKey(null);
            },
            onError: (error) => {
                const msg = (error as unknown as ApiError)?.message;
                toast.error(t('apiKey.toast.updateError'), { description: msg });
            },
        });
    }, [t, updateAPIKey]);

    return (
        <div className={containerClassName}>
            <div className="flex items-center justify-between gap-3">
                <h2 className="text-lg font-bold text-card-foreground flex items-center gap-2">
                    <KeyRound className="h-5 w-5" />
                    {t('apiKey.title')}
                </h2>
                <div className="flex items-center gap-2">
                    <motion.button
                        layoutId={addLayoutId}
                        type="button"
                        onClick={() => setIsAdding(true)}
                        disabled={disabledHeaderActions}
                        className="h-9 w-9 flex items-center justify-center rounded-lg bg-muted/60 text-muted-foreground transition-colors hover:bg-muted disabled:opacity-50"
                        title={t('apiKey.add')}
                    >
                        <Plus className="size-4" />
                    </motion.button>
                    {renderHeaderExtra?.({ disabled: disabledHeaderActions, onCloseAllOverlays: closeAllOverlays })}
                </div>
            </div>
            {showSearch ? (
                <div className="relative">
                    <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                    <Input
                        value={search}
                        onChange={(e) => setSearch(e.target.value)}
                        placeholder={t('apiKey.searchPlaceholder')}
                        aria-label={t('apiKey.searchPlaceholder')}
                        className="h-9 rounded-xl pl-9"
                    />
                </div>
            ) : null}

            <AnimatePresence>
                {isAdding && (
                    <APIKeyFormOverlay
                        layoutId={addLayoutId}
                        isPending={createAPIKey.isPending}
                        submitLabel={t('apiKey.form.create')}
                        onSubmit={handleCreate}
                        onClose={() => setIsAdding(false)}
                    />
                )}
            </AnimatePresence>

            <AnimatePresence>
                {viewingStats && (
                    <APIKeyStatsCard
                        layoutId={viewingStats.layoutId}
                        apiKey={viewingStats.apiKey}
                        onClose={() => setViewingStats(null)}
                    />
                )}
            </AnimatePresence>

            <AnimatePresence>
                {editingKey && (
                    <APIKeyFormOverlay
                        layoutId={editingKey.layoutId}
                        apiKey={editingKey.apiKey}
                        isPending={updateAPIKey.isPending}
                        submitLabel={t('apiKey.form.save')}
                        onSubmit={(data) => handleUpdate(editingKey.apiKey, data)}
                        onClose={() => setEditingKey(null)}
                    />
                )}
            </AnimatePresence>

            <AnimatePresence>
                {exportingKey && (
                    <APIKeyExportOverlay
                        layoutId={exportingKey.layoutId}
                        apiKey={exportingKey.apiKey}
                        baseUrl={effectiveBaseUrl}
                        onClose={() => setExportingKey(null)}
                    />
                )}
            </AnimatePresence>

            <div className={listClassName}>
                {apiKeysLoading ? (
                    <div className="h-full flex items-center justify-center text-sm text-muted-foreground">
                        <Loader className="size-4 animate-spin" />
                    </div>
                ) : apiKeysError ? (
                    <div className="h-full flex items-center justify-center text-sm text-destructive">
                        {t('apiKey.loadFailed')}
                    </div>
                ) : apiKeys?.length === 0 ? (
                    <div className="h-full flex items-center justify-center text-sm text-muted-foreground">
                        {t('apiKey.empty')}
                    </div>
                ) : sortedApiKeys.length === 0 ? (
                    <div className="h-full flex items-center justify-center text-sm text-muted-foreground">
                        {t('apiKey.noSearchResults')}
                    </div>
                ) : (
                    <AnimatePresence>
                        {sortedApiKeys.map((apiKey) => {
                            const statsLayoutId = `${statsPrefix}-${apiKey.id}`;
                            const editLayoutId = `${editPrefix}-${apiKey.id}`;
                            const exportLayoutId = `${exportPrefix}-${apiKey.id}`;
                            const deleteLayoutId = `${deletePrefix}${apiKey.id}`;
                            return (
                                <APIKeyKeyItem
                                    key={apiKey.id}
                                    apiKey={apiKey}
                                    stats={statsList.find((stats) => stats.api_key_id === apiKey.id)}
                                    statsLayoutId={statsLayoutId}
                                    editLayoutId={editLayoutId}
                                    deleteLayoutId={deleteLayoutId}
                                    exportLayoutId={exportLayoutId}
                                    onViewStats={() => {
                                        closeAllOverlays();
                                        setViewingStats({ apiKey, layoutId: statsLayoutId });
                                    }}
                                    onEdit={() => {
                                        closeAllOverlays();
                                        setEditingKey({ apiKey, layoutId: editLayoutId });
                                    }}
                                    onExport={canExport ? () => {
                                        closeAllOverlays();
                                        setExportingKey({ apiKey, layoutId: exportLayoutId });
                                    } : undefined}
                                    onDelete={() => handleDelete(apiKey.id)}
                                    isDeleting={deleteAPIKey.isPending && deletingId === apiKey.id}
                                    onToggleEnabled={() => updateAPIKey.mutate({ ...apiKey, enabled: !apiKey.enabled })}
                                    onToggleToolsOnly={() => updateAPIKey.mutate({ ...apiKey, tools_only: !apiKey.tools_only })}
                                    onToggleVisionBridge={() => updateAPIKey.mutate({ ...apiKey, vision_bridge: !apiKey.vision_bridge })}
                                />
                            );
                        })}
                    </AnimatePresence>
                )}
            </div>
        </div>
    );
}

function APIKeyDialogPanel() {
    const { setIsOpen } = useMorphingDialog();
    const t = useTranslations('setting');
    return (
        <>
            <MorphingDialogTitle className="sr-only">{t('apiKey.title')}</MorphingDialogTitle>
            <MorphingDialogDescription className="sr-only">
                {t('apiKey.dialogDescription')}
            </MorphingDialogDescription>
            <APIKeyPanelBase
                idPrefix="apikey-dialog"
                containerClassName="rounded-3xl border border-border bg-card p-6 space-y-5 relative w-screen max-w-full md:max-w-xl"
                listClassName="space-y-2 h-[calc(100dvh-10rem)] overflow-y-auto"
                renderHeaderExtra={() => (
                    <button
                        type="button"
                        onClick={() => setIsOpen(false)}
                        aria-label={t('apiKey.close')}
                        className="h-9 w-9 flex items-center justify-center rounded-lg bg-muted/60 text-muted-foreground transition-colors hover:bg-muted"
                        title={t('apiKey.close')}
                    >
                        <X className="size-4" />
                    </button>
                )}
            />
        </>
    );
}

export function SettingAPIKey() {
    const t = useTranslations('setting');
    return (
        <APIKeyPanelBase
            idPrefix="apikey"
            containerClassName="rounded-3xl border border-border bg-card p-6 space-y-5 relative"
            listClassName="space-y-2 h-36 overflow-y-auto"
            renderHeaderExtra={() => (
                <MorphingDialog>
                    <MorphingDialogTrigger
                        aria-label={t('apiKey.expand')}
                        title={t('apiKey.expand')}
                        className="h-9 w-9 flex items-center justify-center rounded-lg bg-muted/60 text-muted-foreground transition-colors hover:bg-muted"
                    >
                        <Maximize2 className="size-4" />
                    </MorphingDialogTrigger>
                    <MorphingDialogContainer>
                        <MorphingDialogContent className="relative">
                            <APIKeyDialogPanel />
                        </MorphingDialogContent>
                    </MorphingDialogContainer>
                </MorphingDialog>
            )}
        />
    );
}
