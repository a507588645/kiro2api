package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"kiro2api/logger"
)

// ========== 错误响应结构 ==========

// ClaudeErrorResponse Claude API 规范的错误响应结构
type ClaudeErrorResponse struct {
	Type       string `json:"type"`
	Message    string `json:"message"`
	Code       string `json:"code,omitempty"`
	StopReason string `json:"stop_reason,omitempty"`
	HTTPStatus int    `json:"-"` // 不序列化，仅用于内部传递
}

// CodeWhispererErrorBody AWS CodeWhisperer 错误响应体
type CodeWhispererErrorBody struct {
	Message string `json:"message"`
	Reason  string `json:"reason"`
}

// ========== 错误映射策略接口 ==========

// ErrorMappingStrategy 错误映射策略接口 (DIP 原则)
type ErrorMappingStrategy interface {
	// CanHandle 判断是否能处理该错误
	CanHandle(statusCode int, responseBody []byte) bool
	// MapError 映射错误到 Claude 格式
	MapError(statusCode int, responseBody []byte) *ClaudeErrorResponse
	// GetStrategyName 获取策略名称（用于日志）
	GetStrategyName() string
	// ShouldMarkTokenFailed 是否应该标记 token 失败
	ShouldMarkTokenFailed() bool
}

// ========== 具体策略实现 ==========

// ForbiddenStrategy 403 错误处理策略
// 场景：Token 失效、权限不足
type ForbiddenStrategy struct{}

func (s *ForbiddenStrategy) CanHandle(statusCode int, _ []byte) bool {
	return statusCode == http.StatusForbidden
}

func (s *ForbiddenStrategy) MapError(statusCode int, responseBody []byte) *ClaudeErrorResponse {
	// 尝试解析错误详情
	var errorBody CodeWhispererErrorBody
	_ = json.Unmarshal(responseBody, &errorBody)

	message := "Token 已失效，请重试"
	if errorBody.Message != "" {
		message = errorBody.Message
	}

	return &ClaudeErrorResponse{
		Type:       "error",
		Code:       "unauthorized",
		Message:    message,
		HTTPStatus: http.StatusUnauthorized, // 转换为 401 返回给客户端
	}
}

func (s *ForbiddenStrategy) GetStrategyName() string {
	return "forbidden"
}

func (s *ForbiddenStrategy) ShouldMarkTokenFailed() bool {
	return true
}

// RateLimitStrategy 429 错误处理策略
// 场景：请求过于频繁
type RateLimitStrategy struct{}

func (s *RateLimitStrategy) CanHandle(statusCode int, _ []byte) bool {
	return statusCode == http.StatusTooManyRequests
}

func (s *RateLimitStrategy) MapError(statusCode int, responseBody []byte) *ClaudeErrorResponse {
	var errorBody CodeWhispererErrorBody
	_ = json.Unmarshal(responseBody, &errorBody)

	message := "请求过于频繁，请稍后重试"
	if errorBody.Message != "" {
		message = errorBody.Message
	}

	return &ClaudeErrorResponse{
		Type:       "error",
		Code:       "rate_limited",
		Message:    message,
		HTTPStatus: http.StatusTooManyRequests,
	}
}

func (s *RateLimitStrategy) GetStrategyName() string {
	return "rate_limit"
}

func (s *RateLimitStrategy) ShouldMarkTokenFailed() bool {
	// 修复: 429 错误是瞬态错误，不应标记 token 失败
	// 参考: kiro.rs 2026.1.3 - 修复了 kiro 返回 429 高流量时会导致凭据被禁用的问题
	// 429 错误应该只触发冷却（在 session_token_pool.go 中处理），而不是标记为失败
	return false
}

// PaymentRequiredStrategy 402 错误处理策略
// 场景：月度请求配额耗尽 (MONTHLY_REQUEST_COUNT)
type PaymentRequiredStrategy struct{}

