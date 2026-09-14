import { translateApiErrorCode } from './error-i18n';
import { shouldLogoutOnUnauthorized } from './session-guard';
import type { ApiError, ApiErrorParams } from './types';
import { HttpStatus } from './types';

export const API_BASE_URL = process.env.NEXT_PUBLIC_API_BASE_URL || '.';

/**
 * 获取认证 Store（延迟导入以避免循环依赖）
 */
let getAuthStore: (() => { token: string | null; logout: () => void }) | null = null;

export function setAuthStoreGetter(getter: () => { token: string | null; logout: () => void }) {
    getAuthStore = getter;
}

/**
 * 全局错误处理
 *
 * requestToken 是发送该请求时使用的会话令牌：401 只能登出「发出该请求的
 * 那个会话」，旧请求迟到的新会话 401 不得清空当前登录（F05）。
 */
const handleError = (error: ApiError, requestToken?: string | null) => {
    console.error('API Error:', error);

    // 401 未授权，调用 store 的 logout
    if (error.code === HttpStatus.UNAUTHORIZED) {
        if (getAuthStore) {
            const store = getAuthStore();
            if (shouldLogoutOnUnauthorized(requestToken, store.token)) {
                store.logout();
            }
        }
    }
};

function isApiErrorParams(value: unknown): value is ApiErrorParams {
    if (!value || typeof value !== 'object' || Array.isArray(value)) {
        return false;
    }
    return Object.values(value).every((item) => (
        item === null ||
        item === undefined ||
        typeof item === 'string' ||
        typeof item === 'number' ||
        typeof item === 'boolean'
    ));
}

/** 请求整体超时：覆盖列表轮询与常规管理操作（上传走独立的 mini-client） */
const REQUEST_TIMEOUT_MS = 30_000;

/** 网络层失败（断网/DNS/超时）没有服务器错误码，用本地错误码走同一翻译通道 */
function buildTransportError(errorCode: string): ApiError {
    const message = translateApiErrorCode(errorCode, errorCode);
    return Object.assign(new Error(message), {
        code: 0,
        errorCode,
    }) as ApiError;
}

/**
 * 处理响应
 */
async function handleResponse<T>(response: Response, requestToken?: string | null): Promise<T> {
    const contentType = response.headers.get('content-type');
    const isJson = contentType?.includes('application/json');

    let data: unknown;
    if (isJson) {
        data = await response.json();
    } else {
        data = await response.text();
    }

    if (!response.ok) {
        const rawMessage = (data && typeof data === 'object' && 'message' in data && typeof data.message === 'string')
            ? data.message
            : (typeof data === 'string' ? data : response.statusText);
        const errorCode = (data && typeof data === 'object' && 'error_code' in data && typeof data.error_code === 'string')
            ? data.error_code
            : undefined;
        const errorParams = (data && typeof data === 'object' && 'params' in data && isApiErrorParams(data.params))
            ? data.params
            : undefined;
        const message = translateApiErrorCode(errorCode, rawMessage, errorParams);
        const error = Object.assign(new Error(message), {
            code: response.status,
            errorCode,
            rawMessage,
            params: errorParams,
        }) as ApiError;

        handleError(error, requestToken);
        throw error;
    }

    // 如果是标准的 ApiResponse 格式，返回 data 字段
    if (data && typeof data === 'object' && 'data' in data) {
        return data.data as T;
    }

    return data as T;
}

/**
 * 发送请求
 */
