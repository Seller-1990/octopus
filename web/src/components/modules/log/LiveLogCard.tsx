'use client';

import { useEffect, useState } from 'react';
import { useTranslations } from 'next-intl';
import { ChevronDown, ChevronUp, Coins, Loader2, Square } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import {
    useLiveLogDetail,
    useStopAttempt,
    type ChannelAttempt,
    type LiveLogOverview,
    type LiveRequestState,
} from '@/api/endpoints/log';

function formatTime(value: string): string {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return '--';
    return date.toLocaleTimeString('zh-CN', {
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        hour12: false,
    });
}

function formatDuration(ms: number): string {
    if (ms < 1000) return `${Math.max(0, Math.round(ms))}ms`;
    return `${(ms / 1000).toFixed(2)}s`;
}

// key 倍率展示：与 LogCard/分组成员倍率一致使用 `1.5x` 格式。
// null/undefined = 未获取到倍率（不标注）；0 = 免费 Key（标注 0x）；1 = 标准（标注 1x）。
function formatKeyMultiplier(value: number | undefined | null): string | null {
    if (value == null || !Number.isFinite(value)) return null;
    const rounded = Math.round(value * 100) / 100;
    return `${rounded}x`;
}

const stateStyles: Record<LiveRequestState, string> = {
    running: 'border-blue-500/40 bg-blue-500/10 text-blue-600 dark:text-blue-300',
    success: 'border-emerald-500/40 bg-emerald-500/10 text-emerald-600 dark:text-emerald-300',
    failed: 'border-destructive/40 bg-destructive/10 text-destructive',
    canceled: 'border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-300',
};

