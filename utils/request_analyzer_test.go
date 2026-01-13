package utils

import (
	"strings"
	"testing"

	"kiro2api/types"

	"github.com/stretchr/testify/assert"
)

func TestAnalyzeRequestComplexity_SimpleRequest(t *testing.T) {
	req := types.AnthropicRequest{
		Model:     "claude-3-sonnet-20240229",
		MaxTokens: 500,
		Messages: []types.AnthropicRequestMessage{
			{
				Role:    "user",
				Content: "Hello, how are you?",
			},
		},
	}

	complexity := AnalyzeRequestComplexity(req)
	assert.Equal(t, SimpleRequest, complexity, "简单请求应该被识别为SimpleRequest")
}

func TestAnalyzeRequestComplexity_HighMaxTokens(t *testing.T) {
	req := types.AnthropicRequest{
		Model:     "claude-3-sonnet-20240229",
		MaxTokens: 5000, // > 4000, score +2
		Messages: []types.AnthropicRequestMessage{
			{
				Role:    "user",
				Content: "Tell me a story.",
			},
		},
	}

	complexity := AnalyzeRequestComplexity(req)
	// MaxTokens > 4000 (+2分) 但总分未达到3，仍为Simple
	assert.Equal(t, SimpleRequest, complexity)
}

func TestAnalyzeRequestComplexity_LongContent(t *testing.T) {
	// 创建超过10K字符的内容
	longContent := strings.Repeat("a", 11000)

	req := types.AnthropicRequest{
		Model:     "claude-3-sonnet-20240229",
		MaxTokens: 1000,
		Messages: []types.AnthropicRequestMessage{
			{
				Role:    "user",
				Content: longContent,
			},
		},
	}

	complexity := AnalyzeRequestComplexity(req)
	// 内容长度 > 10000 (+2分) 但总分未达到3
	assert.Equal(t, SimpleRequest, complexity)
}

func TestAnalyzeRequestComplexity_WithTools(t *testing.T) {
	req := types.AnthropicRequest{
		Model:     "claude-3-sonnet-20240229",
		MaxTokens: 2000, // > 1000, score +1
		Messages: []types.AnthropicRequestMessage{
			{
				Role:    "user",
				Content: "Use the calculator tool.",
			},
		},
		Tools: []types.AnthropicTool{
			{
				Name:        "calculator",
				Description: "A simple calculator",
			},
		},
	}

	complexity := AnalyzeRequestComplexity(req)
	// MaxTokens > 1000 (+1) + Tools (+2) = 3分，达到ComplexRequest阈值
	assert.Equal(t, ComplexRequest, complexity, "带工具的请求应该被识别为ComplexRequest")
}

func TestAnalyzeRequestComplexity_ComplexKeywords(t *testing.T) {
	tests := []struct {
		name    string
		content string
		keyword string
	}{
		{"中文关键词-分析", "请分析这段代码", "分析"},
		{"英文关键词-analyze", "Please analyze this code", "analyze"},
		{"中文关键词-详细", "请给出详细的解释", "详细"},
		{"英文关键词-detail", "Give me detailed information", "detail"},
		{"中文关键词-代码审查", "进行代码审查", "代码审查"},
		{"英文关键词-refactor", "Refactor this function", "refactor"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := types.AnthropicRequest{
				Model:     "claude-3-sonnet-20240229",
				MaxTokens: 2000, // > 1000, score +1
				Messages: []types.AnthropicRequestMessage{
					{
						Role:    "user",
						Content: tt.content,
					},
				},
			}

			complexity := AnalyzeRequestComplexity(req)
			// MaxTokens > 1000 (+1) + 关键词 (+1) = 2分，未达到3分阈值
			assert.Equal(t, SimpleRequest, complexity)
		})
	}
}

func TestAnalyzeRequestComplexity_MultipleFactors(t *testing.T) {
	// 创建一个包含多个复杂因素的请求
	longContent := strings.Repeat("Please analyze this code in detail. ", 100) // ~3700字符

	req := types.AnthropicRequest{
		Model:     "claude-3-sonnet-20240229",
		MaxTokens: 5000, // > 4000, score +2
		Messages: []types.AnthropicRequestMessage{
			{
				Role:    "user",
				Content: longContent, // > 3000字符, score +1; 包含"analyze", score +1
			},
		},
		Tools: []types.AnthropicTool{
			{
				Name: "code_analyzer",
			},
		}, // score +2
	}

	complexity := AnalyzeRequestComplexity(req)
	// MaxTokens (+2) + 内容长度 (+1) + 关键词 (+1) + Tools (+2) = 6分
	assert.Equal(t, ComplexRequest, complexity, "多因素复杂请求应该被识别为ComplexRequest")
}

func TestAnalyzeRequestComplexity_WithSystemPrompt(t *testing.T) {
	longSystemPrompt := strings.Repeat("You are an expert assistant. ", 100) // ~3000字符

	req := types.AnthropicRequest{
		Model:     "claude-3-sonnet-20240229",
		MaxTokens: 2000, // > 1000, score +1
		Messages: []types.AnthropicRequestMessage{
			{
				Role:    "user",
				Content: "Hello",
			},
		},
		System: []types.AnthropicSystemMessage{
			{
				Type: "text",
				Text: longSystemPrompt, // > 2000字符, score +1
			},
		},
	}

	complexity := AnalyzeRequestComplexity(req)
	// MaxTokens (+1) + System (+1) = 2分，未达到3分阈值
	assert.Equal(t, SimpleRequest, complexity)
}

func TestAnalyzeRequestComplexity_EdgeCase_ExactlyThreeScore(t *testing.T) {
	req := types.AnthropicRequest{
		Model:     "claude-3-sonnet-20240229",
		MaxTokens: 5000, // > 4000, score +2
		Messages: []types.AnthropicRequestMessage{
			{
				Role:    "user",
				Content: "Please analyze this.", // 包含"analyze", score +1
			},
		},
	}

	complexity := AnalyzeRequestComplexity(req)
	// MaxTokens (+2) + 关键词 (+1) = 3分，达到阈值
	assert.Equal(t, ComplexRequest, complexity, "评分恰好为3应该被识别为ComplexRequest")
}

func TestAnalyzeRequestComplexity_EmptyMessages(t *testing.T) {
	req := types.AnthropicRequest{
		Model:     "claude-3-sonnet-20240229",
		MaxTokens: 100,
		Messages:  []types.AnthropicRequestMessage{},
	}

	complexity := AnalyzeRequestComplexity(req)
	assert.Equal(t, SimpleRequest, complexity, "空消息应该被识别为SimpleRequest")
}

func TestAnalyzeRequestComplexity_MultipleMessages(t *testing.T) {
	req := types.AnthropicRequest{
		Model:     "claude-3-sonnet-20240229",
		MaxTokens: 2000, // > 1000, score +1
		Messages: []types.AnthropicRequestMessage{
			{
				Role:    "user",
				Content: strings.Repeat("a", 2000), // 累计内容 > 3000, score +1
			},
			{
				Role:    "assistant",
				Content: "Response",
			},
			{
				Role:    "user",
				Content: strings.Repeat("b", 2000), // 累计内容 > 3000
			},
		},
	}

	complexity := AnalyzeRequestComplexity(req)
	// MaxTokens (+1) + 内容长度 (+1) = 2分，未达到3分阈值
	assert.Equal(t, SimpleRequest, complexity)
}
