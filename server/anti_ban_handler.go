package server

import (
	"net/http"
	"time"

	"kiro2api/auth"
	"kiro2api/config"

	"github.com/gin-gonic/gin"
)

// handleAntiBanStatus 返回防封号系统状态
func handleAntiBanStatus(c *gin.Context) {
	rateLimiter := auth.GetRateLimiter()
	fpManager := auth.GetFingerprintManager()
	proxyPool := auth.GetProxyPool()

	// 获取频率限制器统计
	rateLimiterStats := rateLimiter.GetStats()

	// 获取指纹统计
	fingerprintStats := fpManager.GetStats()

	// 获取代理池统计
	proxyPoolStats := proxyPool.GetStats()

	// 配置信息
	configInfo := map[string]any{
		"rate_limit": map[string]any{
			"min_token_interval_ms":  config.RateLimitMinTokenInterval.Milliseconds(),
			"max_token_interval_ms":  config.RateLimitMaxTokenInterval.Milliseconds(),
			"global_min_interval_ms": config.RateLimitGlobalMinInterval.Milliseconds(),
			"max_consecutive_use":    config.RateLimitMaxConsecutiveUse,
			"cooldown_duration_sec":  config.RateLimitCooldownDuration.Seconds(),
		},
		"token_cache_ttl_sec": config.TokenCacheTTL.Seconds(),
	}

	c.JSON(http.StatusOK, gin.H{
		"timestamp":    time.Now().Format(time.RFC3339),
		"status":       "active",
		"rate_limiter": rateLimiterStats,
		"fingerprints": fingerprintStats,
		"proxy_pool":   proxyPoolStats,
		"config":       configInfo,
		"features": map[string]bool{
			"fingerprint_randomization": true,
			"rate_limiting":             true,
			"smart_token_rotation":      true,
			"cooldown_on_error":         true,
			"proxy_pool":                proxyPool.IsEnabled(),
		},
	})
}
