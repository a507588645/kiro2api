package utils

import (
	"strings"

	"kiro2api/config"
	"kiro2api/types"
)

// TokenEstimator 本地token估算器
// 设计原则：
// - KISS: 简单高效的估算算法，避免引入复杂的tokenizer库
// - 向后兼容: 支持所有Claude模型和消息格式
// - 性能优先: 本地计算，响应时间<5ms
// - 借鉴 kiro.rs: 使用字符单位计算和短文本修正系数
type TokenEstimator struct{}

// isNonWesternChar 判断字符是否为非西文字符（借鉴 kiro.rs）
// 西文字符包括：
// - ASCII 字符 (U+0000..U+007F)
// - 拉丁字母扩展 (U+0080..U+024F)
// - 拉丁字母扩展附加 (U+1E00..U+1EFF)
// - 拉丁字母扩展-C/D/E
// 返回 true 表示该字符是非西文字符（如中文、日文、韩文、阿拉伯文等）
func isNonWesternChar(r rune) bool {
	// 西文字符范围
	switch {
	case r >= 0x0000 && r <= 0x007F: // 基本 ASCII
		return false
	case r >= 0x0080 && r <= 0x00FF: // 拉丁字母扩展-A
		return false
	case r >= 0x0100 && r <= 0x024F: // 拉丁字母扩展-B
		return false
	case r >= 0x1E00 && r <= 0x1EFF: // 拉丁字母扩展附加
		return false
	case r >= 0x2C60 && r <= 0x2C7F: // 拉丁字母扩展-C
		return false
	case r >= 0xA720 && r <= 0xA7FF: // 拉丁字母扩展-D
		return false
	case r >= 0xAB30 && r <= 0xAB6F: // 拉丁字母扩展-E
		return false
	default:
		return true // 非西文字符
	}
}

// NewTokenEstimator 创建token估算器实例
func NewTokenEstimator() *TokenEstimator {
	return &TokenEstimator{}
}

// EstimateTokens 估算消息的token数量
// 算法说明：
// - 基础估算: 英文平均4字符/token，中文平均1.5字符/token
// - 固定开销: 消息角色标记、JSON结构等
// - 工具开销: 每个工具定义约50-200 tokens
//
// 注意：此为快速估算，与官方tokenizer可能有±10%误差
func (e *TokenEstimator) EstimateTokens(req *types.CountTokensRequest) int {
	totalTokens := 0

	// 1. 系统提示词（system prompt）
	for _, sysMsg := range req.System {
		if sysMsg.Text != "" {
			totalTokens += e.EstimateTextTokens(sysMsg.Text)
			totalTokens += 2 // 系统提示的固定开销（P0优化：从3降至2）
		}
	}

	// 2. 消息内容（messages）
	for _, msg := range req.Messages {
		// 角色标记开销（"user"/"assistant" + JSON结构）
		// 优化：根据官方测试调整
		totalTokens += 3

		// 消息内容
		switch content := msg.Content.(type) {
		case string:
			// 文本消息
			totalTokens += e.EstimateTextTokens(content)
		case []any:
			// 复杂内容块（文本、图片、文档等）
			for _, block := range content {
				totalTokens += e.estimateContentBlock(block)
			}
		case []types.ContentBlock:
			// 类型化内容块
			for _, block := range content {
				totalTokens += e.estimateTypedContentBlock(block)
			}
		default:
			// 其他格式：保守估算为JSON长度
			if jsonBytes, err := SafeMarshal(content); err == nil {
				totalTokens += len(jsonBytes) / 4
			}
		}
	}

	// 3. 工具定义（tools）
	toolCount := len(req.Tools)
	if toolCount > 0 {
		// 工具开销策略：根据工具数量自适应调整
		// - 少量工具（1-3个）：每个工具高开销（包含大量元数据和结构信息）
		// - 大量工具（10+个）：共享开销 + 小增量（避免线性叠加过高）
		var baseToolsOverhead int
		var perToolOverhead int

		if toolCount == 1 {
			// 单工具场景：高开销（包含tools数组初始化、类型信息等）
			// 优化：平衡简单工具(403)和复杂工具(874)的估算
			baseToolsOverhead = 0
			perToolOverhead = 320 // 最优平衡值
		} else if toolCount <= 5 {
			// 少量工具：中等开销
			baseToolsOverhead = config.BaseToolsOverhead // 从150降至100
			perToolOverhead = 120                        // 从150降至120
		} else {
			// 大量工具：共享开销 + 低增量
			baseToolsOverhead = 180 // 从250降至180
			perToolOverhead = 60    // 从80降至60
		}

		totalTokens += baseToolsOverhead

		for _, tool := range req.Tools {
			// 工具名称（特殊处理：下划线分词导致token数增加）
			nameTokens := e.estimateToolName(tool.Name)
			totalTokens += nameTokens

			// 工具描述
			totalTokens += e.EstimateTextTokens(tool.Description)

			// 工具schema（JSON Schema）
			if tool.InputSchema != nil {
				if jsonBytes, err := SafeMarshal(tool.InputSchema); err == nil {
					// Schema编码密度：根据工具数量自适应
					// 优化：平衡编码密度
					var schemaCharsPerToken float64
					if toolCount == 1 {
						schemaCharsPerToken = 1.9 // 单工具平衡值
					} else if toolCount <= 5 {
						schemaCharsPerToken = 2.2 // 少量工具
					} else {
						schemaCharsPerToken = 2.5 // 大量工具
					}

					schemaLen := len(jsonBytes)
					schemaTokens := int(float64(schemaLen) / schemaCharsPerToken)

					// $schema字段URL开销（优化：降低开销）
					if strings.Contains(string(jsonBytes), "$schema") {
						if toolCount == 1 {
							schemaTokens += 10 // 从15降至10
						} else {
							schemaTokens += 5 // 从8降至5
						}
					}

					// 最小schema开销（优化：降低最小值）
					minSchemaTokens := 50 // 从80降至50
					if toolCount > 5 {
						minSchemaTokens = 30 // 从40降至30
					}
					if schemaTokens < minSchemaTokens {
						schemaTokens = minSchemaTokens
					}

					totalTokens += schemaTokens
				}
			}

			totalTokens += perToolOverhead
		}
	}

	// 4. 基础请求开销（API格式固定开销）
	// 优化：根据官方测试调整
	totalTokens += 4 // 调整至4以匹配官方

	return totalTokens
}

