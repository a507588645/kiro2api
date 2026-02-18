package config

import (
	"os"
	"strconv"
	"time"
)

// {{RIPER-10 Action}}
// Role: LD | Time: 2025-12-17T14:23:00Z
// Principle: SOLID-O (开闭原则) - 增加token轮换间隔，减少被AWS检测风险
// Taste: 使用更保守的默认值，避免触发AWS安全检测

// Tuning 性能和行为调优参数
// 支持环境变量覆盖，遵循 KISS 原则

// ========== 解析器配置 ==========

// ParserMaxErrors 解析器容忍的最大错误次数
const ParserMaxErrors = 10

// ========== Token缓存配置 ==========

// TokenCacheTTL Token缓存的生存时间
var TokenCacheTTL = getEnvDuration("TOKEN_CACHE_TTL", 5*time.Minute)

// HTTPClientKeepAlive HTTP客户端Keep-Alive间隔
var HTTPClientKeepAlive = getEnvDuration("HTTP_CLIENT_KEEP_ALIVE", 30*time.Second)

// HTTPClientTLSHandshakeTimeout HTTP客户端TLS握手超时
var HTTPClientTLSHandshakeTimeout = getEnvDuration("HTTP_CLIENT_TLS_TIMEOUT", 15*time.Second)

// ========== 防封号配置（增强版 - 2025-12-17更新） ==========
// 问题：多token快速轮换触发AWS安全检测，导致账户被暂停
// 解决：增加请求间隔，减少轮换频率

// RateLimitMinTokenInterval 单token最小请求间隔
// 从5秒提升到10秒，更安全
var RateLimitMinTokenInterval = getEnvDuration("RATE_LIMIT_MIN_INTERVAL", 10*time.Second)

// RateLimitMaxTokenInterval 单token最大请求间隔（随机范围上限）
// 从15秒提升到30秒，增加随机性
var RateLimitMaxTokenInterval = getEnvDuration("RATE_LIMIT_MAX_INTERVAL", 30*time.Second)

// RateLimitGlobalMinInterval 全局最小请求间隔
// 从2秒提升到5秒
var RateLimitGlobalMinInterval = getEnvDuration("RATE_LIMIT_GLOBAL_MIN_INTERVAL", 5*time.Second)

// RateLimitMaxConsecutiveUse 单token最大连续使用次数
// 从3次提升到10次，减少轮换频率（关键修改！）
// 原因：频繁轮换导致AWS检测到异常模式
var RateLimitMaxConsecutiveUse = getEnvInt("RATE_LIMIT_MAX_CONSECUTIVE", 10)

// RateLimitCooldownDuration token冷却时间（触发403/429后）
// 保持5分钟
var RateLimitCooldownDuration = getEnvDuration("RATE_LIMIT_COOLDOWN", 5*time.Minute)

// ========== 新增：智能退避配置 ==========

// RateLimitBackoffBase 指数退避基数
// 第一次失败后等待此时间，后续翻倍
var RateLimitBackoffBase = getEnvDuration("RATE_LIMIT_BACKOFF_BASE", 2*time.Minute)

// RateLimitBackoffMax 指数退避最大值
// 退避时间不会超过此值
var RateLimitBackoffMax = getEnvDuration("RATE_LIMIT_BACKOFF_MAX", 60*time.Minute)

// RateLimitBackoffMultiplier 指数退避倍数
var RateLimitBackoffMultiplier = getEnvFloat("RATE_LIMIT_BACKOFF_MULTIPLIER", 2.0)

// ========== 新增：每日限制配置 ==========

// RateLimitDailyMaxRequests 每个token每日最大请求次数
// 0 表示不限制
var RateLimitDailyMaxRequests = getEnvInt("RATE_LIMIT_DAILY_MAX", 500)

// ========== 新增：请求抖动配置 ==========

// RateLimitJitterPercent 请求间隔抖动百分比
// 例如：30 表示在基础间隔上增加 0-30% 的随机抖动
var RateLimitJitterPercent = getEnvInt("RATE_LIMIT_JITTER_PERCENT", 30)

// ========== 新增：被暂停token的冷却时间 ==========

// SuspendedTokenCooldown 被暂停token的冷却时间
// 当检测到TEMPORARILY_SUSPENDED错误时，token进入长时间冷却
var SuspendedTokenCooldown = getEnvDuration("SUSPENDED_TOKEN_COOLDOWN", 24*time.Hour)

// ========== 新增：主动刷新配置 ==========

// ProactiveRefreshEnabled 是否启用主动刷新
var ProactiveRefreshEnabled = getEnvBool("PROACTIVE_REFRESH_ENABLED", true)

// ProactiveRefreshInterval 主动刷新检查间隔
var ProactiveRefreshInterval = getEnvDuration("PROACTIVE_REFRESH_INTERVAL", 1*time.Minute)

// ProactiveRefreshThreshold Token过期前多久触发刷新
var ProactiveRefreshThreshold = getEnvDuration("PROACTIVE_REFRESH_THRESHOLD", 5*time.Minute)

// ========== 会话级账号池配置 ==========

// SessionPoolEnabled 是否启用会话级账号池
var SessionPoolEnabled = getEnvBool("SESSION_POOL_ENABLED", true)

// SessionPoolMaxSize 每个会话最大 Token 数
var SessionPoolMaxSize = getEnvInt("SESSION_POOL_MAX_SIZE", 3)

// SessionPoolMaxRetries 429 最大重试次数
var SessionPoolMaxRetries = getEnvInt("SESSION_POOL_MAX_RETRIES", 5)

// SessionPoolTTL 会话池过期时间
var SessionPoolTTL = getEnvDuration("SESSION_POOL_TTL", 30*time.Minute)

// SessionPoolCooldown 429 后默认冷却时间
var SessionPoolCooldown = getEnvDuration("SESSION_POOL_COOLDOWN", 60*time.Second)

// SessionPoolRetryInterval 重试间隔
var SessionPoolRetryInterval = getEnvDuration("SESSION_POOL_RETRY_INTERVAL", 100*time.Millisecond)

// ========== 模型访问控制配置 ==========

// ModelAccessControlEnabled 是否启用按账号等级限制模型访问
var ModelAccessControlEnabled = getEnvBool("MODEL_ACCESS_CONTROL_ENABLED", true)

// ModelAccessUnknownAllowed 账号等级未知时是否放行全部模型
var ModelAccessUnknownAllowed = getEnvBool("MODEL_ACCESS_UNKNOWN_ALLOWED", true)

// ========== 工具限制配置 ==========

// MaxToolDescriptionLength 工具描述的最大长度（字符数，默认：10000）
// 用于限制 tool description 字段的长度
// 防止超长内容导致上游 API 错误
var MaxToolDescriptionLength = getEnvInt("MAX_TOOL_DESCRIPTION_LENGTH", 10000)

// ========== 辅助函数 ==========

// getEnvDuration 从环境变量读取时间间隔，支持格式如 "5s", "1m", "2h"
func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	return defaultVal
}

// getEnvInt 从环境变量读取整数
func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

// getEnvFloat 从环境变量读取浮点数
func getEnvFloat(key string, defaultVal float64) float64 {
	if val := os.Getenv(key); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
	}
	return defaultVal
}

// getEnvBool 从环境变量读取布尔值
func getEnvBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			return b
		}
	}
	return defaultVal
}
