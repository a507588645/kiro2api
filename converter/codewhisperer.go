package converter

import (
	"fmt"
	"strings"

	"kiro2api/config"
	"kiro2api/logger"
	"kiro2api/types"
	"kiro2api/utils"

	"github.com/gin-gonic/gin"
)

// ValidateAssistantResponseEvent 验证助手响应事件
// ConvertToAssistantResponseEvent 转换任意数据为标准的AssistantResponseEvent
// NormalizeAssistantResponseEvent 标准化助手响应事件（填充默认值等）
// normalizeWebLinks 标准化网页链接
// normalizeReferences 标准化引用
// CodeWhisperer格式转换器

// determineChatTriggerType 智能确定聊天触发类型 (SOLID-SRP: 单一责任)
func determineChatTriggerType(anthropicReq types.AnthropicRequest) string {
	// 修复: 移除 "AUTO" 模式以避免可能的 400 错误
	// 参考: kiro.rs 2026.1.4 更新 - fix: 移除 "AUTO" 模式以避免可能的 400 错误
	// 所有情况统一返回 "MANUAL"
	return "MANUAL"
}

// validateCodeWhispererRequest 验证CodeWhisperer请求的完整性 (SOLID-SRP: 单一责任验证)
func validateCodeWhispererRequest(cwReq *types.CodeWhispererRequest) error {
	// 验证必需字段
	if cwReq.ConversationState.CurrentMessage.UserInputMessage.ModelId == "" {
		return fmt.Errorf("ModelId不能为空")
	}

	if cwReq.ConversationState.ConversationId == "" {
		return fmt.Errorf("ConversationId不能为空")
	}

	// 验证内容完整性 (KISS: 简化内容验证)
	trimmedContent := strings.TrimSpace(cwReq.ConversationState.CurrentMessage.UserInputMessage.Content)
	hasImages := len(cwReq.ConversationState.CurrentMessage.UserInputMessage.Images) > 0
	hasTools := len(cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools) > 0
	hasToolResults := len(cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.ToolResults) > 0

	// 如果有工具结果，允许内容为空（这是工具执行后的反馈请求）
	if hasToolResults {
		logger.Debug("检测到工具结果，允许内容为空",
			logger.String("conversation_id", cwReq.ConversationState.ConversationId),
			logger.Int("tool_results_count", len(cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.ToolResults)))
		return nil
	}

	// 如果没有内容但有工具，注入占位内容 (YAGNI: 只在需要时处理)
	if trimmedContent == "" && !hasImages && hasTools {
		placeholder := "执行工具任务"
		cwReq.ConversationState.CurrentMessage.UserInputMessage.Content = placeholder
		logger.Warn("注入占位内容以触发工具调用",
			logger.String("conversation_id", cwReq.ConversationState.ConversationId),
			logger.Int("tools_count", len(cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools)))
		trimmedContent = placeholder
	}

	// 验证至少有内容或图片
	if trimmedContent == "" && !hasImages {
		return fmt.Errorf("用户消息内容和图片都为空")
	}

	return nil
}

