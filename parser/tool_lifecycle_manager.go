package parser

import (
	"kiro2api/logger"
	"kiro2api/utils"
	"time"
)

// DefaultMaxNestingDepth 默认最大嵌套深度
const DefaultMaxNestingDepth = 3

// ToolLifecycleManager 工具调用生命周期管理器
type ToolLifecycleManager struct {
	activeTools         map[string]*ToolExecution
	completedTools      map[string]*ToolExecution
	blockIndexMap       map[string]int
	nextBlockIndex      int
	textIntroGenerated  bool // 跟踪是否已生成文本介绍
	currentNestingDepth int  // 当前嵌套深度
	maxNestingDepth     int  // 最大嵌套深度限制
}

// NewToolLifecycleManager 创建工具生命周期管理器
func NewToolLifecycleManager() *ToolLifecycleManager {
	return &ToolLifecycleManager{
		activeTools:         make(map[string]*ToolExecution),
		completedTools:      make(map[string]*ToolExecution),
		blockIndexMap:       make(map[string]int),
		nextBlockIndex:      1, // 索引0预留给文本内容
		currentNestingDepth: 0,
		maxNestingDepth:     DefaultMaxNestingDepth,
	}
}

// Reset 重置管理器状态
func (tlm *ToolLifecycleManager) Reset() {
	tlm.activeTools = make(map[string]*ToolExecution)
	tlm.completedTools = make(map[string]*ToolExecution)
	tlm.blockIndexMap = make(map[string]int)
	tlm.nextBlockIndex = 1
	tlm.textIntroGenerated = false  // 重置文本介绍生成状态
	tlm.currentNestingDepth = 0     // 重置嵌套深度
}

// HandleToolCallRequest 处理工具调用请求
// HandleToolCallRequest 处理工具调用请求（增强参数验证）
func (tlm *ToolLifecycleManager) HandleToolCallRequest(request ToolCallRequest) []SSEEvent {
	events := make([]SSEEvent, 0, len(request.ToolCalls)*3) // 调整预分配容量，包含文本介绍

	// *** 关键修复：根据Claude规范，在第一个工具调用前自动生成文本介绍（index:0） ***
	if !tlm.textIntroGenerated && len(request.ToolCalls) > 0 {
		// 生成符合Claude规范的文本介绍事件序列
		textIntroEvents := tlm.generateTextIntroduction(request.ToolCalls[0])
		events = append(events, textIntroEvents...)
		tlm.textIntroGenerated = true

		// logger.Debug("自动生成工具调用文本介绍",
		// 	logger.Int("intro_events", len(textIntroEvents)),
		// 	logger.String("first_tool", request.ToolCalls[0].Function.Name))
	}

	for _, toolCall := range request.ToolCalls {
		// 检查工具是否已存在，避免重复创建
		if existing, exists := tlm.activeTools[toolCall.ID]; exists {
			logger.Debug("工具已存在，更新参数",
				logger.String("tool_id", toolCall.ID),
				logger.String("tool_name", toolCall.Function.Name),
				logger.String("existing_status", existing.Status.String()))

			// 解析工具调用参数
			var arguments map[string]any
			if err := utils.SafeUnmarshal([]byte(toolCall.Function.Arguments), &arguments); err != nil {
				logger.Warn("解析工具调用参数失败",
					logger.String("tool_id", toolCall.ID),
					logger.String("tool_name", toolCall.Function.Name),
					logger.Err(err))
				arguments = make(map[string]any)
			}

			// 更新现有工具的参数
			if len(arguments) > 0 {
				existing.Arguments = arguments
			}
			continue
		}

		// 解析工具调用参数
		var arguments map[string]any
		if err := utils.SafeUnmarshal([]byte(toolCall.Function.Arguments), &arguments); err != nil {
			logger.Warn("解析工具调用参数失败",
				logger.String("tool_id", toolCall.ID),
				logger.String("tool_name", toolCall.Function.Name),
				logger.Err(err))
			arguments = make(map[string]any)
		}

		execution := &ToolExecution{
			ID:         toolCall.ID,
			Name:       toolCall.Function.Name,
			StartTime:  time.Now(),
			Status:     ToolStatusPending,
			Arguments:  arguments,
			BlockIndex: tlm.getOrAssignBlockIndex(toolCall.ID),
		}

		tlm.activeTools[toolCall.ID] = execution

		logger.Debug("开始处理工具调用",
			logger.String("tool_id", toolCall.ID),
			logger.String("tool_name", toolCall.Function.Name),
			logger.Int("block_index", execution.BlockIndex))

		// 清理参数中的 null 值，防止下游问题
		cleanedArgs := utils.RemoveNullsFromToolInput(arguments).(map[string]any)

		// 1. 生成标准的 content_block_start 事件（符合Anthropic规范）
		// 这替代了原来的 TOOL_EXECUTION_START 非标准事件
		// 策略调整：content_block_start 中不包含 input 参数，统一通过 delta 发送
		// 这确保了下游转换为 OpenAI 格式时，始终遵循 "先头(无参)后体(参数delta)" 的标准流式模式
		// 避免了部分客户端对 "头带参数" 处理不兼容的问题
		
		events = append(events, SSEEvent{
			Event: "content_block_start",
			Data: map[string]any{
				"type":  "content_block_start",
				"index": execution.BlockIndex,
				"content_block": map[string]any{
					"type":  "tool_use",
					"id":    toolCall.ID,
					"name":  toolCall.Function.Name,
					"input": map[string]any{}, // 强制为空，参数通过 delta 发送
				},
			},
		})

		// 2. 如果有参数，生成参数输入增量事件
		// 即使是一次性完整的参数，也封装为 delta 发送，模拟流式传输
		if len(cleanedArgs) > 0 {
			argsJSON, _ := utils.SafeMarshal(cleanedArgs)
			events = append(events, SSEEvent{
				Event: "content_block_delta",
				Data: map[string]any{
					"type":  "content_block_delta",
					"index": execution.BlockIndex,
					"delta": map[string]any{
						"type":         "input_json_delta",
						"partial_json": string(argsJSON),
					},
				},
			})
		}

		execution.Status = ToolStatusRunning
	}

	return events
}

