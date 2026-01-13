package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"kiro2api/auth"
	"kiro2api/config"
	"kiro2api/logger"
	"kiro2api/parser"
	"kiro2api/types"
	"kiro2api/utils"

	"github.com/gin-gonic/gin"
)

// extractRelevantHeaders 提取相关的请求头信息
func extractRelevantHeaders(c *gin.Context) map[string]string {
	relevantHeaders := map[string]string{}

	// 提取关键的请求头
	headerKeys := []string{
		"Content-Type",
		"Authorization",
		"X-API-Key",
		"X-Request-ID",
		"X-Forwarded-For",
		"Accept",
		"Accept-Encoding",
	}

	for _, key := range headerKeys {
		if value := c.GetHeader(key); value != "" {
			// 对敏感信息进行脱敏处理
			if key == "Authorization" && len(value) > 20 {
				relevantHeaders[key] = value[:10] + "***" + value[len(value)-7:]
			} else if key == "X-API-Key" && len(value) > 10 {
				relevantHeaders[key] = value[:5] + "***" + value[len(value)-3:]
			} else {
				relevantHeaders[key] = value
			}
		}
	}

	return relevantHeaders
}

// handleStreamRequest 处理流式请求
func handleStreamRequest(c *gin.Context, anthropicReq types.AnthropicRequest, token types.TokenInfo) {
	// 记录请求接收日志 - 详细记录请求参数
	logRequestReceived(c, anthropicReq, true)

	// 转换为TokenWithUsage（简化版本）
	tokenWithUsage := &types.TokenWithUsage{
		TokenInfo:      token,
		AvailableCount: 100, // 默认可用次数
		LastUsageCheck: time.Now(),
	}
	sender := &AnthropicStreamSender{}
	handleGenericStreamRequest(c, anthropicReq, tokenWithUsage, sender, createAnthropicStreamEvents)
}

// handleStreamRequestWithRetry 带429重试的流式请求处理
func handleStreamRequestWithRetry(c *gin.Context, anthropicReq types.AnthropicRequest) {
	// 记录请求接收日志 - 详细记录请求参数
	logRequestReceived(c, anthropicReq, true)

	// 使用带重试的请求执行
	resp, err := executeCodeWhispererRequestWithRetry(c, anthropicReq, true)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	// 获取当前token信息用于后续处理
	tokenWithUsage := &types.TokenWithUsage{
		AvailableCount: 100,
		LastUsageCheck: time.Now(),
	}

	// 初始化SSE响应
	sender := &AnthropicStreamSender{}
	if err := initializeSSEResponse(c); err != nil {
		_ = sender.SendError(c, "连接不支持SSE刷新", err)
		return
	}

	// 生成消息ID
	messageID := fmt.Sprintf(config.MessageIDFormat, time.Now().Format(config.MessageIDTimeFormat))
	c.Set("message_id", messageID)

	// 使用统一的 TokenCalculator 计算输入 tokens
	tokenCalculator := GetTokenCalculator()
	inputTokens := tokenCalculator.CalculateInputTokens(c.Request.Context(), anthropicReq)

	// 创建流处理上下文
	ctx := NewStreamProcessorContext(c, anthropicReq, tokenWithUsage, sender, messageID, inputTokens)
	defer ctx.Cleanup()

	// 发送初始事件
	if err := ctx.sendInitialEvents(createAnthropicStreamEvents); err != nil {
		return
	}

	// 处理事件流
	processor := NewEventStreamProcessor(ctx)
	if err := processor.ProcessEventStream(resp.Body); err != nil {
		logger.Error("事件流处理失败", logger.Err(err))
		return
	}

	// 发送结束事件
	if err := ctx.sendFinalEvents(); err != nil {
		logger.Error("发送结束事件失败", logger.Err(err))
		return
	}
}

