'use client';

import { useCallback, useMemo, useState } from 'react';
import { AlertCircle, CircleDollarSign, RefreshCw, SearchX } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { useChannelList } from '@/api/endpoints/channel';
import {
    type CanonicalModel,
    type CatalogPriceOverview,
    useModelCatalog,
    useCatalogPriceOverview,
    useSyncModelCatalog,
    useUpdateCanonicalModel,
} from '@/api/endpoints/model-catalog';
import { toast } from '@/components/common/Toast';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import { Button } from '@/components/ui/button';
import { Switch } from '@/components/ui/switch';
import { useSearchStore } from '@/components/modules/toolbar';
import { cn } from '@/lib/utils';
import { CatalogModelDialog } from './CatalogModelDialog';
import { VendorBadge } from './VendorBadge';

function errorMessage(error: unknown) {
    return error instanceof Error ? error.message : String(error);
}

export function ModelCatalog() {
    const t = useTranslations('model.catalog');
    const catalog = useModelCatalog();
    const pricing = useCatalogPriceOverview();
    const syncCatalog = useSyncModelCatalog();
    const channels = useChannelList();
    const searchTerm = useSearchStore((state) => state.getSearchTerm('model'));
    const [selectedModel, setSelectedModel] = useState<CanonicalModel | null>(null);

    const filtered = useMemo(() => {
        const term = searchTerm.trim().toLowerCase();
        return (catalog.data ?? []).filter((item) => {
            if (!term) return true;
            return [
                item.name,
                item.normalized_name,
                ...item.aliases.map((alias) => alias.alias),
                ...item.route_candidates.map((candidate) => candidate.upstream_model_name),
            ].some((value) => value.toLowerCase().includes(term));
        });
    }, [catalog.data, searchTerm]);

    const channelNameById = useMemo(
        () => new Map((channels.data ?? []).map((item) => [item.raw.id, item.raw.name])),
        [channels.data],
    );

    const priceByCanonicalId = useMemo(
        () => new Map((pricing.data ?? []).map((item) => [item.canonical_model_id, item])),
        [pricing.data],
    );

    const sync = () => {
        syncCatalog.mutate(undefined, {
            onSuccess: (result) =>
                toast.success(t('syncComplete'), {
                    description: t('syncResult', {
                        canonical: result.canonical_created,
                        candidates: result.candidates_created + result.candidates_updated,
                    }),
                }),
            onError: (error) => toast.error(t('syncFailed'), { description: errorMessage(error) }),
        });
    };

    const getItemKey = useCallback((item: CanonicalModel) => item.id, []);

    const renderItem = useCallback(
        (model: CanonicalModel) => (
            <CatalogCard
                model={model}
                priceOverview={priceByCanonicalId.get(model.id)}
                pricingLoading={pricing.isLoading}
                pricingError={pricing.error}
                onSelect={() => setSelectedModel(model)}
            />
        ),
        [priceByCanonicalId, pricing.error, pricing.isLoading],
    );

    const header = (
        <div className="flex items-center justify-between gap-2 px-1 pb-2">
            <div>
                <h2 className="text-sm font-semibold">{t('title')}</h2>
                <p className="text-xs tabular-nums text-muted-foreground">
                    {t('modelCount', { count: filtered.length })}
                </p>
            </div>
            <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label={t('sync')}
                title={t('sync')}
                onClick={sync}
                disabled={syncCatalog.isPending}
            >
                <RefreshCw className={cn(syncCatalog.isPending && 'animate-spin')} />
            </Button>
        </div>
    );

    if (catalog.error && !catalog.data) {
        return (
            <div role="alert" className="m-4 rounded-md border border-destructive/30 bg-destructive/10 p-4 text-sm text-destructive">
                <AlertCircle className="mb-2 size-4" />
                <p>{t('loadFailed', { message: errorMessage(catalog.error) })}</p>
                <Button type="button" variant="outline" size="sm" className="mt-3" onClick={() => void catalog.refetch()}>
                    {t('retry')}
                </Button>
            </div>
        );
    }

    if (!catalog.isLoading && filtered.length === 0) {
        return (
            <div className="flex h-full flex-col">
                {header}
                <div className="grid min-h-40 flex-1 place-items-center px-4 text-center text-sm text-muted-foreground">
                    <div>
                        <SearchX className="mx-auto mb-2 size-5" />
                        {t('empty')}
                    </div>
                </div>
            </div>
        );
    }

    return (
        <>
            <VirtualizedGrid
                items={filtered}
                columns={{ default: 1, sm: 2, md: 2, lg: 3, xl: 4 }}
                estimateItemHeight={88}
                gap={12}
                overscan={4}
                getItemKey={getItemKey}
                renderItem={renderItem}
                header={header}
            />
            <CatalogModelDialog
                model={selectedModel}
                priceOverview={selectedModel ? priceByCanonicalId.get(selectedModel.id) : undefined}
                pricingLoading={pricing.isLoading}
                pricingError={pricing.error}
                channelNameById={channelNameById}
                onClose={() => setSelectedModel(null)}
            />
        </>
    );
}

