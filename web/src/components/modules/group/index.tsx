'use client';

import { useCallback, useMemo, useState } from 'react';
import { useTranslations } from 'next-intl';
import { GroupCard } from './Card';
import { useGroupList, useApplyGroupDefaults, type Group } from '@/api/endpoints/group';
import { useSearchStore, useToolbarViewOptionsStore } from '@/components/modules/toolbar';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import { VendorIcon } from '@/components/shared/VendorIcon';
import { toast } from '@/components/common/Toast';
import { cn } from '@/lib/utils';
import { EyeOff, Settings2, Wrench } from 'lucide-react';

function detectVendorFromModel(modelName: string): string | null {
    const lower = modelName.toLowerCase();
    if (lower.startsWith('gpt-') || lower.startsWith('o1') || lower.startsWith('o3') || lower.startsWith('o4')) return 'openai';
    if (lower.startsWith('claude-')) return 'anthropic';
    if (lower.startsWith('gemini-')) return 'google';
    if (lower.startsWith('deepseek')) return 'deepseek';
    if (lower.startsWith('grok')) return 'xai';
    if (lower.startsWith('qwen')) return 'alibaba';
    if (lower.startsWith('llama')) return 'meta';
    if (lower.startsWith('mistral') || lower.startsWith('codestral') || lower.startsWith('pixtral')) return 'mistral';
    if (lower.startsWith('moonshot') || lower.startsWith('kimi')) return 'moonshotai';
    if (lower.startsWith('glm') || lower.startsWith('chatglm')) return 'zhipuai';
    if (lower.startsWith('doubao') || lower.startsWith('skylark')) return 'bytedance';
    if (lower.startsWith('ernie')) return 'baidu';
    if (lower.startsWith('hunyuan')) return 'tencent';
    return null;
}

const VENDOR_LABELS: Record<string, string> = {
    openai: 'OpenAI', anthropic: 'Anthropic', google: 'Google', deepseek: 'DeepSeek',
    xai: 'xAI', alibaba: 'Qwen', meta: 'Meta', mistral: 'Mistral',
    moonshotai: 'Moonshot', zhipuai: 'Zhipu', bytedance: 'Bytedance', baidu: 'Baidu', tencent: 'Tencent',
};

function getGroupVendors(group: Group): Set<string> {
    const vendors = new Set<string>();
    for (const item of group.items ?? []) {
        const vendor = detectVendorFromModel(item.model_name ?? '');
        if (vendor) vendors.add(vendor);
    }
    return vendors;
}

