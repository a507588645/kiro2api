package server

import (
	"bytes"
	"context"
	"io"
	"math"
	"math/rand"
	"net/http"
	"time"

	"kiro2api/auth"
	"kiro2api/config"
	"kiro2api/logger"
	"kiro2api/types"
)

// ========== 重试配置 ==========

// RetryConfig 重试配置
type RetryConfig struct {
	MaxRetries      int           // 最大重试次数
	InitialInterval time.Duration // 初始重试间隔
	MaxInterval     time.Duration // 最大重试间隔
	BackoffFactor   float64       // 退避因子（指数退避）
	JitterPercent   int           // 抖动百分比（0-100）
}

// DefaultRetryConfig 默认重试配置
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries:      config.SessionPoolMaxRetries,
		InitialInterval: config.SessionPoolRetryInterval,
		MaxInterval:     config.RateLimitBackoffMax,
		BackoffFactor:   config.RateLimitBackoffMultiplier,
		JitterPercent:   config.RateLimitJitterPercent,
	}
}

// ========== 重试结果 ==========

// RetryResult 重试结果
type RetryResult struct {
	Response    *http.Response
	Body        []byte
	TokenKey    string
	Fingerprint *auth.Fingerprint
	Retries     int
	Success     bool
}

// ========== 重试器接口 ==========

// Retrier 重试器接口
type Retrier interface {
	// ExecuteWithRetry 执行请求并在失败时自动重试
	ExecuteWithRetry(ctx context.Context, sessionID string, executeFunc ExecuteFunc) (*RetryResult, error)
}

// ExecuteFunc 请求执行函数类型
type ExecuteFunc func(token types.TokenInfo, fingerprint *auth.Fingerprint) (*http.Response, []byte, error)

// ========== 指数退避重试器 ==========

// ExponentialBackoffRetrier 指数退避重试器
type ExponentialBackoffRetrier struct {
	poolManager *auth.SessionTokenPoolManager
	config      *RetryConfig
	errorMapper *ErrorMapper
}

// NewExponentialBackoffRetrier 创建指数退避重试器
func NewExponentialBackoffRetrier(cfg *RetryConfig) *ExponentialBackoffRetrier {
	if cfg == nil {
		cfg = DefaultRetryConfig()
	}
	return &ExponentialBackoffRetrier{
		poolManager: auth.GetSessionTokenPoolManager(),
		config:      cfg,
		errorMapper: NewErrorMapper(),
	}
}

// ExecuteWithRetry 执行请求并在失败时自动重试
func (r *ExponentialBackoffRetrier) ExecuteWithRetry(ctx context.Context, sessionID string, executeFunc ExecuteFunc) (*RetryResult, error) {
	// 获取初始 Token
	token, fingerprint, tokenKey, err := r.poolManager.GetAvailableToken(sessionID)
	if err != nil {
		return nil, err
	}

	var lastBody []byte
	var lastResp *http.Response

	for retry := 0; retry <= r.config.MaxRetries; retry++ {
		// 检查上下文是否已取消
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// 执行请求
		resp, body, err := executeFunc(token, fingerprint)
		if err != nil {
			return nil, err
		}

		lastBody = body
		lastResp = resp

		// 检查是否需要重试
		if !r.shouldRetry(resp.StatusCode) {
			// 成功或不可重试的错误
			r.poolManager.MarkTokenSuccess(sessionID, tokenKey)
			return &RetryResult{
				Response:    resp,
				Body:        body,
				TokenKey:    tokenKey,
				Fingerprint: fingerprint,
				Retries:     retry,
				Success:     resp.StatusCode == http.StatusOK,
			}, nil
		}

		// 需要重试
		logger.Warn("请求失败，准备重试",
			logger.String("session_id", sessionID),
			logger.String("token_key", tokenKey),
			logger.Int("status_code", resp.StatusCode),
			logger.Int("retry", retry),
			logger.Int("max_retries", r.config.MaxRetries))

		// 计算冷却时间
		cooldown := r.calculateCooldown(body, retry)
		r.poolManager.MarkTokenCooldown(sessionID, tokenKey, cooldown)

		// 关闭当前响应
		resp.Body.Close()

		// 如果已达最大重试次数，返回最后的错误
		if retry >= r.config.MaxRetries {
			logger.Error("达到最大重试次数",
				logger.String("session_id", sessionID),
				logger.Int("retries", retry))

			return &RetryResult{
				Response:    &http.Response{StatusCode: lastResp.StatusCode},
				Body:        lastBody,
				TokenKey:    tokenKey,
				Fingerprint: fingerprint,
				Retries:     retry,
				Success:     false,
			}, nil
		}

		// 获取下一个可用 Token
		nextToken, nextFingerprint, nextTokenKey, err := r.poolManager.GetNextAvailableToken(sessionID, tokenKey)
		if err != nil {
			logger.Warn("无法获取下一个 Token，使用当前 Token 重试",
				logger.String("session_id", sessionID),
				logger.Err(err))
			// 继续使用当前 Token，但等待退避时间
		} else {
			token = nextToken
			fingerprint = nextFingerprint
			tokenKey = nextTokenKey
			logger.Info("切换到新 Token 重试",
				logger.String("session_id", sessionID),
				logger.String("new_token_key", tokenKey),
				logger.Int("retry", retry+1))
		}

		// 等待退避时间
		backoffDuration := r.calculateBackoff(retry)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoffDuration):
		}
	}

	return nil, nil
}

