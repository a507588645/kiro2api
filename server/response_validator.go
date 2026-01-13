package server

import (
	"fmt"

	"kiro2api/logger"
)

// ========== 响应验证器 ==========

// ResponseValidator 响应格式验证器
// 确保返回给客户端的数据符合 Anthropic/OpenAI API 规范
type ResponseValidator struct {
	format string // "anthropic" 或 "openai"
}

// NewAnthropicResponseValidator 创建 Anthropic 格式验证器
func NewAnthropicResponseValidator() *ResponseValidator {
	return &ResponseValidator{format: "anthropic"}
}

// NewOpenAIResponseValidator 创建 OpenAI 格式验证器
func NewOpenAIResponseValidator() *ResponseValidator {
	return &ResponseValidator{format: "openai"}
}

// ValidationError 验证错误
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error: %s - %s", e.Field, e.Message)
}

// ValidateMessageResponse 验证消息响应格式
func (v *ResponseValidator) ValidateMessageResponse(resp map[string]any) []ValidationError {
	var errors []ValidationError

	if v.format == "anthropic" {
		errors = v.validateAnthropicResponse(resp)
	} else {
		errors = v.validateOpenAIResponse(resp)
	}

	if len(errors) > 0 {
		logger.Warn("响应格式验证失败",
			logger.String("format", v.format),
			logger.Int("error_count", len(errors)))
	}

	return errors
}

// validateAnthropicResponse 验证 Anthropic 格式响应
func (v *ResponseValidator) validateAnthropicResponse(resp map[string]any) []ValidationError {
	var errors []ValidationError

	// 必需字段检查
	requiredFields := []string{"type", "role", "content", "model", "stop_reason"}
	for _, field := range requiredFields {
		if _, exists := resp[field]; !exists {
			errors = append(errors, ValidationError{
				Field:   field,
				Message: "required field missing",
			})
		}
	}

	// type 字段验证
	if msgType, ok := resp["type"].(string); ok {
		if msgType != "message" {
			errors = append(errors, ValidationError{
				Field:   "type",
				Message: fmt.Sprintf("expected 'message', got '%s'", msgType),
			})
		}
	}

	// role 字段验证
	if role, ok := resp["role"].(string); ok {
		if role != "assistant" {
			errors = append(errors, ValidationError{
				Field:   "role",
				Message: fmt.Sprintf("expected 'assistant', got '%s'", role),
			})
		}
	}

	// stop_reason 字段验证
	if stopReason, ok := resp["stop_reason"].(string); ok {
		validStopReasons := map[string]bool{
			"end_turn":      true,
			"max_tokens":    true,
			"stop_sequence": true,
			"tool_use":      true,
			"pause_turn":    true,
			"refusal":       true,
		}
		if !validStopReasons[stopReason] {
			errors = append(errors, ValidationError{
				Field:   "stop_reason",
				Message: fmt.Sprintf("invalid stop_reason: '%s'", stopReason),
			})
		}
	}

	// content 字段验证
	if content, ok := resp["content"].([]any); ok {
		for i, block := range content {
			if blockMap, ok := block.(map[string]any); ok {
				blockErrors := v.validateContentBlock(blockMap, i)
				errors = append(errors, blockErrors...)
			}
		}
	}

	// usage 字段验证
	if usage, ok := resp["usage"].(map[string]any); ok {
		if _, exists := usage["input_tokens"]; !exists {
			errors = append(errors, ValidationError{
				Field:   "usage.input_tokens",
				Message: "required field missing",
			})
		}
		if _, exists := usage["output_tokens"]; !exists {
			errors = append(errors, ValidationError{
				Field:   "usage.output_tokens",
				Message: "required field missing",
			})
		}
	}

	return errors
}

// validateContentBlock 验证内容块格式
func (v *ResponseValidator) validateContentBlock(block map[string]any, index int) []ValidationError {
	var errors []ValidationError

	blockType, ok := block["type"].(string)
	if !ok {
		errors = append(errors, ValidationError{
			Field:   fmt.Sprintf("content[%d].type", index),
			Message: "required field missing or invalid type",
		})
		return errors
	}

	switch blockType {
	case "text":
		if _, ok := block["text"].(string); !ok {
			errors = append(errors, ValidationError{
				Field:   fmt.Sprintf("content[%d].text", index),
				Message: "required field missing for text block",
			})
		}
	case "tool_use":
		if _, ok := block["id"].(string); !ok {
			errors = append(errors, ValidationError{
				Field:   fmt.Sprintf("content[%d].id", index),
				Message: "required field missing for tool_use block",
			})
		}
		if _, ok := block["name"].(string); !ok {
			errors = append(errors, ValidationError{
				Field:   fmt.Sprintf("content[%d].name", index),
				Message: "required field missing for tool_use block",
			})
		}
		if _, exists := block["input"]; !exists {
			errors = append(errors, ValidationError{
				Field:   fmt.Sprintf("content[%d].input", index),
				Message: "required field missing for tool_use block",
			})
		}
	case "thinking":
		if _, ok := block["thinking"].(string); !ok {
			errors = append(errors, ValidationError{
				Field:   fmt.Sprintf("content[%d].thinking", index),
				Message: "required field missing for thinking block",
			})
		}
	}

	return errors
}

