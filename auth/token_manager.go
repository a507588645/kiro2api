package auth

import (
	"context"
	"fmt"
	"kiro2api/config"
	"kiro2api/logger"
	"kiro2api/types"
	"sync"
	"time"
)

// {{RIPER-10 Action}}
// Role: LD | Time: 2025-12-14T15:51:12Z
// Principle: SOLID-O (开闭原则) - 改为严格轮询策略，更好地分散请求
// Taste: 使用 currentIndex 实现简单的轮询，避免复杂的加权随机

// TokenManager 简化的token管理器
type TokenManager struct {
	cache        *SimpleTokenCache
	configs      []AuthConfig
	mutex        sync.RWMutex
	lastRefresh  time.Time
	configOrder  []string        // 配置顺序
	currentIndex int             // 当前使用的token索引（轮询用）
	exhausted    map[string]bool // 已耗尽的token记录

	// 智能轮换相关
	rateLimiter        *RateLimiter        // 频率限制器
	fingerprintManager *FingerprintManager // 指纹管理器

	// 主动刷新相关
	ctx    context.Context
	cancel context.CancelFunc
}

// SimpleTokenCache 简化的token缓存（纯数据结构，无锁）
// 所有并发访问由 TokenManager.mutex 统一管理
type SimpleTokenCache struct {
	tokens map[string]*CachedToken
	ttl    time.Duration
}

// CachedToken 缓存的token信息
type CachedToken struct {
	Token     types.TokenInfo
	UsageInfo *types.UsageLimits
	CachedAt  time.Time
	LastUsed  time.Time
	Available float64
}

// NewSimpleTokenCache 创建简单的token缓存
func NewSimpleTokenCache(ttl time.Duration) *SimpleTokenCache {
	return &SimpleTokenCache{
		tokens: make(map[string]*CachedToken),
		ttl:    ttl,
	}
}

// NewTokenManager 创建新的token管理器
func NewTokenManager(configs []AuthConfig) *TokenManager {
	// 生成配置顺序
	configOrder := generateConfigOrder(configs)

	logger.Info("TokenManager初始化（严格轮询策略）",
		logger.Int("config_count", len(configs)),
		logger.Int("config_order_count", len(configOrder)))

	ctx, cancel := context.WithCancel(context.Background())

	tm := &TokenManager{
		cache:              NewSimpleTokenCache(config.TokenCacheTTL),
		configs:            configs,
		configOrder:        configOrder,
		currentIndex:       0,
		exhausted:          make(map[string]bool),
		rateLimiter:        GetRateLimiter(),
		fingerprintManager: GetFingerprintManager(),
		ctx:                ctx,
		cancel:             cancel,
	}

	// 启动主动刷新goroutine
	if config.ProactiveRefreshEnabled {
		go tm.proactiveRefreshLoop()
	}

	// 初始化会话池管理器并设置 TokenManager 引用
	if config.SessionPoolEnabled {
		poolManager := GetSessionTokenPoolManager()
		poolManager.SetTokenManager(tm)
	}

	return tm
}

// Stop 停止TokenManager的后台任务
func (tm *TokenManager) Stop() {
	if tm.cancel != nil {
		tm.cancel()
	}
}

// proactiveRefreshLoop 主动刷新循环
func (tm *TokenManager) proactiveRefreshLoop() {
	ticker := time.NewTicker(config.ProactiveRefreshInterval)
	defer ticker.Stop()

	logger.Info("主动刷新goroutine已启动",
		logger.Duration("interval", config.ProactiveRefreshInterval),
		logger.Duration("threshold", config.ProactiveRefreshThreshold))

	for {
		select {
		case <-tm.ctx.Done():
			logger.Info("主动刷新goroutine已停止")
			return
		case <-ticker.C:
			tm.proactiveRefresh()
		}
	}
}

