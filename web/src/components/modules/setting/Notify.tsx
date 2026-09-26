'use client';

import { useEffect, useState } from 'react';
import { Bell } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { SettingKey } from '@/api/endpoints/setting';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { SettingCard, SettingRow, useSettingField, useSettingToggle } from './shared';

const BROWSER_NOTIFY_KEY = 'notify_browser_enabled';

// 任务结果通知设置（PLAN_TASK_NOTIFY N6）：Webhook 三项 + 浏览器通知开关。
export function SettingNotify() {
    const t = useTranslations('setting.notify');
    const urlField = useSettingField(SettingKey.NotifyWebhookURL);
    const eventsField = useSettingField(SettingKey.NotifyWebhookEvents);
    const webhookEnabled = useSettingToggle(SettingKey.NotifyWebhookEnabled);

    const [browserSupported, setBrowserSupported] = useState(true);
    const [browserEnabled, setBrowserEnabled] = useState(false);

    useEffect(() => {
        // lint react-hooks/set-state-in-effect：本地存储初始化经微任务延迟
        queueMicrotask(() => {
            // HTTP（非 localhost）下 Notification 恒为 denied，与不支持同等对待
            setBrowserSupported('Notification' in window && window.isSecureContext);
            setBrowserEnabled(localStorage.getItem(BROWSER_NOTIFY_KEY) === 'true');
        });
    }, []);

    const toggleBrowser = async (next: boolean) => {
        if (next && Notification.permission !== 'granted') {
            const permission = await Notification.requestPermission();
            if (permission !== 'granted') return;
        }
        setBrowserEnabled(next);
        localStorage.setItem(BROWSER_NOTIFY_KEY, String(next));
    };

    return (
        <SettingCard icon={Bell} title={t('title')} tooltip={t('description')}>
            <SettingRow label={t('enabled')} htmlFor="notify-webhook-enabled">
                <Switch
                    id="notify-webhook-enabled"
                    checked={webhookEnabled.enabled}
                    onCheckedChange={webhookEnabled.toggle}
                />
            </SettingRow>
            <SettingRow label={t('webhookUrl')} htmlFor="notify-webhook-url">
                <Input
                    id="notify-webhook-url"
                    value={urlField.value}
                    onChange={(event) => urlField.setValue(event.target.value)}
                    onBlur={urlField.save}
                    placeholder="https://n8n.local/webhook/octopus"
                    className="rounded-xl"
                    autoComplete="off"
                />
            </SettingRow>
            <SettingRow label={t('events')} tooltip={t('eventsHelp')} htmlFor="notify-webhook-events">
                <Input
                    id="notify-webhook-events"
                    value={eventsField.value}
                    onChange={(event) => eventsField.setValue(event.target.value)}
                    onBlur={eventsField.save}
                    placeholder="site.batch,site.checkin.failed"
                    className="rounded-xl"
                    autoComplete="off"
                />
            </SettingRow>
            <SettingRow
                label={t('browser')}
                tooltip={browserSupported ? t('browserHelp') : t('browserUnsupported')}
                htmlFor="notify-browser"
            >
                {browserSupported ? (
                    <Switch
                        id="notify-browser"
                        checked={browserEnabled}
                        onCheckedChange={toggleBrowser}
                    />
                ) : (
                    <span className="text-muted-foreground text-xs">{t('browserUnsupported')}</span>
                )}
            </SettingRow>
        </SettingCard>
    );
}