func (s *PaymentRequiredStrategy) CanHandle(statusCode int, responseBody []byte) bool {
	if statusCode != http.StatusPaymentRequired {
		return false
	}

	var errorBody CodeWhispererErrorBody
	if err := json.Unmarshal(responseBody, &errorBody); err != nil {
		return false
	}

	// 检查是否是月度配额耗尽错误
	return strings.Contains(errorBody.Reason, "MONTHLY_REQUEST_COUNT") ||
		strings.Contains(errorBody.Message, "monthly") ||
		strings.Contains(errorBody.Message, "quota")
}

func (s *PaymentRequiredStrategy) MapError(statusCode int, responseBody []byte) *ClaudeErrorResponse {
	var errorBody CodeWhispererErrorBody
	_ = json.Unmarshal(responseBody, &errorBody)

	message := "月度请求配额已耗尽，请稍后重试或更换凭据"
	if errorBody.Message != "" {
		message = errorBody.Message
	}

	return &ClaudeErrorResponse{
		Type:       "error",
		Code:       "rate_limited",
		Message:    message,
		HTTPStatus: http.StatusTooManyRequests, // 转换为 429 返回给客户端
	}
}

func (s *PaymentRequiredStrategy) GetStrategyName() string {
	return "payment_required"
}

func (s *PaymentRequiredStrategy) ShouldMarkTokenFailed() bool {
	// 修复: 402 MONTHLY_REQUEST_COUNT 时应禁用凭据并故障转移
	// 参考: kiro.rs 2026.1.4 - fix: 402 MONTHLY_REQUEST_COUNT 时禁用凭据并故障转移
	return true
}

// ContentLengthExceedsStrategy 内容长度超限错误映射策略
// 场景：CONTENT_LENGTH_EXCEEDS_THRESHOLD
type ContentLengthExceedsStrategy struct{}

func (s *ContentLengthExceedsStrategy) CanHandle(statusCode int, responseBody []byte) bool {
	if statusCode != http.StatusBadRequest {
		return false
	}

	var errorBody CodeWhispererErrorBody
	if err := json.Unmarshal(responseBody, &errorBody); err != nil {
		return false
	}

	return errorBody.Reason == "CONTENT_LENGTH_EXCEEDS_THRESHOLD"
}

func (s *ContentLengthExceedsStrategy) MapError(statusCode int, responseBody []byte) *ClaudeErrorResponse {
	return &ClaudeErrorResponse{
		Type:       "message_delta",
		StopReason: "max_tokens",
		Message:    "Content length exceeds threshold, response truncated",
		HTTPStatus: http.StatusOK, // 这种情况返回 200，通过 stop_reason 表示
	}
}

func (s *ContentLengthExceedsStrategy) GetStrategyName() string {
	return "content_length_exceeds"
}

func (s *ContentLengthExceedsStrategy) ShouldMarkTokenFailed() bool {
	return false
}

// ValidationErrorStrategy 请求验证错误策略
// 场景：400 Bad Request（非内容长度超限）
type ValidationErrorStrategy struct{}

func (s *ValidationErrorStrategy) CanHandle(statusCode int, responseBody []byte) bool {
	if statusCode != http.StatusBadRequest {
		return false
	}

	// 排除 ContentLengthExceedsStrategy 已处理的情况
	var errorBody CodeWhispererErrorBody
	if err := json.Unmarshal(responseBody, &errorBody); err == nil {
		if errorBody.Reason == "CONTENT_LENGTH_EXCEEDS_THRESHOLD" {
			return false
		}
	}

	return true
}

func (s *ValidationErrorStrategy) MapError(statusCode int, responseBody []byte) *ClaudeErrorResponse {
	var errorBody CodeWhispererErrorBody
	_ = json.Unmarshal(responseBody, &errorBody)

	message := "请求参数无效"
	if errorBody.Message != "" {
		message = errorBody.Message
	}

	return &ClaudeErrorResponse{
		Type:       "error",
		Code:       "invalid_request_error",
		Message:    message,
		HTTPStatus: http.StatusBadRequest,
	}
}

func (s *ValidationErrorStrategy) GetStrategyName() string {
	return "validation_error"
}

func (s *ValidationErrorStrategy) ShouldMarkTokenFailed() bool {
	return false
}

// ServiceUnavailableStrategy 服务不可用策略
// 场景：503 Service Unavailable
type ServiceUnavailableStrategy struct{}

