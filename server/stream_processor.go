package server

import (
	"encoding/json"
	"fmt"
	"io"

	"kiro2api/config"
	"kiro2api/logger"
	"kiro2api/parser"
	"kiro2api/types"
	"kiro2api/utils"

	"github.com/gin-gonic/gin"
)

// StreamProcessorContext 流处理上下文，封装所有流处理状态
// 遵循单一职责原则：专注于流式数据处理
type StreamProcessorContext struct {
	// 请求上下文
	c           *gin.Context
	req         types.AnthropicRequest
	token       *types.TokenWithUsage
	sender      StreamEventSender
	messageID   string
	inputTokens int

	// 状态管理器
	sseStateManager   *SSEStateManager
	stopReasonManager *StopReasonManager
	tokenEstimator    *utils.TokenEstimator

	// 流解析器
	compliantParser *parser.CompliantEventStreamParser

	// 统计信息
	totalOutputChars     int
	totalOutputTokens    int
	totalReadBytes       int
	totalProcessedEvents int
	lastParseErr         error

	// 工具调用跟踪
	toolUseIdByBlockIndex map[int]string
	completedToolUseIds   map[string]bool // 已完成的工具ID集合（用于stop_reason判断）

	// Thinking 状态机（借鉴 kiro.rs）
	thinkingContext *parser.ThinkingStreamContext

	// Thinking 标签状态跟踪（用于 Anthropic 格式响应）
	// 当检测到 thinking 块时，需要在内容前后添加 <thinking></thinking> 标签
	inThinking           bool // 是否正在 thinking 块内
	thinkingPrefixSent   bool // 是否已发送 <thinking> 前缀
	currentThinkingIndex int  // 当前 thinking 块的索引
}

// NewStreamProcessorContext 创建流处理上下文
func NewStreamProcessorContext(
	c *gin.Context,
	req types.AnthropicRequest,
	token *types.TokenWithUsage,
	sender StreamEventSender,
	messageID string,
	inputTokens int,
) *StreamProcessorContext {
	// 判断是否启用 thinking（借鉴 kiro.rs）
	thinkingEnabled := req.Thinking != nil && req.Thinking.Type == "enabled"

	return &StreamProcessorContext{
		c:                     c,
		req:                   req,
		token:                 token,
		sender:                sender,
		messageID:             messageID,
		inputTokens:           inputTokens,
		sseStateManager:       NewSSEStateManager(false),
		stopReasonManager:     NewStopReasonManager(req),
		tokenEstimator:        utils.NewTokenEstimator(),
		compliantParser:       parser.NewCompliantEventStreamParser(),
		toolUseIdByBlockIndex: make(map[int]string),
		completedToolUseIds:   make(map[string]bool),
		thinkingContext:       parser.NewThinkingStreamContext(thinkingEnabled),
	}
}

// Cleanup 清理资源
// 完整清理所有状态，防止内存泄漏
func (ctx *StreamProcessorContext) Cleanup() {
	// 重置解析器状态
	if ctx.compliantParser != nil {
		ctx.compliantParser.Reset()
	}

	// 清理工具调用映射
	if ctx.toolUseIdByBlockIndex != nil {
		// 清空map，释放内存
		for k := range ctx.toolUseIdByBlockIndex {
			delete(ctx.toolUseIdByBlockIndex, k)
		}
		ctx.toolUseIdByBlockIndex = nil
	}

	// 清理已完成工具集合
	if ctx.completedToolUseIds != nil {
		for k := range ctx.completedToolUseIds {
			delete(ctx.completedToolUseIds, k)
		}
		ctx.completedToolUseIds = nil
	}

	// 清理管理器引用，帮助GC
	ctx.sseStateManager = nil
	ctx.stopReasonManager = nil
	ctx.tokenEstimator = nil

	// 重置 thinking 上下文（借鉴 kiro.rs）
	if ctx.thinkingContext != nil {
		ctx.thinkingContext.Reset()
		ctx.thinkingContext = nil
	}
}

// initializeSSEResponse 初始化SSE响应头
func initializeSSEResponse(c *gin.Context) error {
	// 设置SSE响应头，禁用反向代理缓冲
	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	// 确认底层Writer支持Flush
	if _, ok := c.Writer.(io.Writer); !ok {
		return fmt.Errorf("writer不支持SSE刷新")
	}

	c.Writer.Flush()
	return nil
}

