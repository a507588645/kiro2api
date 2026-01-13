package utils

import (
	"unicode/utf8"
)

// TruncateUTF8 安全地截断 UTF-8 字符串到指定长度
// 修复: UTF-8 字符串截断可能导致 panic 的问题
// 参考: kiro.rs 2026.1.2 - 修复 UTF-8 字符串截断可能导致 panic 的问题
//
// 此函数确保截断操作不会在 UTF-8 字符的中间进行，避免产生无效的 UTF-8 序列
//
// 参数:
//   - s: 要截断的字符串
//   - maxBytes: 最大字节数
//
// 返回:
//   - 截断后的字符串（保证是有效的 UTF-8）
func TruncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}

	// 从 maxBytes 位置向前查找，找到一个有效的 UTF-8 字符边界
	for i := maxBytes; i > 0; i-- {
		if utf8.RuneStart(s[i]) {
			// 验证这个位置是否是有效的 UTF-8 字符开始
			if utf8.ValidString(s[:i]) {
				return s[:i]
			}
		}
	}

	// 如果找不到有效边界，返回空字符串
	return ""
}

// TruncateUTF8WithEllipsis 安全地截断 UTF-8 字符串并添加省略号
//
// 参数:
//   - s: 要截断的字符串
//   - maxBytes: 最大字节数（包括省略号）
//
// 返回:
//   - 截断后的字符串 + "..."（保证是有效的 UTF-8）
func TruncateUTF8WithEllipsis(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}

	ellipsis := "..."
	ellipsisLen := len(ellipsis)

	if maxBytes <= ellipsisLen {
		// 如果最大长度小于等于省略号长度，只返回省略号的一部分
		return ellipsis[:maxBytes]
	}

	// 为省略号预留空间
	truncated := TruncateUTF8(s, maxBytes-ellipsisLen)
	return truncated + ellipsis
}

// TruncateUTF8Runes 按 rune 数量（字符数）截断字符串
//
// 参数:
//   - s: 要截断的字符串
//   - maxRunes: 最大字符数
//
// 返回:
//   - 截断后的字符串（保证是有效的 UTF-8）
func TruncateUTF8Runes(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}

	count := 0
	for i := range s {
		if count >= maxRunes {
			return s[:i]
		}
		count++
	}

	return s
}

// TruncateUTF8RunesWithEllipsis 按 rune 数量截断字符串并添加省略号
//
// 参数:
//   - s: 要截断的字符串
//   - maxRunes: 最大字符数（包括省略号）
//
// 返回:
//   - 截断后的字符串 + "..."（保证是有效的 UTF-8）
func TruncateUTF8RunesWithEllipsis(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}

	ellipsis := "..."
	ellipsisRunes := utf8.RuneCountInString(ellipsis)

	if maxRunes <= ellipsisRunes {
		return ellipsis[:maxRunes]
	}

	truncated := TruncateUTF8Runes(s, maxRunes-ellipsisRunes)
	return truncated + ellipsis
}