// HandleToolCallResult 处理工具调用结果
func (tlm *ToolLifecycleManager) HandleToolCallResult(result ToolCallResult) []SSEEvent {
	events := make([]SSEEvent, 0, 1) // 调整预分配容量（只需要content_block_stop）

	execution, exists := tlm.activeTools[result.ToolCallID]
	if !exists {
		logger.Warn("收到未知工具调用的结果",
			logger.String("tool_call_id", result.ToolCallID))
		return events
	}

	now := time.Now()
	execution.EndTime = &now
	execution.Result = result.Result
	execution.Status = ToolStatusCompleted

	// 计算执行时间
	// executionTime := now.Sub(execution.StartTime).Milliseconds()
	// if result.ExecutionTime > 0 {
	// executionTime = result.ExecutionTime
	// }

	// logger.Debug("工具调用完成",
	// 	logger.String("tool_id", result.ToolCallID),
	// 	logger.String("tool_name", execution.Name),
	// 	logger.Int64("execution_time", executionTime))

	// 生成标准的 content_block_stop 事件（符合Anthropic规范）
	events = append(events, SSEEvent{
		Event: "content_block_stop",
		Data: map[string]any{
			"type":  "content_block_stop",
			"index": execution.BlockIndex,
		},
	})

	// 移动到已完成工具列表
	tlm.completedTools[result.ToolCallID] = execution
	delete(tlm.activeTools, result.ToolCallID)

	// 新增：检查结果中是否包含嵌套工具调用
	if nestedToolCalls := tlm.extractNestedToolCalls(result.Result); len(nestedToolCalls) > 0 {
		logger.Info("检测到嵌套工具调用",
			logger.String("parent_tool_id", result.ToolCallID),
			logger.Int("nested_count", len(nestedToolCalls)),
			logger.Int("current_depth", tlm.currentNestingDepth))

		if tlm.currentNestingDepth >= tlm.maxNestingDepth {
			logger.Warn("嵌套工具调用深度超过限制，停止处理嵌套调用",
				logger.Int("current_depth", tlm.currentNestingDepth),
				logger.Int("max_depth", tlm.maxNestingDepth),
				logger.String("parent_tool_id", result.ToolCallID))
			// 不处理嵌套调用，但记录日志
		} else {
			tlm.currentNestingDepth++
			// 处理嵌套工具调用
			nestedEvents := tlm.HandleToolCallRequest(ToolCallRequest{ToolCalls: nestedToolCalls})
			events = append(events, nestedEvents...)
			tlm.currentNestingDepth--
		}
	}

	// 修复：删除message_delta事件，由sendFinalEvents统一管理
	// 原因：
	// 1. message_delta在一次消息中只能出现一次（Claude API规范）
	// 2. sendFinalEvents会在流的最后发送message_delta，包含正确的stop_reason和完整的usage
	// 3. ToolLifecycleManager的职责是管理工具生命周期，不应该发送message级别的事件
	//
	// 之前的问题：
	// - ToolLifecycleManager.HandleToolCallResult发送message_delta (stop_reason: tool_use)
	// - sendFinalEvents再次发送message_delta (stop_reason: end_turn)
	// - 导致"违规：message_delta只能出现一次"错误
	//
	// 修复后的正确流程：
	// 1. ToolLifecycleManager: content_block_stop ← 关闭工具块
	// 2. sendFinalEvents: message_delta + message_stop ← 统一的消息结束

	return events
}

