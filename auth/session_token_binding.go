package auth

import (
	"context"
	"fmt"
	"kiro2api/logger"
	"kiro2api/types"
	"os"
	"sync"
	"time"
)

// SessionTokenBinding 会话级 Token 绑定
// 解决 Roo/Kilo 工具循环问题：同一会话使用同一 Token，保留上下文
type SessionTokenBinding struct {
	sessionID      string
	tokenKey       string
	token          types.TokenInfo
	fingerprint    *Fingerprint
	createdAt      time.Time
	lastAccessedAt time.Time
	requestCount   int
}

// SessionTokenBindingManager 会话 Token 绑定管理器
type SessionTokenBindingManager struct {
	bindings map[string]*SessionTokenBinding
	mutex    sync.RWMutex
	ttl      time.Duration
	enabled  bool

	// 清理相关
	ctx    context.Context
	cancel context.CancelFunc
}

var (
	sessionBindingManager     *SessionTokenBindingManager
	sessionBindingManagerOnce sync.Once
)

// GetSessionTokenBindingManager 获取全局会话绑定管理器（单例）
func GetSessionTokenBindingManager() *SessionTokenBindingManager {
	sessionBindingManagerOnce.Do(func() {
		enabled := os.Getenv("SESSION_TOKEN_BINDING_ENABLED") != "false" // 默认启用
		ttl := parseDuration(os.Getenv("SESSION_TOKEN_BINDING_TTL"), 30*time.Minute)

		ctx, cancel := context.WithCancel(context.Background())
		sessionBindingManager = &SessionTokenBindingManager{
			bindings: make(map[string]*SessionTokenBinding),
			ttl:      ttl,
			enabled:  enabled,
			ctx:      ctx,
			cancel:   cancel,
		}

		// 启动清理协程
		go sessionBindingManager.cleanupLoop()

		logger.Info("会话级Token绑定管理器已初始化",
			logger.Bool("enabled", enabled),
			logger.Duration("ttl", ttl))
	})
	return sessionBindingManager
}