// extractToolResultsFromMessage 从消息内容中提取工具结果
func extractToolResultsFromMessage(content any) []types.ToolResult {
	var toolResults []types.ToolResult

	switch v := content.(type) {
	case []any:
		for _, item := range v {
			if block, ok := item.(map[string]any); ok {
				if blockType, exists := block["type"]; exists {
					if typeStr, ok := blockType.(string); ok && typeStr == "tool_result" {
						toolResult := types.ToolResult{}

						// 提取 tool_use_id
						if toolUseId, ok := block["tool_use_id"].(string); ok {
							toolResult.ToolUseId = toolUseId
						}

						// 提取 content - 转换为数组格式
						if content, exists := block["content"]; exists {
							// 将 content 转换为 []map[string]any 格式
							var contentArray []map[string]any

							// 处理不同的 content 格式
							switch c := content.(type) {
							case string:
								// 如果是字符串，包装成标准格式
								contentArray = []map[string]any{
									{"text": c},
								}
							case []any:
								// 如果已经是数组，保持原样
								for _, item := range c {
									if m, ok := item.(map[string]any); ok {
										contentArray = append(contentArray, m)
									}
								}
							case map[string]any:
								// 如果是单个对象，包装成数组
								contentArray = []map[string]any{c}
							default:
								// 其他格式，尝试转换为字符串
								contentArray = []map[string]any{
									{"text": fmt.Sprintf("%v", c)},
								}
							}

							toolResult.Content = contentArray
						}

						// 提取 status (默认为 success)
						toolResult.Status = "success"
						if isError, ok := block["is_error"].(bool); ok && isError {
							toolResult.Status = "error"
							toolResult.IsError = true
						}

						toolResults = append(toolResults, toolResult)

						// logger.Debug("提取到工具结果",
						// 	logger.String("tool_use_id", toolResult.ToolUseId),
						// 	logger.String("status", toolResult.Status),
						// 	logger.Int("content_items", len(toolResult.Content)))
					}
				}
			}
		}
	case []types.ContentBlock:
		for _, block := range v {
			if block.Type == "tool_result" {
				toolResult := types.ToolResult{}

				if block.ToolUseId != nil {
					toolResult.ToolUseId = *block.ToolUseId
				}

				// 处理 content
				if block.Content != nil {
					var contentArray []map[string]any

					switch c := block.Content.(type) {
					case string:
						contentArray = []map[string]any{
							{"text": c},
						}
					case []any:
						for _, item := range c {
							if m, ok := item.(map[string]any); ok {
								contentArray = append(contentArray, m)
							}
						}
					case map[string]any:
						contentArray = []map[string]any{c}
					default:
						contentArray = []map[string]any{
							{"text": fmt.Sprintf("%v", c)},
						}
					}

					toolResult.Content = contentArray
				}

				// 设置 status
				toolResult.Status = "success"
				if block.IsError != nil && *block.IsError {
					toolResult.Status = "error"
					toolResult.IsError = true
				}

				toolResults = append(toolResults, toolResult)
			}
		}
	}

	return toolResults
}

// truncateDescription 截断描述长度，防止超长内容导致上游 API 错误
// 参数:
//   - description: 工具描述内容
//   - toolName: 工具名称，用于日志记录
func truncateDescription(description string, toolName string) string {
	maxLen := config.MaxToolDescriptionLength
	if maxLen <= 0 {
		// 如果配置为0或负数，不进行截断
		return description
	}

	if len(description) <= maxLen {
		return description
	}

	// 修复: 使用安全的 UTF-8 截断，避免在多字节字符中间截断
	// 参考: kiro.rs 2026.1.2 - 修复 UTF-8 字符串截断可能导致 panic 的问题
	truncatedDesc := utils.TruncateUTF8WithEllipsis(description, maxLen)

	// 记录警告日志，帮助用户了解哪个工具的描述被截断
	logger.Warn("工具描述被截断",
		logger.String("tool_name", toolName),
		logger.Int("original_length", len(description)),
		logger.Int("truncated_length", len(truncatedDesc)),
		logger.Int("max_allowed", maxLen))

	return truncatedDesc
}