// extractNestedToolCalls 从工具调用结果中提取嵌套的工具调用
// 支持多种结果格式：字符串、map、数组等
func (tlm *ToolLifecycleManager) extractNestedToolCalls(result any) []ToolCall {
	if result == nil {
		return nil
	}

	var toolCalls []ToolCall

	switch v := result.(type) {
	case string:
		// 尝试将字符串解析为JSON
		var parsed any
		if err := utils.SafeUnmarshal([]byte(v), &parsed); err == nil {
			return tlm.extractNestedToolCalls(parsed)
		}
		return nil

	case map[string]any:
		// 检查是否是单个 tool_use 块
		if toolCall := tlm.parseToolUseBlock(v); toolCall != nil {
			toolCalls = append(toolCalls, *toolCall)
		}

		// 检查是否包含 content 数组（Anthropic 格式）
		if content, ok := v["content"].([]any); ok {
			for _, item := range content {
				if block, ok := item.(map[string]any); ok {
					if toolCall := tlm.parseToolUseBlock(block); toolCall != nil {
						toolCalls = append(toolCalls, *toolCall)
					}
				}
			}
		}

		// 检查是否包含 tool_calls 数组（OpenAI 格式）
		if toolCallsArr, ok := v["tool_calls"].([]any); ok {
			for _, item := range toolCallsArr {
				if block, ok := item.(map[string]any); ok {
					if toolCall := tlm.parseToolCallBlock(block); toolCall != nil {
						toolCalls = append(toolCalls, *toolCall)
					}
				}
			}
		}

	case []any:
		// 遍历数组，查找 tool_use 块
		for _, item := range v {
			if block, ok := item.(map[string]any); ok {
				if toolCall := tlm.parseToolUseBlock(block); toolCall != nil {
					toolCalls = append(toolCalls, *toolCall)
				}
			}
		}
	}

	return toolCalls
}

// parseToolUseBlock 解析 Anthropic 格式的 tool_use 块
func (tlm *ToolLifecycleManager) parseToolUseBlock(block map[string]any) *ToolCall {
	// 检查类型是否为 tool_use
	blockType, ok := block["type"].(string)
	if !ok || blockType != "tool_use" {
		return nil
	}

	// 提取必要字段
	id, idOk := block["id"].(string)
	name, nameOk := block["name"].(string)
	if !idOk || !nameOk || id == "" || name == "" {
		return nil
	}

	// 提取 input 参数
	var argsJSON string
	if input, ok := block["input"]; ok {
		if inputMap, ok := input.(map[string]any); ok {
			if jsonBytes, err := utils.SafeMarshal(inputMap); err == nil {
				argsJSON = string(jsonBytes)
			}
		} else if inputStr, ok := input.(string); ok {
			argsJSON = inputStr
		}
	}

	if argsJSON == "" {
		argsJSON = "{}"
	}

	return &ToolCall{
		ID:   id,
		Type: "function",
		Function: ToolCallFunction{
			Name:      name,
			Arguments: argsJSON,
		},
	}
}

