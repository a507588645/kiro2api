package server

import (
	"errors"
	"fmt"
	"kiro2api/logger"
	"kiro2api/parser"
	"strings"

	"github.com/gin-gonic/gin"
)

// BlockState 内容块状态
type BlockState struct {
	Index     int    `json:"index"`
	Type      string `json:"type"` // "text" | "tool_use" | "thinking"
	Started   bool   `json:"started"`
	Stopped   bool   `json:"stopped"`
	ToolUseID string `json:"tool_use_id,omitempty"` // 仅用于工具块
}

// ThinkingTagConstants thinking标签常量
const (
	thinkingStartTag = "<thinking>"
	thinkingEndTag   = "</thinking>"
)

// SSEStateManager SSE事件状态管理器，确保事件序列符合Claude规范
type SSEStateManager struct {
	messageStarted   bool
	messageDeltaSent bool // 新增：跟踪message_delta是否已发送
	activeBlocks     map[int]*BlockState
	messageEnded     bool
	nextBlockIndex   int
	strictMode       bool

	// thinking状态跟踪
	inThinking                     bool   // 是否正在thinking块内
	thinkingBuffer                 string // 用于缓存和检测thinking标签
	thinkingBlockIndex             int    // thinking块的索引
	thinkingBlockStarted           bool   // thinking块是否已启动
	textBlockStartedAfterThinking  bool   // thinking结束后text块是否已启动
	textBlockIndexAfterThinking    int    // thinking结束后text块的索引
}

// NewSSEStateManager 创建SSE状态管理器
func NewSSEStateManager(strictMode bool) *SSEStateManager {
	return &SSEStateManager{
		activeBlocks: make(map[int]*BlockState),
		strictMode:   strictMode,
	}
}

// Reset 重置状态管理器
func (ssm *SSEStateManager) Reset() {
	ssm.messageStarted = false
	ssm.messageDeltaSent = false // 重置message_delta发送状态
	ssm.messageEnded = false
	ssm.activeBlocks = make(map[int]*BlockState)
	ssm.nextBlockIndex = 0

	// 重置thinking状态
	ssm.inThinking = false
	ssm.thinkingBuffer = ""
	ssm.thinkingBlockIndex = 0
	ssm.thinkingBlockStarted = false
	ssm.textBlockStartedAfterThinking = false
	ssm.textBlockIndexAfterThinking = 0
}

// SendEvent 受控的事件发送，确保符合Claude规范
func (ssm *SSEStateManager) SendEvent(c *gin.Context, sender StreamEventSender, eventData map[string]any) error {
	eventType, ok := eventData["type"].(string)
	if !ok {
		return errors.New("无效的事件类型")
	}

	// 状态验证和处理
	switch eventType {
	case "message_start":
		return ssm.handleMessageStart(c, sender, eventData)
	case "content_block_start":
		return ssm.handleContentBlockStart(c, sender, eventData)
	case "content_block_delta":
		return ssm.handleContentBlockDelta(c, sender, eventData)
	case "content_block_stop":
		return ssm.handleContentBlockStop(c, sender, eventData)
	case "message_delta":
		return ssm.handleMessageDelta(c, sender, eventData)
	case "message_stop":
		return ssm.handleMessageStop(c, sender, eventData)
	default:
		// 其他事件直接转发
		return sender.SendEvent(c, eventData)
	}
}

// handleMessageStart 处理消息开始事件
func (ssm *SSEStateManager) handleMessageStart(c *gin.Context, sender StreamEventSender, eventData map[string]any) error {
	if ssm.messageStarted {
		errMsg := "违规：message_start只能出现一次"
		logger.Error(errMsg)
		if ssm.strictMode {
			return errors.New(errMsg)
		}
		return nil // 非严格模式下跳过重复的message_start
	}

	ssm.messageStarted = true
	return sender.SendEvent(c, eventData)
}

