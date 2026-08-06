'use client';

import { PageWrapper } from '@/components/common/PageWrapper';
import { APIKeyPanelBase } from '@/components/modules/setting/APIKey';

export function APIKeyPage() {
    return (
        <div className="h-full min-h-0 overflow-y-auto overscroll-contain rounded-t-3xl">
            <PageWrapper className="pb-24 md:pb-4">
                <APIKeyPanelBase
                    idPrefix="apikey-page"
                    containerClassName="rounded-3xl border border-border bg-card p-6 space-y-5 relative"
                    listClassName="space-y-2 max-h-[calc(100dvh-16rem)] overflow-y-auto"
                    showSearch
                />
            </PageWrapper>
        </div>
    );
}
