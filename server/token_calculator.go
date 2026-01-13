package server

import (
	"context"

	"kiro2api/logger"
	"kiro2api/types"
	"kiro2api/utils"
)

// TokenCalculator 统一的 Token 计算器
// 消除 handlers.go 中重复的 token 计算逻辑
type TokenCalculator struct {
	estimator *utils.TokenEstimator
	counter   *utils.TokenCounter
}

// NewTokenCalculator 创建 Token 计算器
func NewTokenCalculator() *TokenCalculator {
	return &TokenCalculator{
		estimator: utils.NewTokenEstimator(),
		counter:   utils.NewTokenCounterFromEnv(),
	}
}

// CalculateInputTokens 计算输入 tokens
// 优先使用官方 count_tokens API，失败则回退到本地估算
func (tc *TokenCalculator) CalculateInputTokens(ctx context.Context, req types.AnthropicRequest) int {
	countReq := tc.buildCountRequest(req)

	// 尝试使用官方 API
	inputTokens, err := tc.counter.CountInputTokens(ctx, countReq)
	if err != nil {
		logger.Debug("官方 token 计数失败，回退到本地估算",
			logger.Err(err),
			logger.String("model", req.Model))
		inputTokens = tc.estimator.EstimateTokens(countReq)
	}

	return inputTokens
}

// EstimateInputTokens 仅使用本地估算（不调用 API）
func (tc *TokenCalculator) EstimateInputTokens(req types.AnthropicRequest) int {
	countReq := tc.buildCountRequest(req)
	return tc.estimator.EstimateTokens(countReq)
}

// EstimateOutputTokens 估算输出 tokens
// 基于输出字符数和是否包含工具调用
func (tc *TokenCalculator) EstimateOutputTokens(text string, hasToolUse bool) int {
	baseTokens := tc.estimator.EstimateTextTokens(text)
	outputTokens := baseTokens

	if hasToolUse {
		// 工具调用增加 20% 结构化开销
		outputTokens = int(float64(baseTokens) * 1.2)
	}

	if outputTokens < 1 && len(text) > 0 {
		outputTokens = 1
	}

	return outputTokens
}

// buildCountRequest 构建 CountTokensRequest
func (tc *TokenCalculator) buildCountRequest(req types.AnthropicRequest) *types.CountTokensRequest {
	return &types.CountTokensRequest{
		Model:    req.Model,
		System:   req.System,
		Messages: req.Messages,
		Tools:    req.Tools,
	}
}

// ========== 全局单例 ==========

var defaultTokenCalculator *TokenCalculator

// GetTokenCalculator 获取全局 TokenCalculator 实例
func GetTokenCalculator() *TokenCalculator {
	if defaultTokenCalculator == nil {
		defaultTokenCalculator = NewTokenCalculator()
	}
	return defaultTokenCalculator
}
