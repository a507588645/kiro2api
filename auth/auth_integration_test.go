package auth

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewAuthService_FromEnv(t *testing.T) {
	// 保存原始环境变量
	originalToken := os.Getenv("KIRO_AUTH_TOKEN")
	defer os.Setenv("KIRO_AUTH_TOKEN", originalToken)

	// 设置测试环境变量
	testToken := `[{"auth":"Social","refreshToken":"test_token_123"}]`
	os.Setenv("KIRO_AUTH_TOKEN", testToken)

	service, err := NewAuthService()

	assert.NoError(t, err)
	assert.NotNil(t, service)
	assert.NotNil(t, service.tokenManager)
	assert.Len(t, service.configs, 1)
	assert.Equal(t, AuthMethodSocial, service.configs[0].AuthType)
}

func TestAuthService_GetTokenManager(t *testing.T) {
	configs := []AuthConfig{
		{
			AuthType:     AuthMethodSocial,
			RefreshToken: "test_token",
		},
	}

	service := &AuthService{
		tokenManager: NewTokenManager(configs),
		configs:      configs,
	}

	manager := service.GetTokenManager()
	assert.NotNil(t, manager)
	assert.Equal(t, service.tokenManager, manager)
}

func TestAuthService_GetConfigs(t *testing.T) {
	configs := []AuthConfig{
		{
			AuthType:     AuthMethodSocial,
			RefreshToken: "token1",
		},
		{
			AuthType:     AuthMethodIdC,
			RefreshToken: "token2",
			ClientID:     "client123",
			ClientSecret: "secret456",
		},
	}

	service := &AuthService{
		tokenManager: NewTokenManager(configs),
		configs:      configs,
	}

	retrievedConfigs := service.GetConfigs()
	assert.Len(t, retrievedConfigs, 2)
	assert.Equal(t, AuthMethodSocial, retrievedConfigs[0].AuthType)
	assert.Equal(t, AuthMethodIdC, retrievedConfigs[1].AuthType)
}
