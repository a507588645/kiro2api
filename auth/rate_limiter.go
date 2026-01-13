package auth

import (
	"math"
	"math/rand"
	"strings"
	"sync"
	"time"

	"kiro2api/config"
	"kiro2api/logger"
)

// {{RIPER-10 Action}}
// Role: LD | Time: 2025-12-17T14:23:00Z
// Principle: SOLID-O (开闭原则) - 添加被暂停token的处理逻辑
// Taste: 使用 TokenState 结构体统一管理token状态，新增 IsSuspended 字段

// TokenState token的完整状态
type TokenState struct {
	LastRequest    time.Time // 最后请求时间
	RequestCount   int       // 连续请求次数
	CooldownEnd    time.Time // 冷却结束时间
	FailCount      int       // 连续失败次数（用于指数退避）
	DailyRequests  int       // 今日请求次数
	DailyResetTime time.Time // 每日计数重置时间
	IsSuspended    bool      // 是否被AWS暂停
	SuspendedAt    time.Time // 被暂停的时间
	SuspendReason  string    // 暂停原因
}

// RateLimiter 请求频率限制器（增强版）
type RateLimiter struct {
	tokenStates       map[string]*TokenState // 统一的token状态管理
	globalLastRequest time.Time              // 全局最后请求时间
	mutex             sync.Mutex
	rng               *rand.Rand

	// 配置参数
	minTokenInterval  time.Duration // 单token最小请求间隔
	maxTokenInterval  time.Duration // 单token最大请求间隔（随机范围）
	globalMinInterval time.Duration // 全局最小请求间隔
	maxConsecutiveUse int           // 单token最大连续使用次数
	cooldownDuration  time.Duration // token冷却时间

	// 新增：智能退避配置
	backoffBase       time.Duration // 退避基数
	backoffMax        time.Duration // 退避最大值
	backoffMultiplier float64       // 退避倍数

	// 新增：每日限制配置
	dailyMaxRequests int // 每日最大请求次数

	// 新增：抖动配置
	jitterPercent int // 抖动百分比

	// 新增：被暂停token的冷却时间
	suspendedCooldown time.Duration
}

// RateLimiterConfig 频率限制器配置
type RateLimiterConfig struct {
	MinTokenInterval  time.Duration
	MaxTokenInterval  time.Duration
	GlobalMinInterval time.Duration
	MaxConsecutiveUse int
	CooldownDuration  time.Duration
	BackoffBase       time.Duration
	BackoffMax        time.Duration
	BackoffMultiplier float64
	DailyMaxRequests  int
	JitterPercent     int
	SuspendedCooldown time.Duration
}

// DefaultRateLimiterConfig 默认配置（从config包读取）
func DefaultRateLimiterConfig() RateLimiterConfig {
	return RateLimiterConfig{
		MinTokenInterval:  config.RateLimitMinTokenInterval,
		MaxTokenInterval:  config.RateLimitMaxTokenInterval,
		GlobalMinInterval: config.RateLimitGlobalMinInterval,
		MaxConsecutiveUse: config.RateLimitMaxConsecutiveUse,
		CooldownDuration:  config.RateLimitCooldownDuration,
		BackoffBase:       config.RateLimitBackoffBase,
		BackoffMax:        config.RateLimitBackoffMax,
		BackoffMultiplier: config.RateLimitBackoffMultiplier,
		DailyMaxRequests:  config.RateLimitDailyMaxRequests,
		JitterPercent:     config.RateLimitJitterPercent,
		SuspendedCooldown: config.SuspendedTokenCooldown,
	}
}

var (
	globalRateLimiter *RateLimiter
	rateLimiterOnce   sync.Once
)

// GetRateLimiter 获取全局频率限制器
func GetRateLimiter() *RateLimiter {
	rateLimiterOnce.Do(func() {
		cfg := DefaultRateLimiterConfig()
		globalRateLimiter = NewRateLimiter(cfg)
	})
	return globalRateLimiter
}

