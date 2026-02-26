package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"kiro2api/config"
	"kiro2api/logger"
	"kiro2api/types"
	"sync"
	"time"
)

// TokenPoolStatus Token 池状态枚举
type TokenPoolStatus int

const (
	TokenStatusAvailable TokenPoolStatus = iota // 可用
	TokenStatusCooldown                         // 冷却中
	TokenStatusExhausted                        // 已耗尽
)

// PooledToken 池化的 Token 信息
type PooledToken struct {
	TokenKey      string          // token 标识
	Token         types.TokenInfo // token 信息
	Fingerprint   *Fingerprint    // 请求指纹
	Status        TokenPoolStatus // 状态
	CooldownUntil time.Time       // 冷却结束时间
	LastUsedAt    time.Time       // 最后使用时间
	FailCount     int             // 连续失败次数
	SuccessCount  int             // 成功次数
}

// SessionTokenPool 会话级 Token 池
type SessionTokenPool struct {
	SessionID      string         // 会话 ID
	PrimaryToken   *PooledToken   // 主账号
	BackupTokens   []*PooledToken // 备用账号池
	CreatedAt      time.Time      // 创建时间
	LastAccessedAt time.Time      // 最后访问时间
	TotalRequests  int            // 总请求数
	mutex          sync.RWMutex
}

// SessionTokenPoolManager 会话 Token 池管理器
type SessionTokenPoolManager struct {
	pools        map[string]*SessionTokenPool
	mutex        sync.RWMutex
	ttl          time.Duration
	maxPoolSize  int
	maxRetries   int
	cooldown     time.Duration
	tokenManager *TokenManager
	ctx          context.Context
	cancel       context.CancelFunc
}

var (
	sessionPoolManager     *SessionTokenPoolManager
	sessionPoolManagerOnce sync.Once
)

// GetSessionTokenPoolManager 获取全局会话池管理器（单例）
func GetSessionTokenPoolManager() *SessionTokenPoolManager {
	sessionPoolManagerOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		sessionPoolManager = &SessionTokenPoolManager{
			pools:       make(map[string]*SessionTokenPool),
			ttl:         config.SessionPoolTTL,
			maxPoolSize: config.SessionPoolMaxSize,
			maxRetries:  config.SessionPoolMaxRetries,
			cooldown:    config.SessionPoolCooldown,
			ctx:         ctx,
			cancel:      cancel,
		}
		go sessionPoolManager.cleanupLoop()
		logger.Info("会话级Token池管理器已初始化",
			logger.Bool("enabled", config.SessionPoolEnabled),
			logger.Int("max_pool_size", config.SessionPoolMaxSize),
			logger.Int("max_retries", config.SessionPoolMaxRetries))
	})
	return sessionPoolManager
}

// SetTokenManager 设置 TokenManager 引用
func (m *SessionTokenPoolManager) SetTokenManager(tm *TokenManager) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.tokenManager = tm
}

// GetOrCreatePool 获取或创建会话池
func (m *SessionTokenPoolManager) GetOrCreatePool(sessionID string) (*SessionTokenPool, error) {
	return m.GetOrCreatePoolForModel(sessionID, "")
}

// GetOrCreatePoolForModel 获取或创建会话池，并确保可为指定模型分配账号
func (m *SessionTokenPoolManager) GetOrCreatePoolForModel(sessionID, requestedModel string) (*SessionTokenPool, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if pool, exists := m.pools[sessionID]; exists {
		pool.LastAccessedAt = time.Now()
		return pool, nil
	}

	// 创建新池并分配主账号
	pool := &SessionTokenPool{
		SessionID:      sessionID,
		BackupTokens:   make([]*PooledToken, 0),
		CreatedAt:      time.Now(),
		LastAccessedAt: time.Now(),
	}

	// 分配主账号
	if err := m.allocatePrimaryTokenForModel(pool, requestedModel); err != nil {
		return nil, err
	}

	m.pools[sessionID] = pool
	logger.Debug("创建新会话池",
		logger.String("session_id", sessionID),
		logger.String("primary_token", pool.PrimaryToken.TokenKey))
	return pool, nil
}

// allocatePrimaryToken 为池分配主账号
func (m *SessionTokenPoolManager) allocatePrimaryToken(pool *SessionTokenPool) error {
	return m.allocatePrimaryTokenForModel(pool, "")
}

