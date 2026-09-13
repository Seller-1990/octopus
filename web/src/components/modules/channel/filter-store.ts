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
    setType: (type: ChannelTypeFilter) => void;
    setStatus: (status: ChannelStatusFilter) => void;
    setReserve: (reserve: ChannelReserveFilter) => void;
    reset: () => void;
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
            setType: (type) => set({ type }),
            setStatus: (status) => set({ status }),
            setReserve: (reserve) => set({ reserve }),
            reset: () => set({ ...DEFAULTS }),
        }),
        {
            name: 'octopus:channel-filters',
            version: 1,
            // 持久化值无运行时校验会让手改/异常数据造成永久空列表且难自救
            merge: (persisted, current) => {
                const p = (persisted ?? {}) as Partial<ChannelFiltersState>;
                const validType = p.type === 'all' || (typeof p.type === 'number' && p.type >= 0 && p.type <= 5);
                const validStatus = p.status === 'all' || p.status === 'enabled' || p.status === 'disabled';
                const validReserve = p.reserve === 'all' || p.reserve === 'transit' || p.reserve === 'charity';
                return {
                    ...current,
                    type: validType ? (p.type as ChannelTypeFilter) : current.type,
                    status: validStatus ? (p.status as ChannelStatusFilter) : current.status,
                    reserve: validReserve ? (p.reserve as ChannelReserveFilter) : current.reserve,
                };
            },
        }
    )
);
