package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"kiro2api/logger"
)

// OAuthTokenStore OAuth token 存储
type OAuthTokenStore struct {
	Tokens []StoredToken `json:"tokens"`
	mutex  sync.RWMutex
	path   string
}

// StoredToken 存储的 token 信息
type StoredToken struct {
	ID           string    `json:"id"`
	RefreshToken string    `json:"refreshToken"`
	AccessToken  string    `json:"accessToken,omitempty"`
	ClientID     string    `json:"clientId,omitempty"`
	ClientSecret string    `json:"clientSecret,omitempty"`
	Region       string    `json:"region,omitempty"`
	AuthMethod   string    `json:"authMethod,omitempty"`
	Provider     string    `json:"provider"`
	CreatedAt    time.Time `json:"createdAt"`
	ExpiresAt    time.Time `json:"expiresAt,omitempty"`
	Disabled     bool      `json:"disabled,omitempty"`
}

var (
	tokenStore     *OAuthTokenStore
	tokenStoreOnce sync.Once
)

// GetOAuthTokenStore 获取 token 存储单例
func GetOAuthTokenStore() *OAuthTokenStore {
	tokenStoreOnce.Do(func() {
		path := os.Getenv("OAUTH_TOKEN_FILE")
		if path == "" {
			path = "./oauth_tokens.json"
		}
		tokenStore = &OAuthTokenStore{
			Tokens: []StoredToken{},
			path:   path,
		}
		tokenStore.load()
	})
	return tokenStore
}

// AddToken 添加新 token
func (s *OAuthTokenStore) AddToken(token *OAuthToken) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Check for duplicates
	for i, t := range s.Tokens {
		if t.RefreshToken == token.RefreshToken {
			// Update existing token
			s.Tokens[i].AccessToken = token.AccessToken
			s.Tokens[i].ClientID = token.ClientID
			s.Tokens[i].ClientSecret = token.ClientSecret
			s.Tokens[i].ExpiresAt = token.ExpiresAt
			// Preserve other fields if not present in update?
			// For now, just update what we have.
			return s.save()
		}
	}

	stored := StoredToken{
		ID:           generateRandomString(16),
		RefreshToken: token.RefreshToken,
		AccessToken:  token.AccessToken,
		ClientID:     token.ClientID,
		ClientSecret: token.ClientSecret,
		Region:       token.Region,
		AuthMethod:   token.AuthMethod,
		Provider:     token.Provider,
		CreatedAt:    time.Now(),
		ExpiresAt:    token.ExpiresAt,
	}

	// Try to infer AuthMethod if not explicitly set in OAuthToken (which it isn't usually)
	// But for imported tokens, we might want to set it.
	// The AddToken method takes *OAuthToken.
	// I should probably update OAuthToken struct in auth/oauth.go as well to carry these extra fields if needed,
	// OR just rely on what's there.
	// The current OAuthToken struct has ClientID and ClientSecret.
	// It does NOT have Region or AuthMethod.

	s.Tokens = append(s.Tokens, stored)
	return s.save()
}

// GetTokens 获取所有 token
func (s *OAuthTokenStore) GetTokens() []StoredToken {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return append([]StoredToken{}, s.Tokens...)
}

// GetTokenByRefreshToken 根据 RefreshToken 获取 token
func (s *OAuthTokenStore) GetTokenByRefreshToken(refreshToken string) *StoredToken {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	for i := range s.Tokens {
		if s.Tokens[i].RefreshToken == refreshToken {
			// 返回副本
			token := s.Tokens[i]
			return &token
		}
	}
	return nil
}

// DeleteToken 删除指定 ID 的 token
func (s *OAuthTokenStore) DeleteToken(id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	for i, token := range s.Tokens {
		if token.ID == id {
			// 删除指定索引的元素
			s.Tokens = append(s.Tokens[:i], s.Tokens[i+1:]...)
			logger.Info("删除OAuth token", logger.String("id", id), logger.String("provider", token.Provider))
			return s.save()
		}
	}

	return fmt.Errorf("未找到ID为 %s 的token", id)
}

// ToAuthConfigs 转换为 AuthConfig 格式
func (s *OAuthTokenStore) ToAuthConfigs() []AuthConfig {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	configs := make([]AuthConfig, len(s.Tokens))
	for i, t := range s.Tokens {
		authType := AuthMethodSocial
		if t.AuthMethod == "IdC" || (t.ClientID != "" && t.ClientSecret != "") {
			authType = AuthMethodIdC
		}

		configs[i] = AuthConfig{
			AuthType:     authType,
			RefreshToken: t.RefreshToken,
			ClientID:     t.ClientID,
			ClientSecret: t.ClientSecret,
			Source:       "oauth",
			OAuthID:      t.ID,
			Deletable:    true,
			Disabled:     t.Disabled,
		}
	}
	return configs
}

// SetTokenDisabled 设置 token 的禁用状态（临时禁用，后台刷新不停止）
func (s *OAuthTokenStore) SetTokenDisabled(id string, disabled bool) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	for i, token := range s.Tokens {
		if token.ID == id {
			s.Tokens[i].Disabled = disabled
			action := "启用"
			if disabled {
				action = "禁用"
			}
			logger.Info("OAuth token状态已更新", logger.String("id", id), logger.String("action", action))
			return s.save()
		}
	}
	return fmt.Errorf("未找到ID为 %s 的token", id)
}

func (s *OAuthTokenStore) load() {
	logger.Info("尝试加载OAuth token文件", logger.String("path", s.path))
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Info("OAuth token文件不存在", logger.String("path", s.path))
		} else {
			logger.Warn("Failed to load OAuth tokens", logger.Err(err))
		}
		return
	}

	if err := json.Unmarshal(data, &s.Tokens); err != nil {
		logger.Warn("Failed to parse OAuth tokens", logger.Err(err))
		return
	}
	logger.Info("OAuth token加载成功", logger.Int("count", len(s.Tokens)))
}

func (s *OAuthTokenStore) save() error {
	data, err := json.MarshalIndent(s.Tokens, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.path, data, 0600); err != nil {
		logger.Error("保存OAuth token失败", logger.Err(err))
		return err
	}
	logger.Info("OAuth token已保存到文件", logger.String("path", s.path), logger.Int("count", len(s.Tokens)))
	return nil
}