func (s *ServiceUnavailableStrategy) CanHandle(statusCode int, _ []byte) bool {
	return statusCode == http.StatusServiceUnavailable
}

func (s *ServiceUnavailableStrategy) MapError(statusCode int, responseBody []byte) *ClaudeErrorResponse {
	return &ClaudeErrorResponse{
		Type:       "error",
		Code:       "overloaded_error",
		Message:    "服务暂时不可用，请稍后重试",
		HTTPStatus: http.StatusServiceUnavailable,
	}
}

func (s *ServiceUnavailableStrategy) GetStrategyName() string {
	return "service_unavailable"
}

func (s *ServiceUnavailableStrategy) ShouldMarkTokenFailed() bool {
	return false
}

// InternalErrorStrategy 内部错误策略
// 场景：500 Internal Server Error
type InternalErrorStrategy struct{}

func (s *InternalErrorStrategy) CanHandle(statusCode int, _ []byte) bool {
	return statusCode == http.StatusInternalServerError
}

func (s *InternalErrorStrategy) MapError(statusCode int, responseBody []byte) *ClaudeErrorResponse {
	var errorBody CodeWhispererErrorBody
	_ = json.Unmarshal(responseBody, &errorBody)

	message := "上游服务内部错误"
	if errorBody.Message != "" {
		message = errorBody.Message
	}

	return &ClaudeErrorResponse{
		Type:       "error",
		Code:       "api_error",
		Message:    message,
		HTTPStatus: http.StatusInternalServerError,
	}
}

func (s *InternalErrorStrategy) GetStrategyName() string {
	return "internal_error"
}

func (s *InternalErrorStrategy) ShouldMarkTokenFailed() bool {
	return false
}

// DefaultErrorStrategy 默认错误映射策略（兜底）
type DefaultErrorStrategy struct{}

func (s *DefaultErrorStrategy) CanHandle(statusCode int, _ []byte) bool {
	return statusCode != http.StatusOK
}

func (s *DefaultErrorStrategy) MapError(statusCode int, responseBody []byte) *ClaudeErrorResponse {
	return &ClaudeErrorResponse{
		Type:       "error",
		Code:       "api_error",
		Message:    fmt.Sprintf("Upstream error: %s", string(responseBody)),
		HTTPStatus: statusCode,
	}
}

func (s *DefaultErrorStrategy) GetStrategyName() string {
	return "default"
}

func (s *DefaultErrorStrategy) ShouldMarkTokenFailed() bool {
	return false
}

// ========== 错误映射器 ==========

// ErrorMapper 错误映射器 (Strategy Pattern + Chain of Responsibility)
type ErrorMapper struct {
	strategies []ErrorMappingStrategy
}

// NewErrorMapper 创建错误映射器
func NewErrorMapper() *ErrorMapper {
	return &ErrorMapper{
		strategies: []ErrorMappingStrategy{
			// 按优先级排序：特定错误优先，默认兜底
			&ContentLengthExceedsStrategy{}, // 内容长度超限（特殊处理）
			&PaymentRequiredStrategy{},      // 402 月度配额耗尽
			&ForbiddenStrategy{},            // 403 Token 失效
			&RateLimitStrategy{},            // 429 限流
			&ValidationErrorStrategy{},      // 400 验证错误
			&ServiceUnavailableStrategy{},   // 503 服务不可用
			&InternalErrorStrategy{},        // 500 内部错误
			&DefaultErrorStrategy{},         // 默认兜底
		},
	}
}

// MapResult 映射结果
type MapResult struct {
	Response            *ClaudeErrorResponse
	Strategy            ErrorMappingStrategy
	ShouldMarkTokenFail bool
}

