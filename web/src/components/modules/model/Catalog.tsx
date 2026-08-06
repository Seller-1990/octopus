'use client';

import { useCallback, useMemo, useState } from 'react';
import { AlertCircle, RefreshCw, SearchX } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { useChannelList } from '@/api/endpoints/channel';
import {
    type CanonicalModel,
    useModelCatalog,
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
                onSelect={() => setSelectedModel(model)}
            />
        ),
        [],
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
                channelNameById={channelNameById}
                onClose={() => setSelectedModel(null)}
            />
        </>
    );
}

function CatalogCard({
    model,
    onSelect,
}: {
    model: CanonicalModel;
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
        </button>
    );
}