// handleGenericStreamRequest 通用流式请求处理
func handleGenericStreamRequest(c *gin.Context, anthropicReq types.AnthropicRequest, token *types.TokenWithUsage, sender StreamEventSender, eventCreator func(string, int, string) []map[string]any) {
	// 使用统一的 TokenCalculator 计算输入 tokens
	tokenCalculator := GetTokenCalculator()
	inputTokens := tokenCalculator.CalculateInputTokens(c.Request.Context(), anthropicReq)

	// 初始化SSE响应
	if err := initializeSSEResponse(c); err != nil {
		_ = sender.SendError(c, "连接不支持SSE刷新", err)
		return
	}

	// 生成消息ID并注入上下文
	messageID := fmt.Sprintf(config.MessageIDFormat, time.Now().Format(config.MessageIDTimeFormat))
	c.Set("message_id", messageID)

	// 执行CodeWhisperer请求
	resp, err := execCWRequest(c, anthropicReq, token.TokenInfo, true)
	if err != nil {
		var modelNotFoundErrorType *types.ModelNotFoundErrorType
		if errors.As(err, &modelNotFoundErrorType) {
			return
		}
		_ = sender.SendError(c, "构建请求失败", err)
		return
	}
	defer resp.Body.Close()

	// 创建流处理上下文
	ctx := NewStreamProcessorContext(c, anthropicReq, token, sender, messageID, inputTokens)
	defer ctx.Cleanup()

	// 发送初始事件
	if err := ctx.sendInitialEvents(eventCreator); err != nil {
		return
	}

	// 处理事件流
	processor := NewEventStreamProcessor(ctx)
	if err := processor.ProcessEventStream(resp.Body); err != nil {
		logger.Error("事件流处理失败", logger.Err(err))
		return
	}

	// 发送结束事件
	if err := ctx.sendFinalEvents(); err != nil {
		logger.Error("发送结束事件失败", logger.Err(err))
		return
	}
}

// createAnthropicStreamEvents 创建Anthropic流式初始事件
func createAnthropicStreamEvents(messageId string, inputTokens int, model string) []map[string]any {
	// 创建基础初始事件序列，不包含content_block_start
	//
	// 关键修复：移除预先发送的空文本块
	// 问题：如果预先发送content_block_start(text)，但上游只返回tool_use没有文本，
	//      会导致空文本块（start -> stop 之间没有delta），违反Claude API规范
	//
	// 解决方案：依赖sse_state_manager.handleContentBlockDelta()中的自动启动机制
	//          只有在实际收到内容（文本或工具）时才动态生成content_block_start
	//          这确保每个content_block都有实际内容
	events := []map[string]any{
		{
			"type": "message_start",
			"message": map[string]any{
				"id":            messageId,
				"type":          "message",
				"role":          "assistant",
				"content":       []any{},
				"model":         model,
				"stop_reason":   nil,
				"stop_sequence": nil,
				"usage": map[string]any{
					"input_tokens":  inputTokens,
					"output_tokens": 0, // 初始输出tokens为0，最终在message_delta中更新
				},
			},
		},
		{
			"type": "ping",
		},
	}
	return events
}

// createAnthropicFinalEvents 创建Anthropic流式结束事件
func createAnthropicFinalEvents(outputTokens, inputTokens int, stopReason string) []map[string]any {
	// 构建符合Claude规范的完整usage信息
	usage := map[string]any{
		"output_tokens": outputTokens,
		"input_tokens":  inputTokens,
	}

	// 删除硬编码的content_block_stop，依赖sendFinalEvents的动态保护机制
	// sendFinalEvents在调用本函数前已经自动关闭所有未关闭的content_block（stream_processor.go:353-365）
	// 这样避免了重复发送content_block_stop导致的违规错误
	//
	// 三重保护机制确保不会缺失content_block_stop：
	// 1. ProcessEventStream正常转发上游的stop事件（99%场景）
	// 2. sendFinalEvents遍历所有activeBlocks并补发缺失的stop（容错机制，100%覆盖）
	// 3. handleMessageDelta在发送message_delta前的最后检查（最后保险）
	events := []map[string]any{
		{
			"type": "message_delta",
			"delta": map[string]any{
				"stop_reason":   stopReason,
				"stop_sequence": nil,
			},
			"usage": usage,
		},
		{
			"type": "message_stop",
		},
	}

	return events
}

