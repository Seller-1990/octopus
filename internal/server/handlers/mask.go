package handlers

import "strings"

const maskMarker = "****"

// maskSecret 掩码敏感字符串：保留前 4 个字符与后 4 个字符，其余以 **** 代替。
// 按 rune 切割，避免非 ASCII 值被字节截断产生非法 UTF-8。
// 长度不足 9 个字符的值整体掩码；空值保持为空，便于前端区分"无凭证"与"有凭证"。
func maskSecret(value string) string {
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= 8 {
		return maskMarker
	}
	return string(runes[:4]) + maskMarker + string(runes[len(runes)-4:])
}

// isMaskedValue 判断一个值是否为掩码标记的产物（含 **** 子串）。
// 用于 create 路径的显式拒绝——新建时没有数据库原值可还原，
// 掩码值（例如用户从列表复制粘贴）一旦落库会让下游拿着假凭证持续失败。
// 编辑路径的还原不使用子串嗅探，而是与 maskSecret(原值) 精确比对，
// 见 restoreMaskedAccountFields / updateAPIKey。
func isMaskedValue(value string) bool {
	return value != "" && strings.Contains(value, maskMarker)
}