// shouldRetry 判断是否应该重试
func (r *ExponentialBackoffRetrier) shouldRetry(statusCode int) bool {
	// 可重试的状态码
	retryableCodes := map[int]bool{
		http.StatusTooManyRequests:     true, // 429
		http.StatusServiceUnavailable:  true, // 503
		http.StatusGatewayTimeout:      true, // 504
		http.StatusBadGateway:          true, // 502
	}
	return retryableCodes[statusCode]
}

// calculateBackoff 计算退避时间（指数退避 + 抖动）
func (r *ExponentialBackoffRetrier) calculateBackoff(retry int) time.Duration {
	// 指数退避：initialInterval * (backoffFactor ^ retry)
	backoff := float64(r.config.InitialInterval) * math.Pow(r.config.BackoffFactor, float64(retry))

	// 限制最大退避时间
	if backoff > float64(r.config.MaxInterval) {
		backoff = float64(r.config.MaxInterval)
	}

	// 添加抖动
	if r.config.JitterPercent > 0 {
		jitterRange := backoff * float64(r.config.JitterPercent) / 100
		jitter := rand.Float64() * jitterRange
		backoff += jitter
	}

	return time.Duration(backoff)
}

// calculateCooldown 计算冷却时间
func (r *ExponentialBackoffRetrier) calculateCooldown(body []byte, retry int) time.Duration {
	// 尝试从响应体中提取冷却时间
	cooldown := auth.CalculateCooldownDuration(body, config.SessionPoolCooldown)

	// 根据重试次数增加冷却时间
	multiplier := math.Pow(r.config.BackoffFactor, float64(retry))
	return time.Duration(float64(cooldown) * multiplier)
}

// ========== 简单重试器（向后兼容） ==========

// SimpleRetrier 简单重试器（无指数退避）
type SimpleRetrier struct {
	poolManager   *auth.SessionTokenPoolManager
	maxRetries    int
	retryInterval time.Duration
}

// NewSimpleRetrier 创建简单重试器
func NewSimpleRetrier() *SimpleRetrier {
	return &SimpleRetrier{
		poolManager:   auth.GetSessionTokenPoolManager(),
		maxRetries:    config.SessionPoolMaxRetries,
		retryInterval: config.SessionPoolRetryInterval,
	}
}

// ExecuteWithRetry 执行请求并在 429 时自动重试
func (r *SimpleRetrier) ExecuteWithRetry(ctx context.Context, sessionID string, executeFunc ExecuteFunc) (*RetryResult, error) {
	// 获取初始 Token
	token, fingerprint, tokenKey, err := r.poolManager.GetAvailableToken(sessionID)
	if err != nil {
		return nil, err
	}

	for retry := 0; retry <= r.maxRetries; retry++ {
		// 检查上下文
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// 执行请求
		resp, body, err := executeFunc(token, fingerprint)
		if err != nil {
			return nil, err
		}

		// 检查是否为 429
		if resp.StatusCode != http.StatusTooManyRequests {
			// 成功或其他错误
			r.poolManager.MarkTokenSuccess(sessionID, tokenKey)
			return &RetryResult{
				Response:    resp,
				Body:        body,
				TokenKey:    tokenKey,
				Fingerprint: fingerprint,
				Retries:     retry,
				Success:     resp.StatusCode == http.StatusOK,
			}, nil
		}

		// 429 错误处理
		logger.Warn("收到 429 错误，尝试切换 Token 重试",
			logger.String("session_id", sessionID),
			logger.String("token_key", tokenKey),
			logger.Int("retry", retry),
			logger.Int("max_retries", r.maxRetries))

		// 计算冷却时间
		cooldown := auth.CalculateCooldownDuration(body, config.SessionPoolCooldown)
		r.poolManager.MarkTokenCooldown(sessionID, tokenKey, cooldown)

		// 关闭当前响应
		resp.Body.Close()

		// 如果已达最大重试次数
		if retry >= r.maxRetries {
			logger.Error("达到最大重试次数",
				logger.String("session_id", sessionID),
				logger.Int("retries", retry))
			return &RetryResult{
				Response:    &http.Response{StatusCode: http.StatusTooManyRequests},
				Body:        body,
				TokenKey:    tokenKey,
				Fingerprint: fingerprint,
				Retries:     retry,
				Success:     false,
			}, nil
		}

		// 获取下一个可用 Token
		nextToken, nextFingerprint, nextTokenKey, err := r.poolManager.GetNextAvailableToken(sessionID, tokenKey)
		if err != nil {
			logger.Warn("无法获取下一个 Token",
				logger.String("session_id", sessionID),
				logger.Err(err))
			return &RetryResult{
				Response:    &http.Response{StatusCode: http.StatusTooManyRequests},
				Body:        body,
				TokenKey:    tokenKey,
				Fingerprint: fingerprint,
				Retries:     retry,
				Success:     false,
			}, nil
		}

		// 等待重试间隔
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(r.retryInterval):
		}

		// 更新为新 Token
		token = nextToken
		fingerprint = nextFingerprint
		tokenKey = nextTokenKey

		logger.Info("切换到新 Token 重试",
			logger.String("session_id", sessionID),
			logger.String("new_token_key", tokenKey),
			logger.Int("retry", retry+1))
	}

	return nil, nil
}

// ========== 工具函数 ==========

// ReadResponseBody 读取响应体并保留原始 Body
func ReadResponseBody(resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

// ========== 向后兼容 ==========

// RequestRetrier 请求重试器（向后兼容别名）
type RequestRetrier = SimpleRetrier

// NewRequestRetrier 创建请求重试器（向后兼容）
func NewRequestRetrier() *RequestRetrier {
	return NewSimpleRetrier()
}
