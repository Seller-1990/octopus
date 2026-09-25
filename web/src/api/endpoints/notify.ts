import { useEffect, useState } from 'react';
import { apiClient, API_BASE_URL } from '../client';

// 任务通知（PLAN_TASK_NOTIFY N5）：SSE 订阅 /api/v1/notify/stream。
// 事件由服务端 ring buffer 维护（最近 100 条），快照+增量共用同一事件流，
// 前端按 id 去重合并即可，无需区分快照与增量。

export type NotifyLevel = 'info' | 'success' | 'warn' | 'error';

export type NotifyEvent = {
    id: number;
    type: string;
    level: NotifyLevel;
    title: string;
    body: string;
    time: string;
    data?: Record<string, string>;
};

const NOTIFY_RETRY_BASE_MS = 1000;
const NOTIFY_RETRY_MAX_MS = 30_000;
const NOTIFY_MAX_LOCAL_EVENTS = 100;

export function useNotifyStream(enabled = true) {
    const [events, setEvents] = useState<NotifyEvent[]>([]);
    const [connected, setConnected] = useState(false);

    useEffect(() => {
        if (!enabled) return;
        let cancelled = false;
        let eventSource: EventSource | null = null;
        let retryTimer: ReturnType<typeof setTimeout> | null = null;
        let retryDelay = NOTIFY_RETRY_BASE_MS;

        const scheduleReconnect = () => {
            if (cancelled) return;
            setConnected(false);
            retryTimer = setTimeout(() => {
                retryTimer = null;
                void connect();
            }, retryDelay);
            retryDelay = Math.min(retryDelay * 2, NOTIFY_RETRY_MAX_MS);
        };

        const connect = async () => {
            try {
                const { token } = await apiClient.get<{ token: string }>('/api/v1/log/stream-token');
                if (cancelled) return;
                eventSource = new EventSource(`${API_BASE_URL}/api/v1/notify/stream?token=${token}`);
                eventSource.addEventListener('notify', (raw) => {
                    if (cancelled) return;
                    try {
                        const event = JSON.parse((raw as MessageEvent).data) as NotifyEvent;
                        setEvents((prev) => {
                            if (prev.some((item) => item.id === event.id)) return prev;
                            const next = [event, ...prev];
                            return next.slice(0, NOTIFY_MAX_LOCAL_EVENTS);
                        });
                    } catch {
                        // 单条解析失败不影响连接
                    }
                });
                eventSource.onopen = () => {
                    if (cancelled) return;
                    setConnected(true);
                    retryDelay = NOTIFY_RETRY_BASE_MS;
                };
                eventSource.onerror = () => {
                    eventSource?.close();
                    eventSource = null;
                    scheduleReconnect();
                };
            } catch {
                scheduleReconnect();
            }
        };

        void connect();
        return () => {
            cancelled = true;
            if (retryTimer) clearTimeout(retryTimer);
            eventSource?.close();
            setConnected(false);
        };
    }, [enabled]);

    return { events, connected };
}
