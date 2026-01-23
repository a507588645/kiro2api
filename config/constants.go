package config

import "time"

// Token管理常量
const (
	// TokenCacheKeyFormat token缓存key格式
	TokenCacheKeyFormat = "token_%d"

	// TokenRefreshCleanupDelay token刷新完成后的清理延迟
	TokenRefreshCleanupDelay = 5 * time.Second
)

// 消息处理常量
const (
	// MessageIDFormat 消息ID格式
	MessageIDFormat = "msg_%s"

	// MessageIDTimeFormat 消息ID时间格式
	MessageIDTimeFormat = "20060102150405"

	// RetryDelay 重试延迟
	RetryDelay = 100 * time.Millisecond
)

// Token估算常量
const (
	// BaseToolsOverhead 基础工具开销（tokens）
	BaseToolsOverhead = 100

	// ShortTextThreshold 短文本阈值（字符数）
	ShortTextThreshold = 100

	// LongTextThreshold 长文本阈值（字符数）
	LongTextThreshold = 1000
)

// EventStream解析器常量
const (
	// EventStreamMinMessageSize AWS EventStream最小消息长度（字节）
	EventStreamMinMessageSize = 16

	// EventStreamMaxMessageSize AWS EventStream最大消息长度（16MB）
	EventStreamMaxMessageSize = 16 * 1024 * 1024
)

// Token计算常量
const (
	// ToolCallTokenOverhead 工具调用的token开销系数
	ToolCallTokenOverhead = 1.2

	// TokenEstimationRatio 字符到token的估算比例
	TokenEstimationRatio = 4

	// MinOutputTokens 最小输出token数
	MinOutputTokens = 1
)

// Thinking 模式常量（借鉴 kiro.rs）
const (
	// ThinkingBudgetTokensMax thinking budget_tokens 上限
	ThinkingBudgetTokensMax = 24576

	// ThinkingBudgetTokensDefault thinking budget_tokens 默认值
	ThinkingBudgetTokensDefault = 20000

	// ThinkingBudgetTokensMin thinking budget_tokens 最小值
	ThinkingBudgetTokensMin = 1024
)

// 请求体大小限制常量（借鉴 kiro.rs 2026.1.6 - 解决图片上传问题）
const (
	// MaxRequestBodySize 最大请求体大小（100MB）
	// 用于支持大图片上传（base64 编码会使图片大小增加约 33%）
	MaxRequestBodySize = 100 * 1024 * 1024

	// MaxMultipartMemory 最大 multipart 内存（100MB）
	MaxMultipartMemory = 100 * 1024 * 1024
)
