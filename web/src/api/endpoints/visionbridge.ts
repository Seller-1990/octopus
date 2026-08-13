import { useMutation } from '@tanstack/react-query';
import { apiClient } from '../client';

/**
 * 视觉桥单个 VLM 模型的连通性测试结果
 */
export interface VisionBridgeProbeResult {
    model: string;
    ok: boolean;
    latency_ms: number;
    error?: string;
    preview?: string;
}

export interface VisionBridgeTestRequest {
    model: string;
    base_url: string;
    /** 空字符串 = 使用已保存的密钥（列表接口出于安全不回显） */
    api_key: string;
    /** 逗号分隔的备选模型 */
    fallback_models: string;
}

/**
 * 用内置测试图逐个真调 VLM 模型链（设置页「测试」按钮）
 */
export function useVisionBridgeTest() {
    return useMutation({
        mutationFn: async (data: VisionBridgeTestRequest) =>
            apiClient.post<VisionBridgeProbeResult[]>('/api/v1/vision-bridge/test', data),
    });
}
