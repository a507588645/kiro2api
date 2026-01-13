package parser

import (
	"strings"
	"unicode/utf8"
)

// 引用字符集合，用于检测假标签（借鉴 kiro.rs QUOTE_CHARS）
// 当 thinking 标签被这些字符包裹时，认为是在引用标签而非真正的标签
var quoteChars = []byte{
	'`',  // 反引号（代码块）
	'"',  // 双引号
	'\'', // 单引号
	'\\', // 反斜杠（转义）
	'#',  // 井号（标题或注释）
	'[',  // 左方括号
	']',  // 右方括号
	'(',  // 左圆括号
	')',  // 右圆括号
	'{',  // 左花括号
	'}',  // 右花括号
}

const (
	thinkingStartTag = "<thinking>"
	thinkingEndTag   = "</thinking>"
)

// ThinkingTagDetector 假标签检测器（借鉴 kiro.rs）
type ThinkingTagDetector struct{}

// NewThinkingTagDetector 创建假标签检测器
func NewThinkingTagDetector() *ThinkingTagDetector {
	return &ThinkingTagDetector{}
}

// isQuoteChar 检查字符是否为引用字符
func isQuoteChar(b byte) bool {
	for _, q := range quoteChars {
		if b == q {
			return true
		}
	}
	return false
}

// FindRealThinkingStartTag 查找真正的 <thinking> 开始标签
// 跳过被引用字符包裹的假标签（借鉴 kiro.rs find_real_thinking_start_tag）
// 返回标签的起始位置，如果没找到返回 -1
func (d *ThinkingTagDetector) FindRealThinkingStartTag(buffer string) int {
	searchStart := 0

	for {
		idx := strings.Index(buffer[searchStart:], thinkingStartTag)
		if idx == -1 {
			return -1
		}
		idx += searchStart

		// 检查标签前面是否有引用字符
		if idx > 0 {
			prevChar := buffer[idx-1]
			if isQuoteChar(prevChar) {
				// 假标签，继续搜索
				searchStart = idx + len(thinkingStartTag)
				continue
			}
		}

		// 检查标签后面是否有引用字符
		afterIdx := idx + len(thinkingStartTag)
		if afterIdx < len(buffer) {
			nextChar := buffer[afterIdx]
			if isQuoteChar(nextChar) {
				// 假标签，继续搜索
				searchStart = afterIdx
				continue
			}
		}

		// 检查是否在代码块内（反引号包裹）
		// 计算 idx 之前的反引号数量
		backticksBeforeTag := strings.Count(buffer[:idx], "`")
		if backticksBeforeTag%2 == 1 {
			// 奇数个反引号，说明在代码块内
			searchStart = idx + len(thinkingStartTag)
			continue
		}

		return idx
	}
}

// FindRealThinkingEndTag 查找真正的 </thinking> 结束标签
// 真正的结束标签后面必须有 \n\n（借鉴 kiro.rs find_real_thinking_end_tag）
// 返回标签的起始位置，如果没找到返回 -1
func (d *ThinkingTagDetector) FindRealThinkingEndTag(buffer string) int {
	searchStart := 0

	for {
		idx := strings.Index(buffer[searchStart:], thinkingEndTag)
		if idx == -1 {
			return -1
		}
		idx += searchStart

		// 检查标签前面是否有引用字符
		if idx > 0 {
			prevChar := buffer[idx-1]
			if isQuoteChar(prevChar) {
				searchStart = idx + len(thinkingEndTag)
				continue
			}
		}

		// 检查标签后面是否有引用字符
		afterIdx := idx + len(thinkingEndTag)
		if afterIdx < len(buffer) {
			nextChar := buffer[afterIdx]
			if isQuoteChar(nextChar) {
				searchStart = afterIdx
				continue
			}
		}

		// 检查是否在代码块内
		backticksBeforeTag := strings.Count(buffer[:idx], "`")
		if backticksBeforeTag%2 == 1 {
			searchStart = idx + len(thinkingEndTag)
			continue
		}

		// 真正的结束标签后面必须有 \n\n（或者是缓冲区末尾）
		endIdx := idx + len(thinkingEndTag)
		if endIdx < len(buffer) {
			remaining := buffer[endIdx:]
			if len(remaining) >= 2 {
				if remaining[:2] != "\n\n" {
					// 不是真正的结束标签
					searchStart = endIdx
					continue
				}
			} else if len(remaining) == 1 {
				// 只有一个字符，需要等待更多数据
				return -1
			}
		}

		return idx
	}
}

// FindCharBoundary 在 UTF-8 字符串中查找安全的字符边界
// 避免在多字节字符中间切片（借鉴 kiro.rs find_char_boundary）
func FindCharBoundary(s string, target int) int {
	if target <= 0 {
		return 0
	}
	if target >= len(s) {
		return len(s)
	}

	// 向前查找有效的 UTF-8 字符边界
	for target > 0 && !utf8.RuneStart(s[target]) {
		target--
	}

	return target
}

// ExtractThinkingContent 从缓冲区提取 thinking 内容
// 返回: (thinkingContent, remainingBuffer, found)
func (d *ThinkingTagDetector) ExtractThinkingContent(buffer string) (string, string, bool) {
	startIdx := d.FindRealThinkingStartTag(buffer)
	if startIdx == -1 {
		return "", buffer, false
	}

	contentStart := startIdx + len(thinkingStartTag)
	endIdx := d.FindRealThinkingEndTag(buffer[contentStart:])
	if endIdx == -1 {
		return "", buffer, false
	}
	endIdx += contentStart

	thinkingContent := buffer[contentStart:endIdx]

	// 计算剩余内容的起始位置
	remainingStart := endIdx + len(thinkingEndTag)
	if remainingStart < len(buffer) {
		remaining := buffer[remainingStart:]
		// 跳过 \n\n
		if strings.HasPrefix(remaining, "\n\n") {
			remaining = remaining[2:]
		} else if strings.HasPrefix(remaining, "\n") {
			remaining = remaining[1:]
		}
		return thinkingContent, remaining, true
	}

	return thinkingContent, "", true
}

// HasPotentialThinkingTag 检查缓冲区是否可能包含不完整的 thinking 标签
// 用于流式处理中判断是否需要等待更多数据
func (d *ThinkingTagDetector) HasPotentialThinkingTag(buffer string) bool {
	// 检查是否包含 "<" 后跟 "thinking" 的部分字符
	for i := 1; i < len(thinkingStartTag); i++ {
		if strings.HasSuffix(buffer, thinkingStartTag[:i]) {
			return true
		}
	}
	// 检查结束标签
	for i := 1; i < len(thinkingEndTag); i++ {
		if strings.HasSuffix(buffer, thinkingEndTag[:i]) {
			return true
		}
	}
	return false
}
