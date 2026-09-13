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
        }
    )
);
