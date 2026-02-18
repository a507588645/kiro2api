package types

import (
	"encoding/json"
	"fmt"

	"kiro2api/config"
)

// AnthropicTool 表示 Anthropic API 的工具结构
type AnthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// ToolChoice 表示工具选择策略
type ToolChoice struct {
	Type string `json:"type"`           // "auto", "any", "tool"
	Name string `json:"name,omitempty"` // 当type为"tool"时指定的工具名称
}

// Thinking 表示 Claude 深度思考配置
type Thinking struct {
	Type         string `json:"type"`          // "enabled", "adaptive" 或 "disabled"
	BudgetTokens int    `json:"budget_tokens"` // 思考预算 token 数
}

// UnmarshalJSON 自定义反序列化，自动规范化 budget_tokens（借鉴 kiro.rs）
func (t *Thinking) UnmarshalJSON(data []byte) error {
	// 使用别名避免递归
	type ThinkingAlias Thinking
	aux := &struct {
		*ThinkingAlias
	}{
		ThinkingAlias: (*ThinkingAlias)(t),
	}

	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	// 仅当 thinking 启用时规范化 budget_tokens
	// 统一使用 NormalizeBudgetTokens() 避免逻辑重复
	if t.Type == "enabled" || t.Type == "adaptive" {
		t.BudgetTokens = t.NormalizeBudgetTokens()
	}

	return nil
}

// Validate 验证 thinking 配置
func (t *Thinking) Validate() error {
	if t.Type != "enabled" && t.Type != "adaptive" && t.Type != "disabled" && t.Type != "" {
		return fmt.Errorf("thinking.type 必须为 'enabled'、'adaptive' 或 'disabled'，当前为: %s", t.Type)
	}

	if t.Type == "enabled" || t.Type == "adaptive" {
		if t.BudgetTokens < config.ThinkingBudgetTokensMin {
			return fmt.Errorf("budget_tokens 不能小于 %d，当前为: %d",
				config.ThinkingBudgetTokensMin, t.BudgetTokens)
		}
	}

	return nil
}

// IsEnabled 检查是否启用了 thinking（enabled 或 adaptive）
func (t *Thinking) IsEnabled() bool {
	return t.Type == "enabled" || t.Type == "adaptive"
}

// NormalizeBudgetTokens 规范化 budget_tokens 值
func (t *Thinking) NormalizeBudgetTokens() int {
	if t.BudgetTokens <= 0 {
		return config.ThinkingBudgetTokensDefault
	}
	if t.BudgetTokens > config.ThinkingBudgetTokensMax {
		return config.ThinkingBudgetTokensMax
	}
	if t.BudgetTokens < config.ThinkingBudgetTokensMin {
		return config.ThinkingBudgetTokensMin
	}
	return t.BudgetTokens
}

// OutputConfig 表示输出配置（与 kiro.rs 对齐，用于 adaptive thinking 模式）
type OutputConfig struct {
	Effort string `json:"effort"` // "high", "medium", "low"
}

// AnthropicRequest 表示 Anthropic API 的请求结构
type AnthropicRequest struct {
	Model        string                    `json:"model"`
	MaxTokens    int                       `json:"max_tokens"`
	Messages     []AnthropicRequestMessage `json:"messages"`
	System       []AnthropicSystemMessage  `json:"system,omitempty"`
	Tools        []AnthropicTool           `json:"tools,omitempty"`
	ToolChoice   any                       `json:"tool_choice,omitempty"` // 可以是string或ToolChoice对象
	Stream       bool                      `json:"stream"`
	Temperature  *float64                  `json:"temperature,omitempty"`
	Metadata     map[string]any            `json:"metadata,omitempty"`
	Thinking     *Thinking                 `json:"thinking,omitempty"`       // Claude 深度思考配置
	OutputConfig *OutputConfig             `json:"output_config,omitempty"`  // 输出配置（adaptive thinking 的 effort）
}

// UnmarshalJSON 自定义反序列化，支持传统 Anthropic API 格式
// 修复: 传统 Anthropic API 格式兼容问题
// 参考: kiro.rs 2026.1.6 - 修复了传统 Anthropic API 格式兼容问题
func (r *AnthropicRequest) UnmarshalJSON(data []byte) error {
	// 使用别名避免递归
	type AnthropicRequestAlias AnthropicRequest
	
	// 首先尝试标准格式
	aux := &struct {
		*AnthropicRequestAlias
		System json.RawMessage `json:"system,omitempty"`
	}{
		AnthropicRequestAlias: (*AnthropicRequestAlias)(r),
	}
	
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	
	// 处理 system 字段
	if len(aux.System) > 0 {
		// 尝试解析为字符串（传统格式）
		var systemStr string
		if err := json.Unmarshal(aux.System, &systemStr); err == nil {
			// 传统字符串格式，转换为数组格式
			r.System = []AnthropicSystemMessage{{
				Type: "text",
				Text: systemStr,
			}}
		} else {
			// 尝试解析为数组格式（标准格式）
			var systemArr []AnthropicSystemMessage
			if err := json.Unmarshal(aux.System, &systemArr); err == nil {
				r.System = systemArr
			}
			// 如果都失败，System 将保持为 nil
		}
	}
	
	return nil
}

// AnthropicStreamResponse 表示 Anthropic 流式响应的结构
type AnthropicStreamResponse struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentDelta struct {
		Text string `json:"text"`
		Type string `json:"type"`
	} `json:"delta,omitempty"`
	Content []struct {
		Text string `json:"text"`
		Type string `json:"type"`
	} `json:"content,omitempty"`
	StopReason   string `json:"stop_reason,omitempty"`
	StopSequence string `json:"stop_sequence,omitempty"`
	Usage        *Usage `json:"usage,omitempty"`
}

// AnthropicRequestMessage 表示 Anthropic API 的消息结构
type AnthropicRequestMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // 可以是 string 或 []ContentBlock
}

type AnthropicSystemMessage struct {
	Type string `json:"type"`
	Text string `json:"text"` // 可以是 string 或 []ContentBlock
}

// ContentBlock 表示消息内容块的结构
type ContentBlock struct {
	Type      string       `json:"type"`
	Text      *string      `json:"text,omitempty"`
	ToolUseId *string      `json:"tool_use_id,omitempty"`
	Content   any          `json:"content,omitempty"`  // tool_result的内容，可以是string、[]any或map[string]any
	Name      *string      `json:"name,omitempty"`     // tool_use的名称
	Input     *any         `json:"input,omitempty"`    // tool_use的输入参数
	ID        *string      `json:"id,omitempty"`       // tool_use的唯一标识符
	IsError   *bool        `json:"is_error,omitempty"` // tool_result是否表示错误
	Source    *ImageSource `json:"source,omitempty"`   // 图片数据源
}

// ImageSource 表示图片数据源的结构
type ImageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/jpeg", "image/png", "image/gif", "image/webp"
	Data      string `json:"data"`       // base64编码的图片数据
}
