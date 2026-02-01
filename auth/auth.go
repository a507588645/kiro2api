package auth

import (
	"fmt"
	"kiro2api/logger"
	"kiro2api/types"
	"os"
	"sync"
)

// AuthService 认证服务（推荐使用依赖注入方式）
type AuthService struct {
	tokenManager *TokenManager
	configs      []AuthConfig
	mu           sync.RWMutex
}

// 全局 AuthService 实例引用（用于 OAuth token 重载）
var (
	globalAuthService *AuthService
	globalAuthMu      sync.RWMutex
)

// SetGlobalAuthService 设置全局 AuthService 实例
func SetGlobalAuthService(as *AuthService) {
	globalAuthMu.Lock()
	globalAuthService = as
	globalAuthMu.Unlock()
}

// GetGlobalAuthService 获取全局 AuthService 实例
func GetGlobalAuthService() *AuthService {
	globalAuthMu.RLock()
	defer globalAuthMu.RUnlock()
	return globalAuthService
}

// NewAuthService 创建新的认证服务（推荐使用此方法而不是全局函数）
func NewAuthService() (*AuthService, error) {
	logger.Info("开始初始化AuthService")
	logger.Info("OAuth启用状态", logger.Bool("enabled", IsOAuthEnabled()))

	// 加载配置
	configs, err := loadConfigs()
	if err != nil {
		// 如果启用了 OAuth，允许无 token 启动
		if IsOAuthEnabled() {
			logger.Info("OAuth已启用，允许无token启动")
			configs = []AuthConfig{}
		} else {
			return nil, fmt.Errorf("加载配置失败: %w", err)
		}
	}

	// 尝试从 OAuth 存储加载已授权的 token
	if IsOAuthEnabled() {
		store := GetOAuthTokenStore()
		oauthConfigs := store.ToAuthConfigs()
		logger.Info("OAuth存储token数量", logger.Int("count", len(oauthConfigs)), logger.Int("store_tokens", len(store.GetTokens())))
		if len(oauthConfigs) > 0 {
			configs = append(configs, oauthConfigs...)
			logger.Info("从OAuth存储加载token", logger.Int("count", len(oauthConfigs)))
		}
	}

	if len(configs) == 0 && !IsOAuthEnabled() {
		return nil, fmt.Errorf("未找到有效的token配置")
	}

	// 创建token管理器（可能为空配置）
	logger.Info("准备创建TokenManager", logger.Int("config_count", len(configs)))
	var tokenManager *TokenManager
	if len(configs) > 0 {
		tokenManager = NewTokenManager(configs)
		logger.Info("TokenManager创建成功")

		// 预热第一个可用token（可通过环境变量跳过，便于测试/离线环境）
		if !shouldSkipTokenWarmup() {
			_, warmupErr := tokenManager.getBestToken()
			if warmupErr != nil {
				logger.Warn("token预热失败", logger.Err(warmupErr))
			}
		} else {
			logger.Info("已跳过token预热（SKIP_TOKEN_WARMUP=true）")
		}
	}

	logger.Info("AuthService创建完成", logger.Int("config_count", len(configs)))

	return &AuthService{
		tokenManager: tokenManager,
		configs:      configs,
	}, nil
}

// GetToken 获取可用的token
func (as *AuthService) GetToken() (types.TokenInfo, error) {
	if as.tokenManager == nil {
		return types.TokenInfo{}, fmt.Errorf("token管理器未初始化")
	}
	return as.tokenManager.getBestToken()
}

// GetTokenWithFingerprint 获取token及其对应的指纹
func (as *AuthService) GetTokenWithFingerprint() (types.TokenInfo, *Fingerprint, error) {
	if as.tokenManager == nil {
		return types.TokenInfo{}, nil, fmt.Errorf("token管理器未初始化")
	}
	return as.tokenManager.GetTokenWithFingerprint()
}

// GetTokenWithFingerprintForSession 为会话获取token及其对应的指纹
func (as *AuthService) GetTokenWithFingerprintForSession(sessionID string) (types.TokenInfo, *Fingerprint, string, error) {
	if as.tokenManager == nil {
		return types.TokenInfo{}, nil, "", fmt.Errorf("token管理器未初始化")
	}
	return as.tokenManager.GetTokenWithFingerprintForSession(sessionID)
}

// MarkTokenFailed 标记当前token请求失败
func (as *AuthService) MarkTokenFailed() {
	if as.tokenManager == nil {
		return
	}
	tokenKey := as.tokenManager.GetCurrentTokenKey()
	if tokenKey != "" {
		as.tokenManager.MarkTokenFailed(tokenKey)
	}
}

// GetTokenManager 获取底层的TokenManager（用于高级操作）
func (as *AuthService) GetTokenManager() *TokenManager {
	return as.tokenManager
}

// GetConfigs 获取认证配置
func (as *AuthService) GetConfigs() []AuthConfig {
	return as.configs
}

// ReloadTokens 重新加载 token 配置（OAuth token 添加后调用）
func (as *AuthService) ReloadTokens() error {
	as.mu.Lock()
	defer as.mu.Unlock()

	configs := GetOAuthTokenStore().ToAuthConfigs()
	if len(configs) == 0 {
		return fmt.Errorf("没有可用的 token")
	}
	as.tokenManager = NewTokenManager(configs)
	as.configs = configs
	logger.Info("TokenManager 已重载", logger.Int("count", len(configs)))
	return nil
}

// shouldSkipTokenWarmup 判断是否跳过token预热
func shouldSkipTokenWarmup() bool {
	val := os.Getenv("SKIP_TOKEN_WARMUP")
	return val == "true" || val == "1"
}