// allocatePrimaryTokenForModel 为池分配指定模型可用的主账号
func (m *SessionTokenPoolManager) allocatePrimaryTokenForModel(pool *SessionTokenPool, requestedModel string) error {
	if m.tokenManager == nil {
		return fmt.Errorf("TokenManager未初始化")
	}

	token, fingerprint, tokenKey, err := m.tokenManager.GetTokenWithFingerprintForSessionAndModel(pool.SessionID, requestedModel)
	if err != nil {
		return err
	}

	pool.PrimaryToken = &PooledToken{
		TokenKey:    tokenKey,
		Token:       token,
		Fingerprint: fingerprint,
		Status:      TokenStatusAvailable,
		LastUsedAt:  time.Now(),
	}
	return nil
}

// GetAvailableToken 获取可用 Token
func (m *SessionTokenPoolManager) GetAvailableToken(sessionID string) (types.TokenInfo, *Fingerprint, string, error) {
	return m.GetAvailableTokenForModel(sessionID, "")
}

// GetAvailableTokenForModel 获取可用于指定模型的 Token
func (m *SessionTokenPoolManager) GetAvailableTokenForModel(sessionID, requestedModel string) (types.TokenInfo, *Fingerprint, string, error) {
	pool, err := m.GetOrCreatePoolForModel(sessionID, requestedModel)
	if err != nil {
		return types.TokenInfo{}, nil, "", err
	}

	pool.mutex.Lock()
	pool.TotalRequests++
	now := time.Now()

	// 检查主账号
	if pool.PrimaryToken != nil && pool.PrimaryToken.Status == TokenStatusAvailable {
		if now.After(pool.PrimaryToken.CooldownUntil) &&
			m.tokenSupportsModel(pool.PrimaryToken.TokenKey, requestedModel) &&
			!m.tokenIsDisabled(pool.PrimaryToken.TokenKey) {
			pool.PrimaryToken.LastUsedAt = now
			pool.mutex.Unlock()
			return pool.PrimaryToken.Token, pool.PrimaryToken.Fingerprint, pool.PrimaryToken.TokenKey, nil
		}
	}

	// 检查备用账号
	for _, backup := range pool.BackupTokens {
		if backup.Status == TokenStatusAvailable &&
			now.After(backup.CooldownUntil) &&
			m.tokenSupportsModel(backup.TokenKey, requestedModel) &&
			!m.tokenIsDisabled(backup.TokenKey) {
			backup.LastUsedAt = now
			pool.mutex.Unlock()
			return backup.Token, backup.Fingerprint, backup.TokenKey, nil
		}
	}

	pool.mutex.Unlock()

	// 尝试分配新的备用 Token
	return m.TryAllocateBackupTokenForModel(sessionID, requestedModel)
}

// MarkTokenCooldown 标记 Token 进入冷却
func (m *SessionTokenPoolManager) MarkTokenCooldown(sessionID, tokenKey string, cooldownDuration time.Duration) {
	m.mutex.RLock()
	pool, exists := m.pools[sessionID]
	m.mutex.RUnlock()

	if !exists {
		return
	}

	pool.mutex.Lock()
	defer pool.mutex.Unlock()

	if cooldownDuration == 0 {
		cooldownDuration = m.cooldown
	}

	cooldownUntil := time.Now().Add(cooldownDuration)

	if pool.PrimaryToken != nil && pool.PrimaryToken.TokenKey == tokenKey {
		pool.PrimaryToken.Status = TokenStatusCooldown
		pool.PrimaryToken.CooldownUntil = cooldownUntil
		pool.PrimaryToken.FailCount++
		logger.Info("主Token进入冷却",
			logger.String("session_id", sessionID),
			logger.String("token_key", tokenKey),
			logger.Duration("cooldown", cooldownDuration))
		return
	}

	for _, backup := range pool.BackupTokens {
		if backup.TokenKey == tokenKey {
			backup.Status = TokenStatusCooldown
			backup.CooldownUntil = cooldownUntil
			backup.FailCount++
			logger.Info("备用Token进入冷却",
				logger.String("session_id", sessionID),
				logger.String("token_key", tokenKey),
				logger.Duration("cooldown", cooldownDuration))
			return
		}
	}
}