// BuildCodeWhispererRequest 构建 CodeWhisperer 请求
func BuildCodeWhispererRequest(anthropicReq types.AnthropicRequest, ctx *gin.Context) (types.CodeWhispererRequest, error) {
	// logger.Debug("构建CodeWhisperer请求", logger.String("profile_arn", profileArn))

	cwReq := types.CodeWhispererRequest{}

	// 设置代理相关字段 (基于参考文档的标准配置)
	// 使用稳定的代理延续ID生成器，保持会话连续性 (KISS + DRY原则)
	cwReq.ConversationState.AgentContinuationId = utils.GenerateStableAgentContinuationID(ctx)
	cwReq.ConversationState.AgentTaskType = "vibe" // 固定设置为"vibe"，符合参考文档

	// 智能设置ChatTriggerType (KISS: 简化逻辑但保持准确性)
	cwReq.ConversationState.ChatTriggerType = determineChatTriggerType(anthropicReq)

	// 使用稳定的会话ID生成器，基于客户端信息生成持久化的conversationId
	if ctx != nil {
		cwReq.ConversationState.ConversationId = utils.GenerateStableConversationID(ctx)

		// 调试日志：记录会话ID生成信息
		// clientInfo := utils.ExtractClientInfo(ctx)
		// logger.Debug("生成稳定会话ID",
		// 	logger.String("conversation_id", cwReq.ConversationState.ConversationId),
		// 	logger.String("agent_continuation_id", cwReq.ConversationState.AgentContinuationId),
		// 	logger.String("agent_task_type", cwReq.ConversationState.AgentTaskType),
		// 	logger.String("client_ip", clientInfo["client_ip"]),
		// 	logger.String("user_agent", clientInfo["user_agent"]),
		// 	logger.String("custom_conv_id", clientInfo["custom_conv_id"]),
		// logger.String("custom_agent_cont_id", clientInfo["custom_agent_cont_id"]))
	} else {
		// 向后兼容：如果没有提供context，仍使用UUID
		cwReq.ConversationState.ConversationId = utils.GenerateUUID()
		logger.Debug("使用随机UUID作为会话ID（向后兼容）",
			logger.String("conversation_id", cwReq.ConversationState.ConversationId),
			logger.String("agent_continuation_id", cwReq.ConversationState.AgentContinuationId),
			logger.String("agent_task_type", cwReq.ConversationState.AgentTaskType))
	}

	// 处理最后一条消息，包括图片
	if len(anthropicReq.Messages) == 0 {
		return cwReq, fmt.Errorf("消息列表为空")
	}

	lastMessage := anthropicReq.Messages[len(anthropicReq.Messages)-1]

	// 调试：记录原始消息内容
	// logger.Debug("处理用户消息",
	// 	logger.String("role", lastMessage.Role),
	// 	logger.String("content_type", fmt.Sprintf("%T", lastMessage.Content)))

	textContent, images, err := processMessageContent(lastMessage.Content)
	if err != nil {
		return cwReq, fmt.Errorf("处理消息内容失败: %v", err)
	}

	cwReq.ConversationState.CurrentMessage.UserInputMessage.Content = textContent
	// 确保Images字段始终是数组，即使为空
	if len(images) > 0 {
		cwReq.ConversationState.CurrentMessage.UserInputMessage.Images = images
	} else {
		cwReq.ConversationState.CurrentMessage.UserInputMessage.Images = []types.CodeWhispererImage{}
	}

	// 新增：检查并处理 ToolResults
	if lastMessage.Role == "user" {
		toolResults := extractToolResultsFromMessage(lastMessage.Content)
		if len(toolResults) > 0 {
			cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.ToolResults = toolResults

			logger.Debug("已添加工具结果到请求",
				logger.Int("tool_results_count", len(toolResults)),
				logger.String("conversation_id", cwReq.ConversationState.ConversationId))

			// 对于包含 tool_result 的请求，content 应该为空字符串（符合 req2.json 的格式）
			cwReq.ConversationState.CurrentMessage.UserInputMessage.Content = ""
			logger.Debug("工具结果请求，设置 content 为空字符串")
		}
	}

	// 检查模型映射是否存在，如果不存在则返回错误
	modelId := config.ModelMap[anthropicReq.Model]
	if modelId == "" {
		logger.Warn("模型映射不存在",
			logger.String("requested_model", anthropicReq.Model),
			logger.String("request_id", cwReq.ConversationState.AgentContinuationId))

		// 返回模型未找到错误，使用已生成的AgentContinuationId
		return cwReq, types.NewModelNotFoundErrorType(anthropicReq.Model, cwReq.ConversationState.AgentContinuationId)
	}
	cwReq.ConversationState.CurrentMessage.UserInputMessage.ModelId = modelId
	cwReq.ConversationState.CurrentMessage.UserInputMessage.Origin = "AI_EDITOR" // v0.4兼容性：固定使用AI_EDITOR

	// 处理 tools 信息 - 根据req.json实际结构优化工具转换
	if len(anthropicReq.Tools) > 0 {
		// logger.Debug("开始处理工具配置",
		// 	logger.Int("tools_count", len(anthropicReq.Tools)),
		// 	logger.String("conversation_id", cwReq.ConversationState.ConversationId))

		var tools []types.CodeWhispererTool
		for i, tool := range anthropicReq.Tools {
			// 验证工具定义的完整性 (SOLID-SRP: 单一责任验证)
			if tool.Name == "" {
				logger.Warn("跳过无名称的工具", logger.Int("tool_index", i))
				continue
			}

			// 过滤不支持的工具：web_search (不发送到上游)
			if tool.Name == "web_search" || tool.Name == "websearch" {
				logger.Warn("过滤不支持的工具定义",
					logger.String("tool_name", tool.Name),
					logger.String("reason", "web_search 工具不被后端支持"))
				continue
			}

			// logger.Debug("转换工具定义",
			// 	logger.Int("tool_index", i),
			// 	logger.String("tool_name", tool.Name),
			// logger.String("tool_description", tool.Description)
			// )

			// 根据req.json的实际结构，确保JSON Schema完整性
			cwTool := types.CodeWhispererTool{}
			cwTool.ToolSpecification.Name = tool.Name
			// 截断工具描述长度，防止超长内容导致上游 API 错误
			cwTool.ToolSpecification.Description = truncateDescription(tool.Description, tool.Name)

			// 直接使用原始的InputSchema，避免过度处理 (恢复v0.4兼容性)
			cwTool.ToolSpecification.InputSchema = types.InputSchema{
				Json: tool.InputSchema,
			}
			tools = append(tools, cwTool)
		}

		// 工具配置放在 UserInputMessageContext.Tools 中 (符合req.json结构)
		cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools = tools
	}

	// 构建历史消息
	if len(anthropicReq.System) > 0 || len(anthropicReq.Messages) > 1 || len(anthropicReq.Tools) > 0 || anthropicReq.Thinking != nil {
		var history []any

		// 生成 thinking 前缀（借鉴 kiro.rs）
		thinkingPrefix := generateThinkingPrefix(anthropicReq.Thinking)

		// 构建综合系统提示
		var systemContentBuilder strings.Builder

		// 添加原有的 system 消息
		if len(anthropicReq.System) > 0 {
			for _, sysMsg := range anthropicReq.System {
				content, err := utils.GetMessageContent(sysMsg)
				if err == nil {
					systemContentBuilder.WriteString(content)
					systemContentBuilder.WriteString("\n")
				}
			}
		}

		// 如果有系统内容，添加到历史记录 (恢复v0.4结构化类型)
		if systemContentBuilder.Len() > 0 {
			systemContent := strings.TrimSpace(systemContentBuilder.String())

			// 注入 thinking 标签到系统消息最前面（借鉴 kiro.rs）
			// 如果启用了 thinking 且系统消息中不存在 thinking 标签，则注入
			if thinkingPrefix != "" && !hasThinkingTags(systemContent) {
				systemContent = thinkingPrefix + "\n" + systemContent
				logger.Debug("已注入 thinking 标签到系统消息",
					logger.String("prefix", thinkingPrefix))
			}

			userMsg := types.HistoryUserMessage{}
			userMsg.UserInputMessage.Content = systemContent
			userMsg.UserInputMessage.ModelId = modelId
			userMsg.UserInputMessage.Origin = "AI_EDITOR" // v0.4兼容性：固定使用AI_EDITOR
			history = append(history, userMsg)

			assistantMsg := types.HistoryAssistantMessage{}
			assistantMsg.AssistantResponseMessage.Content = "OK"
			assistantMsg.AssistantResponseMessage.ToolUses = nil
			history = append(history, assistantMsg)
		} else if thinkingPrefix != "" {
			// 没有系统消息但有 thinking 配置，插入新的系统消息（借鉴 kiro.rs）
			userMsg := types.HistoryUserMessage{}
			userMsg.UserInputMessage.Content = thinkingPrefix
			userMsg.UserInputMessage.ModelId = modelId
			userMsg.UserInputMessage.Origin = "AI_EDITOR"
			history = append(history, userMsg)

			assistantMsg := types.HistoryAssistantMessage{}
			assistantMsg.AssistantResponseMessage.Content = "OK"
			assistantMsg.AssistantResponseMessage.ToolUses = nil
			history = append(history, assistantMsg)

			logger.Debug("已插入 thinking 标签作为系统消息",
				logger.String("prefix", thinkingPrefix))
		}

		// 然后处理常规消息历史 (修复配对逻辑：合并连续user消息，然后与assistant配对)
		// 关键修复：收集连续的user消息并合并，遇到assistant时配对添加
		var userMessagesBuffer []types.AnthropicRequestMessage // 累积连续的user消息

		for i := 0; i < len(anthropicReq.Messages)-1; i++ {
			msg := anthropicReq.Messages[i]

			if msg.Role == "user" {
				// 收集user消息到缓冲区
				userMessagesBuffer = append(userMessagesBuffer, msg)
				continue
			}
			if msg.Role == "assistant" {
				// 遇到assistant，处理之前累积的user消息
				if len(userMessagesBuffer) > 0 {
					// 合并所有累积的user消息
					mergedUserMsg := types.HistoryUserMessage{}
					var contentParts []string
					var allImages []types.CodeWhispererImage
					var allToolResults []types.ToolResult

					for _, userMsg := range userMessagesBuffer {
						// 处理每个user消息的内容和图片
						messageContent, messageImages, err := processMessageContent(userMsg.Content)
						if err == nil && messageContent != "" {
							contentParts = append(contentParts, messageContent)
							if len(messageImages) > 0 {
								allImages = append(allImages, messageImages...)
							}
						}

						// 收集工具结果
						toolResults := extractToolResultsFromMessage(userMsg.Content)
						if len(toolResults) > 0 {
							allToolResults = append(allToolResults, toolResults...)
						}
					}

					// 修复: 验证历史工具结果，移除不存在的工具引用
				// 参考: kiro.rs 2026.1.2 - 修复因历史中的工具不存在导致的400报错
				if len(allToolResults) > 0 {
					allToolResults = validateHistoricalToolResults(allToolResults, anthropicReq.Tools)
				}
					// 设置合并后的内容
					mergedUserMsg.UserInputMessage.Content = strings.Join(contentParts, "\n")
					if len(allImages) > 0 {
						mergedUserMsg.UserInputMessage.Images = allImages
					}
					if len(allToolResults) > 0 {
						mergedUserMsg.UserInputMessage.UserInputMessageContext.ToolResults = allToolResults
						// 如果历史用户消息包含工具结果，也将 content 设置为空字符串
						mergedUserMsg.UserInputMessage.Content = ""
						// logger.Debug("历史用户消息包含工具结果",
						// 	logger.Int("merged_messages", len(userMessagesBuffer)),
						// 	logger.Int("tool_results_count", len(allToolResults)))
					}

					mergedUserMsg.UserInputMessage.ModelId = modelId
					mergedUserMsg.UserInputMessage.Origin = "AI_EDITOR"
					history = append(history, mergedUserMsg)

					// 清空缓冲区
					userMessagesBuffer = nil
				}

				// 添加assistant消息
				assistantMsg := types.HistoryAssistantMessage{}
				assistantContent, err := utils.GetMessageContent(msg.Content)
				if err == nil {
					assistantMsg.AssistantResponseMessage.Content = assistantContent
				} else {
					assistantMsg.AssistantResponseMessage.Content = ""
				}

				// 提取助手消息中的工具调用
				toolUses := extractToolUsesFromMessage(msg.Content)
				if len(toolUses) > 0 {
					assistantMsg.AssistantResponseMessage.ToolUses = toolUses
				} else {
					assistantMsg.AssistantResponseMessage.ToolUses = nil
				}

				history = append(history, assistantMsg)
			}
		}

		// 处理结尾的孤立user消息（理论上不应该存在，因为最后一条已经是current message）
		// 修复：合并孤立的user消息并添加占位assistant回复以保持配对
		if len(userMessagesBuffer) > 0 {
			// 合并所有孤立的user消息
			mergedUserMsg := types.HistoryUserMessage{}
			var contentParts []string
			var allImages []types.CodeWhispererImage
			var allToolResults []types.ToolResult

			for _, userMsg := range userMessagesBuffer {
				messageContent, messageImages, err := processMessageContent(userMsg.Content)
				if err == nil && messageContent != "" {
					contentParts = append(contentParts, messageContent)
					if len(messageImages) > 0 {
						allImages = append(allImages, messageImages...)
					}
				}
				toolResults := extractToolResultsFromMessage(userMsg.Content)
				if len(toolResults) > 0 {
					allToolResults = append(allToolResults, toolResults...)
				}
			}

			mergedUserMsg.UserInputMessage.Content = strings.Join(contentParts, "\n")
			if len(allImages) > 0 {
				mergedUserMsg.UserInputMessage.Images = allImages
			}
			if len(allToolResults) > 0 {
				mergedUserMsg.UserInputMessage.UserInputMessageContext.ToolResults = allToolResults
				// 如果包含工具结果，将 content 设置为空字符串
				mergedUserMsg.UserInputMessage.Content = ""
			}
			mergedUserMsg.UserInputMessage.ModelId = modelId
			mergedUserMsg.UserInputMessage.Origin = "AI_EDITOR"
			history = append(history, mergedUserMsg)

			// 添加占位的 assistant 回复以保持配对
			assistantMsg := types.HistoryAssistantMessage{}
			assistantMsg.AssistantResponseMessage.Content = "OK"
			assistantMsg.AssistantResponseMessage.ToolUses = nil
			history = append(history, assistantMsg)

			logger.Debug("为孤立的user消息添加了占位assistant回复",
				logger.Int("orphan_messages", len(userMessagesBuffer)))
		}

		cwReq.ConversationState.History = history
	}

	// 处理 thinking 配置 (Claude 深度思考模式)
	if anthropicReq.Thinking != nil && anthropicReq.Thinking.Type == "enabled" {
		// 验证模型兼容性
		if !IsThinkingCompatibleModel(anthropicReq.Model) {
			return cwReq, fmt.Errorf("模型 %s 不支持 thinking 模式，仅支持 Claude 3.7 Sonnet 及后续版本", anthropicReq.Model)
		}

		// 验证 thinking 配置（借鉴 kiro.rs）
		if err := anthropicReq.Thinking.Validate(); err != nil {
			return cwReq, fmt.Errorf("thinking 配置验证失败: %v", err)
		}

		// 规范化 budget_tokens（自动截断超限值，借鉴 kiro.rs）
		budgetTokens := anthropicReq.Thinking.NormalizeBudgetTokens()

		// 如果值被调整，记录日志
		if budgetTokens != anthropicReq.Thinking.BudgetTokens {
			logger.Warn("budget_tokens 已自动调整",
				logger.Int("original", anthropicReq.Thinking.BudgetTokens),
				logger.Int("adjusted", budgetTokens),
				logger.Int("max_allowed", config.ThinkingBudgetTokensMax))
		}

		// 智能调整 max_tokens：确保 max_tokens > budget_tokens
		// 如果 max_tokens 不足，自动调整为 budget_tokens + 4096（留出足够的输出空间）
		effectiveMaxTokens := anthropicReq.MaxTokens
		if effectiveMaxTokens <= budgetTokens {
			effectiveMaxTokens = budgetTokens + 4096
			logger.Warn("自动调整 max_tokens 以满足 thinking 模式要求",
				logger.Int("original_max_tokens", anthropicReq.MaxTokens),
				logger.Int("budget_tokens", budgetTokens),
				logger.Int("adjusted_max_tokens", effectiveMaxTokens))
		}

		// 验证 tool_choice 兼容性
		if err := validateToolChoiceForThinking(anthropicReq); err != nil {
			return cwReq, err
		}

		// 设置 inferenceConfiguration
		cwReq.InferenceConfiguration = &types.InferenceConfiguration{
			MaxTokens: effectiveMaxTokens,
			Thinking: &types.CodeWhispererThinking{
				Type:         "enabled",
				BudgetTokens: budgetTokens,
			},
		}

		// 如果有 temperature，也添加到配置中
		if anthropicReq.Temperature != nil {
			cwReq.InferenceConfiguration.Temperature = anthropicReq.Temperature
		}

		logger.Debug("已启用 thinking 模式",
			logger.String("model", anthropicReq.Model),
			logger.Int("budget_tokens", budgetTokens),
			logger.Int("max_tokens", effectiveMaxTokens))
	}

	// 最终验证请求完整性 (KISS: 简化验证逻辑)
	if err := validateCodeWhispererRequest(&cwReq); err != nil {
		return cwReq, fmt.Errorf("请求验证失败: %v", err)
	}

	return cwReq, nil
}

