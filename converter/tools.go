package converter

import (
	"fmt"
	"strings"

	"kiro2api/config"
	"kiro2api/logger"
	"kiro2api/types"
	"kiro2api/utils"
)

// 工具处理器

// validateAndProcessTools 验证和处理工具定义（向后兼容版本）
// 参考server.py中的clean_gemini_schema函数以及Anthropic官方文档
func validateAndProcessTools(tools []types.OpenAITool) ([]types.AnthropicTool, error) {
	result, err := ValidateAndProcessToolsWithMapping(tools)
	if err != nil {
		return result.Tools, err
	}
	return result.Tools, nil
}

// ValidateAndProcessToolsWithMapping 验证和处理工具定义，并返回参数名映射
// 当参数名超过64字符被截断时，会维护 truncatedName -> originalName 的映射
// 返回 ConvertedToolsResult 包含转换后的工具列表和参数名映射表
func ValidateAndProcessToolsWithMapping(tools []types.OpenAITool) (*types.ConvertedToolsResult, error) {
	result := &types.ConvertedToolsResult{
		Tools:         nil,
		ParamMappings: make(types.ToolParamMappings),
	}

	if len(tools) == 0 {
		return result, nil
	}

	var anthropicTools []types.AnthropicTool
	var validationErrors []string

	for i, tool := range tools {
		if tool.Type != "function" {
			validationErrors = append(validationErrors, fmt.Sprintf("tool[%d]: 不支持的工具类型 '%s'，仅支持 'function'", i, tool.Type))
			continue
		}

		// 验证函数名称
		if tool.Function.Name == "" {
			validationErrors = append(validationErrors, fmt.Sprintf("tool[%d]: 函数名称不能为空", i))
			continue
		}

		// 过滤不支持的工具：web_search (不发送到上游)
		// 评估: kiro.rs 通过 MCP 协议实现了 web_search 支持
		// 当前项目暂不支持 web_search，保留过滤逻辑
		// TODO: 未来可考虑实现 MCP 协议支持 web_search
		if tool.Function.Name == "web_search" || tool.Function.Name == "websearch" {
			logger.Info("过滤 web_search 工具定义",
				logger.String("tool_name", tool.Function.Name),
				logger.String("reason", "当前版本暂不支持 web_search，请参考 kiro.rs 的 MCP 实现"),
				logger.String("status", "filtered"))
			continue
		}

		// 验证参数schema
		if tool.Function.Parameters == nil {
			validationErrors = append(validationErrors, fmt.Sprintf("tool[%d]: 参数schema不能为空", i))
			continue
		}

		// 清理和验证参数，同时获取参数名映射
		cleanedParams, paramMapping, err := cleanAndValidateToolParametersWithMapping(tool.Function.Parameters, tool.Function.Name)
		if err != nil {
			validationErrors = append(validationErrors, fmt.Sprintf("tool[%d] (%s): %v", i, tool.Function.Name, err))
			continue
		}

		// 如果有参数名被截断，保存映射
		if len(paramMapping) > 0 {
			result.ParamMappings[tool.Function.Name] = paramMapping
		}

		// 截断工具描述长度，防止超长内容导致上游 API 错误
			description := truncateToolDescription(tool.Function.Description, tool.Function.Name)

		anthropicTool := types.AnthropicTool{
			Name:        tool.Function.Name,
			Description: description,
			InputSchema: cleanedParams,
		}
		anthropicTools = append(anthropicTools, anthropicTool)
	}

	result.Tools = anthropicTools

	if len(validationErrors) > 0 {
		return result, fmt.Errorf("工具验证失败: %s", strings.Join(validationErrors, "; "))
	}

	return result, nil
}

