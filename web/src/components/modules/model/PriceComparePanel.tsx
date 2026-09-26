'use client';

import { useMemo, useState } from 'react';
import { ArrowDownUp, Search, TriangleAlert } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { useModelCatalog } from '@/api/endpoints/model-catalog';
import { type PriceCompareRow, usePriceCompare } from '@/api/endpoints/model-pricing';
import { Badge } from '@/components/ui/badge';
import { Input } from '@/components/ui/input';
import { cn } from '@/lib/utils';

const PRICE_FORMATTER = new Intl.NumberFormat(undefined, {
    maximumSignificantDigits: 6,
});

function formatUSD(value: number | null | undefined) {
    if (value === null || value === undefined || !Number.isFinite(value)) return '—';
    return `$${PRICE_FORMATTER.format(value)}`;
}

function formatRaw(value: number | null | undefined) {
    if (value === null || value === undefined || !Number.isFinite(value)) return '—';
    return PRICE_FORMATTER.format(value);
}

type SortKey = 'output_usd' | 'input_usd' | 'observed_at';

// 价格对比视图：同一模型在不同站点账号间的价格横向对比（借鉴 all-api-hub）。
// 只读展示，不参与路由打分；折算口径由后端 pricing/compare 统一提供。
export function PriceComparePanel() {
    const t = useTranslations('model.priceCompare');
    const { data: catalog } = useModelCatalog();
    const [query, setQuery] = useState('');
    const [selected, setSelected] = useState('');
    const [sortKey, setSortKey] = useState<SortKey>('output_usd');

    const options = useMemo(() => {
        const names = (catalog ?? []).map((item) => item.name);
        const keyword = query.trim().toLowerCase();
        const filtered = keyword
            ? names.filter((name) => name.toLowerCase().includes(keyword))
            : names;
        return filtered.slice(0, 50);
    }, [catalog, query]);

    const { data, isFetching } = usePriceCompare(selected);

    const rows = useMemo(() => {
        const list = [...(data?.rows ?? [])];
        list.sort((left, right) => {
            if (sortKey === 'observed_at') {
                return right.observed_at.localeCompare(left.observed_at);
            }
            const leftValue = left[sortKey];
            const rightValue = right[sortKey];
            if (leftValue === null || leftValue === undefined) return 1;
            if (rightValue === null || rightValue === undefined) return -1;
            return leftValue - rightValue;
        });
        return list;
    }, [data?.rows, sortKey]);

    const summary = data?.summary;
    const cheapestId = useMemo(() => {
        let best: PriceCompareRow | null = null;
        for (const row of rows) {
            if (row.output_usd === null || row.output_usd === undefined) continue;
            if (!best || (best.output_usd ?? Infinity) > row.output_usd) best = row;
        }
        return best?.quote_id ?? null;
    }, [rows]);

    return (
        <div className="flex h-full min-h-0 flex-col gap-3">
            <div className="relative shrink-0">
                <Search className="text-muted-foreground absolute top-1/2 left-3 size-4 -translate-y-1/2" />
                <Input
                    value={query}
                    onChange={(event) => setQuery(event.target.value)}
                    placeholder={t('searchPlaceholder')}
                    className="pl-9"
                    aria-label={t('searchPlaceholder')}
                    list="price-compare-model-options"
                    onKeyDown={(event) => {
                        if (event.key === 'Enter') {
                            setSelected(query.trim());
                        }
                    }}
                />
                <datalist id="price-compare-model-options">
                    {options.map((name) => (
                        <option key={name} value={name} />
                    ))}
                </datalist>
            </div>
            {options.length > 0 ? (
                <div className="flex flex-wrap gap-1">
                    {options.slice(0, 8).map((name) => (
                        <button
                            key={name}
                            type="button"
                            onClick={() => {
                                setQuery(name);
                                setSelected(name);
                            }}
                            className={cn(
                                'rounded-full border px-2 py-0.5 text-xs transition-colors',
                                selected === name
                                    ? 'bg-primary text-primary-foreground'
                                    : 'text-muted-foreground hover:bg-muted hover:text-foreground',
                            )}
                        >
                            {name}
                        </button>
                    ))}
                </div>
            ) : null}

            <div className="min-h-0 flex-1 overflow-y-auto">
            {summary && summary.row_count > 0 ? (
                <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                    <SummaryCell label={t('summary.min')} value={formatUSD(summary.min_output_usd)} />
                    <SummaryCell label={t('summary.median')} value={formatUSD(summary.median_output_usd)} />
                    <SummaryCell label={t('summary.max')} value={formatUSD(summary.max_output_usd)} />
                    <SummaryCell
                        label={t('summary.spread')}
                        value={
                            summary.spread_ratio === null || summary.spread_ratio === undefined
                                ? '—'
                                : `${PRICE_FORMATTER.format(summary.spread_ratio)}×`
                        }
                        warn={(summary.spread_ratio ?? 0) >= 2}
                    />
                </div>
            ) : null}

            {selected && rows.length === 0 && !isFetching ? (
                <p className="text-muted-foreground text-sm">{t('empty')}</p>
            ) : null}

            {rows.length > 0 ? (
                <div className="overflow-x-auto rounded-lg border">
                    <table className="w-full min-w-[900px] text-sm">
                        <thead className="bg-muted/50 text-muted-foreground text-xs">
                            <tr>
                                <th className="px-3 py-2 text-left">{t('columns.site')}</th>
                                <th className="px-3 py-2 text-left">{t('columns.group')}</th>
                                <th className="px-3 py-2 text-right">{t('columns.rawInput')}</th>
                                <th className="px-3 py-2 text-right">{t('columns.rawOutput')}</th>
                                <th className="px-3 py-2 text-right">{t('columns.modelMultiplier')}</th>
                                <th className="px-3 py-2 text-right">{t('columns.groupMultiplier')}</th>
                                <th className="px-3 py-2 text-right">
                                    <button
                                        type="button"
                                        className="inline-flex items-center gap-1"
                                        onClick={() => setSortKey('output_usd')}
                                    >
                                        {t('columns.outputUSD')}
                                        <ArrowDownUp className="size-3" />
                                    </button>
                                </th>
                                <th className="px-3 py-2 text-right">
                                    <button
                                        type="button"
                                        className="inline-flex items-center gap-1"
                                        onClick={() => setSortKey('input_usd')}
                                    >
                                        {t('columns.inputUSD')}
                                        <ArrowDownUp className="size-3" />
                                    </button>
                                </th>
                                <th className="px-3 py-2 text-right">{t('columns.observedAt')}</th>
                            </tr>
                        </thead>
                        <tbody>
                            {rows.map((row) => (
                                <tr
                                    key={row.quote_id}
                                    className={cn(
                                        'border-t',
                                        row.quote_id === cheapestId && 'bg-primary/5 font-medium',
                                    )}
                                >
                                    <td className="px-3 py-2">
                                        <div className="flex items-center gap-1.5">
                                            <span>{row.site_name || `#${row.site_id}`}</span>
                                            {row.site_account_name ? (
                                                <span className="text-muted-foreground text-xs">
                                                    / {row.site_account_name}
                                                </span>
                                            ) : null}
                                            {row.quote_id === cheapestId ? (
                                                <Badge variant="secondary">{t('cheapest')}</Badge>
                                            ) : null}
                                        </div>
                                    </td>
                                    <td className="px-3 py-2">
                                        <div className="flex items-center gap-1.5">
                                            <span>{row.group_key}</span>
                                            {row.manual_override ? (
                                                <Badge variant="outline">{t('manual')}</Badge>
                                            ) : null}
                                        </div>
                                    </td>
                                    <td className="px-3 py-2 text-right tabular-nums">
                                        {formatRaw(row.input)} {row.currency}
                                    </td>
                                    <td className="px-3 py-2 text-right tabular-nums">
                                        {formatRaw(row.output)} {row.currency}
                                    </td>
                                    <td className="px-3 py-2 text-right tabular-nums">
                                        {row.model_multiplier === null || row.model_multiplier === undefined
                                            ? '—'
                                            : `${row.model_multiplier}×`}
                                    </td>
                                    <td className="px-3 py-2 text-right tabular-nums">
                                        {row.group_multiplier}×
                                        {row.group_multiplier_known ? null : (
                                            <span className="text-muted-foreground ml-1 text-xs">
                                                ({t('multiplierUnknown')})
                                            </span>
                                        )}
                                    </td>
                                    <td className="px-3 py-2 text-right tabular-nums">
                                        {formatUSD(row.output_usd)}
                                    </td>
                                    <td className="px-3 py-2 text-right tabular-nums">
                                        {formatUSD(row.input_usd)}
                                    </td>
                                    <td
                                        className={cn(
                                            'px-3 py-2 text-right whitespace-nowrap',
                                            row.stale && 'text-muted-foreground',
                                        )}
                                    >
                                        {new Date(row.observed_at).toLocaleString()}
                                        {row.stale ? ` (${t('stale')})` : null}
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            ) : null}
            <div className="h-1 pb-24 md:hidden" aria-hidden />
            </div>
        </div>
    );
}

function SummaryCell({ label, value, warn = false }: { label: string; value: string; warn?: boolean }) {
    return (
        <div className="rounded-lg border bg-card px-3 py-2">
            <div className="text-muted-foreground text-xs">{label}</div>
            <div
                className={cn(
                    'flex items-center gap-1 text-base font-medium tabular-nums',
                    warn && 'text-destructive',
                )}
            >
                {warn ? <TriangleAlert className="size-4" /> : null}
                {value}
            </div>
        </div>
    );
}