// estimateToolName 估算工具名称的token数量
// 工具名称通常包含下划线、驼峰等特殊结构，tokenizer会进行更细粒度的分词
// 例如: "mcp__Playwright__browser_navigate_back"
// 可能被分为: ["mcp", "__", "Play", "wright", "__", "browser", "_", "navigate", "_", "back"]
func (e *TokenEstimator) estimateToolName(name string) int {
	if name == "" {
		return 0
	}

	// 基础估算：按字符长度
	baseTokens := len(name) / 2 // 工具名称通常极其密集（比普通文本密集2倍）

	// 下划线分词惩罚：每个下划线可能导致额外的token
	underscoreCount := strings.Count(name, "_")
	underscorePenalty := underscoreCount // 每个下划线约1个额外token

	// 驼峰分词惩罚：大写字母可能是分词边界
	camelCaseCount := 0
	for _, r := range name {
		if r >= 'A' && r <= 'Z' {
			camelCaseCount++
		}
	}
	camelCasePenalty := camelCaseCount / 2 // 每2个大写字母约1个额外token

	totalTokens := baseTokens + underscorePenalty + camelCasePenalty
	if totalTokens < 2 {
		totalTokens = 2 // 最少2个token
	}

	return totalTokens
}

// EstimateTextTokens 估算纯文本的token数量（借鉴 kiro.rs 算法）
// 算法说明（来自 kiro.rs）：
// - 非西文字符：每个计 4.0 个字符单位
// - 西文字符：每个计 1.0 个字符单位
// - 4 个字符单位 = 1 token
// - 短文本修正系数：放大估算值以补偿 BPE 编码开销
func (e *TokenEstimator) EstimateTextTokens(text string) int {
	if text == "" {
		return 0
	}

	// 转换为rune数组以正确计算Unicode字符数
	runes := []rune(text)
	if len(runes) == 0 {
		return 0
	}

	// 计算字符单位（借鉴 kiro.rs）
	// 非西文字符：4.0 单位
	// 西文字符：1.0 单位
	var charUnits float64
	for _, r := range runes {
		if isNonWesternChar(r) {
			charUnits += 4.0
		} else {
			charUnits += 1.0
		}
	}

	// 基础 token 计算：4 个字符单位 = 1 token
	tokens := charUnits / 4.0

	// 短文本修正系数（借鉴 kiro.rs）
	// 原因：BPE 编码对短文本的 token 密度较低，需要放大估算值
	var adjustedTokens float64
	if tokens < 100 {
		// 超短文本：× 1.5
		adjustedTokens = tokens * 1.5
	} else if tokens < 200 {
		// 短文本：× 1.3
		adjustedTokens = tokens * 1.3
	} else if tokens < 300 {
		// 中短文本：× 1.25
		adjustedTokens = tokens * 1.25
	} else if tokens < 800 {
		// 中等文本：× 1.2
		adjustedTokens = tokens * 1.2
	} else {
		// 长文本：不调整
		adjustedTokens = tokens
	}

	result := int(adjustedTokens)
	if result < 1 {
		result = 1 // 最少1个token
	}

	return result
}

