import { create } from 'zustand'
import { persist } from 'zustand/middleware'

// 'visionbridge' 已降级为设置页卡片、'circuit' 降级为设置页/首页入口，均不再占据导航位；
// circuit 保留在类型与顺序中（路由仍可达），visionbridge 彻底移除并经 migrate 重定向旧持久化值。
export type NavItem = 'home' | 'site' | 'channel' | 'group' | 'model' | 'log' | 'apikey' | 'circuit' | 'setting'

const NAV_ORDER: NavItem[] = ['home', 'site', 'channel', 'group', 'model', 'log', 'apikey', 'circuit', 'setting']

interface NavState {
    activeItem: NavItem
    prevItem: NavItem | null
    direction: number
    setActiveItem: (item: NavItem) => void
}

export const useNavStore = create<NavState>()(
    persist(
        (set, get) => ({
            activeItem: 'home',
            prevItem: null,
            direction: 0,
            setActiveItem: (item) => {
                const { activeItem } = get()
                const currentIndex = NAV_ORDER.indexOf(activeItem)
                const newIndex = NAV_ORDER.indexOf(item)
                const direction = newIndex > currentIndex ? 1 : -1

                set({
                    activeItem: item,
                    prevItem: activeItem,
                    direction
                })
            },
        }),
        {
            name: 'nav-storage',
            version: 1,
            // v0 的持久化 activeItem 可能指向已删除的 visionbridge 页，重定向避免内容区白屏
            migrate: (persisted) => {
                // persisted 是旧版本数据，可能含已删除的 'visionbridge'，按 string 处理
                const state = (persisted ?? {}) as { activeItem?: string; prevItem?: string | null };
                if (state.activeItem === 'visionbridge') state.activeItem = 'home';
                if (state.prevItem === 'visionbridge') state.prevItem = null;
                return state as { activeItem: NavItem; prevItem: NavItem | null };
            },
        }
    )
)