// handleContentBlockStart 处理内容块开始事件
func (ssm *SSEStateManager) handleContentBlockStart(c *gin.Context, sender StreamEventSender, eventData map[string]any) error {
	if !ssm.messageStarted {
		errMsg := "违规：content_block_start必须在message_start之后"
		logger.Error(errMsg)
		if ssm.strictMode {
			return errors.New(errMsg)
		}
	}

	if ssm.messageEnded {
		errMsg := "违规：message已结束，不能发送content_block_start"
		logger.Error(errMsg)
		if ssm.strictMode {
			return errors.New(errMsg)
		}
		return nil
	}

	// 提取块索引
	index, ok := eventData["index"].(int)
	if !ok {
		if indexFloat, ok := eventData["index"].(float64); ok {
			index = int(indexFloat)
		} else {
			index = ssm.nextBlockIndex
		}
	}

	// 检查是否重复启动同一块
	if block, exists := ssm.activeBlocks[index]; exists && block.Started && !block.Stopped {
		// *** 修复：宽松处理重复启动事件 ***
		// 场景：上游可能重复发送content_block_start事件
		// 策略：静默跳过，避免中断流处理
		logger.Debug("跳过重复的content_block_start事件",
			logger.Int("block_index", index),
			logger.String("block_type", block.Type))
		return nil // 跳过重复的start
	}

	// 确定块类型
	blockType := "text"
	if contentBlock, ok := eventData["content_block"].(map[string]any); ok {
		if cbType, ok := contentBlock["type"].(string); ok {
			blockType = cbType
		}
	}

	// *** 关键修复：在启动新工具块前，自动关闭文本块 ***
	// 问题场景：AWS上游在工具调用(index:1+)期间仍发送文本内容给index:0
	// 如果不在此时关闭index:0，会导致事件序列混乱：
	// - index:0 started
	// - index:1 started (工具块)
	// - index:0 delta (违规！index:0未关闭)
	// - index:1 stop
	// - index:0 stop (延迟关闭)
	//
	// 修复策略：当检测到新工具块启动时，自动关闭所有未关闭的文本块
	if blockType == "tool_use" {
		// 遍历所有活跃块，找到未关闭的文本块
		for blockIndex, block := range ssm.activeBlocks {
			if block.Type == "text" && block.Started && !block.Stopped {
				// 自动发送content_block_stop来关闭文本块
				stopEvent := map[string]any{
					"type":  "content_block_stop",
					"index": blockIndex,
				}
				logger.Debug("工具块启动前自动关闭文本块",
					logger.Int("text_block_index", blockIndex),
					logger.Int("new_tool_block_index", index),
					logger.String("reason", "prevent_event_interleaving"))

				// 立即发送stop事件（在工具块start之前）
				if err := sender.SendEvent(c, stopEvent); err != nil {
					logger.Error("自动关闭文本块失败", logger.Err(err), logger.Int("index", blockIndex))
				} else {
					// 标记文本块已关闭
					block.Stopped = true
					logger.Debug("文本块已自动关闭", logger.Int("index", blockIndex))
				}
			}
		}

		// *** 新增修复：重置thinking状态，避免后续text_delta被错误重定向 ***
		// 问题场景：thinking结束后启动tool_use块，但thinking状态未重置
		// 导致后续text_delta被错误地重定向到thinking后的text块索引
		if ssm.thinkingBlockStarted {
			logger.Debug("工具块启动时重置thinking状态",
				logger.Int("tool_block_index", index),
				logger.Bool("was_in_thinking", ssm.inThinking),
				logger.Bool("text_block_started", ssm.textBlockStartedAfterThinking))

			ssm.thinkingBlockStarted = false
			ssm.inThinking = false
			ssm.thinkingBuffer = ""
			ssm.textBlockStartedAfterThinking = false
			ssm.textBlockIndexAfterThinking = 0
		}
	}

	// 创建或更新块状态
	toolUseID := ""
	if blockType == "tool_use" {
		if contentBlock, ok := eventData["content_block"].(map[string]any); ok {
			if id, ok := contentBlock["id"].(string); ok {
				toolUseID = id
			}
		}
	}

	ssm.activeBlocks[index] = &BlockState{
		Index:     index,
		Type:      blockType,
		Started:   true,
		Stopped:   false,
		ToolUseID: toolUseID,
	}

	if index >= ssm.nextBlockIndex {
		ssm.nextBlockIndex = index + 1
	}

	// logger.Debug("内容块已启动",
	// 	logger.Int("index", index),
	// 	logger.String("type", blockType),
	// 	logger.String("tool_use_id", toolUseID))

	return sender.SendEvent(c, eventData)
}