// truncateToolDescription 截断工具描述长度
// 使用 config.MaxToolDescriptionLength 配置的最大长度
// 参数:
//   - description: 工具描述内容
//   - toolName: 工具名称，用于日志记录
func truncateToolDescription(description string, toolName string) string {
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

// cleanAndValidateToolParameters 清理和验证工具参数（向后兼容版本）
func cleanAndValidateToolParameters(params map[string]any) (map[string]any, error) {
	result, _, err := cleanAndValidateToolParametersWithMapping(params, "")
	return result, err
}

// truncateParamName 截断超长参数名并返回截断后的名称
// 截断规则：
// - 超过80字符：取前20字符 + "_" + 后20字符
// - 64-80字符：取前30字符 + "_param"
func truncateParamName(paramName string) string {
	if len(paramName) <= 64 {
		return paramName
	}
	if len(paramName) > 80 {
		return paramName[:20] + "_" + paramName[len(paramName)-20:]
	}
	return paramName[:30] + "_param"
}

// cleanAndValidateToolParametersWithMapping 清理和验证工具参数，并返回参数名映射
// 参数:
//   - params: 原始参数 schema
//   - toolName: 工具名称，用于日志记录
//
// 返回:
//   - cleanedParams: 清理后的参数 schema
//   - paramMapping: 参数名映射表 (truncatedName -> originalName)
//   - error: 错误信息
func cleanAndValidateToolParametersWithMapping(params map[string]any, toolName string) (map[string]any, types.ParamNameMapping, error) {
	if params == nil {
		return nil, nil, fmt.Errorf("参数不能为nil")
	}

	paramMapping := make(types.ParamNameMapping)

	// 深拷贝避免修改原始数据
	cleanedParams, _ := utils.SafeMarshal(params)
	var tempParams map[string]any
	if err := utils.SafeUnmarshal(cleanedParams, &tempParams); err != nil {
		return nil, nil, fmt.Errorf("参数序列化失败: %v", err)
	}

	// 移除不支持的顶级字段
	delete(tempParams, "additionalProperties")
	delete(tempParams, "strict")
	delete(tempParams, "$schema")
	delete(tempParams, "$id")
	delete(tempParams, "$ref")
	delete(tempParams, "definitions")
	delete(tempParams, "$defs")

	// 处理超长参数名 - CodeWhisperer限制参数名长度；保留原名映射
	if properties, ok := tempParams["properties"].(map[string]any); ok {
		cleanedProperties := make(map[string]any)
		for paramName, paramDef := range properties {
			cleanedName := truncateParamName(paramName)

			// 如果参数名被截断，记录映射和警告日志
			if cleanedName != paramName {
				paramMapping[cleanedName] = paramName
				logger.Warn("参数名超过64字符已截断",
					logger.String("tool", toolName),
					logger.String("original_name", paramName),
					logger.String("truncated_name", cleanedName),
					logger.Int("original_length", len(paramName)),
				)
			}

			cleanedProperties[cleanedName] = paramDef
		}
		tempParams["properties"] = cleanedProperties

		// 同时更新required字段中的参数名
		if required, ok := tempParams["required"].([]any); ok {
			var cleanedRequired []any
			for _, req := range required {
				if reqStr, ok := req.(string); ok {
					cleanedRequired = append(cleanedRequired, truncateParamName(reqStr))
				}
			}
			tempParams["required"] = cleanedRequired
		}
	}

	// 确保 schema 明确声明顶级 type=object，符合 CodeWhisperer 工具schema约定
	if _, exists := tempParams["type"]; !exists {
		tempParams["type"] = "object"
	}

	// 验证必需的字段
	if schemaType, exists := tempParams["type"]; exists {
		if typeStr, ok := schemaType.(string); ok && typeStr == "object" {
			// 对象类型应该有properties字段
			if _, hasProps := tempParams["properties"]; !hasProps {
				return nil, nil, fmt.Errorf("对象类型缺少properties字段")
			}
		}
	}

	// CodeWhisperer 对 schema 的兼容性处理：
	// - 仅允许标准 JSON Schema 字段：type, properties, required, description
	// - 去除潜在不兼容的字段（上面已经逐步移除）
	// - 保证 required 是字符串数组，properties 为对象
	if req, ok := tempParams["required"]; ok && req != nil {
		if arr, ok := req.([]any); ok {
			cleaned := make([]string, 0, len(arr))
			for _, v := range arr {
				if s, ok := v.(string); ok && s != "" {
					cleaned = append(cleaned, s)
				}
			}
			tempParams["required"] = cleaned
		} else {
			delete(tempParams, "required")
		}
	}
	if props, ok := tempParams["properties"]; ok {
		if _, ok := props.(map[string]any); !ok {
			delete(tempParams, "properties")
			tempParams["properties"] = map[string]any{}
		}
	} else {
		tempParams["properties"] = map[string]any{}
	}

	return tempParams, paramMapping, nil
}

// convertOpenAIToolChoiceToAnthropic 将OpenAI的tool_choice转换为Anthropic格式
// 参考server.py中的转换逻辑以及Anthropic官方文档
func convertOpenAIToolChoiceToAnthropic(openaiToolChoice any) any {
	if openaiToolChoice == nil {
		return nil
	}

	switch choice := openaiToolChoice.(type) {
	case string:
		// 处理字符串类型："auto", "none", "required"
		switch choice {
		case "auto":
			return &types.ToolChoice{Type: "auto"}
		case "required", "any":
			return &types.ToolChoice{Type: "any"}
		case "none":
			// Anthropic没有"none"选项，返回nil表示不强制使用工具
			return nil
		default:
			// 未知字符串，默认为auto
			return &types.ToolChoice{Type: "auto"}
		}

	case map[string]any:
		// 处理对象类型：{"type": "function", "function": {"name": "tool_name"}}
		if choiceType, ok := choice["type"].(string); ok && choiceType == "function" {
			if functionObj, ok := choice["function"].(map[string]any); ok {
				if name, ok := functionObj["name"].(string); ok {
					return &types.ToolChoice{
						Type: "tool",
						Name: name,
					}
				}
			}
		}
		// 如果无法解析，返回auto
		return &types.ToolChoice{Type: "auto"}

	case types.OpenAIToolChoice:
		// 处理结构化的OpenAIToolChoice类型
		if choice.Type == "function" && choice.Function != nil {
			return &types.ToolChoice{
				Type: "tool",
				Name: choice.Function.Name,
			}
		}
		return &types.ToolChoice{Type: "auto"}

	default:
		// 未知类型，默认为auto
		return &types.ToolChoice{Type: "auto"}
	}
}

// convertOpenAIContentToAnthropic 将OpenAI消息内容转换为Anthropic格式
func convertOpenAIContentToAnthropic(content any) (any, error) {
	switch v := content.(type) {
	case string:
		// 简单字符串内容，无需转换
		return v, nil

	case []any:
		// 内容块数组，需要转换格式
		var convertedBlocks []any

		for _, item := range v {
			if block, ok := item.(map[string]any); ok {
				convertedBlock, err := convertContentBlock(block)
				if err != nil {
					// 如果转换失败，跳过该块但继续处理其他块
					continue
				}
				// 如果convertedBlock为nil，表示该块需要被过滤（如web_search）
				if convertedBlock == nil {
					continue
				}
				convertedBlocks = append(convertedBlocks, convertedBlock)
			} else {
				// 非map类型的项目，直接保留
				convertedBlocks = append(convertedBlocks, item)
			}
		}

		return convertedBlocks, nil

	default:
		// 其他类型，直接返回
		return content, nil
	}
}

// convertContentBlock 转换单个内容块
func convertContentBlock(block map[string]any) (map[string]any, error) {
	blockType, exists := block["type"]
	if !exists {
		return block, fmt.Errorf("内容块缺少type字段")
	}

	switch blockType {
	case "text":
		// 文本块无需转换
		return block, nil

	case "image_url":
		// 将OpenAI的image_url格式转换为Anthropic的image格式
		imageURL, exists := block["image_url"]
		if !exists {
			return nil, fmt.Errorf("image_url块缺少image_url字段")
		}

		imageURLMap, ok := imageURL.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("image_url字段必须是对象")
		}

		// 使用utils包中的转换函数
		imageSource, err := utils.ConvertImageURLToImageSource(imageURLMap)
		if err != nil {
			return nil, fmt.Errorf("转换图片格式失败: %v", err)
		}

		// 构建Anthropic格式的图片块，确保source为map[string]any类型
		sourceMap := map[string]any{
			"type":       imageSource.Type,
			"media_type": imageSource.MediaType,
			"data":       imageSource.Data,
		}

		convertedBlock := map[string]any{
			"type":   "image",
			"source": sourceMap,
		}

		return convertedBlock, nil

	case "image":
		// 已经是Anthropic格式，无需转换
		return block, nil

	case "tool_use":
		// 过滤不支持的web_search工具调用（返回nil表示跳过）
		// 评估: kiro.rs 通过 MCP 协议实现了 web_search 支持
		// 当前项目暂不支持 web_search，保留过滤逻辑
		if name, ok := block["name"].(string); ok {
			if name == "web_search" || name == "websearch" {
				logger.Info("过滤 web_search 工具调用",
					logger.String("tool_name", name),
					logger.String("reason", "当前版本暂不支持 web_search"),
					logger.String("status", "filtered"))
				return nil, nil
			}
		}
		return block, nil

	case "tool_result":
		// tool_result块无需转换，直接返回
		return block, nil

	default:
		// 未知类型，直接返回
		return block, nil
	}
}
