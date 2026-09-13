'use client';

import { ProxySelector } from '@/components/modules/proxy-pool/ProxySelector';
import type { ChannelFormData } from '../Form';

interface Props {
    formData: ChannelFormData;
    onFormDataChange: (data: ChannelFormData) => void;
}

export function FormNetworkSection({ formData, onFormDataChange }: Props) {
    return (
        <div className="rounded-xl border bg-card p-4">
            <ProxySelector
                value={{ proxy_mode: formData.proxy_mode, proxy_config_id: formData.proxy_config_id }}
                onChange={(next) => onFormDataChange({
                    ...formData,
                    proxy_mode: next.proxy_mode as ChannelFormData['proxy_mode'],
                    proxy_config_id: next.proxy_config_id ?? null,
                })}
            />
        </div>
    );
}
