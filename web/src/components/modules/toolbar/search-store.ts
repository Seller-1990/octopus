import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import type { ToolbarPage } from './view-options-store';

interface SearchState {
    searchTerms: Partial<Record<ToolbarPage, string>>;
    getSearchTerm: (page: ToolbarPage) => string;
    setSearchTerm: (page: ToolbarPage, term: string) => void;
}

// 仅日志页持久化关键词：日志页的筛选体系（模式/范围/渠道等）均已持久化，
// 关键词是同一筛选模型的一部分——"模式在、词不在"会让用户遇到
// 按残留模式搜不到东西的幻影筛选。其他页面的搜索是一次性动作，维持不持久化。
export const useSearchStore = create<SearchState>()(
    persist(
        (set, get) => ({
            searchTerms: {},
            getSearchTerm: (page) => get().searchTerms[page] || '',
            setSearchTerm: (page, term) => set((state) => ({
                searchTerms: { ...state.searchTerms, [page]: term }
            })),
        }),
        {
            name: 'toolbar-search-storage',
            partialize: (state) => ({
                searchTerms: { log: state.searchTerms.log } as SearchState['searchTerms'],
            }),
        },
    ),
);
