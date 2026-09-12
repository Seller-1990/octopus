/**
 * 401 响应的会话归属判定（F05）。
 *
 * 异步响应没有身份归属约束时，慢请求、过期登录与重新登录交错，会让旧请求
 * 的 401 清空刚建立的新会话。因此发送请求时捕获当时使用的 token，收到 401
 * 后只有「当前会话仍是发出该请求的会话」才允许登出。
 */
export function shouldLogoutOnUnauthorized(
    requestToken: string | null | undefined,
    currentToken: string | null | undefined,
): boolean {
    return requestToken != null && requestToken !== '' && requestToken === currentToken;
}