// validateOpenAIResponse 验证 OpenAI 格式响应
func (v *ResponseValidator) validateOpenAIResponse(resp map[string]any) []ValidationError {
	var errors []ValidationError

	// 必需字段检查
	requiredFields := []string{"id", "object", "created", "model", "choices"}
	for _, field := range requiredFields {
		if _, exists := resp[field]; !exists {
			errors = append(errors, ValidationError{
				Field:   field,
				Message: "required field missing",
			})
		}
	}

	// object 字段验证
	if object, ok := resp["object"].(string); ok {
		if object != "chat.completion" && object != "chat.completion.chunk" {
			errors = append(errors, ValidationError{
				Field:   "object",
				Message: fmt.Sprintf("expected 'chat.completion' or 'chat.completion.chunk', got '%s'", object),
			})
		}
	}

	// choices 字段验证
	if choices, ok := resp["choices"].([]any); ok {
		for i, choice := range choices {
			if choiceMap, ok := choice.(map[string]any); ok {
				choiceErrors := v.validateOpenAIChoice(choiceMap, i)
				errors = append(errors, choiceErrors...)
			}
		}
	}

	return errors
}

// validateOpenAIChoice 验证 OpenAI choice 格式
func (v *ResponseValidator) validateOpenAIChoice(choice map[string]any, index int) []ValidationError {
	var errors []ValidationError

	// index 字段
	if _, exists := choice["index"]; !exists {
		errors = append(errors, ValidationError{
			Field:   fmt.Sprintf("choices[%d].index", index),
			Message: "required field missing",
		})
	}

	// message 或 delta 字段
	hasMessage := false
	if _, exists := choice["message"]; exists {
		hasMessage = true
	}
	if _, exists := choice["delta"]; exists {
		hasMessage = true
	}
	if !hasMessage {
		errors = append(errors, ValidationError{
			Field:   fmt.Sprintf("choices[%d].message/delta", index),
			Message: "either message or delta is required",
		})
	}

	// finish_reason 字段验证（如果存在）
	if finishReason, ok := choice["finish_reason"].(string); ok && finishReason != "" {
		validFinishReasons := map[string]bool{
			"stop":           true,
			"length":         true,
			"tool_calls":     true,
			"content_filter": true,
			"function_call":  true,
		}
		if !validFinishReasons[finishReason] {
			errors = append(errors, ValidationError{
				Field:   fmt.Sprintf("choices[%d].finish_reason", index),
				Message: fmt.Sprintf("invalid finish_reason: '%s'", finishReason),
			})
		}
	}

	return errors
}

// ========== 流式事件验证 ==========

// ValidateStreamEvent 验证流式事件格式
func (v *ResponseValidator) ValidateStreamEvent(event map[string]any) []ValidationError {
	var errors []ValidationError

	if v.format == "anthropic" {
		errors = v.validateAnthropicStreamEvent(event)
	} else {
		errors = v.validateOpenAIStreamEvent(event)
	}

	return errors
}

// validateAnthropicStreamEvent 验证 Anthropic 流式事件
func (v *ResponseValidator) validateAnthropicStreamEvent(event map[string]any) []ValidationError {
	var errors []ValidationError

	eventType, ok := event["type"].(string)
	if !ok {
		errors = append(errors, ValidationError{
			Field:   "type",
			Message: "required field missing",
		})
		return errors
	}

	// 验证事件类型
	validEventTypes := map[string]bool{
		"message_start":       true,
		"message_delta":       true,
		"message_stop":        true,
		"content_block_start": true,
		"content_block_delta": true,
		"content_block_stop":  true,
		"ping":                true,
		"error":               true,
	}

	if !validEventTypes[eventType] {
		errors = append(errors, ValidationError{
			Field:   "type",
			Message: fmt.Sprintf("invalid event type: '%s'", eventType),
		})
	}

	// 根据事件类型验证必需字段
	switch eventType {
	case "content_block_start", "content_block_stop":
		if _, exists := event["index"]; !exists {
			errors = append(errors, ValidationError{
				Field:   "index",
				Message: fmt.Sprintf("required for %s event", eventType),
			})
		}
	case "content_block_delta":
		if _, exists := event["index"]; !exists {
			errors = append(errors, ValidationError{
				Field:   "index",
				Message: "required for content_block_delta event",
			})
		}
		if _, exists := event["delta"]; !exists {
			errors = append(errors, ValidationError{
				Field:   "delta",
				Message: "required for content_block_delta event",
			})
		}
	case "message_delta":
		if _, exists := event["delta"]; !exists {
			errors = append(errors, ValidationError{
				Field:   "delta",
				Message: "required for message_delta event",
			})
		}
	}

	return errors
}

// validateOpenAIStreamEvent 验证 OpenAI 流式事件
func (v *ResponseValidator) validateOpenAIStreamEvent(event map[string]any) []ValidationError {
	var errors []ValidationError

	// OpenAI 流式事件必须有 choices
	if _, exists := event["choices"]; !exists {
		// [DONE] 消息除外
		if event["object"] != "[DONE]" {
			errors = append(errors, ValidationError{
				Field:   "choices",
				Message: "required field missing",
			})
		}
	}

	return errors
}
