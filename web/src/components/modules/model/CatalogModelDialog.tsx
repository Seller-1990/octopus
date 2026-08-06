'use client';

import { CircleDollarSign, Loader2 } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { type CanonicalModel, type CatalogPriceOverview, type CatalogPriceSummary } from '@/api/endpoints/model-catalog';
import { Badge } from '@/components/ui/badge';
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogDescription,
} from '@/components/ui/dialog';
import { VendorBadge } from './VendorBadge';

type NameMap = Map<number, string>;

export function CatalogModelDialog({
    model,
    priceOverview,
    pricingLoading,
    pricingError,
    channelNameById,
    onClose,
}: {
    model: CanonicalModel | null;
    priceOverview?: CatalogPriceOverview;
    pricingLoading: boolean;
    pricingError?: unknown;
    channelNameById: NameMap;
    onClose: () => void;
}) {
    const t = useTranslations('model.catalog');

    return (
        <Dialog open={model !== null} onOpenChange={(open) => { if (!open) onClose(); }}>
            <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-2xl">
                {model ? (
                    <>
                        <DialogHeader>
                            <DialogTitle className="flex items-center gap-2">
                                <span className="min-w-0 truncate">{model.name}</span>
                                {model.vendor ? (
                                    <VendorBadge vendor={model.vendor} unknownLabel="" className="shrink-0" />
                                ) : null}
                            </DialogTitle>
                            <DialogDescription>{model.normalized_name}</DialogDescription>
                        </DialogHeader>

                        <section className="space-y-2">
                            <h3 className="text-sm font-semibold">{t('aliases')}</h3>
                            {model.aliases.length > 0 ? (
                                <div className="flex flex-wrap gap-1.5">
                                    {model.aliases.map((alias) => (
                                        <Badge key={alias.id} variant="secondary" className="max-w-full">
                                            <span className="truncate">{alias.alias}</span>
                                        </Badge>
                                    ))}
                                </div>
                            ) : (
                                <p className="text-xs text-muted-foreground">{t('noAliases')}</p>
                            )}
                        </section>

                        <PricingOverview
                            overview={priceOverview}
                            loading={pricingLoading}
                            error={pricingError}
                        />

                        <section className="space-y-2">
                            <h3 className="text-sm font-semibold">{t('candidates')}</h3>
                            {model.route_candidates.length > 0 ? (
                                <ul className="space-y-1">
                                    {model.route_candidates.map((candidate) => (
                                        <li
                                            key={candidate.id}
                                            className="flex items-center justify-between gap-2 rounded-md border px-3 py-2 text-sm"
                                        >
                                            <span className="min-w-0 truncate">
                                                {channelNameById.get(candidate.channel_id) ?? `#${candidate.channel_id}`}
                                            </span>
                                            <Badge variant="outline" className="shrink-0">
                                                {candidate.upstream_model_name}
                                            </Badge>
                                        </li>
                                    ))}
                                </ul>
                            ) : (
                                <p className="text-xs text-muted-foreground">{t('noCandidates')}</p>
                            )}
                        </section>
                    </>
                ) : null}
            </DialogContent>
        </Dialog>
    );
}

const PRICE_NUMBER_FORMATTER = new Intl.NumberFormat(undefined, {
    maximumSignificantDigits: 6,
});

function formatPrice(value: number) {
    return Number.isFinite(value) && value > 0 ? PRICE_NUMBER_FORMATTER.format(value) : '-';
}

function PricingOverview({
    overview,
    loading,
    error,
}: {
    overview?: CatalogPriceOverview;
    loading: boolean;
    error?: unknown;
}) {
    const t = useTranslations('model.catalog');
    const tp = useTranslations('model.pricing');

    return (
        <section className="space-y-2">
            <h3 className="flex items-center gap-1.5 text-sm font-semibold">
                <CircleDollarSign className="size-3.5 text-muted-foreground" />
                {t('pricingOverview')}
            </h3>
            {loading ? (
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                    <Loader2 className="size-3 animate-spin" />
                    {tp('loading')}
                </div>
            ) : error ? (
                <p role="alert" className="text-xs text-destructive">{t('pricingLoadFailed')}</p>
            ) : overview && overview.prices.length > 0 ? (
                <div className="space-y-2">
                    {overview.best ? (
                        <div className="rounded-md border border-emerald-500/30 bg-emerald-500/5 px-3 py-2">
                            <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
                                <span className="font-medium">{t('lowestPrice')}</span>
                                <span className="tabular-nums">
                                    {formatPrice(overview.best.input)} / {formatPrice(overview.best.output)} {overview.best.currency}
                                </span>
                            </div>
                            <p className="mt-1 text-xs text-muted-foreground">
                                {overview.best.site_name || `#${overview.best.site_id}`}
                                {overview.best.site_account_name ? ` · ${overview.best.site_account_name}` : ''}
                            </p>
                        </div>
                    ) : null}
                    <div className="space-y-1.5">
                        {overview.prices.map((price) => (
                            <CatalogPriceRow key={price.route_candidate_id} price={price} />
                        ))}
                    </div>
                </div>
            ) : (
                <p className="text-xs text-muted-foreground">{overview ? t('noPricing') : t('pricingLoading')}</p>
            )}
        </section>
    );
}

function CatalogPriceRow({ price }: { price: CatalogPriceSummary }) {
    const t = useTranslations('model.catalog');
    const tp = useTranslations('model.pricing');
    return (
        <div className="flex min-w-0 items-start justify-between gap-3 rounded-md border px-3 py-2 text-sm">
            <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-1.5">
                    <span className="max-w-[13rem] truncate font-medium">
                        {price.site_name || `#${price.site_id}`}
                    </span>
                    {price.site_account_name ? (
                        <span className="max-w-[10rem] truncate text-xs text-muted-foreground">
                            {price.site_account_name}
                        </span>
                    ) : null}
                    <Badge variant="outline" className="text-xs">{tp(`source.${price.source}`)}</Badge>
                    {price.stale ? <Badge variant="destructive" className="text-xs">{tp('stale')}</Badge> : null}
                </div>
                <p className="mt-1 truncate text-xs text-muted-foreground">
                    {price.upstream_model_name} · {tp(`unit.${price.unit}`)}
                    {!price.comparable ? ` · ${t('notComparable')}` : ''}
                </p>
            </div>
            <div className="shrink-0 text-right text-xs tabular-nums">
                <div>{tp('input')} {formatPrice(price.input)} {price.currency}</div>
                <div className="text-muted-foreground">{tp('output')} {formatPrice(price.output)} {price.currency}</div>
            </div>
        </div>
    );
}