// LiveLogCard 展示一条实时日志，可展开查看尝试详情并停止当前尝试。
// 请求完成后，概览流会携带与 DB 历史日志一致的 attempts 与 key 倍率，
// 因此已完成日志无需再走历史接口即可展示完整 key 切换记录。
export function LiveLogCard({ log }: { log: LiveLogOverview }) {
    const t = useTranslations('log.live');
    const [expanded, setExpanded] = useState(false);
    const running = log.state === 'running';
    const { attempts, runningAttempt } = useLiveLogDetail(log.id, log.state, expanded);
    const stopAttempt = useStopAttempt();
    const [now, setNow] = useState(() => Date.now());

    const historyAttempts: ChannelAttempt[] = log.attempts ?? [];
    const hasHistoryAttempts = historyAttempts.length > 0;
    const attemptCount = hasHistoryAttempts ? historyAttempts.length : attempts.length;
    const keyMultiplierLabel = formatKeyMultiplier(log.price_group_multiplier);

    useEffect(() => {
        if (!running) return;
        const timer = window.setInterval(() => setNow(Date.now()), 1000);
        return () => window.clearInterval(timer);
    }, [running]);

    const durationMS = running
        ? now - new Date(log.started_at).getTime()
        : log.duration_ms;
    const stateLabel = t(log.state);

    return (
        <div
            className={cn(
                'rounded-2xl border bg-card p-4 text-sm',
                log.state === 'failed' ? 'border-destructive/40' : 'border-border'
            )}
        >
            <div className="flex items-center gap-3 min-w-0">
                <Badge variant="outline" className={cn('shrink-0 border', stateStyles[log.state])}>
                    {running ? <Loader2 className="mr-1 size-3 animate-spin" /> : null}
                    {stateLabel}
                </Badge>
                <span className="font-semibold text-card-foreground truncate" title={log.request_model_name}>
                    {log.request_model_name || '--'}
                </span>
                <span className="text-muted-foreground truncate">
                    {log.channel_name || log.actual_model_name || '--'}
                </span>
                {keyMultiplierLabel ? (
                    <Badge
                        variant="secondary"
                        title={t('keyMultiplier')}
                        className={cn(
                            'shrink-0 gap-1 px-1.5 py-0 text-[10px]',
                            keyMultiplierLabel === '0x'
                                ? 'bg-violet-500/15 text-violet-700 dark:text-violet-300'
                                : keyMultiplierLabel === '1x'
                                    ? 'bg-muted text-muted-foreground'
                                    : 'bg-amber-500/15 text-amber-700 dark:text-amber-300'
                        )}
                    >
                        <Coins className="size-3 shrink-0" />
                        {keyMultiplierLabel}
                    </Badge>
                ) : null}
                <span className="ml-auto shrink-0 tabular-nums text-muted-foreground">
                    {formatTime(log.started_at)} · {formatDuration(durationMS)}
                </span>
                <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    className="shrink-0"
                    onClick={() => setExpanded((value) => !value)}
                >
                    {expanded ? <ChevronUp className="size-4" /> : <ChevronDown className="size-4" />}
                </Button>
            </div>

            <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground tabular-nums">
                <span>{t('attempts')}: {attemptCount}</span>
                <span>↑ {log.input_tokens.toLocaleString()}</span>
                <span>↓ {log.output_tokens.toLocaleString()}</span>
                <span>¥ {log.total_cost.toFixed(6)}</span>
            </div>

            {log.error ? (
                <p className="mt-2 line-clamp-2 whitespace-pre-line text-xs text-destructive">{log.error}</p>
            ) : null}

            {expanded ? (
                <div className="mt-3 border-t border-border pt-3">
                    {running && runningAttempt ? (
                        <div className="mb-3 flex items-center gap-3">
                            <span className="text-xs text-muted-foreground">
                                {t('attemptIndex', { index: runningAttempt.attempt_index })}: {runningAttempt.channel_name}
                            </span>
                            <Button
                                type="button"
                                variant="destructive"
                                size="sm"
                                disabled={stopAttempt.isPending}
                                onClick={() =>
                                    stopAttempt.mutate({
                                        requestId: log.id,
                                        attemptIndex: runningAttempt.attempt_index,
                                    })
                                }
                            >
                                {stopAttempt.isPending ? (
                                    <Loader2 className="mr-1 size-3 animate-spin" />
                                ) : (
                                    <Square className="mr-1 size-3" />
                                )}
                                {stopAttempt.isPending ? t('stopping') : t('stopAttempt')}
                            </Button>
                        </div>
                    ) : null}

                    {attemptCount === 0 ? (
                        <p className="py-2 text-center text-xs text-muted-foreground">{t('attempts')}: 0</p>
                    ) : hasHistoryAttempts ? (
                        <div className="divide-y divide-border">
                            {historyAttempts
                                .slice()
                                .reverse()
                                .map((attempt, index) => (
                                    <div key={`${attempt.attempt_num}-${attempt.channel_id}-${index}`} className="flex flex-col gap-1 py-2 text-xs">
                                        <div className="flex items-center gap-2">
                                            <span className="text-muted-foreground">
                                                {t('attemptIndex', { index: attempt.attempt_num })}
                                            </span>
                                            <span className="font-semibold text-foreground">{attempt.channel_name}</span>
                                            <span className="text-muted-foreground">{attempt.model_name}</span>
                                            <span className="text-muted-foreground">{attempt.status}</span>
                                            {attempt.sticky ? (
                                                <Badge variant="secondary" className="shrink-0 px-1.5 py-0 text-[10px]">
                                                    sticky
                                                </Badge>
                                            ) : null}
                                        </div>
                                        {attempt.msg ? (
                                            <div className="text-[11px] leading-relaxed text-destructive/90 whitespace-pre-wrap break-words">
                                                {attempt.msg}
                                            </div>
                                        ) : null}
                                    </div>
                                ))}
                        </div>
                    ) : (
                        <div className="divide-y divide-border">
                            {attempts
                                .slice()
                                .reverse()
                                .map((attempt) => (
                                    <div key={attempt.attempt_index} className="flex flex-col gap-1 py-2 text-xs">
                                        <div className="flex items-center gap-2">
                                            <span className="text-muted-foreground">
                                                {t('attemptIndex', { index: attempt.attempt_index })}
                                            </span>
                                            <span className="font-semibold text-foreground">{attempt.channel_name}</span>
                                            {runningAttempt?.attempt_index === attempt.attempt_index ? (
                                                <Loader2 className="ml-auto size-3 animate-spin text-muted-foreground" />
                                            ) : null}
                                        </div>
                                        {attempt.error ? (
                                            <div className="text-[11px] leading-relaxed text-destructive/90 whitespace-pre-wrap break-words">
                                                {attempt.error}
                                            </div>
                                        ) : null}
                                    </div>
                                ))}
                        </div>
                    )}
                </div>
            ) : null}
        </div>
    );
}