// MapCodeWhispererError 映射 CodeWhisperer 错误到 Claude 格式
func (em *ErrorMapper) MapCodeWhispererError(statusCode int, responseBody []byte) *MapResult {
	for _, strategy := range em.strategies {
		if strategy.CanHandle(statusCode, responseBody) {
			response := strategy.MapError(statusCode, responseBody)

			logger.Debug("错误映射成功",
				logger.String("strategy", strategy.GetStrategyName()),
				logger.Int("status_code", statusCode),
				logger.String("mapped_type", response.Type),
				logger.String("mapped_code", response.Code),
				logger.String("stop_reason", response.StopReason))

			return &MapResult{
				Response:            response,
				Strategy:            strategy,
				ShouldMarkTokenFail: strategy.ShouldMarkTokenFailed(),
			}
		}
	}

	// 理论上不会到达这里，因为 DefaultErrorStrategy 总是返回 true
	return &MapResult{
		Response: &ClaudeErrorResponse{
			Type:       "error",
			Code:       "unknown_error",
			Message:    "Unknown error",
			HTTPStatus: statusCode,
		},
		Strategy:            &DefaultErrorStrategy{},
		ShouldMarkTokenFail: false,
	}
}

// SendClaudeError 发送 Claude 规范的错误响应
func (em *ErrorMapper) SendClaudeError(c *gin.Context, result *MapResult) {
	claudeError := result.Response

	// 根据错误类型决定发送格式
	if claudeError.StopReason == "max_tokens" {
		// 发送 message_delta 事件，符合 Claude 规范
		em.sendMaxTokensResponse(c, claudeError)
	} else {
		// 发送标准错误响应
		em.sendStandardError(c, claudeError)
	}
}

// SendStreamError 发送流式错误响应
func (em *ErrorMapper) SendStreamError(c *gin.Context, result *MapResult, sender StreamEventSender) {
	claudeError := result.Response

	if claudeError.StopReason == "max_tokens" {
		// max_tokens 场景：发送 message_delta 事件
		response := map[string]any{
			"type": "message_delta",
			"delta": map[string]any{
				"stop_reason":   "max_tokens",
				"stop_sequence": nil,
			},
			"usage": map[string]any{
				"input_tokens":  0,
				"output_tokens": 0,
			},
		}
		_ = sender.SendEvent(c, response)
	} else {
		// 其他错误：发送 error 事件
		_ = sender.SendError(c, claudeError.Message, nil)
	}
}

// sendMaxTokensResponse 发送 max_tokens 类型的响应
func (em *ErrorMapper) sendMaxTokensResponse(c *gin.Context, claudeError *ClaudeErrorResponse) {
	response := map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   "max_tokens",
			"stop_sequence": nil,
		},
		"usage": map[string]any{
			"input_tokens":  0,
			"output_tokens": 0,
		},
	}

	sender := &AnthropicStreamSender{}
	if err := sender.SendEvent(c, response); err != nil {
		logger.Error("发送 max_tokens 响应失败",
			logger.Err(err),
			logger.String("original_message", claudeError.Message))
	}

	logger.Info("已发送 max_tokens stop_reason 响应",
		addReqFields(c,
			logger.String("stop_reason", "max_tokens"),
			logger.String("original_message", claudeError.Message))...)
}

// sendStandardError 发送标准错误响应
func (em *ErrorMapper) sendStandardError(c *gin.Context, claudeError *ClaudeErrorResponse) {
	c.JSON(claudeError.HTTPStatus, gin.H{
		"error": gin.H{
			"type":    claudeError.Code,
			"message": claudeError.Message,
		},
	})
}

// ========== 流式异常处理策略 ==========

// StreamExceptionStrategy 流式异常处理策略接口
type StreamExceptionStrategy interface {
	// CanHandle 判断是否能处理该异常
	CanHandle(exceptionType string, dataMap map[string]any) bool
	// Handle 处理异常，返回是否已处理（true 表示不需要转发原始事件）
	Handle(ctx *StreamProcessorContext, dataMap map[string]any) bool
	// GetStrategyName 获取策略名称
	GetStrategyName() string
}

// ContentLengthExceptionStrategy 内容长度超限异常处理
type ContentLengthExceptionStrategy struct{}

func (s *ContentLengthExceptionStrategy) CanHandle(exceptionType string, _ map[string]any) bool {
	return exceptionType == "ContentLengthExceededException" ||
		strings.Contains(exceptionType, "CONTENT_LENGTH_EXCEEDS")
}