// NewRateLimiter 创建频率限制器
func NewRateLimiter(cfg RateLimiterConfig) *RateLimiter {
	return &RateLimiter{
		tokenStates:       make(map[string]*TokenState),
		rng:               rand.New(rand.NewSource(time.Now().UnixNano())),
		minTokenInterval:  cfg.MinTokenInterval,
		maxTokenInterval:  cfg.MaxTokenInterval,
		globalMinInterval: cfg.GlobalMinInterval,
		maxConsecutiveUse: cfg.MaxConsecutiveUse,
		cooldownDuration:  cfg.CooldownDuration,
		backoffBase:       cfg.BackoffBase,
		backoffMax:        cfg.BackoffMax,
		backoffMultiplier: cfg.BackoffMultiplier,
		dailyMaxRequests:  cfg.DailyMaxRequests,
		jitterPercent:     cfg.JitterPercent,
		suspendedCooldown: cfg.SuspendedCooldown,
	}
}

// getOrCreateState 获取或创建token状态
func (rl *RateLimiter) getOrCreateState(tokenKey string) *TokenState {
	state, exists := rl.tokenStates[tokenKey]
	if !exists {
		state = &TokenState{
			DailyResetTime: time.Now().Truncate(24 * time.Hour).Add(24 * time.Hour),
		}
		rl.tokenStates[tokenKey] = state
	}

	// 检查是否需要重置每日计数
	if time.Now().After(state.DailyResetTime) {
		state.DailyRequests = 0
		state.DailyResetTime = time.Now().Truncate(24 * time.Hour).Add(24 * time.Hour)
		logger.Debug("重置每日请求计数",
			logger.String("token_key", tokenKey))
	}

	return state
}

// WaitForToken 等待直到可以使用指定token，返回实际等待时间
func (rl *RateLimiter) WaitForToken(tokenKey string) time.Duration {
	// 如果配置为 0，表示无限制模式，直接返回
	if rl.globalMinInterval == 0 && rl.minTokenInterval == 0 && rl.maxTokenInterval == 0 {
		return 0
	}

	rl.mutex.Lock()

	now := time.Now()
	var totalWait time.Duration

	// 检查全局频率限制
	if rl.globalMinInterval > 0 && !rl.globalLastRequest.IsZero() {
		globalElapsed := now.Sub(rl.globalLastRequest)
		if globalElapsed < rl.globalMinInterval {
			globalWait := rl.globalMinInterval - globalElapsed
			totalWait = globalWait
		}
	}

	state := rl.getOrCreateState(tokenKey)

	// 检查token频率限制
	if (rl.minTokenInterval > 0 || rl.maxTokenInterval > 0) && !state.LastRequest.IsZero() {
		tokenElapsed := now.Sub(state.LastRequest)
		requiredInterval := rl.randomIntervalWithJitter()

		if tokenElapsed < requiredInterval {
			tokenWait := requiredInterval - tokenElapsed
			if tokenWait > totalWait {
				totalWait = tokenWait
			}
		}
	}

	rl.mutex.Unlock()

	// 执行等待
	if totalWait > 0 {
		logger.Debug("频率限制等待",
			logger.String("token_key", tokenKey),
			logger.Duration("wait_duration", totalWait))
		time.Sleep(totalWait)
	}

	return totalWait
}

// RecordRequest 记录请求
func (rl *RateLimiter) RecordRequest(tokenKey string) {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	now := time.Now()
	rl.globalLastRequest = now

	state := rl.getOrCreateState(tokenKey)
	state.LastRequest = now
	state.RequestCount++
	state.DailyRequests++
}

// ShouldRotate 检查是否应该轮换token（连续使用次数过多）
func (rl *RateLimiter) ShouldRotate(tokenKey string) bool {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	state := rl.getOrCreateState(tokenKey)
	return state.RequestCount >= rl.maxConsecutiveUse
}

// ResetTokenCount 重置token的连续使用计数
func (rl *RateLimiter) ResetTokenCount(tokenKey string) {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	state := rl.getOrCreateState(tokenKey)
	state.RequestCount = 0
}