// estimateContentBlock 估算单个内容块的token数量（通用map格式）
// 支持的内容类型：
// - text: 文本块
// - image: 图片（固定1500 tokens估算）
// - document: 文档（根据大小估算）
func (e *TokenEstimator) estimateContentBlock(block any) int {
	blockMap, ok := block.(map[string]any)
	if !ok {
		return 10 // 未知格式，保守估算
	}

	blockType, _ := blockMap["type"].(string)

	switch blockType {
	case "text":
		// 文本块
		if text, ok := blockMap["text"].(string); ok {
			return e.EstimateTextTokens(text)
		}
		return 10

	case "image":
		// 图片：官方文档显示约1000-2000 tokens
		// 参考: https://docs.anthropic.com/en/docs/build-with-claude/vision
		return 1500

	case "document":
		// 文档：根据大小估算（简化处理）
		return 500

	case "tool_use":
		// 工具调用结果
		if input, ok := blockMap["input"]; ok {
			if jsonBytes, err := SafeMarshal(input); err == nil {
				return len(jsonBytes) / 4
			}
		}
		return 50

	case "tool_result":
		// 工具执行结果
		if content, ok := blockMap["content"].(string); ok {
			return e.EstimateTextTokens(content)
		}
		return 50

	default:
		// 未知类型：JSON长度估算
		if jsonBytes, err := SafeMarshal(block); err == nil {
			return len(jsonBytes) / 4
		}
		return 10
	}
}

// estimateTypedContentBlock 估算类型化内容块的token数量
func (e *TokenEstimator) estimateTypedContentBlock(block types.ContentBlock) int {
	switch block.Type {
	case "text":
		if block.Text != nil {
			return e.EstimateTextTokens(*block.Text)
		}
		return 10

	case "image":
		// 图片：官方文档显示约1000-2000 tokens
		return 1500

	case "tool_use":
		// 工具调用
		if block.Input != nil {
			if jsonBytes, err := SafeMarshal(*block.Input); err == nil {
				return len(jsonBytes) / 4
			}
		}
		return 50

	case "tool_result":
		// 工具执行结果
		switch content := block.Content.(type) {
		case string:
			return e.EstimateTextTokens(content)
		case []any:
			total := 0
			for _, item := range content {
				total += e.estimateContentBlock(item)
			}
			return total
		default:
			return 50
		}

	default:
		// 未知类型
		return 10
	}
}

// IsValidClaudeModel 验证是否为有效的Claude模型
// 支持所有Claude系列模型（不限制具体版本号）
func IsValidClaudeModel(model string) bool {
	if model == "" {
		return false
	}

	model = strings.ToLower(model)

	// 支持的模型前缀
	validPrefixes := []string{
		"claude-",          // 所有Claude模型
		"gpt-",             // OpenAI兼容模式（codex渠道）
		"gemini-",          // Gemini兼容模式
		"text-",            // 传统completion模型
		"anthropic.claude", // Bedrock格式
	}

	for _, prefix := range validPrefixes {
		if strings.HasPrefix(model, prefix) {
			return true
		}
	}

	return false
}
