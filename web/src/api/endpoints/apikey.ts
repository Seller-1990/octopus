import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';
import { logger } from '@/lib/logger';
import { useAuthStore } from './user';
import { StatsAPIKey, StatsAPIKeyFormatted } from './stats';
import { formatCount, formatMoney, formatTime } from '@/lib/utils';

/**
 * API Key 数据
 */
export interface APIKey {
    id: number;
    name: string;
    api_key: string;
    enabled: boolean;
    expire_at?: number; // Unix 时间戳（秒），不传表示永不过期
    max_cost?: number; // 不传表示无限制
    max_rpm?: number; // 不传表示无限制
    supported_models?: string; // 不传表示支持所有模型
    tools_only?: boolean; // 仅 tools：勾选后该 Key 全部请求只走支持 tools 的渠道模型
    vision_bridge?: boolean; // 视觉桥：允许该 Key 的含图请求在纯文本模型上替换为 VLM 描述（还需全局开关）
    quota_limit: number; // 费用额度上限，0 表示不限制
    quota_period: 'daily' | 'weekly' | 'monthly';
    quota_used: number;
    quota_reset_at?: number;
}

/**
 * API Key Stats 响应（包含 stats 和 info）
 */
export interface APIKeyStatsResponse {
    stats: StatsAPIKey;
    info: APIKey;
}

export interface APIKeyStatsResponseFormatted {
    stats: StatsAPIKeyFormatted;
    info: APIKey;
}

/**
 * API Key 登录 Hook（仅校验 key 是否有效）
 */
export function useAPIKeyLogin() {
    const { setAPIKeyAuth, logout } = useAuthStore();

    return useMutation({
        mutationFn: async (apiKey: string) => {
            // 先设置以便 apiClient 发送请求时带上 token
            setAPIKeyAuth(apiKey);
            await apiClient.get<null>('/api/v1/apikey/login');
            return apiKey;
        },
        onError: (error) => {
            logout();
            logger.error('API Key 登录失败:', error);
        },
    });
}

/**
 * 获取当前 API Key 的详细统计数据 Hook（仅 API Key 登录用户使用）
 */
export function useAPIKeyDashboardStats() {
    const { isAPIKeyAuth, isAuthenticated } = useAuthStore();

    return useQuery({
        queryKey: ['apikey', 'dashboard', 'stats'],
        queryFn: () => apiClient.get<APIKeyStatsResponse>('/api/v1/apikey/stats'),
        select: (data): APIKeyStatsResponseFormatted => ({
            stats: {
                api_key_id: data.stats.api_key_id,
                input_token: formatCount(data.stats.input_token),
                output_token: formatCount(data.stats.output_token),
                total_token: formatCount(data.stats.input_token + data.stats.output_token),
                input_cost: formatMoney(data.stats.input_cost),
                output_cost: formatMoney(data.stats.output_cost),
                total_cost: formatMoney(data.stats.input_cost + data.stats.output_cost),
                wait_time: formatTime(data.stats.wait_time),
                request_success: formatCount(data.stats.request_success),
                request_failed: formatCount(data.stats.request_failed),
                request_count: formatCount(data.stats.request_success + data.stats.request_failed),
            },
            info: data.info,
        }),
        enabled: isAPIKeyAuth && isAuthenticated,
        refetchInterval: 30000,
    });
}

/**
 * 创建 API Key 请求
 */
export type CreateAPIKeyRequest = Omit<APIKey, 'id' | 'api_key'> & { enabled?: boolean };

/**
 * 更新 API Key 请求
 */
export type UpdateAPIKeyRequest = Pick<APIKey, 'id'> & CreateAPIKeyRequest;

/**
 * 获取 API Key 列表 Hook
 * 
 * @example
 * const { data: apiKeys, isLoading, error } = useAPIKeyList();
 * 
 * if (isLoading) return <Loading />;
 * if (error) return <Error message={error.message} />;
 * 
 * apiKeys?.forEach(key => console.log(key.name));
 */
export function useAPIKeyList() {
    return useQuery({
        queryKey: ['apikeys', 'list'],
        queryFn: async () => {
            return apiClient.get<APIKey[]>('/api/v1/apikey/list');
        },
        refetchInterval: 30000,
    });
}

/**
 * 创建 API Key Hook
 * 
 * @example
 * const createAPIKey = useCreateAPIKey();
 * 
 * createAPIKey.mutate({
 *   name: 'My API Key',
 * });
 */
export function useCreateAPIKey() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async (data: CreateAPIKeyRequest) => {
            return apiClient.post<APIKey>('/api/v1/apikey/create', data);
        },
        onSuccess: (data) => {
            logger.log('API Key 创建成功:', data);
            queryClient.invalidateQueries({ queryKey: ['apikeys', 'list'] });
        },
        onError: (error) => {
            logger.error('API Key 创建失败:', error);
        },
    });
}

/**
 * 更新 API Key Hook
 * 
 * @example
 * const updateAPIKey = useUpdateAPIKey();
 * 
 * updateAPIKey.mutate({
 *   id: 1,
 *   name: 'Updated API Key',
 *   enabled: false,
 * });
 */
export function useUpdateAPIKey() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async (data: UpdateAPIKeyRequest) => {
            return apiClient.post<APIKey>('/api/v1/apikey/update', data);
        },
        // 乐观更新：行内三个开关共用整对象 spread（后端 Save 全量覆盖），
        // 若等 refetch 才回写缓存，连续点击会基于旧对象把彼此的改动清零。
        onMutate: async (data) => {
            await queryClient.cancelQueries({ queryKey: ['apikeys', 'list'] });
            const previous = queryClient.getQueryData<APIKey[]>(['apikeys', 'list']);
            queryClient.setQueryData<APIKey[]>(['apikeys', 'list'], (old) =>
                old?.map((key) => (key.id === data.id ? { ...key, ...data } : key)),
            );
            return { previous };
        },
        onError: (error, _data, context) => {
            if (context?.previous) {
                queryClient.setQueryData(['apikeys', 'list'], context.previous);
            }
            logger.error('API Key 更新失败:', error);
        },
        onSuccess: (data) => {
            logger.log('API Key 更新成功:', data);
        },
        onSettled: () => {
            queryClient.invalidateQueries({ queryKey: ['apikeys', 'list'] });
        },
    });
}

/**
 * 删除 API Key Hook
 * 
 * @example
 * const deleteAPIKey = useDeleteAPIKey();
 * 
 * deleteAPIKey.mutate(1); // 删除 ID 为 1 的 API Key
 */
export function useDeleteAPIKey() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async (id: number) => {
            return apiClient.delete<null>(`/api/v1/apikey/delete/${id}`);
        },
        onSuccess: () => {
            logger.log('API Key 删除成功');
            queryClient.invalidateQueries({ queryKey: ['apikeys', 'list'] });
        },
        onError: (error) => {
            logger.error('API Key 删除失败:', error);
        },
    });
}