// MarkTokenCooldown 标记token进入冷却期（使用智能退避）
func (rl *RateLimiter) MarkTokenCooldown(tokenKey string) {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	state := rl.getOrCreateState(tokenKey)
	state.FailCount++

	// 计算指数退避时间
	backoffDuration := rl.calculateBackoff(state.FailCount)
	state.CooldownEnd = time.Now().Add(backoffDuration)
	state.RequestCount = 0

	logger.Info("Token进入冷却期（智能退避）",
		logger.String("token_key", tokenKey),
		logger.Int("fail_count", state.FailCount),
		logger.Duration("cooldown", backoffDuration))
}

// MarkTokenSuspended 标记token被AWS暂停
// 当检测到TEMPORARILY_SUSPENDED错误时调用
func (rl *RateLimiter) MarkTokenSuspended(tokenKey string, reason string) {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	state := rl.getOrCreateState(tokenKey)
	state.IsSuspended = true
	state.SuspendedAt = time.Now()
	state.SuspendReason = reason
	state.CooldownEnd = time.Now().Add(rl.suspendedCooldown)
	state.RequestCount = 0

	logger.Error("Token被AWS暂停，进入长时间冷却",
		logger.String("token_key", tokenKey),
		logger.String("reason", reason),
		logger.Duration("cooldown", rl.suspendedCooldown),
		logger.String("cooldown_end", state.CooldownEnd.Format(time.RFC3339)))
}

// IsTokenSuspended 检查token是否被暂停
func (rl *RateLimiter) IsTokenSuspended(tokenKey string) bool {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	state, exists := rl.tokenStates[tokenKey]
	if !exists {
		return false
	}

	if !state.IsSuspended {
		return false
	}

	// 检查暂停冷却期是否已过
	if time.Now().After(state.CooldownEnd) {
		state.IsSuspended = false
		state.SuspendReason = ""
		logger.Info("Token暂停冷却期结束，恢复可用",
			logger.String("token_key", tokenKey))
		return false
	}

	return true
}

// calculateBackoff 计算指数退避时间
func (rl *RateLimiter) calculateBackoff(failCount int) time.Duration {
	if failCount <= 0 {
		return rl.cooldownDuration
	}

	// 指数退避: base * multiplier^(failCount-1)
	multiplier := math.Pow(rl.backoffMultiplier, float64(failCount-1))
	backoff := time.Duration(float64(rl.backoffBase) * multiplier)

	// 添加随机抖动 (0-20%)
	jitter := time.Duration(rl.rng.Float64() * 0.2 * float64(backoff))
	backoff += jitter

	// 限制最大值
	if backoff > rl.backoffMax {
		backoff = rl.backoffMax
	}

	return backoff
}

// IsTokenInCooldown 检查token是否在冷却期
func (rl *RateLimiter) IsTokenInCooldown(tokenKey string) bool {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	state, exists := rl.tokenStates[tokenKey]
	if !exists {
		return false
	}

	// 先检查是否被暂停
	if state.IsSuspended && time.Now().Before(state.CooldownEnd) {
		logger.Debug("Token被暂停，跳过",
			logger.String("token_key", tokenKey),
			logger.String("reason", state.SuspendReason),
			logger.Duration("remaining", state.CooldownEnd.Sub(time.Now())))
		return true
	}

	if time.Now().Before(state.CooldownEnd) {
		return true
	}

	// 冷却期已过，重置失败计数
	if state.FailCount > 0 {
		state.FailCount = 0
		logger.Debug("Token冷却期结束，重置失败计数",
			logger.String("token_key", tokenKey))
	}

	// 重置暂停状态
	if state.IsSuspended {
		state.IsSuspended = false
		state.SuspendReason = ""
		logger.Info("Token暂停冷却期结束，恢复可用",
			logger.String("token_key", tokenKey))
	}

	return false
}

// IsDailyLimitExceeded 检查是否超过每日限制
func (rl *RateLimiter) IsDailyLimitExceeded(tokenKey string) bool {
	if rl.dailyMaxRequests <= 0 {
		return false // 0 表示不限制
	}

	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	state := rl.getOrCreateState(tokenKey)
	return state.DailyRequests >= rl.dailyMaxRequests
}