// parseToolCallBlock 解析 OpenAI 格式的 tool_call 块
func (tlm *ToolLifecycleManager) parseToolCallBlock(block map[string]any) *ToolCall {
	// 检查类型是否为 function
	blockType, _ := block["type"].(string)
	if blockType != "" && blockType != "function" {
		return nil
	}

	// 提取 ID
	id, idOk := block["id"].(string)
	if !idOk || id == "" {
		return nil
	}

	// 提取 function 信息
	funcInfo, funcOk := block["function"].(map[string]any)
	if !funcOk {
		return nil
	}

	name, nameOk := funcInfo["name"].(string)
	if !nameOk || name == "" {
		return nil
	}

	// 提取 arguments
	var argsJSON string
	if args, ok := funcInfo["arguments"]; ok {
		if argsStr, ok := args.(string); ok {
			argsJSON = argsStr
		} else if argsMap, ok := args.(map[string]any); ok {
			if jsonBytes, err := utils.SafeMarshal(argsMap); err == nil {
				argsJSON = string(jsonBytes)
			}
		}
	}

	if argsJSON == "" {
		argsJSON = "{}"
	}

	return &ToolCall{
		ID:   id,
		Type: "function",
		Function: ToolCallFunction{
			Name:      name,
			Arguments: argsJSON,
		},
	}
}

// SetMaxNestingDepth 设置最大嵌套深度
func (tlm *ToolLifecycleManager) SetMaxNestingDepth(depth int) {
	if depth > 0 {
		tlm.maxNestingDepth = depth
	}
}

// GetCurrentNestingDepth 获取当前嵌套深度
func (tlm *ToolLifecycleManager) GetCurrentNestingDepth() int {
	return tlm.currentNestingDepth
}

// GetMaxNestingDepth 获取最大嵌套深度
func (tlm *ToolLifecycleManager) GetMaxNestingDepth() int {
	return tlm.maxNestingDepth
}

// HandleToolCallError 处理工具调用错误
func (tlm *ToolLifecycleManager) HandleToolCallError(errorInfo ToolCallError) []SSEEvent {
	events := make([]SSEEvent, 0, 2) // 调整预分配容量（error + content_block_stop）

	execution, exists := tlm.activeTools[errorInfo.ToolCallID]
	if !exists {
		logger.Warn("收到未知工具调用的错误",
			logger.String("tool_call_id", errorInfo.ToolCallID))
		return events
	}

	now := time.Now()
	execution.EndTime = &now
	execution.Error = errorInfo.Error
	execution.Status = ToolStatusError

	executionTime := now.Sub(execution.StartTime).Milliseconds()

	logger.Warn("工具调用失败",
		logger.String("tool_id", errorInfo.ToolCallID),
		logger.String("tool_name", execution.Name),
		logger.String("error", errorInfo.Error),
		logger.Int64("execution_time", executionTime))

	// 1. 生成标准的错误事件（符合Anthropic规范）
	// 这替代了原来的 TOOL_CALL_ERROR 非标准事件
	events = append(events, SSEEvent{
		Event: "error",
		Data: map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":         "tool_error",
				"message":      errorInfo.Error,
				"tool_call_id": errorInfo.ToolCallID,
			},
		},
	})

	// 2. 生成标准的 content_block_stop 事件（符合Anthropic规范）
	// 即使出错也要正确结束内容块
	events = append(events, SSEEvent{
		Event: "content_block_stop",
		Data: map[string]any{
			"type":  "content_block_stop",
			"index": execution.BlockIndex,
		},
	})

	// 修复：删除message_delta事件，由sendFinalEvents统一管理
	// 原因：
	// 1. message_delta在一次消息中只能出现一次（Claude API规范）
	// 2. sendFinalEvents会在流的最后发送message_delta，包含正确的stop_reason和完整的usage
	// 3. ToolLifecycleManager的职责是管理工具生命周期，不应该发送message级别的事件
	//
	// 之前的问题：
	// - ToolLifecycleManager.HandleToolCallError发送message_delta (stop_reason: tool_error)
	// - sendFinalEvents再次发送message_delta (stop_reason: end_turn)
	// - 导致"违规：message_delta只能出现一次"错误
	//
	// 修复后的正确流程：
	// 1. ToolLifecycleManager: error event + content_block_stop ← 关闭工具块并报告错误
	// 2. sendFinalEvents: message_delta + message_stop ← 统一的消息结束

	// 移动到已完成工具列表
	tlm.completedTools[errorInfo.ToolCallID] = execution
	delete(tlm.activeTools, errorInfo.ToolCallID)

	return events
}