// BindSessionToken 绑定会话到 Token
func (m *SessionTokenBindingManager) BindSessionToken(
	sessionID string,
	tokenKey string,
	token types.TokenInfo,
	fingerprint *Fingerprint,
) {
	if !m.enabled {
		return
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	now := time.Now()
	m.bindings[sessionID] = &SessionTokenBinding{
		sessionID:      sessionID,
		tokenKey:       tokenKey,
		token:          token,
		fingerprint:    fingerprint,
		createdAt:      now,
		lastAccessedAt: now,
		requestCount:   1,
	}

	logger.Debug("会话已绑定Token",
		logger.String("session_id", sessionID),
		logger.String("token_key", tokenKey))
}

// GetSessionToken 获取会话绑定的 Token
func (m *SessionTokenBindingManager) GetSessionToken(sessionID string) (types.TokenInfo, *Fingerprint, string, bool) {
	if !m.enabled {
		return types.TokenInfo{}, nil, "", false
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	binding, exists := m.bindings[sessionID]
	if !exists {
		return types.TokenInfo{}, nil, "", false
	}

	// 检查是否过期
	if time.Since(binding.lastAccessedAt) > m.ttl {
		delete(m.bindings, sessionID)
		logger.Debug("会话绑定已过期",
			logger.String("session_id", sessionID),
			logger.Duration("age", time.Since(binding.createdAt)))
		return types.TokenInfo{}, nil, "", false
	}

	// 更新访问时间和计数
	binding.lastAccessedAt = time.Now()
	binding.requestCount++

	logger.Debug("使用会话绑定的Token",
		logger.String("session_id", sessionID),
		logger.String("token_key", binding.tokenKey),
		logger.Int("request_count", binding.requestCount))

	return binding.token, binding.fingerprint, binding.tokenKey, true
}

// UnbindSession 解绑会话
func (m *SessionTokenBindingManager) UnbindSession(sessionID string) {
	if !m.enabled {
		return
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	if binding, exists := m.bindings[sessionID]; exists {
		logger.Debug("会话已解绑",
			logger.String("session_id", sessionID),
			logger.String("token_key", binding.tokenKey),
			logger.Int("total_requests", binding.requestCount),
			logger.Duration("session_duration", time.Since(binding.createdAt)))
		delete(m.bindings, sessionID)
	}
}

// GetSessionStats 获取会话统计信息
func (m *SessionTokenBindingManager) GetSessionStats(sessionID string) map[string]any {
	if !m.enabled {
		return map[string]any{"enabled": false}
	}

	m.mutex.RLock()
	defer m.mutex.RUnlock()

	binding, exists := m.bindings[sessionID]
	if !exists {
		return map[string]any{
			"enabled": true,
			"bound":   false,
		}
	}

	return map[string]any{
		"enabled":          true,
		"bound":            true,
		"session_id":       binding.sessionID,
		"token_key":        binding.tokenKey,
		"created_at":       binding.createdAt.Format(time.RFC3339),
		"last_accessed_at": binding.lastAccessedAt.Format(time.RFC3339),
		"request_count":    binding.requestCount,
		"age_seconds":      time.Since(binding.createdAt).Seconds(),
		"ttl_seconds":      m.ttl.Seconds(),
	}
}

// GetAllStats 获取所有会话统计
func (m *SessionTokenBindingManager) GetAllStats() map[string]any {
	if !m.enabled {
		return map[string]any{"enabled": false}
	}

	m.mutex.RLock()
	defer m.mutex.RUnlock()

	sessions := make([]map[string]any, 0, len(m.bindings))
	totalRequests := 0

	for _, binding := range m.bindings {
		sessions = append(sessions, map[string]any{
			"session_id":       binding.sessionID,
			"token_key":        binding.tokenKey,
			"request_count":    binding.requestCount,
			"age_seconds":      time.Since(binding.createdAt).Seconds(),
			"last_accessed_at": binding.lastAccessedAt.Format(time.RFC3339),
		})
		totalRequests += binding.requestCount
	}

	return map[string]any{
		"enabled":        true,
		"total_sessions": len(m.bindings),
		"total_requests": totalRequests,
		"ttl_seconds":    m.ttl.Seconds(),
		"sessions":       sessions,
	}
}

// cleanupLoop 定期清理过期会话
func (m *SessionTokenBindingManager) cleanupLoop() {
	ticker := time.NewTicker(m.ttl / 2) // 每半个 TTL 清理一次
	defer ticker.Stop()

	logger.Info("会话绑定清理协程已启动",
		logger.Duration("cleanup_interval", m.ttl/2))

	for {
		select {
		case <-m.ctx.Done():
			logger.Info("会话绑定清理协程已停止")
			return
		case <-ticker.C:
			m.cleanup()
		}
	}
}

// cleanup 清理过期会话
func (m *SessionTokenBindingManager) cleanup() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	now := time.Now()
	expiredCount := 0

	for sessionID, binding := range m.bindings {
		if now.Sub(binding.lastAccessedAt) > m.ttl {
			logger.Debug("清理过期会话绑定",
				logger.String("session_id", sessionID),
				logger.String("token_key", binding.tokenKey),
				logger.Duration("age", now.Sub(binding.createdAt)))
			delete(m.bindings, sessionID)
			expiredCount++
		}
	}

	if expiredCount > 0 {
		logger.Info("会话绑定清理完成",
			logger.Int("expired_count", expiredCount),
			logger.Int("remaining_count", len(m.bindings)))
	}
}

// Stop 停止管理器
func (m *SessionTokenBindingManager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
}

// parseDuration 解析时间字符串，失败时返回默认值
func parseDuration(s string, defaultDuration time.Duration) time.Duration {
	if s == "" {
		return defaultDuration
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		logger.Warn("解析时间配置失败，使用默认值",
			logger.String("input", s),
			logger.Duration("default", defaultDuration),
			logger.Err(err))
		return defaultDuration
	}
	return d
}

// ExtractSessionID 从请求中提取会话 ID
// 优先级：X-Session-ID header > X-Request-ID header > 生成新 ID
func ExtractSessionID(headers map[string]string) string {
	// 1. 检查 X-Session-ID
	if sessionID := headers["X-Session-ID"]; sessionID != "" {
		return sessionID
	}

	// 2. 检查 X-Request-ID（Roo/Kilo 通常会发送）
	if requestID := headers["X-Request-ID"]; requestID != "" {
		return fmt.Sprintf("req_%s", requestID)
	}

	// 3. 生成新 ID
	return fmt.Sprintf("session_%d", time.Now().UnixNano())
}