// sendInitialEvents 发送初始事件
func (ctx *StreamProcessorContext) sendInitialEvents(eventCreator func(string, int, string) []map[string]any) error {
	// 直接使用上下文中的 inputTokens（已经通过 TokenEstimator 精确计算）
	initialEvents := eventCreator(ctx.messageID, ctx.inputTokens, ctx.req.Model)

	// 注意：初始事件现在只包含 message_start 和 ping
	// content_block_start 会在收到实际内容时由 sse_state_manager 自动生成
	// 这避免了发送空内容块（如果上游只返回 tool_use 而没有文本）
	for _, event := range initialEvents {
		// 使用状态管理器发送事件
		if err := ctx.sseStateManager.SendEvent(ctx.c, ctx.sender, event); err != nil {
			logger.Error("初始SSE事件发送失败", logger.Err(err))
			return err
		}
	}

	return nil
}

// processToolUseStart 处理工具使用开始事件
func (ctx *StreamProcessorContext) processToolUseStart(dataMap map[string]any) {
	cb, ok := dataMap["content_block"].(map[string]any)
	if !ok {
		return
	}

	cbType, _ := cb["type"].(string)
	if cbType != "tool_use" {
		return
	}

	// 提取索引
	idx := extractIndex(dataMap)
	if idx < 0 {
		return
	}

	// 提取tool_use_id
	id, _ := cb["id"].(string)
	if id == "" {
		return
	}

	// 记录索引到tool_use_id的映射
	ctx.toolUseIdByBlockIndex[idx] = id

	logger.Debug("转发tool_use开始",
		logger.String("tool_use_id", id),
		logger.String("tool_name", getStringField(cb, "name")),
		logger.Int("index", idx))
}

// processToolUseStop 处理工具使用结束事件
func (ctx *StreamProcessorContext) processToolUseStop(dataMap map[string]any) {
	idx := extractIndex(dataMap)
	if idx < 0 {
		return
	}

	if toolId, exists := ctx.toolUseIdByBlockIndex[idx]; exists && toolId != "" {
		// *** 关键修复：在删除前先记录到已完成工具集合 ***
		// 问题：直接删除导致sendFinalEvents()中len(toolUseIdByBlockIndex)==0
		// 结果：stop_reason错误判断为end_turn而非tool_use
		// 解决：先添加到completedToolUseIds，保持工具调用的证据
		ctx.completedToolUseIds[toolId] = true

		delete(ctx.toolUseIdByBlockIndex, idx)
	} else {
		logger.Debug("非tool_use或未知索引的内容块结束",
			logger.Int("block_index", idx))
	}
}

// 直传模式：不再进行文本聚合

