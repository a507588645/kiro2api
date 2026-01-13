package utils

import (
	"github.com/bytedance/sonic"
)

// 高性能JSON配置
var (
	// FastestConfig 最快的JSON配置，用于性能关键路径
	FastestConfig = sonic.ConfigFastest

	// SafeConfig 安全的JSON配置，带有更多验证
	SafeConfig = sonic.ConfigStd
)

// FastMarshal 高性能JSON序列化
func FastMarshal(v any) ([]byte, error) {
	return FastestConfig.Marshal(v)
}

// FastUnmarshal 高性能JSON反序列化
func FastUnmarshal(data []byte, v any) error {
	return FastestConfig.Unmarshal(data, v)
}

// SafeMarshal 安全JSON序列化（带验证）
func SafeMarshal(v any) ([]byte, error) {
	return SafeConfig.Marshal(v)
}

// SafeUnmarshal 安全JSON反序列化（带验证）
func SafeUnmarshal(data []byte, v any) error {
	return SafeConfig.Unmarshal(data, v)
}

// MarshalIndent 带缩进的JSON序列化
func MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	// sonic的MarshalIndent
	return SafeConfig.MarshalIndent(v, prefix, indent)
}

// RemoveNullsFromToolInput 递归移除 map/slice 中的 null 值
// 防止工具调用参数中的 null 值导致下游问题（如 "在 null 中搜索"）
func RemoveNullsFromToolInput(value any) any {
	switch v := value.(type) {
	case map[string]any:
		cleaned := make(map[string]any)
		for k, val := range v {
			if val == nil {
				continue
			}
			cleaned[k] = RemoveNullsFromToolInput(val)
		}
		return cleaned
	case []any:
		cleaned := make([]any, 0, len(v))
		for _, item := range v {
			if item == nil {
				continue
			}
			cleaned = append(cleaned, RemoveNullsFromToolInput(item))
		}
		return cleaned
	default:
		return value
	}
}