func (s *ContentLengthExceptionStrategy) Handle(ctx *StreamProcessorContext, dataMap map[string]any) bool {
	logger.Info("检测到内容长度超限异常，映射为 max_tokens stop_reason",
		addReqFields(ctx.c,
			logger.String("exception_type", dataMap["exception_type"].(string)),
			logger.String("claude_stop_reason", "max_tokens"))...)

	// 关闭所有活跃的 content_block
	activeBlocks := ctx.sseStateManager.GetActiveBlocks()
	for index, block := range activeBlocks {
		if block.Started && !block.Stopped {
			stopEvent := map[string]any{
				"type":  "content_block_stop",
				"index": index,
			}
			_ = ctx.sseStateManager.SendEvent(ctx.c, ctx.sender, stopEvent)
		}
	}

	// 构造符合 Claude 规范的 max_tokens 响应
	maxTokensEvent := map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   "max_tokens",
			"stop_sequence": nil,
		},
		"usage": map[string]any{
			"input_tokens":  ctx.inputTokens,
			"output_tokens": ctx.totalOutputTokens,
		},
	}

	if err := ctx.sseStateManager.SendEvent(ctx.c, ctx.sender, maxTokensEvent); err != nil {
		logger.Error("发送 max_tokens 响应失败", logger.Err(err))
		return false
	}

	// 发送 message_stop 事件
	stopEvent := map[string]any{
		"type": "message_stop",
	}
	if err := ctx.sseStateManager.SendEvent(ctx.c, ctx.sender, stopEvent); err != nil {
		logger.Error("发送 message_stop 失败", logger.Err(err))
		return false
	}

	ctx.c.Writer.Flush()
	return true
}

func (s *ContentLengthExceptionStrategy) GetStrategyName() string {
	return "content_length_exception"
}

// ThrottlingExceptionStrategy 限流异常处理
type ThrottlingExceptionStrategy struct{}

func (s *ThrottlingExceptionStrategy) CanHandle(exceptionType string, _ map[string]any) bool {
	return exceptionType == "ThrottlingException" ||
		strings.Contains(exceptionType, "Throttling")
}

func (s *ThrottlingExceptionStrategy) Handle(ctx *StreamProcessorContext, dataMap map[string]any) bool {
	logger.Warn("检测到限流异常",
		addReqFields(ctx.c,
			logger.String("exception_type", dataMap["exception_type"].(string)))...)

	// 发送 overloaded_error
	errorEvent := map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    "overloaded_error",
			"message": "服务繁忙，请稍后重试",
		},
	}

	if err := ctx.sseStateManager.SendEvent(ctx.c, ctx.sender, errorEvent); err != nil {
		logger.Error("发送限流错误失败", logger.Err(err))
		return false
	}

	ctx.c.Writer.Flush()
	return true
}

func (s *ThrottlingExceptionStrategy) GetStrategyName() string {
	return "throttling_exception"
}

// StreamExceptionMapper 流式异常映射器
type StreamExceptionMapper struct {
	strategies []StreamExceptionStrategy
}

// NewStreamExceptionMapper 创建流式异常映射器
func NewStreamExceptionMapper() *StreamExceptionMapper {
	return &StreamExceptionMapper{
		strategies: []StreamExceptionStrategy{
			&ContentLengthExceptionStrategy{},
			&ThrottlingExceptionStrategy{},
		},
	}
}

// HandleException 处理流式异常
// 返回 true 表示已处理，不需要转发原始事件
func (sem *StreamExceptionMapper) HandleException(ctx *StreamProcessorContext, dataMap map[string]any) bool {
	exceptionType, _ := dataMap["exception_type"].(string)
	if exceptionType == "" {
		return false
	}

	for _, strategy := range sem.strategies {
		if strategy.CanHandle(exceptionType, dataMap) {
			logger.Debug("使用策略处理流式异常",
				logger.String("strategy", strategy.GetStrategyName()),
				logger.String("exception_type", exceptionType))
			return strategy.Handle(ctx, dataMap)
		}
	}

	// 没有匹配的策略，返回 false 让原始事件继续转发
	return false
}