// handleContentBlockDelta 处理内容块增量事件
func (ssm *SSEStateManager) handleContentBlockDelta(c *gin.Context, sender StreamEventSender, eventData map[string]any) error {
	index, ok := eventData["index"].(int)
	if !ok {
		if indexFloat, ok := eventData["index"].(float64); ok {
			index = int(indexFloat)
		} else {
			errMsg := "content_block_delta缺少有效索引"
			logger.Error(errMsg)
			if ssm.strictMode {
				return errors.New(errMsg)
			}
			return nil
		}
	}

	// *** 关键修复：检测并处理thinking标签 ***
	// 从delta中提取文本内容
	var deltaText string
	var deltaType string
	if delta, ok := eventData["delta"].(map[string]any); ok {
		if dt, ok := delta["type"].(string); ok {
			deltaType = dt
		}
		// 尝试从text字段获取内容
		if text, ok := delta["text"].(string); ok {
			deltaText = text
		}
	}

	// 将内容添加到thinking缓冲区进行检测
	if deltaType == "text_delta" && deltaText != "" {
		ssm.thinkingBuffer += deltaText

		// 检测thinking开始标签
		if !ssm.inThinking && strings.HasPrefix(ssm.thinkingBuffer, thinkingStartTag) {
			logger.Debug("检测到thinking开始标签",
				logger.String("buffer_preview", ssm.thinkingBuffer[:min(50, len(ssm.thinkingBuffer))]))

			ssm.inThinking = true
			// 去掉<thinking>标签
			ssm.thinkingBuffer = ssm.thinkingBuffer[len(thinkingStartTag):]

			// 如果thinking块未启动，启动thinking块
			if !ssm.thinkingBlockStarted {
				ssm.thinkingBlockIndex = index
				ssm.thinkingBlockStarted = true

				// 发送thinking类型的content_block_start
				thinkingStartEvent := map[string]any{
					"type":  "content_block_start",
					"index": index,
					"content_block": map[string]any{
						"type":     "thinking",
						"thinking": "",
					},
				}
				if err := ssm.handleContentBlockStart(c, sender, thinkingStartEvent); err != nil {
					return err
				}
				logger.Debug("已启动thinking块", logger.Int("index", index))
			}
		}

		// 如果在thinking块内，检测thinking结束标签
		if ssm.inThinking {
			endIdx := strings.Index(ssm.thinkingBuffer, thinkingEndTag)
			if endIdx != -1 {
				// 找到结束标签
				thinkingContent := ssm.thinkingBuffer[:endIdx]
				afterThinking := ssm.thinkingBuffer[endIdx+len(thinkingEndTag):]

				logger.Debug("检测到thinking结束标签",
					logger.Int("thinking_content_len", len(thinkingContent)),
					logger.Int("after_thinking_len", len(afterThinking)))

				// 发送剩余的thinking内容
				if thinkingContent != "" {
					thinkingDeltaEvent := map[string]any{
						"type":  "content_block_delta",
						"index": ssm.thinkingBlockIndex,
						"delta": map[string]any{
							"type":     "thinking_delta",
							"thinking": thinkingContent,
						},
					}
					if err := sender.SendEvent(c, thinkingDeltaEvent); err != nil {
						return err
					}
				}

				// 关闭thinking块
				thinkingStopEvent := map[string]any{
					"type":  "content_block_stop",
					"index": ssm.thinkingBlockIndex,
				}
				if err := ssm.handleContentBlockStop(c, sender, thinkingStopEvent); err != nil {
					return err
				}
				logger.Debug("已关闭thinking块", logger.Int("index", ssm.thinkingBlockIndex))

				// 重置thinking状态
				ssm.inThinking = false
				ssm.thinkingBuffer = ""

				// 处理thinking结束后的文本内容
				// 跳过可能的换行符
				afterThinking = strings.TrimPrefix(afterThinking, "\n\n")
				afterThinking = strings.TrimPrefix(afterThinking, "\n")

				if afterThinking != "" {
					// 启动新的text块
					ssm.textBlockIndexAfterThinking = ssm.nextBlockIndex
					textStartEvent := map[string]any{
						"type":  "content_block_start",
						"index": ssm.textBlockIndexAfterThinking,
						"content_block": map[string]any{
							"type": "text",
							"text": "",
						},
					}
					if err := ssm.handleContentBlockStart(c, sender, textStartEvent); err != nil {
						return err
					}
					ssm.textBlockStartedAfterThinking = true
					logger.Debug("已启动thinking后的text块", logger.Int("index", ssm.textBlockIndexAfterThinking))

					// 发送text内容
					textDeltaEvent := map[string]any{
						"type":  "content_block_delta",
						"index": ssm.textBlockIndexAfterThinking,
						"delta": map[string]any{
							"type": "text_delta",
							"text": afterThinking,
						},
					}
					return sender.SendEvent(c, textDeltaEvent)
				}
				return nil
			}

			// 没有找到结束标签，流式输出thinking内容
			// 保留可能是部分标签的内容
			safeLen := len(ssm.thinkingBuffer) - len(thinkingEndTag) + 1
			if safeLen > 0 {
				// 使用 FindCharBoundary 确保不会截断多字节字符
				safeLen = parser.FindCharBoundary(ssm.thinkingBuffer, safeLen)
				if safeLen <= 0 {
					return nil
				}
				safeContent := ssm.thinkingBuffer[:safeLen]
				ssm.thinkingBuffer = ssm.thinkingBuffer[safeLen:]

				// 确保thinking块已启动
				if !ssm.thinkingBlockStarted {
					ssm.thinkingBlockIndex = index
					ssm.thinkingBlockStarted = true

					thinkingStartEvent := map[string]any{
						"type":  "content_block_start",
						"index": index,
						"content_block": map[string]any{
							"type":     "thinking",
							"thinking": "",
						},
					}
					if err := ssm.handleContentBlockStart(c, sender, thinkingStartEvent); err != nil {
						return err
					}
				}

				// 发送thinking_delta
				thinkingDeltaEvent := map[string]any{
					"type":  "content_block_delta",
					"index": ssm.thinkingBlockIndex,
					"delta": map[string]any{
						"type":     "thinking_delta",
						"thinking": safeContent,
					},
				}
				return sender.SendEvent(c, thinkingDeltaEvent)
			}
			return nil
		}

		// 不在thinking块内，检查是否可能是部分thinking开始标签
		if !ssm.inThinking && !ssm.thinkingBlockStarted {
			// 检查缓冲区是否可能包含不完整的thinking开始标签
			for i := 1; i < len(thinkingStartTag); i++ {
				if strings.HasSuffix(ssm.thinkingBuffer, thinkingStartTag[:i]) {
					// 可能是部分标签，等待更多数据
					return nil
				}
			}

			// 不是thinking内容，作为普通text处理
			// 清空缓冲区并发送原始事件
			ssm.thinkingBuffer = ""
		}

		// thinking已结束，后续内容作为text处理
		// *** 修复：只处理来自原始索引的text_delta，避免错误重定向其他索引的内容 ***
		if ssm.thinkingBlockStarted && !ssm.inThinking && index == ssm.thinkingBlockIndex {
			logger.Debug("thinking已结束，处理后续text内容",
				logger.Bool("textBlockStartedAfterThinking", ssm.textBlockStartedAfterThinking),
				logger.Int("textBlockIndexAfterThinking", ssm.textBlockIndexAfterThinking),
				logger.Int("bufferLen", len(ssm.thinkingBuffer)),
				logger.Int("current_index", index))

			// 如果text块还没启动，需要启动
			if !ssm.textBlockStartedAfterThinking {
				ssm.textBlockIndexAfterThinking = ssm.nextBlockIndex
				textStartEvent := map[string]any{
					"type":  "content_block_start",
					"index": ssm.textBlockIndexAfterThinking,
					"content_block": map[string]any{
						"type": "text",
						"text": "",
					},
				}
				if err := ssm.handleContentBlockStart(c, sender, textStartEvent); err != nil {
					return err
				}
				ssm.textBlockStartedAfterThinking = true
				logger.Debug("已启动thinking后的text块（延迟启动）", logger.Int("index", ssm.textBlockIndexAfterThinking))
			}

			// 发送text_delta事件，使用缓冲区中的内容
			textContent := ssm.thinkingBuffer
			ssm.thinkingBuffer = ""

			if textContent != "" {
				textDeltaEvent := map[string]any{
					"type":  "content_block_delta",
					"index": ssm.textBlockIndexAfterThinking,
					"delta": map[string]any{
						"type": "text_delta",
						"text": textContent,
					},
				}
				logger.Debug("发送thinking后的text_delta",
					logger.Int("index", ssm.textBlockIndexAfterThinking),
					logger.Int("textLen", len(textContent)))
				return sender.SendEvent(c, textDeltaEvent)
			}
			return nil
		}
	}

	// 检查块是否已启动，如果没有则自动启动（遵循Claude规范的动态启动）
	block, exists := ssm.activeBlocks[index]
	if !exists || !block.Started {
		logger.Debug("检测到content_block_delta但块未启动，自动生成content_block_start",
			logger.Int("block_index", index))

		// 推断块类型：检查delta内容来确定类型
		blockType := "text" // 默认为文本块
		if delta, ok := eventData["delta"].(map[string]any); ok {
			if dt, ok := delta["type"].(string); ok {
				if dt == "input_json_delta" {
					blockType = "tool_use"
				}
			}
		}

		// 自动生成并发送content_block_start事件
		startEvent := map[string]any{
			"type":  "content_block_start",
			"index": index,
			"content_block": map[string]any{
				"type": blockType,
			},
		}

		switch blockType {
		case "text":
			startEvent["content_block"].(map[string]any)["text"] = ""
		case "tool_use":
			// 为工具使用块添加必要字段
			startEvent["content_block"].(map[string]any)["id"] = fmt.Sprintf("tooluse_auto_%d", index)
			startEvent["content_block"].(map[string]any)["name"] = "auto_detected"
			startEvent["content_block"].(map[string]any)["input"] = map[string]any{}
		}

		// 先处理start事件来更新状态
		if err := ssm.handleContentBlockStart(c, sender, startEvent); err != nil {
			return err
		}

		// 重新获取更新后的block状态
		block = ssm.activeBlocks[index]
	}

	if block != nil && block.Stopped {
		// *** 修复：宽松处理已停止块的delta事件 ***
		// 场景：上游在工具块启动后仍发送文本块delta，但文本块已被自动关闭
		// 策略：静默跳过，避免中断流处理
		logger.Debug("跳过已停止块的delta事件",
			logger.Int("block_index", index),
			logger.String("block_type", block.Type))
		return nil
	}

	return sender.SendEvent(c, eventData)
}