// GetToolExecution 获取工具执行信息
func (tlm *ToolLifecycleManager) GetToolExecution(toolID string) *ToolExecution {
	if tool, exists := tlm.activeTools[toolID]; exists {
		return tool
	}
	if tool, exists := tlm.completedTools[toolID]; exists {
		return tool
	}
	return nil
}

// GetActiveTools 获取所有活跃的工具
func (tlm *ToolLifecycleManager) GetActiveTools() map[string]*ToolExecution {
	result := make(map[string]*ToolExecution)
	for id, tool := range tlm.activeTools {
		result[id] = tool
	}
	return result
}

// GetCompletedTools 获取所有已完成的工具
func (tlm *ToolLifecycleManager) GetCompletedTools() map[string]*ToolExecution {
	result := make(map[string]*ToolExecution)
	for id, tool := range tlm.completedTools {
		result[id] = tool
	}
	return result
}

// getOrAssignBlockIndex 获取或分配块索引
func (tlm *ToolLifecycleManager) getOrAssignBlockIndex(toolID string) int {
	if index, exists := tlm.blockIndexMap[toolID]; exists {
		return index
	}

	index := tlm.nextBlockIndex
	tlm.blockIndexMap[toolID] = index
	tlm.nextBlockIndex++
	return index
}

// GetBlockIndex 获取工具的块索引
func (tlm *ToolLifecycleManager) GetBlockIndex(toolID string) int {
	if index, exists := tlm.blockIndexMap[toolID]; exists {
		return index
	}
	return -1
}

// generateTextIntroduction 生成符合Claude规范的文本介绍事件序列
// 根据Claude官方示例，工具调用前应有文本介绍，如："Okay, let's check the weather for San Francisco, CA:"
func (tlm *ToolLifecycleManager) generateTextIntroduction(firstTool ToolCall) []SSEEvent {
	// 根据工具类型生成合适的介绍文本
	introText := tlm.generateIntroText(firstTool.Function.Name)

	// 修复：删除重复的content_block_start和content_block_stop
	// 原因：block[0]已在sendInitialEvents()中启动（handlers.go:148-156），会在sendFinalEvents()中关闭
	// 这里只需要发送content_block_delta来添加介绍文本即可
	//
	// 之前的问题：
	// 1. sendInitialEvents发送content_block_start(index:0) → SSEStateManager标记block[0].Started=true
	// 2. generateTextIntroduction再次发送content_block_start(index:0) → 违规！block已started但未stopped
	// 3. generateTextIntroduction发送content_block_stop(index:0) → 过早关闭，与后续工具调用冲突
	//
	// 修复后的事件序列：
	// 1. sendInitialEvents: content_block_start(index:0) ← 初始化文本块
	// 2. generateTextIntroduction: content_block_delta(index:0) ← 添加介绍文本
	// 3. [工具调用处理]: content_block_start(index:1), content_block_stop(index:1), ...
	// 4. sendFinalEvents: content_block_stop(index:0) ← 关闭文本块
	return []SSEEvent{
		{
			Event: "content_block_delta",
			Data: map[string]any{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]any{
					"type": "text_delta",
					"text": introText,
				},
			},
		},
	}
}

// generateIntroText 根据工具类型生成合适的介绍文本
func (tlm *ToolLifecycleManager) generateIntroText(_ string) string {
	//switch strings.ToLower(toolName) {
	//case "search", "web_search":
	//	return "让我为您搜索相关信息。"
	//case "calculator", "calc":
	//	return "让我为您进行计算。"
	//case "todowrite":
	//	return "让我为您更新任务列表。"
	//default:
	//	return fmt.Sprintf("让我使用%s工具来帮助您。", toolName)
	//}
	return ""
}

