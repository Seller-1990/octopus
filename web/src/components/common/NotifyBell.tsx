'use client';

import { useEffect, useMemo, useRef, useState } from 'react';
import { Bell, CheckCheck } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { type NotifyEvent, type NotifyLevel, useNotifyStream } from '@/api/endpoints/notify';
import { Button } from '@/components/ui/button';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { cn } from '@/lib/utils';

const READ_WATERMARK_KEY = 'notify_read_watermark';
const BROWSER_NOTIFY_KEY = 'notify_browser_enabled';
const NOTIFIED_WATERMARK_KEY = 'notify_notified_watermark';

const LEVEL_DOT: Record<NotifyLevel, string> = {
    info: 'bg-blue-500',
    success: 'bg-emerald-500',
    warn: 'bg-amber-500',
    error: 'bg-red-500',
};

// 顶栏通知铃铛（N5）：未读数 = 事件 id 大于本地已读水位。
// 浏览器 Notification 为可选开关（localStorage），授权一次后新事件到达即弹。
export function NotifyBell() {
    const t = useTranslations('notify');
    const { events, connected } = useNotifyStream();

    const [open, setOpen] = useState(false);
    const [watermark, setWatermark] = useState(0);
    const [browserEnabled, setBrowserEnabled] = useState(false);
    const initialized = useRef(false);

    useEffect(() => {
        // lint react-hooks/set-state-in-effect：本地存储初始化经微任务延迟
        queueMicrotask(() => {
            const stored = Number(localStorage.getItem(READ_WATERMARK_KEY) ?? '0');
            setWatermark(Number.isFinite(stored) ? stored : 0);
            setBrowserEnabled(localStorage.getItem(BROWSER_NOTIFY_KEY) === 'true' && typeof Notification !== 'undefined');
            initialized.current = true;
        });
    }, []);

    // 打开面板即把水位推到最新事件（经微任务延迟满足 set-state-in-effect）
    useEffect(() => {
        if (!open) return;
        const latest = events[0]?.id ?? 0;
        queueMicrotask(() => {
            setWatermark((prev) => {
                const next = Math.max(prev, latest);
                if (next !== prev) localStorage.setItem(READ_WATERMARK_KEY, String(next));
                return next;
            });
        });
    }, [open, events]);

    // 浏览器通知：只弹「已通知水位」之后的新事件（error/warn）。水位持久化
    // 到 localStorage——重连/刷新后服务端快照重放旧事件不再逐条轰炸（ocr 采纳）。
    const notifiedId = useRef(0);
    useEffect(() => {
        if (!browserEnabled || typeof Notification === 'undefined' || Notification.permission !== 'granted') return;
        if (notifiedId.current === 0) {
            // 首次装载：以当前最大 id 为基线，历史事件静默跳过
            const maxId = events.reduce((max, item) => Math.max(max, item.id), 0);
            notifiedId.current = Math.max(maxId, Number(localStorage.getItem(NOTIFIED_WATERMARK_KEY) ?? '0'));
            return;
        }
        const latest = events[0];
        if (!latest || latest.id <= notifiedId.current) return;
        notifiedId.current = latest.id;
        localStorage.setItem(NOTIFIED_WATERMARK_KEY, String(latest.id));
        if (latest.level === 'error' || latest.level === 'warn') {
            try {
                new Notification(latest.title, { body: latest.body });
            } catch {
                // 通知失败不影响页面
            }
        }
    }, [events, browserEnabled]);

    const unreadCount = useMemo(
        () => events.filter((event) => event.id > watermark).length,
        [events, watermark],
    );

    const toggleBrowser = async () => {
        const next = !browserEnabled;
        try {
            if (next && typeof Notification !== 'undefined' && Notification.permission !== 'granted') {
                const permission = await Notification.requestPermission();
                if (permission !== 'granted') return;
            }
        } catch {
            return; // 部分浏览器会 reject 权限请求
        }
        setBrowserEnabled(next);
        localStorage.setItem(BROWSER_NOTIFY_KEY, String(next));
    };

    const markAllRead = () => {
        // Math.max 防回退：事件列表为空（如刚刷新）时不得把水位降回 0
        setWatermark((prev) => {
            const latest = Math.max(prev, events[0]?.id ?? 0);
            localStorage.setItem(READ_WATERMARK_KEY, String(latest));
            return latest;
        });
    };

    return (
        <Popover open={open} onOpenChange={setOpen}>
            <PopoverTrigger asChild>
                <button
                    type="button"
                    aria-label={t('bell')}
                    className="relative inline-flex size-10 items-center justify-center rounded-full border bg-card text-foreground transition-colors hover:bg-muted"
                >
                    <Bell className="size-4" />
                    {unreadCount > 0 ? (
                        <span className="absolute -top-1 -right-1 flex size-5 items-center justify-center rounded-full bg-destructive text-[10px] font-medium text-white">
                            {unreadCount > 99 ? '99+' : unreadCount}
                        </span>
                    ) : null}
                    <span
                        aria-hidden
                        className={cn(
                            'absolute bottom-1.5 right-1.5 size-1.5 rounded-full transition-colors',
                            connected ? 'bg-emerald-500' : 'bg-muted-foreground/40',
                        )}
                    />
                </button>
            </PopoverTrigger>
            <PopoverContent align="end" className="w-96 p-0">
                <div className="flex items-center justify-between border-b px-3 py-2">
                    <span className="text-sm font-medium">{t('title')}</span>
                    <div className="flex items-center gap-1">
                        <Button variant="ghost" size="sm" onClick={toggleBrowser} className="h-7 px-2 text-xs">
                            {browserEnabled ? t('browserOn') : t('browserOff')}
                        </Button>
                        <Button variant="ghost" size="sm" onClick={markAllRead} className="h-7 px-2 text-xs">
                            <CheckCheck className="size-3.5" />
                            {t('markAllRead')}
                        </Button>
                    </div>
                </div>
                <div className="max-h-96 overflow-y-auto">
                    {events.length === 0 ? (
                        <p className="text-muted-foreground px-3 py-6 text-center text-sm">{t('empty')}</p>
                    ) : (
                        events.map((event) => (
                            <NotifyRow key={event.id} event={event} unread={event.id > watermark} />
                        ))
                    )}
                </div>
            </PopoverContent>
        </Popover>
    );
}

function NotifyRow({ event, unread }: { event: NotifyEvent; unread: boolean }) {
    const t = useTranslations('notify');
    return (
        <div className={cn('border-t px-3 py-2', unread && 'bg-muted/40')}>
            <div className="flex items-center gap-2">
                <span className={cn('size-2 shrink-0 rounded-full', LEVEL_DOT[event.level] ?? 'bg-gray-400')} />
                <span className="min-w-0 flex-1 truncate text-sm font-medium">{event.title}</span>
                <span className="text-muted-foreground shrink-0 text-xs whitespace-nowrap">
                    {new Date(event.time).toLocaleTimeString()}
                </span>
            </div>
            {event.body ? (
                <p className="text-muted-foreground mt-1 line-clamp-2 pl-4 text-xs">{event.body}</p>
            ) : null}
            {unread ? <span className="sr-only">{t('unread')}</span> : null}
        </div>
    );
}
