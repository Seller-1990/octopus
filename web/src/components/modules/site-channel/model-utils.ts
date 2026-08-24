import {
    type SiteChannelCard,
    type SiteChannelGroup,
    type SiteModelRouteType,
} from "@/api/endpoints/site-channel";
import { isSupportedRouteType } from "./constants";
import { routeTypeLabel, type SiteModelView } from "./utils";
import {
    type SiteChannelQuickFilter,
    type SiteChannelTableSort,
} from "./ui-store";

export function getBaseGroupKey(groupKey: string) {
    return groupKey.split('::', 1)[0] || groupKey;
}

export function makeModelKey(groupKey: string, modelName: string) {
    return `${groupKey}\u0000${modelName}`;
}

export function removeKeys<T>(record: Record<string, T>, keys: string[]) {
    if (keys.length === 0) return record;
    const next = { ...record };
    let changed = false;

    for (const key of keys) {
        if (!(key in next)) continue;
        delete next[key];
        changed = true;
    }

    return changed ? next : record;
}

export function addPendingKeys(current: Set<string>, keys: string[]) {
    if (keys.length === 0) return current;
    const next = new Set(current);
    for (const key of keys) {
        next.add(key);
    }
    return next;
}

export function removePendingKeys(current: Set<string>, keys: string[]) {
    if (keys.length === 0) return current;
    const next = new Set(current);
    let changed = false;

    for (const key of keys) {
        if (!next.delete(key)) continue;
        changed = true;
    }

    return changed ? next : current;
}

export function collectSiteSummary(card: SiteChannelCard) {
    let groupCount = 0;
    let modelCount = 0;
    let totalKeys = 0;
    let enabledKeys = 0;
    let groupsWithKeys = 0;
    let groupsMissingKeys = 0;
    const routeCounts = new Map<SiteModelRouteType, number>();

    for (const account of card.accounts) {
        groupCount += account.group_count;
        modelCount += account.model_count;

        for (const group of account.groups) {
            totalKeys += group.key_count;
            enabledKeys += group.enabled_key_count;
            if (group.has_keys) {
                groupsWithKeys++;
            } else {
                groupsMissingKeys++;
            }
        }

        for (const route of account.route_summaries) {
            routeCounts.set(route.route_type, (routeCounts.get(route.route_type) ?? 0) + route.count);
        }
    }

    return { groupCount, modelCount, totalKeys, enabledKeys, groupsWithKeys, groupsMissingKeys, routeCounts };
}

export function collectSiteRuntimeSummary(card: SiteChannelCard) {
    let successCount = 0;
    let failureCount = 0;
    let totalCost = 0;
    let lastRequestAt: number | null = null;
    let maskedPendingKeys = 0;

    for (const account of card.accounts) {
        for (const group of account.groups) {
            maskedPendingKeys += group.masked_pending_key_count;
            for (const key of group.projected_keys) {
                totalCost += key.total_cost;
                if (key.last_use_time_stamp > 0) {
                    lastRequestAt = Math.max(lastRequestAt ?? 0, key.last_use_time_stamp);
                }
            }
            for (const m of group.models) {
                const h = m.history;
                if (!h) continue;
                successCount += h.success_count;
                failureCount += h.failure_count;
                if (typeof h.last_request_at === 'number' && h.last_request_at > 0) {
                    lastRequestAt = Math.max(lastRequestAt ?? 0, h.last_request_at);
                }
            }
        }
    }

    return {
        totalRequests: successCount + failureCount,
        successCount,
        failureCount,
        totalCost,
        lastRequestAt,
        maskedPendingKeys,
    };
}

export const SHORT_ROUTE_LABEL: Partial<Record<SiteModelRouteType, string>> = {
    openai_chat: 'Chat',
    openai_response: 'Response',
    openai_embedding: 'Embedding',
};

export function getUnknownRouteReason(model: SiteModelView) {
    const metadata = model.route_metadata;
    if (!metadata || metadata.route_supported) return null;

    const details = [metadata.unsupported_reason];
    if (metadata.supported_endpoint_types?.length) {
        details.push(`检测到端点: ${metadata.supported_endpoint_types.join(', ')}`);
    }
    if (metadata.heuristic_endpoint_types?.length) {
        details.push(`启发式推断: ${metadata.heuristic_endpoint_types.join(', ')}`);
    }

    return details.filter((item): item is string => Boolean(item && item.trim())).join(' · ') || null;
}

