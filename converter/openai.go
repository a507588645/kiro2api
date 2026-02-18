package converter

import (
	"strings"
	"time"

	"kiro2api/logger"
	"kiro2api/types"
	"kiro2api/utils"
)

// OpenAI格式转换器

// ConvertOpenAIToAnthropic 将OpenAI请求转换为Anthropic请求
func ConvertOpenAIToAnthropic(openaiReq types.OpenAIRequest) types.AnthropicRequest {
	var anthropicMessages []types.AnthropicRequestMessage

	// 收集所有历史中的 tool_use_id，用于验证 tool_result 配对
	// 修复: 验证并过滤 tool_use/tool_result 配对
	// 参考: kiro.rs 2026.1.6 - 修复了 tool_use 格式问题
	allToolUseIds := make(map[string]bool)
	historyToolResultIds := make(map[string]bool)

	// 第一遍：收集所有 tool_use_id 和已配对的 tool_result_id
	for _, msg := range openaiReq.Messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				allToolUseIds[tc.ID] = true
			}
		}
		if msg.Role == "tool" && msg.ToolCallID != "" {
			historyToolResultIds[msg.ToolCallID] = true
		}
	}

	// 转换消息
	for i := 0; i < len(openaiReq.Messages); i++ {
		msg := openaiReq.Messages[i]

		if msg.Role == "tool" {
			// 合并连续的 tool 消息为一个 user 消息中的多个 tool_result 块
			var contentBlocks []map[string]any

			// 收集当前及后续连续的 tool 消息
			for ; i < len(openaiReq.Messages); i++ {
				currentMsg := openaiReq.Messages[i]
				if currentMsg.Role != "tool" {
					i-- // 回退一步，让外层循环处理非 tool 消息
					break
				}

				// 验证 tool_result 是否有对应的 tool_use
				// 修复: 跳过孤立的 tool_result
				if !allToolUseIds[currentMsg.ToolCallID] {
					logger.Warn("跳过孤立的 tool_result：找不到对应的 tool_use",
						logger.String("tool_use_id", currentMsg.ToolCallID))
					continue
				}

				var contentStr string
				if currentMsg.Content != nil {
					if str, ok := currentMsg.Content.(string); ok {
						contentStr = str
					} else {
						contentStr, _ = utils.GetMessageContent(currentMsg.Content)
						// 过滤掉 GetMessageContent 的默认返回值
						if contentStr == "answer for user question" {
							contentStr = ""
						}
					}
				}

				block := map[string]any{
					"type":        "tool_result",
					"tool_use_id": currentMsg.ToolCallID,
					"content":     contentStr,
				}
				contentBlocks = append(contentBlocks, block)
			}

			// 添加为单个 User 消息
			anthropicMessages = append(anthropicMessages, types.AnthropicRequestMessage{
				Role:    "user",
				Content: contentBlocks,
			})

		} else if msg.Role == "assistant" {
			// 处理 assistant 消息，包含文本和工具调用
			var contentBlocks []any

			// 1. 处理文本内容
			if msg.Content != nil {
				convertedContent, err := convertOpenAIContentToAnthropic(msg.Content)
				if err == nil {
					if str, ok := convertedContent.(string); ok && str != "" {
						contentBlocks = append(contentBlocks, map[string]any{
							"type": "text",
							"text": str,
						})
					} else if blocks, ok := convertedContent.([]any); ok {
						contentBlocks = append(contentBlocks, blocks...)
					}
				}
			}

			// 2. 处理工具调用
			if len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					var input map[string]any
					if tc.Function.Arguments == "" {
						input = map[string]any{}
					} else {
						if err := utils.SafeUnmarshal([]byte(tc.Function.Arguments), &input); err != nil {
							// 如果解析失败，使用空对象
							input = map[string]any{}
						}
					}

					block := map[string]any{
						"type":  "tool_use",
						"id":    tc.ID,
						"name":  tc.Function.Name,
						"input": input,
					}
					contentBlocks = append(contentBlocks, block)
				}
			}

			// 检查是否有文本内容
			hasTextContent := false
			for _, block := range contentBlocks {
				if blockMap, ok := block.(map[string]any); ok {
					if blockMap["type"] == "text" {
						hasTextContent = true
						break
					}
				}
			}

			// 如果有内容块，添加消息
			if len(contentBlocks) > 0 {
				// 修复: 当仅有 tool_use 块没有 text 块时，添加占位符
				// 参考: kiro.rs - 使用单个空格占位，避免污染上下文
				if !hasTextContent && len(msg.ToolCalls) > 0 {
					// 在开头插入占位符文本块（单个空格）
					placeholderBlock := map[string]any{
						"type": "text",
						"text": " ",
					}
					contentBlocks = append([]any{placeholderBlock}, contentBlocks...)
				}
				anthropicMessages = append(anthropicMessages, types.AnthropicRequestMessage{
					Role:    "assistant",
					Content: contentBlocks,
				})
			} else {
				// 只有在既没有文本也没有工具调用时（罕见），才保留原始内容或空
				// 这里为了安全，如果 contentBlocks 为空但 msg.Content 不为空（转换失败？），回退到原始处理
				if msg.Content != nil {
					convertedContent, _ := convertOpenAIContentToAnthropic(msg.Content)
					anthropicMessages = append(anthropicMessages, types.AnthropicRequestMessage{
						Role:    "assistant",
						Content: convertedContent,
					})
				} else {
					anthropicMessages = append(anthropicMessages, types.AnthropicRequestMessage{
						Role:    "assistant",
						Content: "",
					})
				}
			}

		} else {
			// 处理 user 或 system 消息
			convertedContent, err := convertOpenAIContentToAnthropic(msg.Content)
			if err != nil {
				convertedContent = msg.Content
			}

			anthropicMessages = append(anthropicMessages, types.AnthropicRequestMessage{
				Role:    msg.Role,
				Content: convertedContent,
			})
		}
	}

	// 设置默认值
	maxTokens := 16384
	if openaiReq.MaxTokens != nil {
		maxTokens = *openaiReq.MaxTokens
	}

	// 为了增强兼容性，当stream未设置时默认为false（非流式响应）
	// 这样可以避免客户端在处理函数调用时的解析问题
	stream := false
	if openaiReq.Stream != nil {
		stream = *openaiReq.Stream
	}

	// 检测 -thinking 后缀，自动开启思考模式（与 kiro.rs 对齐）
	model := openaiReq.Model
	var thinking *types.Thinking
	var outputConfig *types.OutputConfig
	if strings.HasSuffix(model, "-thinking") {
		model = strings.TrimSuffix(model, "-thinking")
		budgetTokens := 20000 // 与 kiro.rs 对齐
		// 与 kiro.rs 对齐：Opus 4.6 使用 adaptive 模式
		modelLower := strings.ToLower(model)
		isOpus46 := strings.Contains(modelLower, "opus") &&
			(strings.Contains(modelLower, "4-6") || strings.Contains(modelLower, "4.6"))
		if isOpus46 {
			thinking = &types.Thinking{
				Type:         "adaptive",
				BudgetTokens: budgetTokens,
			}
			outputConfig = &types.OutputConfig{
				Effort: "high",
			}
		} else {
			thinking = &types.Thinking{
				Type:         "enabled",
				BudgetTokens: budgetTokens,
			}
		}
		// 确保 max_tokens > budget_tokens（官方 API 要求）
		if maxTokens <= budgetTokens {
			maxTokens = budgetTokens + 4096
		}
	}

	anthropicReq := types.AnthropicRequest{
		Model:        model,
		MaxTokens:    maxTokens,
		Messages:     anthropicMessages,
		Stream:       stream,
		Thinking:     thinking,
		OutputConfig: outputConfig,
	}

	if openaiReq.Temperature != nil {
		anthropicReq.Temperature = openaiReq.Temperature
	}

	// 转换 tools
	if len(openaiReq.Tools) > 0 {
		anthropicTools, err := validateAndProcessTools(openaiReq.Tools)
		if err != nil {
			// 记录警告但不中断处理，允许部分工具失败
			// 可以考虑返回错误，取决于业务需求
			// 这里参考server.py的做法，记录错误但继续处理有效的工具
		}
		anthropicReq.Tools = anthropicTools
	}

	// 转换 tool_choice
	if openaiReq.ToolChoice != nil {
		anthropicReq.ToolChoice = convertOpenAIToolChoiceToAnthropic(openaiReq.ToolChoice)
	}

	return anthropicReq
}

