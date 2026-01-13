package converter

import (
	"testing"

	"kiro2api/types"

	"github.com/stretchr/testify/assert"
)

func TestConvertOpenAIToAnthropic_BasicMessage(t *testing.T) {
	maxTokens := 1024
	stream := true

	openaiReq := types.OpenAIRequest{
		Model:     "gpt-4",
		MaxTokens: &maxTokens,
		Stream:    &stream,
		Messages: []types.OpenAIMessage{
			{
				Role:    "user",
				Content: "Hello, how are you?",
			},
		},
	}

	anthropicReq := ConvertOpenAIToAnthropic(openaiReq)

	assert.NotEmpty(t, anthropicReq.Model, "模型不应为空")
	assert.Equal(t, 1024, anthropicReq.MaxTokens)
	assert.True(t, anthropicReq.Stream)
	assert.Len(t, anthropicReq.Messages, 1)
	assert.Equal(t, "user", anthropicReq.Messages[0].Role)
}

func TestConvertOpenAIToAnthropic_SystemMessage(t *testing.T) {
	maxTokens := 2048

	openaiReq := types.OpenAIRequest{
		Model:     "gpt-4",
		MaxTokens: &maxTokens,
		Messages: []types.OpenAIMessage{
			{
				Role:    "system",
				Content: "You are a helpful assistant.",
			},
			{
				Role:    "user",
				Content: "Hello!",
			},
		},
	}

	anthropicReq := ConvertOpenAIToAnthropic(openaiReq)

	// 当前实现保留system消息在messages中（不提取到System字段）
	assert.Len(t, anthropicReq.Messages, 2)
	assert.Equal(t, "system", anthropicReq.Messages[0].Role)
	assert.Equal(t, "user", anthropicReq.Messages[1].Role)
}

func TestConvertOpenAIToAnthropic_MultipleMessages(t *testing.T) {
	maxTokens := 1024

	openaiReq := types.OpenAIRequest{
		Model:     "gpt-4",
		MaxTokens: &maxTokens,
		Messages: []types.OpenAIMessage{
			{
				Role:    "user",
				Content: "What is 2+2?",
			},
			{
				Role:    "assistant",
				Content: "2+2 equals 4.",
			},
			{
				Role:    "user",
				Content: "What about 3+3?",
			},
		},
	}

	anthropicReq := ConvertOpenAIToAnthropic(openaiReq)

	assert.Len(t, anthropicReq.Messages, 3)
	assert.Equal(t, "user", anthropicReq.Messages[0].Role)
	assert.Equal(t, "assistant", anthropicReq.Messages[1].Role)
	assert.Equal(t, "user", anthropicReq.Messages[2].Role)
}

func TestConvertOpenAIToAnthropic_DefaultMaxTokens(t *testing.T) {
	openaiReq := types.OpenAIRequest{
		Model: "gpt-4",
		Messages: []types.OpenAIMessage{
			{
				Role:    "user",
				Content: "Test",
			},
		},
	}

	anthropicReq := ConvertOpenAIToAnthropic(openaiReq)

	// 应该使用默认值16384
	assert.Equal(t, 16384, anthropicReq.MaxTokens)
}

func TestConvertOpenAIToAnthropic_StreamDefault(t *testing.T) {
	openaiReq := types.OpenAIRequest{
		Model: "gpt-4",
		Messages: []types.OpenAIMessage{
			{
				Role:    "user",
				Content: "Test",
			},
		},
	}

	anthropicReq := ConvertOpenAIToAnthropic(openaiReq)

	// Stream默认应该为false
	assert.False(t, anthropicReq.Stream)
}

func TestConvertAnthropicToOpenAI_BasicResponse(t *testing.T) {
	anthropicResp := map[string]any{
		"id":   "msg_123",
		"type": "message",
		"role": "assistant",
		"content": []any{
			map[string]any{
				"type": "text",
				"text": "Hello! How can I help you?",
			},
		},
		"stop_reason": "end_turn",
		"usage": map[string]any{
			"input_tokens":  10,
			"output_tokens": 20,
		},
	}

	openaiResp := ConvertAnthropicToOpenAI(anthropicResp, "claude-3-sonnet-20240229", "msg_123")

	assert.Equal(t, "msg_123", openaiResp.ID)
	assert.Equal(t, "chat.completion", openaiResp.Object)
	assert.Len(t, openaiResp.Choices, 1)
	assert.Equal(t, "assistant", openaiResp.Choices[0].Message.Role)
	assert.Equal(t, "Hello! How can I help you?", openaiResp.Choices[0].Message.Content)
	assert.Equal(t, "stop", openaiResp.Choices[0].FinishReason)
	assert.Equal(t, 10, openaiResp.Usage.PromptTokens)
	assert.Equal(t, 20, openaiResp.Usage.CompletionTokens)
	assert.Equal(t, 30, openaiResp.Usage.TotalTokens)
}

func TestConvertAnthropicToOpenAI_MultipleContentBlocks(t *testing.T) {
	anthropicResp := map[string]any{
		"id":   "msg_456",
		"type": "message",
		"role": "assistant",
		"content": []any{
			map[string]any{
				"type": "text",
				"text": "First part. ",
			},
			map[string]any{
				"type": "text",
				"text": "Second part.",
			},
		},
		"stop_reason": "end_turn",
	}

	openaiResp := ConvertAnthropicToOpenAI(anthropicResp, "claude-3-sonnet-20240229", "msg_456")

	assert.Len(t, openaiResp.Choices, 1)
	// 多个content block应该被合并
	assert.Contains(t, openaiResp.Choices[0].Message.Content, "First part")
	assert.Contains(t, openaiResp.Choices[0].Message.Content, "Second part")
}

func TestConvertAnthropicToOpenAI_StopReasonMapping(t *testing.T) {
	tests := []struct {
		name                 string
		anthropicStopReason  string
		expectedFinishReason string
	}{
		{"end_turn映射为stop", "end_turn", "stop"},
		{"max_tokens映射为stop", "max_tokens", "stop"},
		{"stop_sequence映射为stop", "stop_sequence", "stop"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			anthropicResp := map[string]any{
				"id":   "msg_test",
				"type": "message",
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type": "text",
						"text": "test",
					},
				},
				"stop_reason": tt.anthropicStopReason,
			}

			openaiResp := ConvertAnthropicToOpenAI(anthropicResp, "claude-3-sonnet-20240229", "msg_test")

			assert.Equal(t, tt.expectedFinishReason, openaiResp.Choices[0].FinishReason)
		})
	}
}

func TestConvertOpenAIToAnthropic_EmptyMessages(t *testing.T) {
	openaiReq := types.OpenAIRequest{
		Model:    "gpt-4",
		Messages: []types.OpenAIMessage{},
	}

	anthropicReq := ConvertOpenAIToAnthropic(openaiReq)

	// 应该返回空消息数组
	assert.Empty(t, anthropicReq.Messages)
}

func TestConvertAnthropicToOpenAI_EmptyContent(t *testing.T) {
	anthropicResp := map[string]any{
		"id":          "msg_empty",
		"type":        "message",
		"role":        "assistant",
		"content":     []any{},
		"stop_reason": "end_turn",
	}

	openaiResp := ConvertAnthropicToOpenAI(anthropicResp, "claude-3-sonnet-20240229", "msg_empty")

	assert.Len(t, openaiResp.Choices, 1)
	assert.Empty(t, openaiResp.Choices[0].Message.Content)
}
