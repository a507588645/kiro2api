package parser

import (
	"strings"
	"sync"
)

// ThinkingState 状态机状态枚举（借鉴 kiro.rs）
type ThinkingState int

const (
	// StateNotInThinking 未进入 thinking 块
	StateNotInThinking ThinkingState = iota
	// StateInThinking 在 thinking 块内
	StateInThinking
	// StateThinkingExtracted thinking 已提取完成
	StateThinkingExtracted
)

// ProcessChunkResult 处理结果
type ProcessChunkResult struct {
	ThinkingContent string // thinking 内容
	TextContent     string // 文本内容
	ThinkingStarted bool   // thinking 块是否开始
	ThinkingEnded   bool   // thinking 块是否结束
}

// ThinkingStreamContext Thinking 流式上下文（借鉴 kiro.rs StreamContext）
type ThinkingStreamContext struct {
	mu sync.Mutex

	// 配置
	ThinkingEnabled bool

	// 状态
	state             ThinkingState
	buffer            strings.Builder
	ThinkingExtracted bool

	// 块索引管理（借鉴 kiro.rs）
	ThinkingBlockIndex *int // 通常为 0
	TextBlockIndex     *int // thinking 启用时为 1
	nextBlockIndex     int

	// 检测器
	detector *ThinkingTagDetector
}

// NewThinkingStreamContext 创建 thinking 流式上下文
func NewThinkingStreamContext(thinkingEnabled bool) *ThinkingStreamContext {
	ctx := &ThinkingStreamContext{
		ThinkingEnabled: thinkingEnabled,
		state:           StateNotInThinking,
		nextBlockIndex:  0,
		detector:        NewThinkingTagDetector(),
	}

	if thinkingEnabled {
		// thinking 启用时，thinking 块索引为 0，文本块索引为 1
		thinkingIdx := 0
		textIdx := 1
		ctx.ThinkingBlockIndex = &thinkingIdx
		ctx.TextBlockIndex = &textIdx
		ctx.nextBlockIndex = 2 // 下一个可用索引
	}

	return ctx
}

// Reset 重置状态
func (ctx *ThinkingStreamContext) Reset() {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	ctx.buffer.Reset()
	ctx.state = StateNotInThinking
	ctx.ThinkingExtracted = false
	ctx.nextBlockIndex = 0
	if ctx.ThinkingEnabled {
		ctx.nextBlockIndex = 2
	}
}

// ProcessChunk 处理流式数据块
func (ctx *ThinkingStreamContext) ProcessChunk(chunk string) ProcessChunkResult {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	result := ProcessChunkResult{}

	if !ctx.ThinkingEnabled {
		// thinking 未启用，直接返回文本内容
		result.TextContent = chunk
		return result
	}

	// 将新数据添加到缓冲区
	ctx.buffer.WriteString(chunk)
	bufferStr := ctx.buffer.String()

	switch ctx.state {
	case StateNotInThinking:
		result = ctx.processNotInThinking(bufferStr)
	case StateInThinking:
		result = ctx.processInThinking(bufferStr)
	case StateThinkingExtracted:
		// thinking 已提取完成，后续内容都是文本
		result.TextContent = bufferStr
		ctx.buffer.Reset()
	}

	return result
}

// processNotInThinking 处理未进入 thinking 块的状态
func (ctx *ThinkingStreamContext) processNotInThinking(buffer string) ProcessChunkResult {
	result := ProcessChunkResult{}

	startIdx := ctx.detector.FindRealThinkingStartTag(buffer)
	if startIdx != -1 {
		// 找到开始标签
		ctx.state = StateInThinking
		result.ThinkingStarted = true

		// 开始标签之前的内容作为文本
		if startIdx > 0 {
			result.TextContent = buffer[:startIdx]
		}

		// 更新缓冲区，移除已处理的部分
		ctx.buffer.Reset()
		ctx.buffer.WriteString(buffer[startIdx+len(thinkingStartTag):])
	} else {
		// 没有找到开始标签，安全输出部分内容
		// 保留可能是部分标签的内容
		safeLen := len(buffer) - len(thinkingStartTag) + 1
		if safeLen > 0 {
			safeBoundary := FindCharBoundary(buffer, safeLen)
			if safeBoundary > 0 {
				result.TextContent = buffer[:safeBoundary]
				ctx.buffer.Reset()
				ctx.buffer.WriteString(buffer[safeBoundary:])
			}
		}
	}

	return result
}

