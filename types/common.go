package types

import (
	"fmt"
)

// Usage 表示API使用统计的通用结构
type Usage struct {
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`
	// Anthropic格式的兼容字段
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
}

// ToAnthropicFormat 转换为Anthropic格式
func (u *Usage) ToAnthropicFormat() Usage {
	return Usage{
		InputTokens:  u.PromptTokens,
		OutputTokens: u.CompletionTokens,
	}
}

// ToOpenAIFormat 转换为OpenAI格式
func (u *Usage) ToOpenAIFormat() Usage {
	total := u.PromptTokens + u.CompletionTokens
	if total == 0 {
		total = u.InputTokens + u.OutputTokens
	}
	return Usage{
		PromptTokens:     u.PromptTokens + u.InputTokens,
		CompletionTokens: u.CompletionTokens + u.OutputTokens,
		TotalTokens:      total,
	}
}

// ModelNotFoundError 模型未找到错误结构
type ModelNotFoundError struct {
	Error ModelNotFoundErrorDetail `json:"error"`
}

// ModelNotFoundErrorDetail 模型未找到错误详细信息
type ModelNotFoundErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Type    string `json:"type"`
}

// NewModelNotFoundError 创建模型未找到错误
func NewModelNotFoundError(model, requestId string) *ModelNotFoundError {
	return &ModelNotFoundError{
		Error: ModelNotFoundErrorDetail{
			Code: "model_not_found",
			Message: fmt.Sprintf("分组 default 下模型 %s 无可用渠道（distributor） (request id: %s)",
				model, requestId),
			Type: "new_api_error",
		},
	}
}

// ModelNotFoundErrorType 模型未找到错误的类型包装器，用于在错误处理中识别
type ModelNotFoundErrorType struct {
	ErrorData *ModelNotFoundError
}

// Error 实现 error 接口
func (e *ModelNotFoundErrorType) Error() string {
	return fmt.Sprintf("model not found: %s", e.ErrorData.Error.Message)
}

// NewModelNotFoundErrorType 创建模型未找到错误类型
func NewModelNotFoundErrorType(model, requestId string) *ModelNotFoundErrorType {
	return &ModelNotFoundErrorType{
		ErrorData: NewModelNotFoundError(model, requestId),
	}
}

// ParamNameMapping 存储参数名映射：truncatedName -> originalName
// 用于在工具调用时将截断后的参数名映射回原始参数名
type ParamNameMapping map[string]string

// ToolParamMappings 存储所有工具的参数名映射：toolName -> ParamNameMapping
// 当参数名超过64字符被截断时，需要维护此映射以便后续处理工具调用结果
type ToolParamMappings map[string]ParamNameMapping

// ConvertedToolsResult 工具转换结果，包含转换后的工具列表和参数名映射
type ConvertedToolsResult struct {
	// Tools 转换后的 Anthropic 格式工具列表
	Tools []AnthropicTool
	// ParamMappings 参数名映射表，key 为工具名，value 为该工具的参数名映射
	// 映射方向：truncatedName -> originalName
	ParamMappings ToolParamMappings
}

// HasMappings 检查是否存在任何参数名映射
func (r *ConvertedToolsResult) HasMappings() bool {
	return len(r.ParamMappings) > 0
}

// GetOriginalParamName 获取原始参数名
// 如果参数名未被截断，返回原参数名
func (r *ConvertedToolsResult) GetOriginalParamName(toolName, truncatedName string) string {
	if toolMapping, ok := r.ParamMappings[toolName]; ok {
		if originalName, ok := toolMapping[truncatedName]; ok {
			return originalName
		}
	}
	return truncatedName
}
