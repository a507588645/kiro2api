package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"sync"
	"time"
)

// PKCEParams PKCE 参数
type PKCEParams struct {
	CodeVerifier  string
	CodeChallenge string
	State         string
	Provider      string
	CallbackURL   string
	CreatedAt     time.Time
}

// OAuthStateManager 管理 OAuth 状态
type OAuthStateManager struct {
	states map[string]*PKCEParams
	mutex  sync.RWMutex
	ttl    time.Duration
}

var (
	oauthStateManager *OAuthStateManager
	stateManagerOnce  sync.Once
)

// GetOAuthStateManager 获取单例状态管理器
func GetOAuthStateManager() *OAuthStateManager {
	stateManagerOnce.Do(func() {
		oauthStateManager = &OAuthStateManager{
			states: make(map[string]*PKCEParams),
			ttl:    5 * time.Minute,
		}
		go oauthStateManager.cleanupLoop()
	})
	return oauthStateManager
}

// GeneratePKCE 生成 PKCE 参数
func GeneratePKCE(provider, callbackURL string) *PKCEParams {
	verifier := generateRandomString(64)
	challenge := generateCodeChallenge(verifier)
	state := generateRandomString(32)

	params := &PKCEParams{
		CodeVerifier:  verifier,
		CodeChallenge: challenge,
		State:         state,
		Provider:      provider,
		CallbackURL:   callbackURL,
		CreatedAt:     time.Now(),
	}

	GetOAuthStateManager().Store(state, params)
	return params
}

// Store 存储状态
func (m *OAuthStateManager) Store(state string, params *PKCEParams) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.states[state] = params
}

// Get 获取并删除状态（一次性使用）
func (m *OAuthStateManager) Get(state string) (*PKCEParams, bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	params, exists := m.states[state]
	if exists {
		delete(m.states, state)
	}
	return params, exists
}

// cleanupLoop 定期清理过期状态
func (m *OAuthStateManager) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	for range ticker.C {
		m.cleanup()
	}
}

func (m *OAuthStateManager) cleanup() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	now := time.Now()
	for state, params := range m.states {
		if now.Sub(params.CreatedAt) > m.ttl {
			delete(m.states, state)
		}
	}
}

// generateRandomString 生成随机字符串
func generateRandomString(length int) string {
	bytes := make([]byte, length)
	rand.Read(bytes)
	return base64.RawURLEncoding.EncodeToString(bytes)[:length]
}

// generateCodeChallenge 生成 code_challenge (S256)
func generateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}
