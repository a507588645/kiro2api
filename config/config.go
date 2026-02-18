package config

import "fmt"

// DefaultRegion 默认区域（支持 KIRO_REGION 环境变量覆盖）
var DefaultRegion = getEnvString("KIRO_REGION", "us-east-1")

// ModelMap 模型映射表
var ModelMap = map[string]string{
	"claude-opus-4-5-20251101":   "CLAUDE_OPUS_4_5_20251101_V1_0",
	"claude-opus-4-6":            "CLAUDE_OPUS_4_6_V1_0",
	"claude-sonnet-4-5-20250929": "CLAUDE_SONNET_4_5_20250929_V1_0",
	"claude-sonnet-4-6":          "CLAUDE_SONNET_4_6_V1_0",
	"claude-sonnet-4-20250514":   "CLAUDE_SONNET_4_20250514_V1_0",
	"claude-3-7-sonnet-20250219": "CLAUDE_3_7_SONNET_20250219_V1_0",
	"claude-3-5-haiku-20241022":  "auto",
	"claude-haiku-4-5-20251001":  "auto",
}

// GetRefreshTokenURL 获取社交认证刷新 token URL（支持区域配置）
func GetRefreshTokenURL() string {
	return fmt.Sprintf("https://prod.%s.auth.desktop.kiro.dev/refreshToken", DefaultRegion)
}

// GetIdcRefreshTokenURL 获取 IdC 认证刷新 token URL（支持区域配置）
func GetIdcRefreshTokenURL() string {
	return fmt.Sprintf("https://oidc.%s.amazonaws.com/token", DefaultRegion)
}

// GetCodeWhispererURL 获取 API URL（与 kiro.rs 对齐使用 q.{region}.amazonaws.com）
func GetCodeWhispererURL() string {
	return fmt.Sprintf("https://q.%s.amazonaws.com/generateAssistantResponse", DefaultRegion)
}

// GetCodeWhispererHost 获取 API Host 头（与 kiro.rs 对齐）
func GetCodeWhispererHost() string {
	return fmt.Sprintf("q.%s.amazonaws.com", DefaultRegion)
}

// GetUsageLimitsURL 获取使用限制检查 URL（与 kiro.rs 对齐）
func GetUsageLimitsURL() string {
	return fmt.Sprintf("https://q.%s.amazonaws.com/getUsageLimits", DefaultRegion)
}