function CatalogCard({
    model,
    priceOverview,
    pricingLoading,
    pricingError,
    onSelect,
}: {
    model: CanonicalModel;
    priceOverview?: CatalogPriceOverview;
    pricingLoading: boolean;
    pricingError?: unknown;
    onSelect: () => void;
}) {
    const t = useTranslations('model.catalog');
    const updateCanonical = useUpdateCanonicalModel();

    const toggleEnabled = (checked: boolean) => {
        updateCanonical.mutate(
            {
                id: model.id,
                routing_strategy: model.routing_strategy,
                protocol_policy: model.protocol_policy,
                allow_lossy: model.allow_lossy,
                enabled: checked,
            },
            {
                onError: (error) =>
                    toast.error(t('canonicalSaveFailed'), { description: errorMessage(error) }),
            },
        );
    };

    return (
        <button
            type="button"
            onClick={onSelect}
            className="flex w-full flex-col gap-2 rounded-lg border bg-card p-3 text-left transition-colors hover:bg-muted/50"
        >
            <div className="flex items-center gap-2">
                <span className="min-w-0 flex-1 truncate text-sm font-medium">
                    {model.name}
                </span>
                {model.vendor ? (
                    <VendorBadge vendor={model.vendor} unknownLabel="" className="shrink-0" />
                ) : null}
            </div>
            <div className="flex items-center justify-between gap-2">
                <span className="text-xs text-muted-foreground">
                    {t('routeCount', { count: model.route_candidates.length })}
                </span>
                <div
                    onClick={(e) => e.stopPropagation()}
                    onKeyDown={(e) => e.stopPropagation()}
                >
                    <Switch
                        checked={model.enabled}
                        onCheckedChange={toggleEnabled}
                        disabled={updateCanonical.isPending}
                        aria-label={t('enabled')}
                    />
                </div>
            </div>
            <div className="flex min-w-0 items-center gap-1.5 border-t pt-2 text-xs">
                <CircleDollarSign className="size-3.5 shrink-0 text-muted-foreground" />
                {priceOverview?.best ? (
                    <>
                        <span className="min-w-0 truncate text-muted-foreground">
                            {priceOverview.best.site_name || `#${priceOverview.best.site_id}`}
                        </span>
                        <span
                            className="min-w-0 truncate font-medium tabular-nums"
                            title={formatCatalogPricePair(priceOverview.best.input, priceOverview.best.output, priceOverview.best.currency)}
                        >
                            {formatCatalogPricePair(priceOverview.best.input, priceOverview.best.output, priceOverview.best.currency)}
                        </span>
                        <span className="shrink-0 text-muted-foreground">{t('perMillion')}</span>
                        <BadgeLowest label={t('lowest')} />
                    </>
                ) : priceOverview ? (
                    <span className="truncate text-muted-foreground">{t('noComparablePrice')}</span>
                ) : pricingError ? (
                    <span className="truncate text-destructive">{t('pricingUnavailable')}</span>
                ) : pricingLoading ? (
                    <span className="truncate text-muted-foreground">{t('pricingLoading')}</span>
                ) : (
                    <span className="truncate text-muted-foreground">{t('noPricing')}</span>
                )}
            </div>
        </button>
    );
}

function formatCatalogPrice(value: number, currency: string) {
    if (!Number.isFinite(value)) return '-';
    return `${new Intl.NumberFormat(undefined, { maximumSignificantDigits: 6 }).format(value)} ${currency}`;
}

function formatCatalogPricePair(input: number, output: number, currency: string) {
    return `${formatCatalogPrice(input, currency)} / ${formatCatalogPrice(output, currency)}`;
}

function BadgeLowest({ label }: { label: string }) {
    return <span className="shrink-0 rounded-sm border border-emerald-500/30 px-1 py-0.5 text-[10px] text-emerald-600">{label}</span>;
}