// proactiveRefresh 检查并主动刷新即将过期的token
func (tm *TokenManager) proactiveRefresh() {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	now := time.Now()
	refreshed := 0

	for i, cfg := range tm.configs {
		if cfg.Disabled {
			continue
		}

		cacheKey := fmt.Sprintf(config.TokenCacheKeyFormat, i)
		cached, exists := tm.cache.tokens[cacheKey]

		// 检查是否需要刷新：不存在、已过期、或即将过期
		needRefresh := !exists ||
			now.After(cached.Token.ExpiresAt) ||
			cached.Token.ExpiresAt.Sub(now) < config.ProactiveRefreshThreshold

		if !needRefresh {
			continue
		}

		// 刷新token
		token, err := tm.refreshSingleToken(cfg)
		if err != nil {
			logger.Warn("主动刷新token失败",
				logger.Int("config_index", i),
				logger.Err(err))
			continue
		}

		// 检查使用限制
		var usageInfo *types.UsageLimits
		var available float64

		checker := NewUsageLimitsChecker()
		if usage, checkErr := checker.CheckUsageLimits(token); checkErr == nil {
			usageInfo = usage
			available = CalculateAvailableCount(usage)
		}

		// 更新缓存
		tm.cache.tokens[cacheKey] = &CachedToken{
			Token:     token,
			UsageInfo: usageInfo,
			CachedAt:  now,
			Available: available,
		}

		refreshed++
		logger.Debug("主动刷新token成功",
			logger.String("cache_key", cacheKey),
			logger.String("new_expires_at", token.ExpiresAt.Format("2006-01-02T15:04:05Z07:00")))
	}

	if refreshed > 0 {
		logger.Info("主动刷新完成",
			logger.Int("refreshed_count", refreshed))
	}
}

// getBestToken 获取最优可用token（带严格轮询和频率限制）
// 统一锁管理：所有操作在单一锁保护下完成，避免多次加锁/解锁
func (tm *TokenManager) getBestToken() (types.TokenInfo, error) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	// 检查是否需要刷新缓存（在锁内）
	if time.Since(tm.lastRefresh) > config.TokenCacheTTL {
		if err := tm.refreshCacheUnlocked(); err != nil {
			logger.Warn("刷新token缓存失败", logger.Err(err))
		}
	}

	// 选择下一个可用token（严格轮询）
	bestToken, tokenKey := tm.selectNextAvailableTokenUnlocked()
	if bestToken == nil {
		return types.TokenInfo{}, fmt.Errorf("没有可用的token")
	}

	// 释放锁后执行频率限制等待（避免长时间持锁）
	tm.mutex.Unlock()

	// 频率限制等待
	if tm.rateLimiter != nil {
		tm.rateLimiter.WaitForToken(tokenKey)
		tm.rateLimiter.RecordRequest(tokenKey)

		// 检查是否需要轮换（连续使用次数过多）
		if tm.rateLimiter.ShouldRotate(tokenKey) {
			tm.rateLimiter.ResetTokenCount(tokenKey)
			tm.mutex.Lock()
			tm.advanceToNextToken()
			logger.Info("触发轮询切换",
				logger.String("reason", "consecutive_use_limit"),
				logger.String("from_token", tokenKey),
				logger.Int("next_index", tm.currentIndex))
			tm.mutex.Unlock()
		}
	}

	// 重新获取锁更新状态
	tm.mutex.Lock()

	// 更新最后使用时间（在锁内，安全）
	bestToken.LastUsed = time.Now()
	if bestToken.Available > 0 {
		bestToken.Available--
	}

	return bestToken.Token, nil
}