// sendFinalEvents 发送结束事件
func (ctx *StreamProcessorContext) sendFinalEvents() error {
	// 关闭所有未关闭的content_block
	activeBlocks := ctx.sseStateManager.GetActiveBlocks()
	for index, block := range activeBlocks {
		if block.Started && !block.Stopped {
			stopEvent := map[string]any{
				"type":  "content_block_stop",
				"index": index,
			}
			logger.Debug("最终事件前关闭未关闭的content_block", logger.Int("index", index))
			if err := ctx.sseStateManager.SendEvent(ctx.c, ctx.sender, stopEvent); err != nil {
				logger.Error("关闭content_block失败", logger.Err(err), logger.Int("index", index))
			}
		}
	}

	// 更新工具调用状态
	// 使用已完成工具集合来判断，因为toolUseIdByBlockIndex在stop时已被清空
	hasActiveTools := len(ctx.toolUseIdByBlockIndex) > 0
	hasCompletedTools := len(ctx.completedToolUseIds) > 0

	// logger.Debug("更新工具调用状态",
	// 	logger.Bool("has_active_tools", hasActiveTools),
	// 	logger.Bool("has_completed_tools", hasCompletedTools),
	// 	logger.Int("active_count", len(ctx.toolUseIdByBlockIndex)),
	// 	logger.Int("completed_count", len(ctx.completedToolUseIds)))

	ctx.stopReasonManager.UpdateToolCallStatus(hasActiveTools, hasCompletedTools)

	// 计算输出tokens：
	// 1) 优先使用上游 message_delta.usage.output_tokens（如果有）
	// 2) 否则基于流式 delta 累计的 totalOutputTokens
	// 3) 最后才回退到历史的“按输出负载字节数估算”
	outputTokens := ctx.totalOutputTokens
	if outputTokens <= 0 {
		baseTokens := ctx.totalOutputChars / config.TokenEstimationRatio
		outputTokens = baseTokens
		// 仅在回退路径下，对工具调用增加结构化开销（避免与delta累计重复计数）
		if len(ctx.toolUseIdByBlockIndex) > 0 || len(ctx.completedToolUseIds) > 0 {
			outputTokens = int(float64(baseTokens) * config.ToolCallTokenOverhead)
		}
		if outputTokens < config.MinOutputTokens && ctx.totalOutputChars > 0 {
			outputTokens = config.MinOutputTokens
		}
	}

	// 确定stop_reason
	stopReason := ctx.stopReasonManager.DetermineStopReason()

	logger.Debug("创建结束事件",
		logger.String("stop_reason", stopReason),
		logger.String("stop_reason_description", GetStopReasonDescription(stopReason)),
		logger.Int("output_tokens", outputTokens))

	// 创建并发送结束事件
	finalEvents := createAnthropicFinalEvents(outputTokens, ctx.inputTokens, stopReason)
	for _, event := range finalEvents {
		if err := ctx.sseStateManager.SendEvent(ctx.c, ctx.sender, event); err != nil {
			logger.Error("结束事件发送违规", logger.Err(err))
		}
	}

	return nil
}

// 辅助函数

// extractIndex 从数据映射中提取索引
func extractIndex(dataMap map[string]any) int {
	if v, ok := dataMap["index"].(int); ok {
		return v
	}
	if f, ok := dataMap["index"].(float64); ok {
		return int(f)
	}
	return -1
}

