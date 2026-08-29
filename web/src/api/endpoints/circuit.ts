import { useQuery } from '@tanstack/react-query';
import { apiClient } from '../client';

export interface CircuitStatusItem {
    channel_id: number;
    channel_key_id: number;
    model_name: string;
    state: number;
    state_label: 'closed' | 'open' | 'half_open';
    consecutive_failures: number;
    trip_count: number;
    last_failure_time: string;
    cooldown_until?: string;
}

export interface CircuitStatusResponse {
    items: CircuitStatusItem[];
    open: number;
    half_open: number;
}

/**
 * 熔断状态概览。熔断页从主导航降级后，首页告警条与设置页入口共用本查询；
 * 低频轮询即可——熔断状态分钟级变化，open/half_open 计数由后端汇总。
 */
export function useCircuitAlert(enabled = true) {
    return useQuery({
        queryKey: ['circuit', 'status'],
        queryFn: () => apiClient.get<CircuitStatusResponse>('/api/v1/circuit/status'),
        enabled,
        staleTime: 30_000,
        refetchInterval: 60_000,
        refetchOnWindowFocus: true,
    });
}