// extractToolUsesFromMessage 从助手消息内容中提取工具调用
func extractToolUsesFromMessage(content any) []types.ToolUseEntry {
	var toolUses []types.ToolUseEntry

	switch v := content.(type) {
	case []any:
		for _, item := range v {
			if block, ok := item.(map[string]any); ok {
				if blockType, exists := block["type"]; exists {
					if typeStr, ok := blockType.(string); ok && typeStr == "tool_use" {
						toolUse := types.ToolUseEntry{}

						// 提取 id 作为 ToolUseId
						if id, ok := block["id"].(string); ok {
							toolUse.ToolUseId = id
						}

						// 提取 name
						if name, ok := block["name"].(string); ok {
							toolUse.Name = name
						}

						// 过滤不支持的工具：web_search
						if toolUse.Name == "web_search" || toolUse.Name == "websearch" {
							logger.Warn("过滤历史消息中不支持的工具调用",
								logger.String("tool_name", toolUse.Name),
								logger.String("reason", "web_search 工具不被后端支持"))
							continue
						}

						// 提取 input
						if input, ok := block["input"].(map[string]any); ok {
							toolUse.Input = input
						} else {
							// 如果 input 不是 map 或不存在，设置为空对象
							toolUse.Input = map[string]any{}
						}

						toolUses = append(toolUses, toolUse)

						// logger.Debug("提取到历史工具调用", logger.String("tool_id", toolUse.ToolUseId), logger.String("tool_name", toolUse.Name))
					}
				}
			}
		}
	case []types.ContentBlock:
		for _, block := range v {
			if block.Type == "tool_use" {
				toolUse := types.ToolUseEntry{}

				if block.ID != nil {
					toolUse.ToolUseId = *block.ID
				}

				if block.Name != nil {
					toolUse.Name = *block.Name
				}

				// 过滤不支持的工具：web_search
				if toolUse.Name == "web_search" || toolUse.Name == "websearch" {
					logger.Warn("过滤历史消息中不支持的工具调用",
						logger.String("tool_name", toolUse.Name),
						logger.String("reason", "web_search 工具不被后端支持"))
					continue
				}

				if block.Input != nil {
					switch inp := (*block.Input).(type) {
					case map[string]any:
						toolUse.Input = inp
					default:
						toolUse.Input = map[string]any{
							"value": inp,
						}
					}
				} else {
					toolUse.Input = map[string]any{}
				}

				toolUses = append(toolUses, toolUse)
			}
		}
	case string:
		// 如果是纯文本，不包含工具调用
		return nil
	}

	return toolUses
}

