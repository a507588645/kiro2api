package auth

import (
	"kiro2api/config"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// 保存原始配置
	origProactive := config.ProactiveRefreshEnabled
	origSessionPool := config.SessionPoolEnabled
	origMinInterval := config.RateLimitMinTokenInterval
	origMaxInterval := config.RateLimitMaxTokenInterval
	origGlobalInterval := config.RateLimitGlobalMinInterval
	origDailyMax := config.RateLimitDailyMaxRequests
	origSkipWarmup := os.Getenv("SKIP_TOKEN_WARMUP")

	// 测试环境：关闭主动刷新与会话池，避免网络与后台任务干扰
	config.ProactiveRefreshEnabled = false
	config.SessionPoolEnabled = false
	config.RateLimitMinTokenInterval = 0
	config.RateLimitMaxTokenInterval = 0
	config.RateLimitGlobalMinInterval = 0
	config.RateLimitDailyMaxRequests = 0
	_ = os.Setenv("SKIP_TOKEN_WARMUP", "1")

	code := m.Run()

	// 还原配置
	config.ProactiveRefreshEnabled = origProactive
	config.SessionPoolEnabled = origSessionPool
	config.RateLimitMinTokenInterval = origMinInterval
	config.RateLimitMaxTokenInterval = origMaxInterval
	config.RateLimitGlobalMinInterval = origGlobalInterval
	config.RateLimitDailyMaxRequests = origDailyMax
	if origSkipWarmup == "" {
		_ = os.Unsetenv("SKIP_TOKEN_WARMUP")
	} else {
		_ = os.Setenv("SKIP_TOKEN_WARMUP", origSkipWarmup)
	}

	os.Exit(code)
}
