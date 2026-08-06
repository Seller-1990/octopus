'use client';

import { useTranslations } from 'next-intl';
import { Route } from 'lucide-react';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Input } from '@/components/ui/input';
import { SettingKey } from '@/api/endpoints/setting';
import { SettingCard, SettingRow, useSettingField } from './shared';

const LOAD_BALANCE_OPTIONS = ['round_robin', 'random', 'failover', 'weighted'] as const;
const SORT_STRATEGY_OPTIONS = ['non_relay_balance', 'non_relay_multiplier', 'multiplier_balance', 'balance_only'] as const;

export function SettingRouting() {
    const t = useTranslations('setting');
    const field = useSettingField(SettingKey.DefaultGroupLoadBalance);
    const sortField = useSettingField(SettingKey.DefaultGroupSortStrategy);
    const multiplierCapField = useSettingField(SettingKey.DefaultMultiplierCap);

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
            <SettingRow
                label={t('routing.defaultSortStrategy.label')}
                tooltip={t('routing.defaultSortStrategy.description')}
            >
                <Select
                    value={sortField.value || ''}
                    onValueChange={(v) => sortField.commit(v)}
                >
                    <SelectTrigger className="w-48 rounded-xl">
                        <SelectValue placeholder={t('routing.defaultSortStrategy.placeholder')} />
                    </SelectTrigger>
                    <SelectContent>
                        {SORT_STRATEGY_OPTIONS.map((opt) => (
                            <SelectItem key={opt} value={opt}>
                                {t(`routing.defaultSortStrategy.options.${opt}`)}
                            </SelectItem>
                        ))}
                    </SelectContent>
                </Select>
            </SettingRow>
            <SettingRow
                label={t('routing.defaultMultiplierCap.label')}
                tooltip={t('routing.defaultMultiplierCap.description')}
            >
                <Input
                    type="number"
                    className="w-48 rounded-xl"
                    value={multiplierCapField.value}
                    placeholder={t('routing.defaultMultiplierCap.placeholder')}
                    onChange={(e) => multiplierCapField.setValue(e.target.value)}
                    onBlur={() => multiplierCapField.save()}
                    onKeyDown={(e) => { if (e.key === 'Enter') multiplierCapField.save(); }}
                />
            </SettingRow>
        </SettingCard>
    );
}
