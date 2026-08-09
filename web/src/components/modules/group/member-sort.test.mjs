import assert from 'node:assert/strict';
import test from 'node:test';

import { sortGroupMembers } from './member-sort.ts';

// 阶段 5 新语义：仅 multiplier_known===true 用真实 group_multiplier，否则一律按 1x 参与排序
// （与后端修订 11 对齐；candidate multiplier 不再参与排序）。

test('sorts relay members by known group multiplier, unknown treated as 1x', () => {
    const members = [
        // known=true 用真实倍率：0.2x 排最前
        { id: 'paid', is_reserve: true, balance: 30, multiplier: 0.01, group_multiplier: 0.2, multiplier_known: true },
        // known 缺失（undefined）→ 按 1x
        { id: 'legacy', is_reserve: true, balance: 20, multiplier: 0.1, group_multiplier: null },
        // known=true 真实 0x
        { id: 'free', is_reserve: true, balance: 10, multiplier: 1, group_multiplier: 0, multiplier_known: true },
    ];

    // multiplier 升序：free(0x) < paid(0.2x) < legacy(1x)
    assert.deepEqual(
        sortGroupMembers(members).map((member) => member.id),
        ['free', 'paid', 'legacy'],
    );
});

test('keeps direct channels ahead of relay channels and sorts them by balance', () => {
    const members = [
        { id: 'relay', is_reserve: true, balance: 100, group_multiplier: 0 },
        { id: 'direct-low', is_reserve: false, balance: 10, group_multiplier: 5 },
        { id: 'direct-high', is_reserve: false, balance: 20, group_multiplier: 10 },
    ];

    assert.deepEqual(
        sortGroupMembers(members).map((member) => member.id),
        ['direct-high', 'direct-low', 'relay'],
    );
});

test('known=false with retained value sorts as 1x, not the retained value', () => {
    const members = [
        // known=false 保留值 5x → 排序按 1x（不按 5x 排前面）
        { id: 'retained', is_reserve: true, balance: 5, group_multiplier: 5, multiplier_known: false },
        // known=true 真实 2x → 排最前
        { id: 'real-2x', is_reserve: true, balance: 5, group_multiplier: 2, multiplier_known: true },
    ];

    // multiplier 升序：retained 按 1x < real-2x 按 2x → retained 在前
    assert.deepEqual(
        sortGroupMembers(members).map((member) => member.id),
        ['retained', 'real-2x'],
    );
});
