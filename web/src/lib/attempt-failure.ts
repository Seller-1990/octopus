import type { ChannelAttempt } from '@/api/endpoints/log';

const HTTP_STATUS_SHORT_REASON: Record<number, string> = {
    400: 'bad_request',
    401: 'unauthorized',
    402: 'payment_required',
    403: 'forbidden',
    404: 'not_found',
    408: 'timeout',
    409: 'conflict',
    413: 'payload_too_large',
    422: 'validation_failed',
    429: 'rate_limited',
    500: 'internal_error',
    502: 'bad_gateway',
    503: 'service_unavailable',
    504: 'gateway_timeout',
};

// 失败简称：从完整错误链中提取一个稳定、可读的短原因，配合 HTTP 状态码使用，
// 避免在 attempt 时间线里重复铺陈整段上游 body。
export function shortFailureReason(raw: string | undefined | null, statusCode?: number): string {
    if (statusCode) return HTTP_STATUS_SHORT_REASON[statusCode] ?? 'upstream_error';
    const text = (raw ?? '').toLowerCase();
    if (text.includes('insufficient account balance')) return 'insufficient_balance';
    if (text.includes('channel disabled')) return 'channel_disabled';
    if (text.includes('no available key')) return 'no_key';
    if (text.includes('circuit')) return 'circuit_break';
    if (text.includes('context canceled') || text.includes('client canceled')) return 'client_canceled';
    if (text.includes('deadline exceeded') || text.includes('timeout')) return 'timeout';
    if (text.includes('connection refused')) return 'connection_refused';
    if (text.includes('connection reset')) return 'connection_reset';
    if (text.includes('eof')) return 'connection_closed';
    if (text.includes('protocol terminal')) return 'upstream_terminal';
    if (text.includes('unsupported channel')) return 'unsupported_channel';
    if (text.includes('not compatible')) return 'incompatible_channel';
    if (text.includes('channel not found')) return 'channel_not_found';
    if (text.includes('not found')) return 'not_found';
    return 'failed';
}

// 失败信息精简格式："HTTP 429 · rate_limited"；无状态码时仅显示失败简称。
export function formatFailureSummary(raw: string | undefined | null, statusCode?: number): string {
    const reason = shortFailureReason(raw, statusCode);
    if (statusCode) return `HTTP ${statusCode} · ${reason}`;
    return reason;
}

export function formatAttemptFailure(attempt: Pick<ChannelAttempt, 'msg' | 'status_code'>): string {
    return formatFailureSummary(attempt.msg, attempt.status_code);
}