// MarkTokenSuccess 标记 Token 请求成功
func (m *SessionTokenPoolManager) MarkTokenSuccess(sessionID, tokenKey string) {
	m.mutex.RLock()
	pool, exists := m.pools[sessionID]
	m.mutex.RUnlock()

	if !exists {
		return
	}

	pool.mutex.Lock()
	defer pool.mutex.Unlock()

	if pool.PrimaryToken != nil && pool.PrimaryToken.TokenKey == tokenKey {
		pool.PrimaryToken.SuccessCount++
		pool.PrimaryToken.FailCount = 0
		return
	}

	for _, backup := range pool.BackupTokens {
		if backup.TokenKey == tokenKey {
			backup.SuccessCount++
			backup.FailCount = 0
			return
		}
	}
}

// TryAllocateBackupToken 尝试分配备用 Token
func (m *SessionTokenPoolManager) TryAllocateBackupToken(sessionID string) (types.TokenInfo, *Fingerprint, string, error) {
	return m.TryAllocateBackupTokenForModel(sessionID, "")
}

// TryAllocateBackupTokenForModel 尝试分配指定模型可用的备用 Token
func (m *SessionTokenPoolManager) TryAllocateBackupTokenForModel(sessionID, requestedModel string) (types.TokenInfo, *Fingerprint, string, error) {
	m.mutex.RLock()
	pool, exists := m.pools[sessionID]
	m.mutex.RUnlock()

	if !exists {
		return types.TokenInfo{}, nil, "", fmt.Errorf("会话池不存在")
	}

	pool.mutex.Lock()
	defer pool.mutex.Unlock()

	// 检查池大小限制
	currentSize := 1 + len(pool.BackupTokens) // 主账号 + 备用账号
	if currentSize >= m.maxPoolSize {
		return types.TokenInfo{}, nil, "", fmt.Errorf("会话池已满")
	}

	// 从全局池分配新 Token
	if m.tokenManager == nil {
		return types.TokenInfo{}, nil, "", fmt.Errorf("TokenManager未初始化")
	}

	token, fingerprint, tokenKey, err := m.tokenManager.GetTokenWithFingerprintForSessionAndModel(sessionID+"_backup", requestedModel)
	if err != nil {
		return types.TokenInfo{}, nil, "", err
	}

	// 检查是否已存在
	if pool.PrimaryToken != nil && pool.PrimaryToken.TokenKey == tokenKey {
		return types.TokenInfo{}, nil, "", fmt.Errorf("分配到重复Token")
	}
	for _, backup := range pool.BackupTokens {
		if backup.TokenKey == tokenKey {
			return types.TokenInfo{}, nil, "", fmt.Errorf("分配到重复Token")
		}
	}

	backup := &PooledToken{
		TokenKey:    tokenKey,
		Token:       token,
		Fingerprint: fingerprint,
		Status:      TokenStatusAvailable,
		LastUsedAt:  time.Now(),
	}
	pool.BackupTokens = append(pool.BackupTokens, backup)

	logger.Info("分配备用Token",
		logger.String("session_id", sessionID),
		logger.String("token_key", tokenKey),
		logger.Int("pool_size", currentSize+1))

	return token, fingerprint, tokenKey, nil
}

// GetNextAvailableToken 获取下一个可用 Token（429 重试时调用）
func (m *SessionTokenPoolManager) GetNextAvailableToken(sessionID, currentTokenKey string) (types.TokenInfo, *Fingerprint, string, error) {
	return m.GetNextAvailableTokenForModel(sessionID, currentTokenKey, "")
}

// GetNextAvailableTokenForModel 获取指定模型的下一个可用 Token（429 重试时调用）
func (m *SessionTokenPoolManager) GetNextAvailableTokenForModel(sessionID, currentTokenKey, requestedModel string) (types.TokenInfo, *Fingerprint, string, error) {
	m.mutex.RLock()
	pool, exists := m.pools[sessionID]
	m.mutex.RUnlock()

	if !exists {
		return types.TokenInfo{}, nil, "", fmt.Errorf("会话池不存在")
	}

	pool.mutex.RLock()
	now := time.Now()

	// 检查主账号（如果不是当前失败的）
	if pool.PrimaryToken != nil && pool.PrimaryToken.TokenKey != currentTokenKey {
		if pool.PrimaryToken.Status == TokenStatusAvailable &&
			now.After(pool.PrimaryToken.CooldownUntil) &&
			m.tokenSupportsModel(pool.PrimaryToken.TokenKey, requestedModel) &&
			!m.tokenIsDisabled(pool.PrimaryToken.TokenKey) {
			pool.mutex.RUnlock()
			return pool.PrimaryToken.Token, pool.PrimaryToken.Fingerprint, pool.PrimaryToken.TokenKey, nil
		}
	}

	// 检查备用账号
	for _, backup := range pool.BackupTokens {
		if backup.TokenKey != currentTokenKey &&
			backup.Status == TokenStatusAvailable &&
			now.After(backup.CooldownUntil) &&
			m.tokenSupportsModel(backup.TokenKey, requestedModel) &&
			!m.tokenIsDisabled(backup.TokenKey) {
			pool.mutex.RUnlock()
			return backup.Token, backup.Fingerprint, backup.TokenKey, nil
		}
	}
	pool.mutex.RUnlock()

	// 尝试分配新的备用 Token
	return m.TryAllocateBackupTokenForModel(sessionID, requestedModel)
}

