package types

import (
	"time"
)

// Token 统一的token管理结构，合并了TokenInfo、RefreshResponse、RefreshRequest的功能
type Token struct {
	// 核心token信息
	AccessToken  string    `json:"accessToken,omitempty"`
	RefreshToken string    `json:"refreshToken"`
	ExpiresAt    time.Time `json:"expiresAt,omitempty"`

	// API响应字段
	ExpiresIn  int    `json:"expiresIn,omitempty"`  // 多少秒后失效，来自RefreshResponse
	ProfileArn string `json:"profileArn,omitempty"` // 来自RefreshResponse
}

// FromRefreshResponse 从RefreshResponse创建Token
func (t *Token) FromRefreshResponse(resp RefreshResponse, originalRefreshToken string) {
	t.AccessToken = resp.AccessToken
	t.RefreshToken = originalRefreshToken // 保持原始refresh token
	t.ExpiresIn = resp.ExpiresIn
	t.ProfileArn = resp.ProfileArn
	t.ExpiresAt = time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
}

// IsExpired 检查token是否已过期
func (t *Token) IsExpired() bool {
	return time.Now().After(t.ExpiresAt)
}

// 兼容性别名 - 逐步迁移时使用
type TokenInfo = Token // TokenInfo现在是Token的别名
// RefreshResponse 统一的token刷新响应结构，支持Social和IdC两种认证方式
type RefreshResponse struct {
	AccessToken  string `json:"accessToken"`
	ExpiresIn    int    `json:"expiresIn"` // 多少秒后失效
	RefreshToken string `json:"refreshToken,omitempty"`

	// Social认证方式专用字段
	ProfileArn string `json:"profileArn,omitempty"`

	// IdC认证方式专用字段
	TokenType string `json:"tokenType,omitempty"`

	// 可能的其他响应字段
	OriginSessionId    *string `json:"originSessionId,omitempty"`
	IssuedTokenType    *string `json:"issuedTokenType,omitempty"`
	AwsSsoAppSessionId *string `json:"aws_sso_app_session_id,omitempty"`
	IdToken            *string `json:"idToken,omitempty"`
}

// RefreshRequest Social认证方式的刷新请求结构
type RefreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

// IdcRefreshRequest IdC认证方式的刷新请求结构
type IdcRefreshRequest struct {
	ClientId     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	GrantType    string `json:"grantType"`
	RefreshToken string `json:"refreshToken"`
}
