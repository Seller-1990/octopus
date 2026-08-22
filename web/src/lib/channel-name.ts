const MANAGED_CHANNEL_SUFFIXES = ['-chat', '-response', '-anthropic', '-gemini', '-volcengine', '-embedding', '-unsupported'] as const;

export interface ChannelDisplayName {
    /** 投影站点名称（如 "K API"）。非投影渠道为空字符串。 */
    siteName: string;
    /** 剥离站点名称后的分组展示名（如 "seller1990/默认分组-Response"）。 */
    groupName: string;
}

/**
 * 投影渠道的命名格式为 `{站点名}/{账号名}/{分组名}-{端点后缀}`（后端 buildManagedChannelName），
 * 例如 "K API/seller1990/默认分组-Response"。日志展示只需保留分组信息，站点名单独标记。
 * 仅当名称满足「至少三段 + 末段以已知端点后缀结尾」时才剥离首段站点名，
 * 避免误伤用户手工命名的普通渠道。
 */
export function splitChannelDisplayName(channelName: string | null | undefined): ChannelDisplayName {
    const text = channelName?.trim() ?? '';
    if (!text) return { siteName: '', groupName: '' };
    const parts = text.split('/');
    if (parts.length >= 3) {
        const last = parts[parts.length - 1].toLowerCase();
        if (MANAGED_CHANNEL_SUFFIXES.some((suffix) => last.endsWith(suffix))) {
            return { siteName: parts[0], groupName: parts.slice(1).join('/') };
        }
    }
    return { siteName: '', groupName: text };
}

export function channelGroupDisplayName(channelName: string | null | undefined): string {
    return splitChannelDisplayName(channelName).groupName;
}
