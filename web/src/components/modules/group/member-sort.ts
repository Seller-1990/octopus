export type SortableGroupMember = {
    is_reserve?: boolean;
    balance?: number | null;
    multiplier?: number | null;
    group_multiplier?: number | null;
    multiplier_known?: boolean | null;
};

export type GroupSortStrategy =
    | 'non_relay_balance'    // Non-relay first, then balance desc (current default)
    | 'non_relay_multiplier' // Non-relay first, then multiplier asc
    | 'multiplier_balance'   // Multiplier asc, then balance desc (ignore tier)
    | 'balance_only';        // Pure balance desc (ignore tier)

function getBalance(m: SortableGroupMember): number {
    return m.balance ?? 0;
}

// 阶段 5 v2 Y5：与后端修订 11 对齐——仅 known=true 用真实分组倍率；暂定/未知一律按 1x；
// candidate multiplier（item.multiplier）不再参与排序（D2' A'）。
function getMultiplier(m: SortableGroupMember): number {
    if (m.multiplier_known === true) {
        return m.group_multiplier ?? 1;
    }
    return 1;
}

function getTier(m: SortableGroupMember): number {
    return m.is_reserve ? 1 : 0;
}

function sortNonRelayBalance<T extends SortableGroupMember>(members: T[]): T[] {
    return members.sort((a, b) => {
        const tierDiff = getTier(a) - getTier(b);
        if (tierDiff !== 0) return tierDiff;

        if (getTier(a) === 0) {
            // tier0: balance desc
            const balDiff = getBalance(b) - getBalance(a);
            if (balDiff !== 0) return balDiff;
        } else {
            // tier1: multiplier asc, then balance desc
            const mulDiff = getMultiplier(a) - getMultiplier(b);
            if (mulDiff !== 0) return mulDiff;
            const balDiff = getBalance(b) - getBalance(a);
            if (balDiff !== 0) return balDiff;
        }
        return 0;
    });
}

function sortNonRelayMultiplier<T extends SortableGroupMember>(members: T[]): T[] {
    return members.sort((a, b) => {
        const tierDiff = getTier(a) - getTier(b);
        if (tierDiff !== 0) return tierDiff;

        if (getTier(a) === 0) {
            // tier0: multiplier asc, then balance desc
            const mulDiff = getMultiplier(a) - getMultiplier(b);
            if (mulDiff !== 0) return mulDiff;
            const balDiff = getBalance(b) - getBalance(a);
            if (balDiff !== 0) return balDiff;
        } else {
            // tier1: multiplier asc
            const mulDiff = getMultiplier(a) - getMultiplier(b);
            if (mulDiff !== 0) return mulDiff;
        }
        return 0;
    });
}

function sortMultiplierBalance<T extends SortableGroupMember>(members: T[]): T[] {
    return members.sort((a, b) => {
        // Ignore tier: multiplier asc first, then balance desc
        const mulDiff = getMultiplier(a) - getMultiplier(b);
        if (mulDiff !== 0) return mulDiff;
        const balDiff = getBalance(b) - getBalance(a);
        if (balDiff !== 0) return balDiff;
        return 0;
    });
}

function sortBalanceOnly<T extends SortableGroupMember>(members: T[]): T[] {
    return members.sort((a, b) => {
        // Ignore tier: pure balance desc
        const balDiff = getBalance(b) - getBalance(a);
        if (balDiff !== 0) return balDiff;
        return 0;
    });
}

export function sortGroupMembers<T extends SortableGroupMember>(
    members: readonly T[],
    strategy: GroupSortStrategy = 'non_relay_balance',
): T[] {
    const copy = [...members];
    switch (strategy) {
        case 'non_relay_balance':
            return sortNonRelayBalance(copy);
        case 'non_relay_multiplier':
            return sortNonRelayMultiplier(copy);
        case 'multiplier_balance':
            return sortMultiplierBalance(copy);
        case 'balance_only':
            return sortBalanceOnly(copy);
    }
}
