import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';
import { logger } from '@/lib/logger';

/**
 * 后端 /api/v1/update 返回的最新发布信息
 */
export interface LatestInfo {
    tag_name: string;
    published_at: string;
    body: string;
    message: string;
    /** 当前运行在容器中时后端置 true，前端据此禁用自动更新入口 */
    container?: boolean;
    /** 当前发行形态支持安全原地更新时为 true */
    auto_update?: boolean;
    /** 当前 fork 的最新 Release 页面 */
    release_url?: string;
}

/**
 * 获取最新发布信息 Hook
 * 
 * @example
 * const { data: latestInfo, isLoading, error } = useLatestInfo();
 * 
 * if (isLoading) return <Loading />;
 * if (error) return <Error message={error.message} />;
 * 
 * console.log('Latest tag:', latestInfo?.tag_name);
 */
export function useLatestInfo() {
    return useQuery({
        queryKey: ['update', 'latest'],
        queryFn: async () => {
            return apiClient.get<LatestInfo>('/api/v1/update');
        },
        refetchInterval: 3600000, // 1 小时
    });
}

/**
 * 获取后端当前版本 Hook
 *
 * 后端: GET /api/v1/update/now-version -> string
 */
export function useNowVersion() {
    return useQuery({
        queryKey: ['update', 'now-version'],
        queryFn: async () => {
            return apiClient.get<string>('/api/v1/update/now-version');
        },
        refetchInterval: 3600000, // 1 小时
    });
}

/**
 * 执行更新 Hook
 * 
 * @example
 * const updateCore = useUpdateCore();
 * 
 * updateCore.mutate(undefined, {
 *   onSuccess: () => {
 *     console.log('Update started successfully');
 *   },
 * });
 */
export function useUpdateCore() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async () => {
            return apiClient.post<string>('/api/v1/update');
        },
        onSuccess: (data) => {
            logger.log('更新成功:', data);
            queryClient.invalidateQueries({ queryKey: ['update', 'latest'] });
            queryClient.invalidateQueries({ queryKey: ['update', 'now-version'] });
        },
        onError: (error) => {
            logger.error('更新失败:', error);
        },
    });
}
