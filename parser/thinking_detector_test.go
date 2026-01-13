package parser

import (
	"testing"
)

func TestIsQuoteChar(t *testing.T) {
	tests := []struct {
		char     byte
		expected bool
	}{
		{'`', true},
		{'"', true},
		{'\'', true},
		{'\\', true},
		{'#', true},
		{'[', true},
		{']', true},
		{'(', true},
		{')', true},
		{'{', true},
		{'}', true},
		{'a', false},
		{'<', false},
		{'>', false},
		{' ', false},
		{'\n', false},
	}

	for _, tt := range tests {
		result := isQuoteChar(tt.char)
		if result != tt.expected {
			t.Errorf("isQuoteChar(%q) = %v, want %v", tt.char, result, tt.expected)
		}
	}
}

func TestFindRealThinkingStartTag(t *testing.T) {
	detector := NewThinkingTagDetector()

	tests := []struct {
		name     string
		buffer   string
		expected int
	}{
		{
			name:     "真标签在开头",
			buffer:   "<thinking>content",
			expected: 0,
		},
		{
			name:     "真标签在中间",
			buffer:   "prefix<thinking>content",
			expected: 6,
		},
		{
			name:     "反引号包裹-假标签",
			buffer:   "`<thinking>`",
			expected: -1,
		},
		{
			name:     "双引号包裹-假标签",
			buffer:   `"<thinking>"`,
			expected: -1,
		},
		{
			name:     "单引号包裹-假标签",
			buffer:   `'<thinking>'`,
			expected: -1,
		},
		{
			name:     "反斜杠前缀-假标签",
			buffer:   `\<thinking>`,
			expected: -1,
		},
		{
			name:     "井号前缀-假标签",
			buffer:   "#<thinking>",
			expected: -1,
		},
		{
			name:     "方括号包裹-假标签",
			buffer:   "[<thinking>]",
			expected: -1,
		},
		{
			name:     "代码块内-假标签",
			buffer:   "```\n<thinking>\n```",
			expected: -1,
		},
		{
			name:     "代码块外-真标签",
			buffer:   "text ``code`` <thinking>content", // 标签前有空格
			expected: 14,                                // 位置从0开始
		},
		{
			name:     "没有标签",
			buffer:   "just some text",
			expected: -1,
		},
		{
			name:     "空缓冲区",
			buffer:   "",
			expected: -1,
		},
		{
			name:     "假标签后的真标签",
			buffer:   "`<thinking>` <thinking>real", // 真标签前有空格
			expected: 13,
		},
		{
			name:     "花括号包裹-假标签",
			buffer:   "{<thinking>}",
			expected: -1,
		},
		{
			name:     "圆括号包裹-假标签",
			buffer:   "(<thinking>)",
			expected: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detector.FindRealThinkingStartTag(tt.buffer)
			if result != tt.expected {
				t.Errorf("FindRealThinkingStartTag(%q) = %d, want %d", tt.buffer, result, tt.expected)
			}
		})
	}
}

func TestFindRealThinkingEndTag(t *testing.T) {
	detector := NewThinkingTagDetector()

	tests := []struct {
		name     string
		buffer   string
		expected int
	}{
		{
			name:     "真结束标签带双换行",
			buffer:   "content</thinking>\n\nafter",
			expected: 7,
		},
		{
			name:     "真结束标签在末尾",
			buffer:   "content</thinking>",
			expected: 7,
		},
		{
			name:     "结束标签后无双换行-假标签",
			buffer:   "content</thinking>text",
			expected: -1,
		},
		{
			name:     "结束标签后只有一个换行-等待更多数据",
			buffer:   "content</thinking>\n",
			expected: -1,
		},
		{
			name:     "反引号包裹-假标签",
			buffer:   "`</thinking>`\n\n",
			expected: -1,
		},
		{
			name:     "双引号包裹-假标签",
			buffer:   `"</thinking>"` + "\n\n",
			expected: -1,
		},
		{
			name:     "代码块内-假标签",
			buffer:   "```\n</thinking>\n```\n\n",
			expected: -1,
		},
		{
			name:     "没有结束标签",
			buffer:   "just some text",
			expected: -1,
		},
		{
			name:     "空缓冲区",
			buffer:   "",
			expected: -1,
		},
		{
			name:     "假标签后的真标签",
			buffer:   "`</thinking>` content</thinking>\n\n", // 真标签前有空格
			expected: 21,                                     // 位置从0开始
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detector.FindRealThinkingEndTag(tt.buffer)
			if result != tt.expected {
				t.Errorf("FindRealThinkingEndTag(%q) = %d, want %d", tt.buffer, result, tt.expected)
			}
		})
	}
}

