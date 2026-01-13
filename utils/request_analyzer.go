package utils

import (
	"strings"

	"kiro2api/types"
)

// RequestComplexity 请求复杂度枚举
type RequestComplexity int

const (
	SimpleRequest  RequestComplexity = iota // 简单请求
	ComplexRequest                          // 复杂请求
)

// AnalyzeRequestComplexity 分析请求复杂度
func AnalyzeRequestComplexity(req types.AnthropicRequest) RequestComplexity {
	complexityScore := 0

	// 1. 检查最大token数量
	if req.MaxTokens > 4000 {
		complexityScore += 2
	} else if req.MaxTokens > 1000 {
		complexityScore += 1
	}

	// 2. 检查消息数量和长度
	totalContentLength := 0
	for _, msg := range req.Messages {
		content, _ := GetMessageContent(msg.Content)
		totalContentLength += len(content)
	}

	if totalContentLength > 10000 { // 超过10K字符
		complexityScore += 2
	} else if totalContentLength > 3000 { // 超过3K字符
		complexityScore += 1
	}

	// 3. 检查工具使用
	if len(req.Tools) > 0 {
		complexityScore += 2 // 工具调用通常需要更多时间
	}

	// 4. 检查系统提示复杂度
	if len(req.System) > 0 {
		systemContent := ""
		for _, sysMsg := range req.System {
			systemContent += sysMsg.Text
		}

		if len(systemContent) > 2000 {
			complexityScore += 1
		}
	}

	// 5. 检查消息内容复杂度
	for _, msg := range req.Messages {
		content, _ := GetMessageContent(msg.Content)
		// 检查是否包含复杂任务关键词
		complexKeywords := []string{
			"分析", "analyze", "详细", "detail", "总结", "summary",
			"代码审查", "code review", "重构", "refactor", "优化", "optimize",
			"复杂", "complex", "深入", "comprehensive", "完整", "complete",
		}

		contentLower := strings.ToLower(content)
		for _, keyword := range complexKeywords {
			if strings.Contains(contentLower, keyword) {
				complexityScore += 1
				break
			}
		}
	}

	// 根据复杂度评分判断
	if complexityScore >= 3 {
		return ComplexRequest
	}
	return SimpleRequest
}