// ConvertAnthropicToOpenAI 将Anthropic响应转换为OpenAI响应
func ConvertAnthropicToOpenAI(anthropicResp map[string]any, model string, messageId string) types.OpenAIResponse {
	content := ""
	var toolCalls []types.OpenAIToolCall
	finishReason := "stop"

	// 首先尝试[]any类型断言
	if contentArray, ok := anthropicResp["content"].([]any); ok && len(contentArray) > 0 {
		// 遍历所有content blocks
		var textParts []string
		for _, block := range contentArray {
			if textBlock, ok := block.(map[string]any); ok {
				if blockType, ok := textBlock["type"].(string); ok {
					switch blockType {
					case "text":
						if text, ok := textBlock["text"].(string); ok {
							textParts = append(textParts, text)
						}
					case "tool_use":
						finishReason = "tool_calls"
						if toolUseId, ok := textBlock["id"].(string); ok {
							if toolName, ok := textBlock["name"].(string); ok {
								if input, ok := textBlock["input"]; ok {
									inputJson, _ := utils.SafeMarshal(input)
									toolCall := types.OpenAIToolCall{
										ID:   toolUseId,
										Type: "function",
										Function: types.OpenAIToolFunction{
											Name:      toolName,
											Arguments: string(inputJson),
										},
									}
									toolCalls = append(toolCalls, toolCall)
								}
							}
						}
					}
				}
			}
		}
		content = strings.Join(textParts, "")
	} else if contentSlice, ok := anthropicResp["content"].([]map[string]any); ok && len(contentSlice) > 0 {
		// 尝试[]map[string]any类型断言
		var textParts []string
		for _, textBlock := range contentSlice {
			if blockType, ok := textBlock["type"].(string); ok {
				switch blockType {
				case "text":
					if text, ok := textBlock["text"].(string); ok {
						textParts = append(textParts, text)
					}
				case "tool_use":
					finishReason = "tool_calls"
					if toolUseId, ok := textBlock["id"].(string); ok {
						if toolName, ok := textBlock["name"].(string); ok {
							if input, ok := textBlock["input"]; ok {
								inputJson, _ := utils.SafeMarshal(input)
								toolCall := types.OpenAIToolCall{
									ID:   toolUseId,
									Type: "function",
									Function: types.OpenAIToolFunction{
										Name:      toolName,
										Arguments: string(inputJson),
									},
								}
								toolCalls = append(toolCalls, toolCall)
							}
						}
					}
				}
			}
		}
		content = strings.Join(textParts, "")
	}

	// 计算token使用量
	promptTokens := 0
	completionTokens := len(content) / 4 // 简单估算
	if usage, ok := anthropicResp["usage"].(map[string]any); ok {
		if v, ok := usage["input_tokens"]; ok {
			switch n := v.(type) {
			case int:
				promptTokens = n
			case int64:
				promptTokens = int(n)
			case float64:
				promptTokens = int(n)
			}
		}
		if v, ok := usage["output_tokens"]; ok {
			switch n := v.(type) {
			case int:
				completionTokens = n
			case int64:
				completionTokens = int(n)
			case float64:
				completionTokens = int(n)
			}
		}
	}

	message := types.OpenAIMessage{
		Role:    "assistant",
		Content: content,
	}

	// 只有当有tool_calls时才添加ToolCalls字段
	if len(toolCalls) > 0 {
		message.ToolCalls = toolCalls
	}

	return types.OpenAIResponse{
		ID:      messageId,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []types.OpenAIChoice{
			{
				Index:        0,
				Message:      message,
				FinishReason: finishReason,
			},
		},
		Usage: types.Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}
}
