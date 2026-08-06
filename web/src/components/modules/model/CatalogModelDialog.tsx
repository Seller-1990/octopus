'use client';

import { useTranslations } from 'next-intl';
import { type CanonicalModel } from '@/api/endpoints/model-catalog';
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
    channelNameById,
    onClose,
}: {
    model: CanonicalModel | null;
    channelNameById: NameMap;
    onClose: () => void;
}) {
    const t = useTranslations('model.catalog');

    return (
        <Dialog open={model !== null} onOpenChange={(open) => { if (!open) onClose(); }}>
            <DialogContent className="sm:max-w-md">
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
