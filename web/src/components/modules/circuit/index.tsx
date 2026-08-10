'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient, useMutation } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { apiClient } from '@/api/client';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { toast } from '@/components/common/Toast';
import { RefreshCw, ShieldAlert, ShieldCheck, ShieldQuestion, AlertTriangle } from 'lucide-react';
import { cn } from '@/lib/utils';

/** 本地时钟：驱动剩余冷却倒计时（渲染期不调 Date.now()——lint purity）。 */
function useNow(intervalMs = 1000): number {
    const [now, setNow] = useState(() => Date.now());
    useEffect(() => {
        const id = setInterval(() => setNow(Date.now()), intervalMs);
        return () => clearInterval(id);
    }, [intervalMs]);
    return now;
}

/** 熔断状态条目（与后端 balancer.CircuitStatus 对齐）。 */
export interface CircuitStatus {
    channel_id: number;
    channel_key_id: number;
    model_name: string;
    state: number;
    state_label: 'closed' | 'open' | 'half_open';
    consecutive_failures: number;
    trip_count: number;
    last_failure_time: string;
    cooldown_until?: string;
}

interface CircuitStatusResponse {
    items: CircuitStatus[];
    open: number;
    half_open: number;
}

const STATE_LABEL: Record<string, { text: string; cls: string; icon: typeof ShieldCheck }> = {
    closed: { text: 'closed', cls: 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-300', icon: ShieldCheck },
    open: { text: 'open', cls: 'bg-destructive/15 text-destructive', icon: ShieldAlert },
    half_open: { text: 'half_open', cls: 'bg-amber-500/15 text-amber-700 dark:text-amber-300', icon: ShieldQuestion },
};

/** 未知状态 → 灰色「未知」（P1 修复：绝不能 fallback 到 closed 绿色，否则管理员误判为无熔断）。 */
const STATE_UNKNOWN: { text: string; cls: string; icon: typeof ShieldCheck } = {
    text: 'unknown',
    cls: 'bg-muted text-muted-foreground',
    icon: ShieldQuestion,
};

export function Circuit() {
    const t = useTranslations('circuit');
    const queryClient = useQueryClient();
    const now = useNow(); // 本地时钟，倒计时每秒刷新（无需高频轮询）
    const [filter, setFilter] = useState<'tripped' | 'all'>('tripped'); // 默认只看熔断中（P1：closed 噪音过滤）
    const [channelFilter, setChannelFilter] = useState('');

    const { data, isLoading } = useQuery({
        queryKey: ['circuit', 'status'],
        queryFn: async () => apiClient.get<CircuitStatusResponse>('/api/v1/circuit/status'),
        refetchInterval: 10000, // 低频轮询；剩余冷却前端本地倒计时（P2）
    });

    const reset = useMutation({
        mutationFn: async (payload: { scope: string; channel_id?: number; channel_key_id?: number; model_name?: string }) =>
            apiClient.post<null>('/api/v1/circuit/reset', payload),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['circuit', 'status'] });
            toast.success(t('resetDone'));
        },
        onError: (e) => toast.error(t('resetFailed'), { description: (e as Error).message }),
    });

    const items = (data?.items ?? []).filter((it) => {
        if (filter === 'tripped' && it.state_label === 'closed') return false;
        if (channelFilter && it.channel_id !== Number(channelFilter)) return false;
        return true;
    });

    const channels = [...new Set((data?.items ?? []).map((it) => it.channel_id))].sort((a, b) => a - b);
    const trippedCount = (data?.open ?? 0) + (data?.half_open ?? 0);

    // 全量重置强确认（P1：误点清空熔断 → 对故障渠道重放流量）
    // 确认态 3s 后自动还原（P1 修复：防「武装后遗忘，误触第二次立即执行」）。
    const [confirmAll, setConfirmAll] = useState(false);
    const confirmTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const cancelConfirmAll = useCallback(() => {
        if (confirmTimerRef.current) clearTimeout(confirmTimerRef.current);
        setConfirmAll(false);
    }, []);
    useEffect(() => {
        if (confirmAll) {
            confirmTimerRef.current = setTimeout(cancelConfirmAll, 3000);
        }
        return () => {
            if (confirmTimerRef.current) clearTimeout(confirmTimerRef.current);
        };
    }, [confirmAll, cancelConfirmAll]);

    const handleResetAll = useCallback(() => {
        if (!confirmAll) {
            setConfirmAll(true);
            return;
        }
        cancelConfirmAll();
        reset.mutate({ scope: 'all' });
    }, [confirmAll, reset, cancelConfirmAll]);

    const handleResetItem = useCallback((it: CircuitStatus) => {
        reset.mutate({ scope: 'item', channel_id: it.channel_id, channel_key_id: it.channel_key_id, model_name: it.model_name });
    }, [reset]);

    const handleResetChannel = useCallback((cid: number) => {
        reset.mutate({ scope: 'channel', channel_id: cid });
    }, [reset]);

    return (
        <div className="p-4 flex flex-col gap-3 min-h-0">
            <div className="flex items-center justify-between flex-wrap gap-2">
                <div className="flex items-center gap-2">
                    <h2 className="text-lg font-bold">{t('title')}</h2>
                    <Badge variant="secondary" className="gap-1">
                        <AlertTriangle className="size-3" />
                        {t('tripped', { count: trippedCount })}
                    </Badge>
                </div>
                <div className="flex items-center gap-2">
                    <select
                        value={filter}
                        onChange={(e) => setFilter(e.target.value as 'tripped' | 'all')}
                        className="h-8 rounded-lg border border-border/60 bg-background px-2 text-xs"
                    >
                        <option value="tripped">{t('filter.trippedOnly')}</option>
                        <option value="all">{t('filter.all')}</option>
                    </select>
                    <select
                        value={channelFilter}
                        onChange={(e) => setChannelFilter(e.target.value)}
                        className="h-8 rounded-lg border border-border/60 bg-background px-2 text-xs"
                    >
                        <option value="">{t('filter.allChannels')}</option>
                        {channels.map((cid) => <option key={cid} value={cid}>{t('channel')} {cid}</option>)}
                    </select>
                    {confirmAll ? (
                        <Button size="sm" variant="destructive" onClick={handleResetAll} disabled={reset.isPending}>
                            {t('resetAllConfirm')}
                        </Button>
                    ) : (
                        <Button size="sm" variant="secondary" onClick={handleResetAll} disabled={reset.isPending}>
                            {t('resetAll')}
                        </Button>
                    )}
                </div>
            </div>

            <p className="text-xs text-muted-foreground">{t('memoryHint')}</p>

            <div className="flex-1 min-h-0 overflow-y-auto rounded-xl border border-border/50 bg-card">
                {isLoading ? (
                    <div className="p-8 text-center text-sm text-muted-foreground">{t('loading')}</div>
                ) : items.length === 0 ? (
                    <div className="p-8 text-center text-sm text-muted-foreground">{t('empty')}</div>
                ) : (
                    <table className="w-full text-xs">
                        <thead className="sticky top-0 bg-muted/50">
                            <tr className="text-left text-muted-foreground">
                                <th className="px-3 py-2">{t('col.channel')}</th>
                                <th className="px-3 py-2">{t('col.key')}</th>
                                <th className="px-3 py-2">{t('col.model')}</th>
                                <th className="px-3 py-2">{t('col.state')}</th>
                                <th className="px-3 py-2">{t('col.failures')}</th>
                                <th className="px-3 py-2">{t('col.trips')}</th>
                                <th className="px-3 py-2">{t('col.cooldown')}</th>
                                <th className="px-3 py-2 text-right">{t('col.actions')}</th>
                            </tr>
                        </thead>
                        <tbody>
                            {items.map((it) => {
                                const s = STATE_LABEL[it.state_label] ?? STATE_UNKNOWN; // P1：未知 → 灰色，绝不 fallback closed 绿
                                const Icon = s.icon;
                                const remaining = it.cooldown_until ? Math.max(0, Math.ceil((new Date(it.cooldown_until).getTime() - now) / 1000)) : 0;
                                return (
                                    <tr key={`${it.channel_id}:${it.channel_key_id}:${it.model_name}`} className="border-t border-border/30">
                                        <td className="px-3 py-1.5">{it.channel_id}</td>
                                        <td className="px-3 py-1.5">{it.channel_key_id}</td>
                                        <td className="px-3 py-1.5 font-medium">{it.model_name}</td>
                                        <td className="px-3 py-1.5">
                                            <span className={cn('inline-flex items-center gap-1 rounded px-1.5 py-px text-[10px] font-medium', s.cls)}>
                                                <Icon className="size-3" />
                                                {t(`state.${it.state_label}`)}
                                            </span>
                                        </td>
                                        <td className="px-3 py-1.5">{it.consecutive_failures}</td>
                                        <td className="px-3 py-1.5">{it.trip_count}</td>
                                        <td className="px-3 py-1.5">
                                            {it.state_label === 'open' ? `${remaining}s` : '-'}
                                        </td>
                                        <td className="px-3 py-1.5 text-right">
                                            <button
                                                type="button"
                                                onClick={() => handleResetItem(it)}
                                                disabled={reset.isPending}
                                                className="rounded px-1.5 py-0.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-50"
                                                title={t('resetItem')}
                                            >
                                                <RefreshCw className="size-3" />
                                            </button>
                                            <button
                                                type="button"
                                                onClick={() => handleResetChannel(it.channel_id)}
                                                disabled={reset.isPending}
                                                className="rounded px-1.5 py-0.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-50"
                                                title={t('resetChannel')}
                                            >
                                                {t('resetChannel')}
                                            </button>
                                        </td>
                                    </tr>
                                );
                            })}
                        </tbody>
                    </table>
                )}
            </div>
        </div>
    );
}