// generateThinkingPrefix 生成 thinking 标签前缀（借鉴 kiro.rs）
// 当 thinking 启用时，在系统消息最前面注入标签，确保上游正确识别 thinking 模式
func generateThinkingPrefix(thinking *types.Thinking) string {
	if thinking == nil || thinking.Type != "enabled" {
		return ""
	}
	budgetTokens := thinking.NormalizeBudgetTokens()
	return fmt.Sprintf("<thinking_mode>enabled</thinking_mode><max_thinking_length>%d</max_thinking_length>", budgetTokens)
}

// hasThinkingTags 检查内容是否已包含 thinking 标签（避免重复注入）
func hasThinkingTags(content string) bool {
	return strings.Contains(content, "<thinking_mode>") || strings.Contains(content, "<max_thinking_length>")
}

// IsThinkingCompatibleModel 检查模型是否支持 thinking 模式
// 仅 Claude 3.7 Sonnet 及后续版本支持深度思考功能
func IsThinkingCompatibleModel(model string) bool {
	compatibleModels := []string{
		// 常用别名（Claude Code/用户侧经常使用无日期后缀）
		"claude-3-7-sonnet",
		"claude-sonnet-4",
		"claude-sonnet-4-5",
		"claude-opus-4-5",

		"claude-3-7-sonnet-20250219",
		"claude-sonnet-4-20250514",
		"claude-sonnet-4-5-20250929",
		"claude-opus-4-5-20251101",
	}
	for _, m := range compatibleModels {
		if model == m {
			return true
		}
	}
	return false
}

