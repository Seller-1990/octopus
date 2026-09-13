import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import { type ChannelType } from '@/api/endpoints/channel';

export type ChannelTypeFilter = 'all' | ChannelType;
export type ChannelStatusFilter = 'all' | 'enabled' | 'disabled';
// is_reserve=true 为中转渠道，false 为公益渠道
export type ChannelReserveFilter = 'all' | 'transit' | 'charity';

interface ChannelFiltersState {
    type: ChannelTypeFilter;
    status: ChannelStatusFilter;
    reserve: ChannelReserveFilter;
    // 批量选择的选中集合与执行状态：放 store 是为了视图切换（表格/卡片）或
    // tab 切换导致 ChannelsTable 卸载时选择不丢失；刻意不持久化
    selectedIds: Set<number>;
    isBatchBusy: boolean;
    setType: (type: ChannelTypeFilter) => void;
    setStatus: (status: ChannelStatusFilter) => void;
    setReserve: (reserve: ChannelReserveFilter) => void;
    reset: () => void;
    setSelectedIds: (updater: (prev: Set<number>) => Set<number>) => void;
    setIsBatchBusy: (busy: boolean) => void;
}

const DEFAULTS = {
    type: 'all' as ChannelTypeFilter,
    status: 'all' as ChannelStatusFilter,
    reserve: 'all' as ChannelReserveFilter,
};

export const useChannelFiltersStore = create<ChannelFiltersState>()(
    persist(
        (set) => ({
            ...DEFAULTS,
            selectedIds: new Set<number>(),
            isBatchBusy: false,
            setType: (type) => set({ type }),
            setStatus: (status) => set({ status }),
            setReserve: (reserve) => set({ reserve }),
            reset: () => set({ ...DEFAULTS }),
            setSelectedIds: (updater) => set((state) => ({ selectedIds: updater(state.selectedIds) })),
            setIsBatchBusy: (busy) => set({ isBatchBusy: busy }),
        }),
        {
            name: 'octopus:channel-filters',
            version: 1,
            // 只持久化筛选偏好；选中集合是会话内瞬态。
            // 非法持久化值无需运行时校验：异常筛选会让列表为空但「清除筛选」
            // 按钮自动出现（hasActiveFilters 对非法值恒真），一键自救
            partialize: (state) => ({
                type: state.type,
                status: state.status,
                reserve: state.reserve,
            }),
        }
    )
);