// handleNonStreamRequest 处理非流式请求
func handleNonStreamRequest(c *gin.Context, anthropicReq types.AnthropicRequest, token types.TokenInfo) {
	// 记录请求接收日志 - 详细记录请求参数
	logRequestReceived(c, anthropicReq, false)

	// 使用统一的 TokenCalculator 计算 tokens
	tokenCalculator := GetTokenCalculator()
	inputTokens := tokenCalculator.EstimateInputTokens(anthropicReq)

	resp, err := executeCodeWhispererRequest(c, anthropicReq, token, false)
	if err != nil {
		return
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	// 读取响应体
	body, err := utils.ReadHTTPResponse(resp.Body)
	if err != nil {
		handleResponseReadError(c, err)
		return
	}

	// 使用新的符合AWS规范的解析器，但在非流式模式下增加超时保护
	compliantParser := parser.NewCompliantEventStreamParser()
	compliantParser.SetMaxErrors(5) // 限制最大错误次数以防死循环

	// 为非流式解析添加超时保护
	result, err := func() (*parser.ParseResult, error) {
		done := make(chan struct{})
		var result *parser.ParseResult
		var err error

		go func() {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("解析器panic: %v", r)
				}
				close(done)
			}()
			result, err = compliantParser.ParseResponse(body)
		}()

		select {
		case <-done:
			return result, err
		case <-time.After(10 * time.Second): // 10秒超时
			logger.Error("非流式解析超时")
			return nil, fmt.Errorf("解析超时")
		}
	}()

	if err != nil {
		logger.Error("非流式解析失败",
			logger.Err(err),
			logger.String("model", anthropicReq.Model),
			logger.Int("response_size", len(body)))

		// 提供更详细的错误信息和建议
		errorResp := gin.H{
			"error":   "响应解析失败",
			"type":    "parsing_error",
			"message": "无法解析AWS CodeWhisperer响应格式",
		}

		// 根据错误类型提供不同的HTTP状态码
		statusCode := http.StatusInternalServerError
		if strings.Contains(err.Error(), "解析超时") {
			statusCode = http.StatusRequestTimeout
			errorResp["message"] = "请求处理超时，请稍后重试"
		} else if strings.Contains(err.Error(), "格式错误") {
			statusCode = http.StatusBadRequest
			errorResp["message"] = "请求格式不正确"
		}

		c.JSON(statusCode, errorResp)
		return
	}

	// 转换为Anthropic格式
	var contexts []map[string]any
	textAgg := result.GetCompletionText()

	// 先获取工具管理器的所有工具，确保sawToolUse的判断基于实际工具
	toolManager := compliantParser.GetToolManager()
	allTools := make([]*parser.ToolExecution, 0)

	// 获取活跃工具
	for _, tool := range toolManager.GetActiveTools() {
		allTools = append(allTools, tool)
	}

	// 获取已完成工具
	for _, tool := range toolManager.GetCompletedTools() {
		allTools = append(allTools, tool)
	}

	// 基于实际工具数量判断是否包含工具调用
	sawToolUse := len(allTools) > 0

	// 添加文本内容
	if textAgg != "" {
		contexts = append(contexts, map[string]any{
			"type": "text",
			"text": textAgg,
		})
	}

	// 添加工具调用
	for _, tool := range allTools {
		// 创建标准的tool_use块，确保包含完整的状态信息
		toolUseBlock := map[string]any{
			"type":  "tool_use",
			"id":    tool.ID,
			"name":  tool.Name,
			"input": tool.Arguments,
		}

		// 如果工具参数为空或nil，确保为空对象而不是nil
		if tool.Arguments == nil {
			toolUseBlock["input"] = map[string]any{}
		}

		contexts = append(contexts, toolUseBlock)
	}

	// 使用新的stop_reason管理器，确保符合Claude官方规范
	stopReasonManager := NewStopReasonManager(anthropicReq)

	// 使用统一的 TokenCalculator 计算输出 tokens
	outputTokens := tokenCalculator.EstimateOutputTokens(textAgg, sawToolUse)

	stopReasonManager.UpdateToolCallStatus(sawToolUse, sawToolUse)
	stopReason := stopReasonManager.DetermineStopReason()

	anthropicResp := map[string]any{
		"content":       contexts,
		"model":         anthropicReq.Model,
		"role":          "assistant",
		"stop_reason":   stopReason,
		"stop_sequence": nil,
		"type":          "message",
		"usage": map[string]any{
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
		},
	}

	logger.Debug("下发非流式响应",
		addReqFields(c,
			logger.String("direction", "downstream_send"),
			logger.Any("contexts", contexts),
			logger.Bool("saw_tool_use", sawToolUse),
			logger.Int("content_count", len(contexts)),
		)...)
	c.JSON(http.StatusOK, anthropicResp)
}

