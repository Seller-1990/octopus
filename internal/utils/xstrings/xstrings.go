package xstrings

import "strings"

// SplitTrimCompact splits strings by sep, trims whitespace, and drops empty items.
// Commonly used for parsing comma-separated configs like "a, b, ,c,".
func SplitTrimCompact(sep string, parts ...string) []string {
	out := make([]string, 0)
	for _, p := range parts {
		if p == "" {
			continue
		}
		for _, item := range strings.Split(p, sep) {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			out = append(out, item)
		}
	}
	return out
}

// SplitTrimCompactUnique 在 SplitTrimCompact 基础上按拆分结果去重（保序）。
// 渠道模型名列表（Model+CustomModel 合并串）等来源天然可能重复。
func SplitTrimCompactUnique(sep string, parts ...string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, item := range SplitTrimCompact(sep, parts...) {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

// TrimCompact trims whitespace and drops empty items in a string slice.
func TrimCompact(items []string) []string {
	out := make([]string, 0, len(items))
	for _, s := range items {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}