// GenerateToolSummary 生成工具执行摘要
func (tlm *ToolLifecycleManager) GenerateToolSummary() map[string]any {
	activeCount := len(tlm.activeTools)
	completedCount := len(tlm.completedTools)
	errorCount := 0
	totalExecutionTime := int64(0)

	for _, tool := range tlm.completedTools {
		if tool.Status == ToolStatusError {
			errorCount++
		}
		if tool.EndTime != nil {
			totalExecutionTime += tool.EndTime.Sub(tool.StartTime).Milliseconds()
		}
	}

	return map[string]any{
		"active_tools":         activeCount,
		"completed_tools":      completedCount,
		"error_tools":          errorCount,
		"total_execution_time": totalExecutionTime,
		"success_rate":         float64(completedCount-errorCount) / float64(completedCount+activeCount),
	}
}

// UpdateToolArguments 更新工具调用的参数
func (tlm *ToolLifecycleManager) UpdateToolArguments(toolID string, arguments map[string]any) {
	// logger.Debug("更新工具调用参数",
	// 	logger.String("tool_id", toolID),
	// 	logger.Any("arguments", arguments))

	// 检查活跃工具
	if execution, exists := tlm.activeTools[toolID]; exists {
		execution.Arguments = arguments
		// logger.Debug("已更新活跃工具的参数",
		// 	logger.String("tool_id", toolID),
		// 	logger.String("tool_name", execution.Name))
		return
	}

	// 检查已完成工具
	if execution, exists := tlm.completedTools[toolID]; exists {
		execution.Arguments = arguments
		// logger.Debug("已更新已完成工具的参数",
		// 	logger.String("tool_id", toolID),
		// 	logger.String("tool_name", execution.Name))
		return
	}

	logger.Warn("未找到要更新参数的工具",
		logger.String("tool_id", toolID))
}

// UpdateToolArgumentsFromJSON 从JSON字符串更新工具调用参数
func (tlm *ToolLifecycleManager) UpdateToolArgumentsFromJSON(toolID string, jsonArgs string) {
	var arguments map[string]any
	if err := utils.SafeUnmarshal([]byte(jsonArgs), &arguments); err != nil {
		logger.Warn("解析工具参数JSON失败",
			logger.String("tool_id", toolID),
			logger.String("json", jsonArgs),
			logger.Err(err))
		return
	}

	tlm.UpdateToolArguments(toolID, arguments)
}

// ValidateToolPairing 验证工具调用和工具结果的配对关系
// 修复: 验证并过滤工具配对以移除孤立结果
// 参考: kiro.rs 2026.1.4 - feat: 验证并过滤工具配对以移除孤立结果
func (tlm *ToolLifecycleManager) ValidateToolPairing(toolResults []map[string]any) []map[string]any {
	if len(toolResults) == 0 {
		return toolResults
	}

	validResults := make([]map[string]any, 0, len(toolResults))
	orphanedCount := 0

	for _, result := range toolResults {
		toolUseID, ok := result["tool_use_id"].(string)
		if !ok || toolUseID == "" {
			logger.Warn("工具结果缺少 tool_use_id，跳过",
				logger.Any("result", result))
			orphanedCount++
			continue
		}

		// 检查是否存在对应的工具调用
		execution := tlm.GetToolExecution(toolUseID)
		if execution == nil {
			logger.Warn("发现孤立的工具结果，对应的工具调用不存在",
				logger.String("tool_use_id", toolUseID))
			orphanedCount++
			continue
		}

		// 验证通过，保留此结果
		validResults = append(validResults, result)
	}

	if orphanedCount > 0 {
		logger.Info("工具配对验证完成，已移除孤立结果",
			logger.Int("total_results", len(toolResults)),
			logger.Int("valid_results", len(validResults)),
			logger.Int("orphaned_results", orphanedCount))
	}

	return validResults
}

// HasToolCall 检查是否存在指定的工具调用
func (tlm *ToolLifecycleManager) HasToolCall(toolID string) bool {
	_, activeExists := tlm.activeTools[toolID]
	_, completedExists := tlm.completedTools[toolID]
	return activeExists || completedExists
}