// createTokenPreview 创建token预览显示格式 (***+后10位)
func createTokenPreview(token string) string {
	if len(token) <= 10 {
		// 如果token太短，全部用*代替
		return strings.Repeat("*", len(token))
	}

	// 3个*号 + 后10位
	suffix := token[len(token)-10:]
	return "***" + suffix
}

// handleTokenPoolAPI 处理Token池API请求 - 恢复多token显示
func handleTokenPoolAPI(c *gin.Context) {
	var tokenList []any
	var activeCount int

	// 从auth包获取配置信息
	configs, err := auth.GetConfigs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "加载配置失败: " + err.Error(),
		})
		return
	}

	if len(configs) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"timestamp":     time.Now().Format(time.RFC3339),
			"total_tokens":  0,
			"active_tokens": 0,
			"tokens":        []any{},
			"pool_stats": map[string]any{
				"total_tokens":  0,
				"active_tokens": 0,
			},
		})
		return
	}

	// 遍历所有配置
	for i, authConfig := range configs {
		// 检查配置是否被禁用
		if authConfig.Disabled {
			tokenData := map[string]any{
				"index":           i,
				"user_email":      "已禁用",
				"token_preview":   "***已禁用",
				"auth_type":       strings.ToLower(authConfig.AuthType),
				"remaining_usage": 0,
				"expires_at":      time.Now().Add(time.Hour).Format(time.RFC3339),
				"last_used":       "未知",
				"status":          "disabled",
				"error":           "配置已禁用",
			}
			tokenList = append(tokenList, tokenData)
			continue
		}

		// 尝试获取token信息
		tokenInfo, err := refreshSingleTokenByConfig(authConfig)
		if err != nil {
			tokenData := map[string]any{
				"index":           i,
				"user_email":      "获取失败",
				"token_preview":   createTokenPreview(authConfig.RefreshToken),
				"auth_type":       strings.ToLower(authConfig.AuthType),
				"remaining_usage": 0,
				"expires_at":      time.Now().Add(time.Hour).Format(time.RFC3339),
				"last_used":       "未知",
				"status":          "error",
				"error":           err.Error(),
			}
			tokenList = append(tokenList, tokenData)
			continue
		}

		// 检查使用限制
		var usageInfo *types.UsageLimits
		var available float64 // 默认值 (浮点数)
		var userEmail = "未知用户"

		checker := auth.NewUsageLimitsChecker()
		if usage, checkErr := checker.CheckUsageLimits(tokenInfo); checkErr == nil {
			usageInfo = usage
			available = auth.CalculateAvailableCount(usage)

			// 提取用户邮箱
			if usage.UserInfo.Email != "" {
				userEmail = usage.UserInfo.Email
			}
		}

		// 构建token数据
		tokenData := map[string]any{
			"index":           i,
			"user_email":      userEmail,
			"token_preview":   createTokenPreview(tokenInfo.AccessToken),
			"auth_type":       strings.ToLower(authConfig.AuthType),
			"remaining_usage": available,
			"expires_at":      tokenInfo.ExpiresAt.Format(time.RFC3339),
			"last_used":       time.Now().Format(time.RFC3339),
			"status":          "active",
		}

		// 添加使用限制详细信息 (基于CREDIT资源类型)
		if usageInfo != nil {
			for _, breakdown := range usageInfo.UsageBreakdownList {
				if breakdown.ResourceType == "CREDIT" {
					var totalLimit float64
					var totalUsed float64

					// 基础额度
					totalLimit += breakdown.UsageLimitWithPrecision
					totalUsed += breakdown.CurrentUsageWithPrecision

					// 免费试用额度
					if breakdown.FreeTrialInfo != nil && breakdown.FreeTrialInfo.FreeTrialStatus == "ACTIVE" {
						totalLimit += breakdown.FreeTrialInfo.UsageLimitWithPrecision
						totalUsed += breakdown.FreeTrialInfo.CurrentUsageWithPrecision
					}

					tokenData["usage_limits"] = map[string]any{
						"total_limit":   totalLimit, // 保留浮点精度
						"current_usage": totalUsed,  // 保留浮点精度
						"is_exceeded":   available <= 0,
					}
					break
				}
			}
		}

		// 如果token不可用，标记状态
		if available <= 0 {
			tokenData["status"] = "exhausted"
		} else {
			activeCount++
		}

		// 如果是 IdC 认证，显示额外信息
		if authConfig.AuthType == auth.AuthMethodIdC && authConfig.ClientID != "" {
			tokenData["client_id"] = func() string {
				if len(authConfig.ClientID) > 10 {
					return authConfig.ClientID[:5] + "***" + authConfig.ClientID[len(authConfig.ClientID)-3:]
				}
				return authConfig.ClientID
			}()
		}

		tokenList = append(tokenList, tokenData)
	}

	// 返回多token数据
	c.JSON(http.StatusOK, gin.H{
		"timestamp":     time.Now().Format(time.RFC3339),
		"total_tokens":  len(tokenList),
		"active_tokens": activeCount,
		"tokens":        tokenList,
		"pool_stats": map[string]any{
			"total_tokens":  len(configs),
			"active_tokens": activeCount,
		},
	})
}