// GetTokenWithFingerprint 获取token及其对应的指纹
func (tm *TokenManager) GetTokenWithFingerprint() (types.TokenInfo, *Fingerprint, error) {
	tm.mutex.Lock()

	// 检查是否需要刷新缓存
	if time.Since(tm.lastRefresh) > config.TokenCacheTTL {
		if err := tm.refreshCacheUnlocked(); err != nil {
			logger.Warn("刷新token缓存失败", logger.Err(err))
		}
	}

	// 选择下一个可用token（严格轮询）
	bestToken, tokenKey := tm.selectNextAvailableTokenUnlocked()
	if bestToken == nil {
		tm.mutex.Unlock()
		return types.TokenInfo{}, nil, fmt.Errorf("没有可用的token")
	}

	tm.mutex.Unlock()

	// 频率限制等待
	if tm.rateLimiter != nil {
		tm.rateLimiter.WaitForToken(tokenKey)
		tm.rateLimiter.RecordRequest(tokenKey)

		if tm.rateLimiter.ShouldRotate(tokenKey) {
			tm.rateLimiter.ResetTokenCount(tokenKey)
			tm.mutex.Lock()
			tm.advanceToNextToken()
			tm.mutex.Unlock()
		}
	}

	// 获取指纹
	var fingerprint *Fingerprint
	if tm.fingerprintManager != nil {
		fingerprint = tm.fingerprintManager.GetFingerprint(tokenKey)
	}

	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	bestToken.LastUsed = time.Now()
	if bestToken.Available > 0 {
		bestToken.Available--
	}

	return bestToken.Token, fingerprint, nil
}

// GetTokenWithFingerprintForSession 为会话获取 Token（支持会话绑定）
func (tm *TokenManager) GetTokenWithFingerprintForSession(sessionID string) (types.TokenInfo, *Fingerprint, string, error) {
	// 尝试获取会话绑定的 Token
	sessionManager := GetSessionTokenBindingManager()
	if token, fingerprint, tokenKey, bound := sessionManager.GetSessionToken(sessionID); bound {
		// 检查 Token 是否仍然有效
		if time.Now().Before(token.ExpiresAt) {
			logger.Debug("使用会话绑定的Token",
				logger.String("session_id", sessionID),
				logger.String("token_key", tokenKey))
			return token, fingerprint, tokenKey, nil
		}
		// Token 已过期，解绑会话
		sessionManager.UnbindSession(sessionID)
		logger.Debug("会话绑定的Token已过期，重新分配",
			logger.String("session_id", sessionID))
	}

	// 获取新 Token
	tm.mutex.Lock()

	// 检查是否需要刷新缓存
	if time.Since(tm.lastRefresh) > config.TokenCacheTTL {
		if err := tm.refreshCacheUnlocked(); err != nil {
			logger.Warn("刷新token缓存失败", logger.Err(err))
		}
	}

	// 选择下一个可用token（严格轮询）
	bestToken, tokenKey := tm.selectNextAvailableTokenUnlocked()
	if bestToken == nil {
		tm.mutex.Unlock()
		return types.TokenInfo{}, nil, "", fmt.Errorf("没有可用的token")
	}

	tm.mutex.Unlock()

	// 频率限制等待
	if tm.rateLimiter != nil {
		tm.rateLimiter.WaitForToken(tokenKey)
		tm.rateLimiter.RecordRequest(tokenKey)

		if tm.rateLimiter.ShouldRotate(tokenKey) {
			tm.rateLimiter.ResetTokenCount(tokenKey)
			tm.mutex.Lock()
			tm.advanceToNextToken()
			tm.mutex.Unlock()
		}
	}

	// 获取指纹
	var fingerprint *Fingerprint
	if tm.fingerprintManager != nil {
		fingerprint = tm.fingerprintManager.GetFingerprint(tokenKey)
	}

	tm.mutex.Lock()
	bestToken.LastUsed = time.Now()
	if bestToken.Available > 0 {
		bestToken.Available--
	}
	token := bestToken.Token
	tm.mutex.Unlock()

	// 绑定会话到 Token
	sessionManager.BindSessionToken(sessionID, tokenKey, token, fingerprint)

	logger.Debug("为会话分配新Token",
		logger.String("session_id", sessionID),
		logger.String("token_key", tokenKey))

	return token, fingerprint, tokenKey, nil
}