// GetDailyRemaining 获取今日剩余请求次数
func (rl *RateLimiter) GetDailyRemaining(tokenKey string) int {
	if rl.dailyMaxRequests <= 0 {
		return -1 // -1 表示不限制
	}

	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	state := rl.getOrCreateState(tokenKey)
	remaining := rl.dailyMaxRequests - state.DailyRequests
	if remaining < 0 {
		return 0
	}
	return remaining
}

// randomIntervalWithJitter 生成带抖动的随机间隔时间
func (rl *RateLimiter) randomIntervalWithJitter() time.Duration {
	// 基础随机间隔
	delta := rl.maxTokenInterval - rl.minTokenInterval
	randomDelta := time.Duration(rl.rng.Int63n(int64(delta)))
	baseInterval := rl.minTokenInterval + randomDelta

	// 添加额外抖动
	if rl.jitterPercent > 0 {
		jitterRange := float64(baseInterval) * float64(rl.jitterPercent) / 100.0
		jitter := time.Duration(rl.rng.Float64() * jitterRange)
		baseInterval += jitter
	}

	return baseInterval
}

// randomInterval 生成随机间隔时间（保持向后兼容）
func (rl *RateLimiter) randomInterval() time.Duration {
	delta := rl.maxTokenInterval - rl.minTokenInterval
	randomDelta := time.Duration(rl.rng.Int63n(int64(delta)))
	return rl.minTokenInterval + randomDelta
}

// RecordSuccess 记录成功请求，重置失败计数
func (rl *RateLimiter) RecordSuccess(tokenKey string) {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	state := rl.getOrCreateState(tokenKey)
	if state.FailCount > 0 {
		state.FailCount = 0
		logger.Debug("请求成功，重置失败计数",
			logger.String("token_key", tokenKey))
	}
}

// CheckAndMarkSuspended 检查错误消息是否包含暂停信息，如果是则标记token
// 返回true表示token被暂停
func (rl *RateLimiter) CheckAndMarkSuspended(tokenKey string, errorMsg string) bool {
	if strings.Contains(errorMsg, "TEMPORARILY_SUSPENDED") ||
		strings.Contains(errorMsg, "temporarily is suspended") {
		rl.MarkTokenSuspended(tokenKey, errorMsg)
		return true
	}
	return false
}

// GetStats 获取统计信息
func (rl *RateLimiter) GetStats() map[string]any {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	tokenStats := make(map[string]any)
	for key, state := range rl.tokenStates {
		inCooldown := time.Now().Before(state.CooldownEnd)
		var cooldownRemaining time.Duration
		if inCooldown {
			cooldownRemaining = state.CooldownEnd.Sub(time.Now())
		}

		tokenStats[key] = map[string]any{
			"consecutive_count":    state.RequestCount,
			"last_request":         state.LastRequest.Format(time.RFC3339),
			"seconds_ago":          time.Since(state.LastRequest).Seconds(),
			"in_cooldown":          inCooldown,
			"cooldown_remaining_s": cooldownRemaining.Seconds(),
			"fail_count":           state.FailCount,
			"daily_requests":       state.DailyRequests,
			"daily_remaining":      rl.dailyMaxRequests - state.DailyRequests,
			"is_suspended":         state.IsSuspended,
			"suspend_reason":       state.SuspendReason,
		}
	}

	return map[string]any{
		"global_last_request": rl.globalLastRequest.Format(time.RFC3339),
		"config": map[string]any{
			"min_interval_s":     rl.minTokenInterval.Seconds(),
			"max_interval_s":     rl.maxTokenInterval.Seconds(),
			"global_min_s":       rl.globalMinInterval.Seconds(),
			"max_consecutive":    rl.maxConsecutiveUse,
			"cooldown_s":         rl.cooldownDuration.Seconds(),
			"backoff_base_s":     rl.backoffBase.Seconds(),
			"backoff_max_s":      rl.backoffMax.Seconds(),
			"backoff_multiplier": rl.backoffMultiplier,
			"daily_max_requests": rl.dailyMaxRequests,
			"jitter_percent":     rl.jitterPercent,
			"suspended_cooldown": rl.suspendedCooldown.Seconds(),
		},
		"token_stats": tokenStats,
	}
}