// refreshSingleTokenByConfig 根据配置刷新单个token
func refreshSingleTokenByConfig(config auth.AuthConfig) (types.TokenInfo, error) {
	switch config.AuthType {
	case auth.AuthMethodSocial:
		return auth.RefreshSocialToken(config.RefreshToken)
	case auth.AuthMethodIdC:
		return auth.RefreshIdCToken(config)
	default:
		return types.TokenInfo{}, fmt.Errorf("不支持的认证类型: %s", config.AuthType)
	}
}

// 已移除复杂的token数据收集函数，现在使用简单的内存数据读取

// logRequestReceived 记录请求接收日志 - 详细记录请求参数
// 用于调试和监控，记录模型、消息数量、是否流式、thinking配置等关键信息
func logRequestReceived(c *gin.Context, req types.AnthropicRequest, isStream bool) {
	// 计算消息数量
	messageCount := len(req.Messages)

	// 检查是否启用 thinking
	thinkingEnabled := false
	thinkingBudget := 0
	if req.Thinking != nil {
		thinkingEnabled = req.Thinking.Type == "enabled"
		thinkingBudget = req.Thinking.BudgetTokens
	}

	// 检查是否有工具
	toolCount := len(req.Tools)

	// 获取系统提示长度
	systemLength := len(req.System)

	// 获取temperature值（处理指针类型）
	temperature := float64(0)
	if req.Temperature != nil {
		temperature = *req.Temperature
	}

	// 记录详细的请求接收日志
	logger.Debug("收到 Anthropic 格式请求",
		addReqFields(c,
			logger.String("direction", "client_request"),
			logger.String("model", req.Model),
			logger.Int("message_count", messageCount),
			logger.Bool("is_stream", isStream),
			logger.Bool("thinking_enabled", thinkingEnabled),
			logger.Int("thinking_budget", thinkingBudget),
			logger.Int("tool_count", toolCount),
			logger.Int("system_length", systemLength),
			logger.Int("max_tokens", req.MaxTokens),
			logger.Float64("temperature", temperature),
		)...)
}

// logResponseEvent 记录响应处理中的关键事件
// 用于调试 thinking 标签处理和内容块类型
func logResponseEvent(c *gin.Context, eventType string, details map[string]any) {
	fields := []logger.Field{
		logger.String("direction", "response_processing"),
		logger.String("event_type", eventType),
	}

	// 添加详细信息
	for key, value := range details {
		switch v := value.(type) {
		case string:
			fields = append(fields, logger.String(key, v))
		case int:
			fields = append(fields, logger.Int(key, v))
		case bool:
			fields = append(fields, logger.Bool(key, v))
		default:
			fields = append(fields, logger.Any(key, v))
		}
	}

	logger.Debug("响应处理事件", addReqFields(c, fields...)...)
}