func TestFindCharBoundary(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		target   int
		expected int
	}{
		{
			name:     "ASCII字符串",
			s:        "hello",
			target:   3,
			expected: 3,
		},
		{
			name:     "目标为0",
			s:        "hello",
			target:   0,
			expected: 0,
		},
		{
			name:     "目标超过长度",
			s:        "hello",
			target:   10,
			expected: 5,
		},
		{
			name:     "中文字符-在字符边界",
			s:        "你好世界",
			target:   3,
			expected: 3, // "你" 占3字节，target=3正好是边界
		},
		{
			name:     "中文字符-在字符中间",
			s:        "你好世界",
			target:   4,
			expected: 3, // "你" 占3字节，target=4在"好"的中间，回退到3
		},
		{
			name:     "中文字符-在字符中间2",
			s:        "你好世界",
			target:   5,
			expected: 3, // "你" 占3字节，target=5在"好"的中间，回退到3
		},
		{
			name:     "混合ASCII和中文",
			s:        "a你b好c",
			target:   2,
			expected: 1, // "a" 占1字节，target=2在"你"的中间，回退到1
		},
		{
			name:     "空字符串",
			s:        "",
			target:   0,
			expected: 0,
		},
		{
			name:     "负数目标",
			s:        "hello",
			target:   -1,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FindCharBoundary(tt.s, tt.target)
			if result != tt.expected {
				t.Errorf("FindCharBoundary(%q, %d) = %d, want %d", tt.s, tt.target, result, tt.expected)
			}
		})
	}
}

func TestExtractThinkingContent(t *testing.T) {
	detector := NewThinkingTagDetector()

	tests := []struct {
		name              string
		buffer            string
		expectedThinking  string
		expectedRemaining string
		expectedFound     bool
	}{
		{
			name:              "完整的thinking块",
			buffer:            "<thinking>my thoughts</thinking>\n\nafter text",
			expectedThinking:  "my thoughts",
			expectedRemaining: "after text",
			expectedFound:     true,
		},
		{
			name:              "只有开始标签",
			buffer:            "<thinking>incomplete",
			expectedThinking:  "",
			expectedRemaining: "<thinking>incomplete",
			expectedFound:     false,
		},
		{
			name:              "没有标签",
			buffer:            "just text",
			expectedThinking:  "",
			expectedRemaining: "just text",
			expectedFound:     false,
		},
		{
			name:              "空thinking块",
			buffer:            "<thinking></thinking>\n\n",
			expectedThinking:  "",
			expectedRemaining: "",
			expectedFound:     true,
		},
		{
			name:              "带换行的thinking内容",
			buffer:            "<thinking>line1\nline2\nline3</thinking>\n\nafter",
			expectedThinking:  "line1\nline2\nline3",
			expectedRemaining: "after",
			expectedFound:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			thinking, remaining, found := detector.ExtractThinkingContent(tt.buffer)
			if thinking != tt.expectedThinking {
				t.Errorf("thinking = %q, want %q", thinking, tt.expectedThinking)
			}
			if remaining != tt.expectedRemaining {
				t.Errorf("remaining = %q, want %q", remaining, tt.expectedRemaining)
			}
			if found != tt.expectedFound {
				t.Errorf("found = %v, want %v", found, tt.expectedFound)
			}
		})
	}
}

func TestHasPotentialThinkingTag(t *testing.T) {
	detector := NewThinkingTagDetector()

	tests := []struct {
		name     string
		buffer   string
		expected bool
	}{
		{
			name:     "部分开始标签-1",
			buffer:   "text<",
			expected: true,
		},
		{
			name:     "部分开始标签-2",
			buffer:   "text<th",
			expected: true,
		},
		{
			name:     "部分开始标签-3",
			buffer:   "text<thinkin",
			expected: true,
		},
		{
			name:     "部分结束标签",
			buffer:   "text</thin",
			expected: true,
		},
		{
			name:     "完整标签-不是部分",
			buffer:   "text<thinking>",
			expected: false,
		},
		{
			name:     "普通文本",
			buffer:   "just text",
			expected: false,
		},
		{
			name:     "空缓冲区",
			buffer:   "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detector.HasPotentialThinkingTag(tt.buffer)
			if result != tt.expected {
				t.Errorf("HasPotentialThinkingTag(%q) = %v, want %v", tt.buffer, result, tt.expected)
			}
		})
	}
}