async function request<T>(
    method: string,
    path: string,
    body?: BodyInit,
    params?: Record<string, string | number | boolean>
): Promise<T> {
    // 构建 URL
    const searchParams = params ? new URLSearchParams(
        Object.entries(params).map(([k, v]) => [k, String(v)])
    ).toString() : '';
    const url = `${API_BASE_URL}${path}${searchParams ? `?${searchParams}` : ''}`;

    // 构建请求头
    const headers = new Headers();

    // 只在有 body 时设置 Content-Type
    if (body) {
        headers.set('Content-Type', 'application/json');
    }

    // 添加 Authorization - 从 zustand store 获取 token
    // 捕获发送时使用的令牌：401 的登出需要核对会话归属（F05）
    let requestToken: string | null = null;
    if (typeof window !== 'undefined' && getAuthStore) {
        const store = getAuthStore();
        if (store.token) {
            requestToken = store.token;
            headers.set('Authorization', `Bearer ${requestToken}`);
        }
    }

    // 发送请求。带整体超时：服务器挂起时此前 React Query 的 loading 永远转圈，
    // 30s 轮询会在前一个请求未完成时持续堆积在途请求。
    let response: Response;
    try {
        response = await fetch(url.toString(), {
            method,
            headers,
            body,
            signal: typeof AbortSignal !== 'undefined' && 'timeout' in AbortSignal
                ? AbortSignal.timeout(REQUEST_TIMEOUT_MS)
                : undefined,
        });
    } catch (error) {
        // 断网/DNS 失败抛 TypeError（"Failed to fetch" 等浏览器本地语言文案），
        // 超时抛 TimeoutError——归一为 ApiError 走统一错误翻译通道。
        if (error instanceof DOMException && error.name === 'TimeoutError') {
            throw buildTransportError('common.request_timeout');
        }
        throw buildTransportError('common.request_failed');
    }

    return handleResponse<T>(response, requestToken);
}

function parseDownloadFilename(contentDisposition: string | null): string | null {
    if (!contentDisposition) return null;
    const encoded = contentDisposition.match(/filename\*=UTF-8''([^;]+)/i)?.[1];
    if (encoded) {
        try {
            return decodeURIComponent(encoded);
        } catch {
            // Fall through to the plain filename parameter.
        }
    }
    return contentDisposition.match(/filename="([^"]+)"/i)?.[1] ?? null;
}

function safeDownloadFilename(value: string) {
    return value.replace(/[\\/\u0000-\u001f\u007f]/g, '_');
}

function triggerBlobDownload(blob: Blob, filename: string) {
    const url = URL.createObjectURL(blob);
    try {
        const anchor = document.createElement('a');
        anchor.href = url;
        anchor.download = safeDownloadFilename(filename);
        document.body.appendChild(anchor);
        anchor.click();
        anchor.remove();
    } finally {
        window.setTimeout(() => URL.revokeObjectURL(url), 1000);
    }
}

export async function downloadApiFile(path: string, fallbackFilename: string) {
    const headers = new Headers();
    const token = getAuthStore?.().token;
    if (!token) throw new Error('Not authenticated');
    headers.set('Authorization', `Bearer ${token}`);

    const response = await fetch(`${API_BASE_URL}${path}`, {
        method: 'GET',
        headers,
    });
    if (!response.ok) {
        await handleResponse<never>(response, token);
        throw new Error(response.statusText);
    }

    const filename =
        parseDownloadFilename(response.headers.get('content-disposition')) || fallbackFilename;
    triggerBlobDownload(await response.blob(), filename);
    return { filename: safeDownloadFilename(filename) };
}

/**
 * API 客户端 - 基础 HTTP 方法
 */
export const apiClient = {
    /**
     * GET 请求
     */
    get: <T>(path: string, params?: Record<string, string | number | boolean>): Promise<T> =>
        request<T>('GET', path, undefined, params),

    /**
     * POST 请求
     */
    post: <T>(path: string, data?: unknown, params?: Record<string, string | number | boolean>): Promise<T> =>
        request<T>('POST', path, data ? JSON.stringify(data) : undefined, params),

    /**
     * PUT 请求
     */
    put: <T>(path: string, data?: unknown, params?: Record<string, string | number | boolean>): Promise<T> =>
        request<T>('PUT', path, data ? JSON.stringify(data) : undefined, params),

    /**
     * DELETE 请求
     */
    delete: <T>(path: string, params?: Record<string, string | number | boolean>): Promise<T> =>
        request<T>('DELETE', path, undefined, params),

    /**
     * PATCH 请求
     */
    patch: <T>(path: string, data?: unknown, params?: Record<string, string | number | boolean>): Promise<T> =>
        request<T>('PATCH', path, data ? JSON.stringify(data) : undefined, params),
};
