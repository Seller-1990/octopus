'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { ChevronDown, ChevronUp } from 'lucide-react';
import { useSiteCheckinLogs } from '@/api/endpoints/site';
import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';

function formatDateTime(value: string): string {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return '--';
    return date.toLocaleString('zh-CN', {
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
    });
}

// AccountCheckinHistory 展示账号最近的签到执行记录。
export function AccountCheckinHistory({ accountId }: { accountId: number }) {
    const t = useTranslations('siteManagement.checkin');
    const [expanded, setExpanded] = useState(false);
    const { data: logs = [] } = useSiteCheckinLogs(accountId, expanded);

    return (
        <div className="mt-2">
            <button
                type="button"
                onClick={() => setExpanded((value) => !value)}
                className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
            >
                {expanded ? <ChevronUp className="size-3.5" /> : <ChevronDown className="size-3.5" />}
                {t('history')} {logs.length > 0 ? `(${logs.length})` : ''}
            </button>

            {expanded ? (
                logs.length === 0 ? (
                    <p className="mt-1 text-xs text-muted-foreground">{t('historyEmpty')}</p>
                ) : (
                    <div className="mt-1 divide-y divide-border rounded-lg border border-border bg-muted/20">
                        {logs.map((log) => {
                            const statusLabel =
                                log.status === 'success'
                                    ? t('filters.success')
                                    : log.status === 'skipped'
                                        ? t('filters.skipped')
                                        : t('filters.failed');
                            const statusClass =
                                log.status === 'success'
                                    ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-600 dark:text-emerald-300'
                                    : log.status === 'skipped'
                                        ? 'border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-300'
                                        : 'border-destructive/40 bg-destructive/10 text-destructive';
                            return (
                                <div key={log.id} className="flex items-start gap-2 px-2 py-1.5 text-xs">
                                    <Badge
                                        variant="outline"
                                        className={cn('shrink-0 border', statusClass)}
                                    >
                                        {statusLabel}
                                    </Badge>
                                    <span className="min-w-0 flex-1">
                                        <span className="block text-muted-foreground">
                                            {formatDateTime(log.created_at)}
                                            {log.reward ? ` · ${t('reward')}: ${log.reward}` : ''}
                                        </span>
                                        {log.message ? (
                                            <span className="block whitespace-pre-line text-foreground/80">{log.message}</span>
                                        ) : null}
                                    </span>
                                </div>
                            );
                        })}
                    </div>
                )
            ) : null}
        </div>
    );
}