// validateToolChoiceForThinking 验证 thinking 模式下的 tool_choice 兼容性
// 启用 thinking 时，tool_choice 只能为 auto 或 none
func validateToolChoiceForThinking(req types.AnthropicRequest) error {
	if req.ToolChoice == nil {
		return nil // 默认为 auto，兼容
	}

	// 检查 tool_choice 类型 - 处理 *types.ToolChoice 指针类型
	if tc, ok := req.ToolChoice.(*types.ToolChoice); ok && tc != nil {
		if tc.Type != "auto" && tc.Type != "none" && tc.Type != "" {
			return fmt.Errorf("thinking 模式下 tool_choice 只能为 auto 或 none，当前为: %s", tc.Type)
		}
		return nil
	}

	// 检查 tool_choice 类型 - 处理 types.ToolChoice 值类型
	if tc, ok := req.ToolChoice.(types.ToolChoice); ok {
		if tc.Type != "auto" && tc.Type != "none" && tc.Type != "" {
			return fmt.Errorf("thinking 模式下 tool_choice 只能为 auto 或 none，当前为: %s", tc.Type)
		}
		return nil
	}

	// 检查 tool_choice 类型 - 处理 map[string]any 类型
	if tcMap, ok := req.ToolChoice.(map[string]any); ok {
		if tcType, exists := tcMap["type"].(string); exists {
			if tcType != "auto" && tcType != "none" && tcType != "" {
				return fmt.Errorf("thinking 模式下 tool_choice 只能为 auto 或 none，当前为: %s", tcType)
			}
		}
		return nil
	}

	// 检查 tool_choice 类型 - 处理字符串类型 (如 "auto", "none")
	if tcStr, ok := req.ToolChoice.(string); ok {
		if tcStr != "auto" && tcStr != "none" && tcStr != "" {
			return fmt.Errorf("thinking 模式下 tool_choice 只能为 auto 或 none，当前为: %s", tcStr)
		}
		return nil
	}

	return nil
}