export function Group() {
    const { data: groups } = useGroupList();
    const applyDefaults = useApplyGroupDefaults();
    const t = useTranslations('group');
    const pageKey = 'group' as const;
    const searchTerm = useSearchStore((s) => s.getSearchTerm(pageKey));
    const sortField = useToolbarViewOptionsStore((s) => s.getSortField(pageKey));
    const sortOrder = useToolbarViewOptionsStore((s) => s.getSortOrder(pageKey));
    const [vendorFilter, setVendorFilter] = useState<Set<string>>(new Set());
    // 「仅 tools 查看筛选」（v3.1 R11）：谓词 supports_tools !== false——隐藏 ✗，保留 true+nil，对齐路由语义。
    const [toolsOnly, setToolsOnly] = useState(false);
    // 「隐藏不可用 key」：勾选后余额不足（≤0.1）与被禁用置灰的 key 从卡片列表中隐藏。
    const [hideUnavailable, setHideUnavailable] = useState(false);

    const allVendors = useMemo(() => {
        const vendors = new Set<string>();
        for (const group of groups ?? []) {
            for (const v of getGroupVendors(group)) vendors.add(v);
        }
        return [...vendors].sort();
    }, [groups]);

    const sortedGroups = useMemo(() => {
        if (!groups) return [];
        return [...groups].sort((a, b) => {
            if (!!a.pinned !== !!b.pinned) return a.pinned ? -1 : 1;
            if (a.pinned && b.pinned) {
                const ta = a.pinned_at ? new Date(a.pinned_at).getTime() : 0;
                const tb = b.pinned_at ? new Date(b.pinned_at).getTime() : 0;
                if (ta !== tb) return tb - ta;
            }
            const diff = sortField === 'name'
                ? a.name.localeCompare(b.name)
                : (a.id || 0) - (b.id || 0);
            return sortOrder === 'asc' ? diff : -diff;
        });
    }, [groups, sortField, sortOrder]);

    const visibleGroups = useMemo(() => {
        let result = sortedGroups;
        const term = searchTerm.toLowerCase().trim();
        if (term) result = result.filter((g) => g.name.toLowerCase().includes(term));
        if (vendorFilter.size > 0) {
            result = result.filter((g) => {
                const gv = getGroupVendors(g);
                for (const v of vendorFilter) {
                    if (gv.has(v)) return true;
                }
                return false;
            });
        }
        // 「仅 tools」：隐藏所有条目均确认不支持 tools 的分组（谓词 !== false，保留 true+nil）。
        if (toolsOnly) {
            result = result.filter((g) => (g.items ?? []).some((it) => it.supports_tools !== false));
        }
        return result;
    }, [sortedGroups, searchTerm, vendorFilter, toolsOnly]);

    const toggleVendor = (vendor: string) => {
        setVendorFilter((prev) => {
            const next = new Set(prev);
            if (next.has(vendor)) next.delete(vendor);
            else next.add(vendor);
            return next;
        });
    };

    const groupColumnCompute = useCallback((width: number) => {
        if (width >= 1240) return 4;
        if (width >= 830) return 3;
        if (width >= 560) return 2;
        return 1;
    }, []);

    // R10 修复：tools 筛选与 applyDefaults 独立渲染，不依赖 allVendors.length——
    // 否则单 vendor 部署时整个控制区消失（tools 筛选 + 默认策略按钮都看不到）。
    const vendorChips = allVendors.length > 1 ? (
        <div className="flex flex-wrap items-center gap-1.5 px-1 pb-2">
            <button
                type="button"
                onClick={() => setVendorFilter(new Set())}
                className={cn(
                    'rounded-lg px-2.5 py-1 text-xs font-medium transition-colors',
                    vendorFilter.size === 0
                        ? 'bg-primary text-primary-foreground'
                        : 'bg-muted text-muted-foreground hover:bg-muted/80',
                )}
            >
                All
            </button>
            {allVendors.map((vendor) => (
                <button
                    key={vendor}
                    type="button"
                    onClick={() => toggleVendor(vendor)}
                    className={cn(
                        'flex items-center gap-1.5 rounded-lg px-2.5 py-1 text-xs font-medium transition-colors',
                        vendorFilter.has(vendor)
                            ? 'bg-primary text-primary-foreground'
                            : 'bg-muted text-muted-foreground hover:bg-muted/80',
                    )}
                >
                    <VendorIcon vendor={vendor} className="size-3.5" />
                    {VENDOR_LABELS[vendor] ?? vendor}
                </button>
            ))}
        </div>
    ) : null;

    const filterControls = (
        <div className="flex flex-wrap items-center gap-1.5 px-1 pb-2">
            <button
                type="button"
                onClick={() => setToolsOnly((prev) => !prev)}
                className={cn(
                    'flex items-center gap-1.5 rounded-lg px-2.5 py-1 text-xs font-medium transition-colors',
                    toolsOnly
                        ? 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-300'
                        : 'bg-muted text-muted-foreground hover:bg-muted/80',
                )}
                title={t('tools.onlyToolsHint')}
            >
                <Wrench className="size-3.5" />
                {t('tools.onlyTools')}
            </button>
            <button
                type="button"
                onClick={() => setHideUnavailable((prev) => !prev)}
                className={cn(
                    'flex items-center gap-1.5 rounded-lg px-2.5 py-1 text-xs font-medium transition-colors',
                    hideUnavailable
                        ? 'bg-amber-500/15 text-amber-700 dark:text-amber-300'
                        : 'bg-muted text-muted-foreground hover:bg-muted/80',
                )}
                title={t('hideUnavailableKeysHint')}
            >
                <EyeOff className="size-3.5" />
                {t('hideUnavailableKeys')}
            </button>
            <button
                type="button"
                onClick={() => applyDefaults.mutate(undefined, {
                    onSuccess: (data) => {
                        const msg = t('toast.applyDefaultsSuccess', {
                            updated: data?.groups_updated ?? 0,
                            blocked: data?.items_blocked ?? 0,
                            sorted: data?.items_sorted ?? 0,
                        });
                        toast.success(msg);
                    },
                    onError: (error) => toast.error(t('toast.applyDefaultsFailed'), { description: error.message }),
                })}
                disabled={applyDefaults.isPending}
                className="ml-auto flex items-center gap-1.5 rounded-lg bg-muted px-2.5 py-1 text-xs font-medium text-muted-foreground transition-colors hover:bg-muted/80 disabled:opacity-50"
            >
                <Settings2 className="size-3.5" />
                {t('applyDefaults')}
            </button>
        </div>
    );

    return (
        <VirtualizedGrid
            items={visibleGroups}
            columns={groupColumnCompute}
            estimateItemHeight={520}
            getItemKey={(group, index) => group.id ?? `group-${index}`}
            renderItem={(group) => <GroupCard group={group} hideUnavailable={hideUnavailable} />}
            header={
                <>
                    {vendorChips}
                    {filterControls}
                </>
            }
        />
    );
}
