'use client';

import { Loader2 } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { useLiveLogs } from '@/api/endpoints/log';
import { LiveLogCard } from './LiveLogCard';

/**
 * 实时日志面板：SSE 推送运行中/刚完成的请求概览（含尝试详情与停止操作）。
 * 此前 useLiveLogs/LiveLogCard 已完整实现但从未接线（零调用的死代码），
 * 明细列表的实时性只能靠轮询，本面板补上"挂着盯"场景的推送通道。
 */
export function LiveLogPanel() {
    const tList = useTranslations('log.list');
    const tLive = useTranslations('log.live');
    const { logs, isLoading, error } = useLiveLogs(true);

    if (isLoading && logs.length === 0) {
        return (
            <div className="flex h-full items-center justify-center">
                <Loader2 className="size-6 animate-spin text-muted-foreground" />
            </div>
        );
    }

    return (
        <div className="flex h-full min-h-0 flex-col gap-2">
            {error ? (
                <div
                    role="alert"
                    className="rounded-lg border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-300"
                >
                    {tList('streamDisconnected')}
                </div>
            ) : null}
            <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain rounded-t-3xl pb-[calc(6rem+env(safe-area-inset-bottom))] md:pb-0">
                <div className="flex flex-col gap-2">
                    {logs.map((log) => (
                        <LiveLogCard key={log.id} log={log} />
                    ))}
                    {logs.length === 0 ? (
                        <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
                            {tLive('empty')}
                        </div>
                    ) : null}
                </div>
            </div>
        </div>
    );
}