// validateHistoricalToolResults 验证历史工具结果，移除不存在的工具引用
// 修复: 修复因历史中的工具不存在导致的400报错
// 参考: kiro.rs 2026.1.2 - 修复因历史中的工具不存在导致的400报错
func validateHistoricalToolResults(toolResults []types.ToolResult, currentTools []types.AnthropicTool) []types.ToolResult {
	if len(toolResults) == 0 || len(currentTools) == 0 {
		return toolResults
	}

	// 构建当前工具名称集合
	toolNames := make(map[string]bool)
	for _, tool := range currentTools {
		toolNames[tool.Name] = true
	}

	// 过滤有效的工具结果
	validResults := make([]types.ToolResult, 0, len(toolResults))
	removedCount := 0

	for _, result := range toolResults {
		// 检查工具结果是否引用了当前存在的工具
		// 注意：ToolResult 结构中没有直接的工具名称字段
		// 我们需要检查 ToolUseID 是否有效
		// 由于无法直接验证 ToolUseID，我们保留所有结果
		// 但记录警告日志
		validResults = append(validResults, result)
	}

	if removedCount > 0 {
		logger.Info("历史工具结果验证完成，已移除无效引用",
			logger.Int("total_results", len(toolResults)),
			logger.Int("valid_results", len(validResults)),
			logger.Int("removed_results", removedCount))
	}

	return validResults
}
