'use client';

import { AlertTriangle } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { useCircuitAlert } from '@/api/endpoints/circuit';
import { useNavStore } from '@/components/modules/navbar';

/**
 * 首页熔断告警条：熔断页从主导航降级后，这里承担"故障被发现"的职责——
 * 有通道处于熔断/半开状态时显示，点击进入熔断页查看详情与手动重置；
 * 一切正常时完全不渲染，不占用首页空间。
 */
export function CircuitAlertBanner() {
    const t = useTranslations('circuit');
    const { data } = useCircuitAlert();
    const open = data?.open ?? 0;
    const halfOpen = data?.half_open ?? 0;
    if (open === 0 && halfOpen === 0) return null;

    return (
        <button
            type="button"
            onClick={() => useNavStore.getState().setActiveItem('circuit')}
            className="flex w-full items-center gap-3 rounded-2xl border border-amber-500/40 bg-amber-500/10 px-4 py-3 text-left transition-colors hover:bg-amber-500/15"
        >
            <AlertTriangle className="size-4 shrink-0 text-amber-600 dark:text-amber-400" />
            <span className="text-sm font-medium text-amber-700 dark:text-amber-300">
                {t('alertBanner', { open, halfOpen })}
            </span>
        </button>
    );
}