// handleContentBlockStop 处理内容块停止事件
func (ssm *SSEStateManager) handleContentBlockStop(c *gin.Context, sender StreamEventSender, eventData map[string]any) error {
	index, ok := eventData["index"].(int)
	if !ok {
		if indexFloat, ok := eventData["index"].(float64); ok {
			index = int(indexFloat)
		} else {
			errMsg := "content_block_stop缺少有效索引"
			logger.Error(errMsg)
			if ssm.strictMode {
				return errors.New(errMsg)
			}
			return nil
		}
	}

	// 验证块状态
	block, exists := ssm.activeBlocks[index]
	if !exists || !block.Started {
		errMsg := fmt.Sprintf("违规：索引%d的content_block未启动就发送stop", index)
		logger.Error(errMsg, logger.Int("block_index", index))
		if ssm.strictMode {
			return errors.New(errMsg)
		}
		return nil
	}

	if block.Stopped {
		errMsg := fmt.Sprintf("违规：索引%d的content_block重复停止", index)
		logger.Error(errMsg, logger.Int("block_index", index))
		if ssm.strictMode {
			return errors.New(errMsg)
		}
		return nil
	}

	// 标记为已停止
	block.Stopped = true

	return sender.SendEvent(c, eventData)
}

// handleMessageDelta 处理消息增量事件
func (ssm *SSEStateManager) handleMessageDelta(c *gin.Context, sender StreamEventSender, eventData map[string]any) error {
	if !ssm.messageStarted {
		errMsg := "违规：message_delta必须在message_start之后"
		logger.Error(errMsg)
		if ssm.strictMode {
			return errors.New(errMsg)
		}
	}

	// *** 关键修复：防止重复的message_delta事件 ***
	// 根据Claude规范，message_delta在一次消息中只能出现一次
	if ssm.messageDeltaSent {
		errMsg := "违规：message_delta只能出现一次"
		logger.Error(errMsg,
			logger.Bool("message_started", ssm.messageStarted),
			logger.Bool("message_delta_sent", ssm.messageDeltaSent),
			logger.Bool("message_ended", ssm.messageEnded))
		if ssm.strictMode {
			return errors.New(errMsg)
		}
		logger.Debug("跳过重复的message_delta事件")
		return nil // 非严格模式下跳过重复的message_delta
	}

	// *** 关键修复：在发送message_delta之前，确保所有content_block都已关闭 ***
	// 根据Claude规范，message_delta必须在所有content_block_stop之后发送
	var unclosedBlocks []int
	for index, block := range ssm.activeBlocks {
		if block.Started && !block.Stopped {
			unclosedBlocks = append(unclosedBlocks, index)
		}
	}

	if len(unclosedBlocks) > 0 {
		logger.Debug("message_delta前自动关闭未关闭的content_block",
			logger.Any("unclosed_blocks", unclosedBlocks))
		// 在非严格模式下，自动关闭未关闭的块
		if !ssm.strictMode {
			for _, index := range unclosedBlocks {
				stopEvent := map[string]any{
					"type":  "content_block_stop",
					"index": index,
				}
				sender.SendEvent(c, stopEvent)
				ssm.activeBlocks[index].Stopped = true
				logger.Debug("自动关闭未关闭的content_block（message_delta前）", logger.Int("index", index))
			}
		}
	}

	// 标记message_delta已发送，防止后续重复发送
	ssm.messageDeltaSent = true

	return sender.SendEvent(c, eventData)
}