func extractIntAny(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

// getStringField 从映射中安全提取字符串字段
func getStringField(m map[string]any, key string) string {
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

// EventStreamProcessor 事件流处理器
// 遵循单一职责原则：专注于处理事件流
type EventStreamProcessor struct {
	ctx *StreamProcessorContext
}

// NewEventStreamProcessor 创建事件流处理器
func NewEventStreamProcessor(ctx *StreamProcessorContext) *EventStreamProcessor {
	return &EventStreamProcessor{
		ctx: ctx,
	}
}

// ProcessEventStream 处理事件流的主循环
func (esp *EventStreamProcessor) ProcessEventStream(reader io.Reader) error {
	buf := make([]byte, 1024)

	for {
		n, err := reader.Read(buf)
		esp.ctx.totalReadBytes += n

		if n > 0 {
			// 解析事件流
			events, parseErr := esp.ctx.compliantParser.ParseStream(buf[:n])
			esp.ctx.lastParseErr = parseErr

			if parseErr != nil {
				logger.Warn("符合规范的解析器处理失败",
					addReqFields(esp.ctx.c,
						logger.Err(parseErr),
						logger.Int("read_bytes", n),
						logger.String("direction", "upstream_response"),
					)...)
			}

			esp.ctx.totalProcessedEvents += len(events)

			// 处理每个事件
			for _, event := range events {
				if err := esp.processEvent(event); err != nil {
					return err
				}
			}
		}

		if err != nil {
			if err == io.EOF {
				logger.Debug("响应流结束",
					addReqFields(esp.ctx.c,
						logger.Int("total_read_bytes", esp.ctx.totalReadBytes),
					)...)
			} else {
				logger.Error("读取响应流时发生错误",
					addReqFields(esp.ctx.c,
						logger.Err(err),
						logger.Int("total_read_bytes", esp.ctx.totalReadBytes),
						logger.String("direction", "upstream_response"),
					)...)
			}
			break
		}
	}

	// 直传模式：无需冲刷剩余文本
	return nil
}

// processEvent 处理单个事件
func (esp *EventStreamProcessor) processEvent(event parser.SSEEvent) error {
	dataMap, ok := event.Data.(map[string]any)
	if !ok {
		logger.Warn("事件数据类型不匹配,跳过", logger.String("event_type", event.Event))
		return nil
	}

	eventType, _ := dataMap["type"].(string)

	// 处理不同类型的事件
	switch eventType {
	case "content_block_start":
		esp.ctx.processToolUseStart(dataMap)
		// 处理 thinking 块开始 - 检测并发送 <thinking> 前缀
		esp.handleThinkingBlockStart(dataMap)

	case "content_block_delta":
		// 处理 thinking_delta - 确保在内容前发送 <thinking> 前缀
		esp.handleThinkingDelta(dataMap)

	case "content_block_stop":
		esp.ctx.processToolUseStop(dataMap)
		// 处理 thinking 块结束 - 发送 </thinking> 后缀
		esp.handleThinkingBlockStop(dataMap)

	case "message_delta":
		// 如果上游携带了usage.output_tokens，优先记录下来作为最终输出
		if usage, ok := dataMap["usage"].(map[string]any); ok {
			if v, exists := usage["output_tokens"]; exists {
				if out, ok2 := extractIntAny(v); ok2 && out > 0 {
					esp.ctx.totalOutputTokens = out
				}
			}
		}

	case "exception":
		// 处理上游异常事件，检查是否需要映射为max_tokens
		if esp.handleExceptionEvent(dataMap) {
			return nil // 已转换并发送，不转发原始exception事件
		}
	}

	// 使用状态管理器发送事件（直传）
	if err := esp.ctx.sseStateManager.SendEvent(esp.ctx.c, esp.ctx.sender, dataMap); err != nil {
		logger.Error("SSE事件发送违规", logger.Err(err))
		// 非严格模式下，违规事件被跳过但不中断流
	}

	// 更新输出字符统计 - 统计 dataMap 全量内容（符合"统计输出所有内容"的要求）
	// 通过 JSON 序列化计算输出负载的字节长度，更贴近实际传输内容
	if b, err := json.Marshal(dataMap); err == nil {
		esp.ctx.totalOutputChars += len(b)
	} else {
		// 序列化失败时保底：不影响主流程，仅跳过统计
		logger.Debug("数据序列化用于统计失败，跳过该事件", logger.Err(err))
	}

	// 更新输出 token 统计（仅在上游未直接给出 output_tokens 时有意义）
	// 关注：text_delta / thinking_delta / input_json_delta
	if eventType == "content_block_delta" {
		if delta, ok := dataMap["delta"].(map[string]any); ok {
			dt, _ := delta["type"].(string)
			switch dt {
			case "text_delta":
				if text, ok := delta["text"].(string); ok && text != "" {
					esp.ctx.totalOutputTokens += utils.CountTokensWithTiktoken(text, "cl100k_base")
				}
			case "thinking_delta":
				if thinking, ok := delta["thinking"].(string); ok && thinking != "" {
					esp.ctx.totalOutputTokens += utils.CountTokensWithTiktoken(thinking, "cl100k_base")
				}
			case "input_json_delta":
				if pj, ok := delta["partial_json"].(string); ok && pj != "" {
					esp.ctx.totalOutputTokens += utils.CountTokensWithTiktoken(pj, "cl100k_base")
				}
			}
		}
	}

	esp.ctx.c.Writer.Flush()
	return nil
}

// handleThinkingBlockStart 处理 thinking 块开始事件
// 当检测到 content_block_start 事件中的 blockType == "thinking" 时，发送 <thinking>\n 前缀
func (esp *EventStreamProcessor) handleThinkingBlockStart(dataMap map[string]any) {
	cb, ok := dataMap["content_block"].(map[string]any)
	if !ok {
		return
	}

	blockType, _ := cb["type"].(string)
	blockIndex := extractIndex(dataMap)

	// 检测到 thinking 块开始
	if blockType == "thinking" {
		logger.Debug("检测到 thinking 块开始",
			logger.Int("block_index", blockIndex),
			logger.Bool("already_in_thinking", esp.ctx.inThinking))

		if !esp.ctx.inThinking {
			// 发送 <thinking> 前缀
			esp.sendThinkingPrefix()
			esp.ctx.inThinking = true
			esp.ctx.thinkingPrefixSent = true
			esp.ctx.currentThinkingIndex = blockIndex
		}
	} else if esp.ctx.inThinking {
		// 如果正在 thinking 中，遇到非 thinking 类型的新块，先闭合 thinking
		logger.Debug("thinking 块中遇到非 thinking 类型块，闭合 thinking",
			logger.String("new_block_type", blockType),
			logger.Int("block_index", blockIndex))

		esp.sendThinkingSuffix()
		esp.ctx.inThinking = false
		esp.ctx.thinkingPrefixSent = false
	}
}

// handleThinkingDelta 处理 thinking_delta 事件
// 如果还没有发送过 <thinking> 前缀，在内容前发送
func (esp *EventStreamProcessor) handleThinkingDelta(dataMap map[string]any) {
	delta, ok := dataMap["delta"].(map[string]any)
	if !ok {
		return
	}

	deltaType, _ := delta["type"].(string)

	if deltaType == "thinking_delta" {
		// 如果还没有发送过 thinking 前缀，先发送
		if !esp.ctx.thinkingPrefixSent {
			logger.Debug("thinking_delta 事件触发发送 thinking 前缀",
				logger.Bool("in_thinking", esp.ctx.inThinking))

			esp.sendThinkingPrefix()
			esp.ctx.inThinking = true
			esp.ctx.thinkingPrefixSent = true
			esp.ctx.currentThinkingIndex = extractIndex(dataMap)
		}
	} else if deltaType == "text_delta" && esp.ctx.inThinking {
		// *** 修复：移除错误的逻辑 ***
		// 不要在text_delta中强制关闭thinking，应该等待content_block_stop
		// 或者新的非thinking类型的content_block_start来关闭
		logger.Debug("在thinking中遇到text_delta，但不强制关闭thinking")
	}
}

// handleThinkingBlockStop 处理 thinking 块结束事件
// 当 thinking 块结束时，发送 </thinking>\n\n 后缀
func (esp *EventStreamProcessor) handleThinkingBlockStop(dataMap map[string]any) {
	blockIndex := extractIndex(dataMap)

	// 检查是否是当前 thinking 块的结束
	if esp.ctx.inThinking && blockIndex == esp.ctx.currentThinkingIndex {
		logger.Debug("thinking 块结束，发送后缀",
			logger.Int("block_index", blockIndex))

		esp.sendThinkingSuffix()
		esp.ctx.inThinking = false
		esp.ctx.thinkingPrefixSent = false
	}
}

// sendThinkingPrefix 发送 <thinking> 前缀
func (esp *EventStreamProcessor) sendThinkingPrefix() {
	prefixEvent := map[string]any{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]any{
			"type": "text_delta",
			"text": "<thinking>\n",
		},
	}

	logger.Debug("发送 thinking 前缀 <thinking>")

	if err := esp.ctx.sseStateManager.SendEvent(esp.ctx.c, esp.ctx.sender, prefixEvent); err != nil {
		logger.Error("发送 thinking 前缀失败", logger.Err(err))
	}
	esp.ctx.c.Writer.Flush()
}

// sendThinkingSuffix 发送 </thinking> 后缀
func (esp *EventStreamProcessor) sendThinkingSuffix() {
	suffixEvent := map[string]any{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]any{
			"type": "text_delta",
			"text": "\n</thinking>\n\n",
		},
	}

	logger.Debug("发送 thinking 后缀 </thinking>")

	if err := esp.ctx.sseStateManager.SendEvent(esp.ctx.c, esp.ctx.sender, suffixEvent); err != nil {
		logger.Error("发送 thinking 后缀失败", logger.Err(err))
	}
	esp.ctx.c.Writer.Flush()
}

// processContentBlockDelta 处理content_block_delta事件
// 返回true表示已处理（聚合），不需要转发原始事件
// processContentBlockDelta 已废弃（直传模式不再需要）

// handleExceptionEvent 处理上游异常事件
// 使用 StreamExceptionMapper 策略模式处理各种异常类型
// 返回 true 表示已处理并转换，不需要转发原始 exception 事件
func (esp *EventStreamProcessor) handleExceptionEvent(dataMap map[string]any) bool {
	// 使用统一的流式异常映射器
	exceptionMapper := NewStreamExceptionMapper()
	return exceptionMapper.HandleException(esp.ctx, dataMap)
}

// 直传模式：无flush逻辑
