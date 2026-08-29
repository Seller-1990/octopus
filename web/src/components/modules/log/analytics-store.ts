'use client';

import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import type { UsageDimension, UsageMetricScope } from '@/api/endpoints/log-analytics';

export type LogView = 'analytics' | 'detail' | 'live';

function browserTimezone() {
    try {
        return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
    } catch {
        return 'UTC';
    }
}

interface LogAnalyticsState {
    view: LogView;
    scope: UsageMetricScope;
    dimension: UsageDimension;
    timezone: string;
    siteIds: number[];
    siteAccountIds: number[];
    apiKeyIds: number[];
    requestModels: string[];
    actualModels: string[];
    canonicalModels: string[];
    setView: (value: LogView) => void;
    setScope: (value: UsageMetricScope) => void;
    setDimension: (value: UsageDimension) => void;
    setTimezone: (value: string) => void;
    setSiteIds: (value: number[]) => void;
    setSiteAccountIds: (value: number[]) => void;
    setAPIKeyIds: (value: number[]) => void;
    setRequestModels: (value: string[]) => void;
    setActualModels: (value: string[]) => void;
    setCanonicalModels: (value: string[]) => void;
    clearBusinessFilters: () => void;
}

// 视图切换不持久化：日志页每次进入都默认展示明细，使用分析由使用者按需切换。
function withoutView<S extends { view: LogView }>(state: S): Omit<S, 'view'> {
    const { view: _view, ...rest } = state;
    void _view;
    return rest;
}

export const useLogAnalyticsStore = create<LogAnalyticsState>()(
    persist(
        (set) => ({
            view: 'detail',
            scope: 'request',
            dimension: 'site',
            timezone: browserTimezone(),
            siteIds: [],
            siteAccountIds: [],
            apiKeyIds: [],
            requestModels: [],
            actualModels: [],
            canonicalModels: [],
            setView: (view) => set({ view }),
            setScope: (scope) => set({ scope }),
            setDimension: (dimension) => set({ dimension }),
            setTimezone: (timezone) => set({ timezone }),
            // 纯赋值，不清空 siteAccountIds：下钻/筛选是叠加语义，静默清除其他维度
            // 会让用户的对比基准消失；站点-账号归属校验由 Controls 的站点下拉负责。
            setSiteIds: (siteIds) => set({ siteIds }),
            setSiteAccountIds: (siteAccountIds) => set({ siteAccountIds }),
            setAPIKeyIds: (apiKeyIds) => set({ apiKeyIds }),
            setRequestModels: (requestModels) => set({ requestModels }),
            setActualModels: (actualModels) => set({ actualModels }),
            setCanonicalModels: (canonicalModels) => set({ canonicalModels }),
            clearBusinessFilters: () =>
                set({
                    siteIds: [],
                    siteAccountIds: [],
                    apiKeyIds: [],
                    requestModels: [],
                    actualModels: [],
                    canonicalModels: [],
                }),
        }),
        {
            name: 'log-analytics-options',
            version: 2,
            migrate: (persisted) => withoutView(persisted as LogAnalyticsState),
            partialize: withoutView,
        },
    ),
);