// handleMessageStop 处理消息停止事件
func (ssm *SSEStateManager) handleMessageStop(c *gin.Context, sender StreamEventSender, eventData map[string]any) error {
	if !ssm.messageStarted {
		errMsg := "违规：message_stop必须在message_start之后"
		logger.Error(errMsg)
		if ssm.strictMode {
			return errors.New(errMsg)
		}
	}

	if ssm.messageEnded {
		errMsg := "违规：message_stop只能出现一次"
		logger.Error(errMsg)
		if ssm.strictMode {
			return errors.New(errMsg)
		}
		return nil
	}

	// 注意：未关闭的content_block检查已移至handleMessageDelta中
	// 确保符合Claude规范：所有content_block_stop必须在message_delta之前发送

	ssm.messageEnded = true
	return sender.SendEvent(c, eventData)
}

// GetActiveBlocks 获取所有活跃块
func (ssm *SSEStateManager) GetActiveBlocks() map[int]*BlockState {
	return ssm.activeBlocks
}

// IsMessageStarted 检查消息是否已开始
func (ssm *SSEStateManager) IsMessageStarted() bool {
	return ssm.messageStarted
}

// IsMessageEnded 检查消息是否已结束
func (ssm *SSEStateManager) IsMessageEnded() bool {
	return ssm.messageEnded
}

// IsMessageDeltaSent 检查message_delta是否已发送
func (ssm *SSEStateManager) IsMessageDeltaSent() bool {
	return ssm.messageDeltaSent
}
