export type UAPresetHeader = {
    header_key: string;
    header_value: string;
};

// 上游 UA 白名单常用的 CLI 客户端标识。白名单通常只校验前缀，
// 因此预设值可以保持静态；Codex CLI 的 UA 形如
// codex_cli_rs/<version> (<OS> <version>; <arch>)，这里覆盖主流平台。
export const CHANNEL_UA_PRESETS: readonly string[] = [
    'claude-cli/2.1.161 (external, cli)',
    'claude-cli/2.1.161',
    'claude-code/1.0.0',
    'claude-code/0.1.0',
    'Kilo-Code/1.0',
    'codex_cli_rs/0.132.0 (Mac OS 15.3.1; arm64)',
    'codex_cli_rs/0.132.0 (Mac OS 15.3.1; x86_64)',
    'codex_cli_rs/0.132.0 (Windows 10.0.26100; x86_64)',
    'codex_cli_rs/0.132.0 (Debian 13.0.0; x86_64)',
];

// currentUAHeader 返回 custom_header 中第一个 User-Agent（忽略大小写）。
export function currentUAHeader(headers: UAPresetHeader[] | undefined): UAPresetHeader | undefined {
    return (headers ?? []).find((h) => h.header_key.trim().toLowerCase() === 'user-agent');
}

// setUAHeader 在 custom_header 中新增或更新 User-Agent；
// ua 为空时删除该 Header，其余 Header 保持原样。
export function setUAHeader(headers: UAPresetHeader[] | undefined, ua: string): UAPresetHeader[] {
    const next = (headers ?? []).filter((h) => h.header_key.trim().toLowerCase() !== 'user-agent');
    const trimmed = ua.trim();
    if (!trimmed) return next;
    return [...next, { header_key: 'User-Agent', header_value: trimmed }];
}