// GetPoolStats 获取会话池统计信息
func (m *SessionTokenPoolManager) GetPoolStats(sessionID string) map[string]any {
	m.mutex.RLock()
	pool, exists := m.pools[sessionID]
	m.mutex.RUnlock()

	if !exists {
		return map[string]any{"exists": false}
	}

	pool.mutex.RLock()
	defer pool.mutex.RUnlock()

	backupStats := make([]map[string]any, 0, len(pool.BackupTokens))
	for _, backup := range pool.BackupTokens {
		backupStats = append(backupStats, map[string]any{
			"token_key":     backup.TokenKey,
			"status":        backup.Status,
			"fail_count":    backup.FailCount,
			"success_count": backup.SuccessCount,
		})
	}

	return map[string]any{
		"exists":           true,
		"session_id":       pool.SessionID,
		"total_requests":   pool.TotalRequests,
		"created_at":       pool.CreatedAt.Format(time.RFC3339),
		"last_accessed":    pool.LastAccessedAt.Format(time.RFC3339),
		"primary_token":    pool.PrimaryToken.TokenKey,
		"backup_count":     len(pool.BackupTokens),
		"backup_tokens":    backupStats,
		"max_pool_size":    m.maxPoolSize,
		"max_retries":      m.maxRetries,
		"cooldown_seconds": m.cooldown.Seconds(),
	}
}

// UnbindSession 解绑会话
func (m *SessionTokenPoolManager) UnbindSession(sessionID string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if pool, exists := m.pools[sessionID]; exists {
		logger.Debug("解绑会话池",
			logger.String("session_id", sessionID),
			logger.Int("total_requests", pool.TotalRequests))
		delete(m.pools, sessionID)
	}
}

func (m *SessionTokenPoolManager) tokenSupportsModel(tokenKey, requestedModel string) bool {
	if requestedModel == "" || m.tokenManager == nil {
		return true
	}
	return m.tokenManager.IsTokenAllowedForModel(tokenKey, requestedModel)
}

// tokenIsDisabled 检查 token 是否被临时禁用
func (m *SessionTokenPoolManager) tokenIsDisabled(tokenKey string) bool {
	if m.tokenManager == nil {
		return false
	}
	return m.tokenManager.isTokenDisabled(tokenKey)
}

// cleanupLoop 定期清理过期会话池
func (m *SessionTokenPoolManager) cleanupLoop() {
	ticker := time.NewTicker(m.ttl / 2)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.cleanup()
		}
	}
}

// cleanup 清理过期会话池
func (m *SessionTokenPoolManager) cleanup() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	now := time.Now()
	expiredCount := 0

	for sessionID, pool := range m.pools {
		if now.Sub(pool.LastAccessedAt) > m.ttl {
			delete(m.pools, sessionID)
			expiredCount++
		}
	}

	if expiredCount > 0 {
		logger.Info("会话池清理完成",
			logger.Int("expired_count", expiredCount),
			logger.Int("remaining_count", len(m.pools)))
	}
}

// Stop 停止管理器
func (m *SessionTokenPoolManager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
}

// CalculateCooldownDuration 计算冷却时间（解析 API 返回的 quota_reset_timestamp）
func CalculateCooldownDuration(responseBody []byte, defaultDuration time.Duration) time.Duration {
	var errorResp struct {
		QuotaResetTimestamp int64 `json:"quota_reset_timestamp"`
	}
	if err := json.Unmarshal(responseBody, &errorResp); err == nil {
		if errorResp.QuotaResetTimestamp > 0 {
			resetTime := time.Unix(errorResp.QuotaResetTimestamp, 0)
			cooldown := time.Until(resetTime)
			if cooldown > 0 && cooldown < 24*time.Hour {
				return cooldown
			}
		}
	}
	return defaultDuration
}