// processInThinking 处理在 thinking 块内的状态
func (ctx *ThinkingStreamContext) processInThinking(buffer string) ProcessChunkResult {
	result := ProcessChunkResult{}

	endIdx := ctx.detector.FindRealThinkingEndTag(buffer)
	if endIdx != -1 {
		// 找到结束标签
		result.ThinkingContent = buffer[:endIdx]
		ctx.state = StateThinkingExtracted
		ctx.ThinkingExtracted = true
		result.ThinkingEnded = true

		// 结束标签之后的内容作为文本
		afterEnd := endIdx + len(thinkingEndTag)
		if afterEnd < len(buffer) {
			remaining := buffer[afterEnd:]
			// 跳过 \n\n
			if strings.HasPrefix(remaining, "\n\n") {
				remaining = remaining[2:]
			} else if strings.HasPrefix(remaining, "\n") {
				remaining = remaining[1:]
			}
			result.TextContent = remaining
		}

		// 清空缓冲区
		ctx.buffer.Reset()
	} else {
		// 没有找到结束标签，流式输出 thinking 内容
		// 保留可能是部分标签的内容
		safeLen := len(buffer) - len(thinkingEndTag) + 1
		if safeLen > 0 {
			safeBoundary := FindCharBoundary(buffer, safeLen)
			if safeBoundary > 0 {
				result.ThinkingContent = buffer[:safeBoundary]
				ctx.buffer.Reset()
				ctx.buffer.WriteString(buffer[safeBoundary:])
			}
		}
	}

	return result
}

// GetThinkingBlockIndex 获取 thinking 块索引
func (ctx *ThinkingStreamContext) GetThinkingBlockIndex() int {
	if ctx.ThinkingBlockIndex != nil {
		return *ctx.ThinkingBlockIndex
	}
	return 0
}

// GetTextBlockIndex 获取文本块索引
func (ctx *ThinkingStreamContext) GetTextBlockIndex() int {
	if ctx.TextBlockIndex != nil {
		return *ctx.TextBlockIndex
	}
	return 0
}

// AllocateBlockIndex 分配新的块索引
func (ctx *ThinkingStreamContext) AllocateBlockIndex() int {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	idx := ctx.nextBlockIndex
	ctx.nextBlockIndex++
	return idx
}

// IsInThinkingBlock 检查是否在 thinking 块内
func (ctx *ThinkingStreamContext) IsInThinkingBlock() bool {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	return ctx.state == StateInThinking
}

// IsThinkingExtracted 检查 thinking 是否已提取
func (ctx *ThinkingStreamContext) IsThinkingExtracted() bool {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	return ctx.ThinkingExtracted
}

// GetState 获取当前状态
func (ctx *ThinkingStreamContext) GetState() ThinkingState {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	return ctx.state
}

// FlushBuffer 刷新缓冲区，返回剩余内容
func (ctx *ThinkingStreamContext) FlushBuffer() ProcessChunkResult {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	result := ProcessChunkResult{}
	bufferStr := ctx.buffer.String()

	if bufferStr == "" {
		return result
	}

	switch ctx.state {
	case StateInThinking:
		// 仍在 thinking 块内，输出剩余内容作为 thinking
		result.ThinkingContent = bufferStr
	default:
		// 其他状态，输出剩余内容作为文本
		result.TextContent = bufferStr
	}

	ctx.buffer.Reset()
	return result
}