// MarkTokenFailed 标记token请求失败，触发冷却
func (tm *TokenManager) MarkTokenFailed(tokenKey string) {
	if tm.rateLimiter != nil {
		tm.rateLimiter.MarkTokenCooldown(tokenKey)
	}

	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	// 切换到下一个token
	tm.advanceToNextToken()
	logger.Warn("Token请求失败，切换到下一个",
		logger.String("failed_token", tokenKey),
		logger.Int("next_index", tm.currentIndex))
}

// MarkTokenSuccess 标记token请求成功，重置失败计数
func (tm *TokenManager) MarkTokenSuccess(tokenKey string) {
	if tm.rateLimiter != nil {
		tm.rateLimiter.RecordSuccess(tokenKey)
	}
}

// GetCurrentTokenKey 获取当前token的key
func (tm *TokenManager) GetCurrentTokenKey() string {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	if len(tm.configOrder) == 0 {
		return ""
	}
	return tm.configOrder[tm.currentIndex]
}

// advanceToNextToken 前进到下一个token（内部方法，调用者必须持有锁）
func (tm *TokenManager) advanceToNextToken() {
	if len(tm.configOrder) > 0 {
		tm.currentIndex = (tm.currentIndex + 1) % len(tm.configOrder)
	}
}

// selectNextAvailableTokenUnlocked 严格轮询选择下一个可用token
// 内部方法：调用者必须持有 tm.mutex
// 策略：从 currentIndex 开始，找到第一个可用的token
func (tm *TokenManager) selectNextAvailableTokenUnlocked() (*CachedToken, string) {
	if len(tm.configOrder) == 0 {
		// 降级到按map遍历顺序
		for key, cached := range tm.cache.tokens {
			if time.Since(cached.CachedAt) <= tm.cache.ttl && cached.IsUsable() {
				logger.Debug("选择token（无顺序配置）",
					logger.String("selected_key", key),
					logger.Float64("available_count", cached.Available))
				return cached, key
			}
		}
		return nil, ""
	}

	// 从当前索引开始，尝试找到一个可用的token
	startIndex := tm.currentIndex
	tried := 0

	for tried < len(tm.configOrder) {
		key := tm.configOrder[tm.currentIndex]

		// 检查冷却期
		if tm.rateLimiter != nil && tm.rateLimiter.IsTokenInCooldown(key) {
			logger.Debug("token在冷却期，跳过",
				logger.String("token_key", key))
			tm.advanceToNextToken()
			tried++
			continue
		}

		// 检查每日限制
		if tm.rateLimiter != nil && tm.rateLimiter.IsDailyLimitExceeded(key) {
			logger.Debug("token已达每日限制，跳过",
				logger.String("token_key", key),
				logger.Int("daily_remaining", tm.rateLimiter.GetDailyRemaining(key)))
			tm.advanceToNextToken()
			tried++
			continue
		}

		cached, exists := tm.cache.tokens[key]
		if !exists {
			tm.advanceToNextToken()
			tried++
			continue
		}

		// 检查token是否过期
		if time.Since(cached.CachedAt) > tm.cache.ttl {
			tm.advanceToNextToken()
			tried++
			continue
		}

		// 检查token是否可用
		if !cached.IsUsable() {
			tm.advanceToNextToken()
			tried++
			continue
		}

		// 找到可用token，记录日志
		logger.Debug("轮询选择token",
			logger.String("selected_key", key),
			logger.Float64("available_count", cached.Available),
			logger.Int("current_index", tm.currentIndex),
			logger.Int("start_index", startIndex))

		return cached, key
	}

	// 所有token都不可用
	logger.Warn("所有token都不可用（轮询一圈后）",
		logger.Int("total_count", len(tm.configOrder)))
	return nil, ""
}