export function getGuessedRouteReason(model: SiteModelView) {
    const metadata = model.route_metadata;
    if (!metadata?.route_guessed) return null;

    const details = ['站点未报告可识别的端点格式，已按模型名称猜测，如不准确可手动调整'];
    if (metadata.supported_endpoint_types?.length) {
        details.push(`站点报告端点: ${metadata.supported_endpoint_types.join(', ')}`);
    }
    return details.join(' · ');
}

export function getModelLastRequestAt(model: SiteModelView) {
    return model.history?.last_request_at ?? null;
}

export function getModelHistoryCount(model: SiteModelView) {
    return (model.history?.success_count ?? 0) + (model.history?.failure_count ?? 0);
}

export function hasModelHistory(model: SiteModelView) {
    return getModelHistoryCount(model) > 0 || getModelLastRequestAt(model) !== null;
}

export function modelNeedsAttention(model: SiteModelView) {
    return !model.has_keys || !model.projected_channel_id || !isSupportedRouteType(model.route_type);
}

export function compareNullableNumber(left: number | null, right: number | null, order: 'asc' | 'desc') {
    const normalizedLeft = left ?? -1;
    const normalizedRight = right ?? -1;
    const diff = normalizedLeft - normalizedRight;
    return order === 'asc' ? diff : -diff;
}

export function compareText(left: string, right: string, order: 'asc' | 'desc') {
    const diff = left.localeCompare(right);
    return order === 'asc' ? diff : -diff;
}

export function matchesQuickFilters(model: SiteModelView, quickFilters: SiteChannelQuickFilter[]) {
    if (quickFilters.length === 0) return true;

    return quickFilters.every((filter) => {
        switch (filter) {
            case 'attention':
                return modelNeedsAttention(model);
            case 'with_history':
                return hasModelHistory(model);
            case 'disabled':
                return model.disabled;
            default:
                return true;
        }
    });
}

export function sortModels(models: SiteModelView[], tableSort: SiteChannelTableSort) {
    return [...models].sort((left, right) => {
        switch (tableSort.field) {
            case 'group_name':
                return compareText(left.group_name || left.group_key, right.group_name || right.group_key, tableSort.order);
            case 'route_type':
                return compareText(routeTypeLabel(left.route_type), routeTypeLabel(right.route_type), tableSort.order);
            case 'last_request_at':
                return compareNullableNumber(getModelLastRequestAt(left), getModelLastRequestAt(right), tableSort.order);
            case 'model_name':
            default:
                return compareText(left.model_name, right.model_name, tableSort.order);
        }
    });
}

export const STALE_MODEL_SYNC_STATUSES = ['stale', 'failed', 'unresolved'];

export function formatGroupMultiplier(value: number | null | undefined, known?: boolean): string {
    if (value == null || !Number.isFinite(value)) return '倍率未知';
    return known ? `${Math.round(value * 100) / 100}x` : `暂定 ${Math.round(value * 100) / 100}x`;
}

export function getGroupStatusBadge(group: SiteChannelGroup): { label: string; className: string } | null {
    if (group.policy_blocked && group.projection_suspended) {
        return { label: '暂停 + 倍率阻断', className: 'rounded-full bg-destructive/10 px-1.5 py-0.5 text-[10px] text-destructive' };
    }
    if (group.policy_blocked) {
        return { label: '倍率阻断', className: 'rounded-full bg-destructive/10 px-1.5 py-0.5 text-[10px] text-destructive' };
    }
    if (group.projection_suspended) {
        return { label: '暂停', className: 'rounded-full bg-destructive/10 px-1.5 py-0.5 text-[10px] text-destructive' };
    }
    if (!group.has_keys) {
        return { label: '待建', className: 'rounded-full bg-amber-500/15 px-1.5 py-0.5 text-[10px] text-amber-700 dark:text-amber-300' };
    }
    if (STALE_MODEL_SYNC_STATUSES.includes(group.model_sync_status)) {
        return { label: '沿用', className: 'rounded-full bg-amber-500/15 px-1.5 py-0.5 text-[10px] text-amber-700 dark:text-amber-300' };
    }
    if (group.masked_pending_key_count > 0 && group.enabled_key_count === 0) {
        return { label: '待补全', className: 'rounded-full bg-amber-500/15 px-1.5 py-0.5 text-[10px] text-amber-700 dark:text-amber-300' };
    }
    // 阶段 5 v2 Y3：暂定徽标最后（仅已有 key 的组），不遮蔽「待建」运营状态
    if (group.has_keys && group.multiplier_known !== true) {
        return { label: '暂定', className: 'rounded-full bg-sky-500/15 px-1.5 py-0.5 text-[10px] text-sky-700 dark:text-sky-300' };
    }
    return null;
}
