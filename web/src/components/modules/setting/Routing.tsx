'use client';

import { useTranslations } from 'next-intl';
import { Route } from 'lucide-react';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { SettingKey } from '@/api/endpoints/setting';
import { SettingCard, SettingRow, useSettingField } from './shared';

const LOAD_BALANCE_OPTIONS = ['round_robin', 'random', 'failover', 'weighted'] as const;

export function SettingRouting() {
    const t = useTranslations('setting');
    const field = useSettingField(SettingKey.DefaultGroupLoadBalance);

    return (
        <SettingCard icon={Route} title={t('routing.title')}>
            <SettingRow
                label={t('routing.defaultLoadBalance.label')}
                tooltip={t('routing.defaultLoadBalance.description')}
            >
                <Select
                    value={field.value || ''}
                    onValueChange={(v) => field.commit(v)}
                >
                    <SelectTrigger className="w-48 rounded-xl">
                        <SelectValue placeholder={t('routing.defaultLoadBalance.placeholder')} />
                    </SelectTrigger>
                    <SelectContent>
                        {LOAD_BALANCE_OPTIONS.map((opt) => (
                            <SelectItem key={opt} value={opt}>
                                {t(`routing.defaultLoadBalance.options.${opt}`)}
                            </SelectItem>
                        ))}
                    </SelectContent>
                </Select>
            </SettingRow>
        </SettingCard>
    );
}