// selectBestTokenUnlocked 按配置顺序选择下一个可用token（保持向后兼容）
// 内部方法：调用者必须持有 tm.mutex
func (tm *TokenManager) selectBestTokenUnlocked() *CachedToken {
	token, _ := tm.selectNextAvailableTokenUnlocked()
	return token
}

// selectBestTokenWithKeyUnlocked 保持向后兼容的别名
func (tm *TokenManager) selectBestTokenWithKeyUnlocked() (*CachedToken, string) {
	return tm.selectNextAvailableTokenUnlocked()
}

// refreshCacheUnlocked 刷新token缓存
// 内部方法：调用者必须持有 tm.mutex
func (tm *TokenManager) refreshCacheUnlocked() error {
	logger.Debug("开始刷新token缓存")

	for i, cfg := range tm.configs {
		if cfg.Disabled {
			continue
		}

		// 刷新token
		token, err := tm.refreshSingleToken(cfg)
		if err != nil {
			logger.Warn("刷新单个token失败",
				logger.Int("config_index", i),
				logger.String("auth_type", cfg.AuthType),
				logger.Err(err))
			continue
		}

		// 检查使用限制
		var usageInfo *types.UsageLimits
		var available float64

		checker := NewUsageLimitsChecker()
		if usage, checkErr := checker.CheckUsageLimits(token); checkErr == nil {
			usageInfo = usage
			available = CalculateAvailableCount(usage)
		} else {
			logger.Warn("检查使用限制失败", logger.Err(checkErr))
		}

		// 更新缓存（直接访问，已在tm.mutex保护下）
		cacheKey := fmt.Sprintf(config.TokenCacheKeyFormat, i)
		tm.cache.tokens[cacheKey] = &CachedToken{
			Token:     token,
			UsageInfo: usageInfo,
			CachedAt:  time.Now(),
			Available: available,
		}

		logger.Debug("token缓存更新",
			logger.String("cache_key", cacheKey),
			logger.Float64("available", available))
	}

	tm.lastRefresh = time.Now()
	return nil
}

// IsUsable 检查缓存的token是否可用
func (ct *CachedToken) IsUsable() bool {
	// 检查token是否过期
	if time.Now().After(ct.Token.ExpiresAt) {
		return false
	}

	// 检查可用次数
	return ct.Available > 0
}

// CalculateAvailableCount 计算可用次数 (基于CREDIT资源类型，返回浮点精度)
func CalculateAvailableCount(usage *types.UsageLimits) float64 {
	for _, breakdown := range usage.UsageBreakdownList {
		if breakdown.ResourceType == "CREDIT" {
			var totalAvailable float64

			// 优先使用免费试用额度 (如果存在且处于ACTIVE状态)
			if breakdown.FreeTrialInfo != nil && breakdown.FreeTrialInfo.FreeTrialStatus == "ACTIVE" {
				freeTrialAvailable := breakdown.FreeTrialInfo.UsageLimitWithPrecision - breakdown.FreeTrialInfo.CurrentUsageWithPrecision
				totalAvailable += freeTrialAvailable
			}

			// 加上基础额度
			baseAvailable := breakdown.UsageLimitWithPrecision - breakdown.CurrentUsageWithPrecision
			totalAvailable += baseAvailable

			if totalAvailable < 0 {
				return 0.0
			}
			return totalAvailable
		}
	}
	return 0.0
}

// generateConfigOrder 生成token配置的顺序
func generateConfigOrder(configs []AuthConfig) []string {
	var order []string

	for i := range configs {
		// 使用索引生成cache key，与refreshCache中的逻辑保持一致
		cacheKey := fmt.Sprintf(config.TokenCacheKeyFormat, i)
		order = append(order, cacheKey)
	}

	logger.Debug("生成配置顺序",
		logger.Int("config_count", len(configs)),
		logger.Any("order", order))

	return order
}
