package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"kiro2api/logger"
)

// OAuthToken OAuth 获取的 token
type OAuthToken struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	ExpiresIn    int       `json:"expiresIn"`
	ExpiresAt    time.Time `json:"expiresAt"`
	Provider     string    `json:"provider"`
	ClientID     string    `json:"clientId,omitempty"`
	ClientSecret string    `json:"clientSecret,omitempty"`
	Region       string    `json:"region,omitempty"`
	AuthMethod   string    `json:"authMethod,omitempty"`
}

// DeviceAuthResponse 设备授权响应
type DeviceAuthResponse struct {
	DeviceCode              string `json:"deviceCode"`
	UserCode                string `json:"userCode"`
	VerificationUri         string `json:"verificationUri"`
	VerificationUriComplete string `json:"verificationUriComplete"`
	ExpiresIn               int    `json:"expiresIn"`
	Interval                int    `json:"interval"`
	ClientID                string `json:"clientId"`
	ClientSecret            string `json:"clientSecret"`
}

// 活动的轮询任务
var (
	activePollingTasks = make(map[string]*pollingTask)
	pollingMutex       sync.Mutex
)

type pollingTask struct {
	shouldStop bool
}

// BuildAuthURL 构建授权 URL (Social Auth)
func BuildAuthURL(provider, callbackURL string) (string, *PKCEParams) {
	pkce := GeneratePKCE(provider, callbackURL)

	params := url.Values{
		"idp":                   {provider},
		"redirect_uri":          {callbackURL},
		"code_challenge":        {pkce.CodeChallenge},
		"code_challenge_method": {"S256"},
		"state":                 {pkce.State},
	}

	authURL := KiroOAuthConfig.AuthServiceEndpoint + "/login?" + params.Encode()
	return authURL, pkce
}

// StartBuilderIDAuth 启动 AWS Builder ID 设备码授权
func StartBuilderIDAuth() (*DeviceAuthResponse, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	// 1. 注册 OIDC 客户端
	regReq := map[string]interface{}{
		"clientName":  "Kiro IDE",
		"clientType":  "public",
		"scopes":      KiroOAuthConfig.Scopes,
		"grantTypes":  []string{"urn:ietf:params:oauth:grant-type:device_code", "refresh_token"},
	}
	regBody, _ := json.Marshal(regReq)

	resp, err := client.Post(
		KiroOAuthConfig.SSOOIDCEndpoint+"/client/register",
		"application/json",
		bytes.NewReader(regBody),
	)
	if err != nil {
		return nil, fmt.Errorf("register client failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("register client failed: %s", string(body))
	}

	var regResp struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
		return nil, fmt.Errorf("parse register response failed: %w", err)
	}

	// 2. 启动设备授权
	authReq := map[string]interface{}{
		"clientId":     regResp.ClientID,
		"clientSecret": regResp.ClientSecret,
		"startUrl":     KiroOAuthConfig.BuilderIDStartURL,
	}
	authBody, _ := json.Marshal(authReq)

	resp2, err := client.Post(
		KiroOAuthConfig.SSOOIDCEndpoint+"/device_authorization",
		"application/json",
		bytes.NewReader(authBody),
	)
	if err != nil {
		return nil, fmt.Errorf("device authorization failed: %w", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp2.Body)
		return nil, fmt.Errorf("device authorization failed: %s", string(body))
	}

	var deviceResp DeviceAuthResponse
	if err := json.NewDecoder(resp2.Body).Decode(&deviceResp); err != nil {
		return nil, fmt.Errorf("parse device auth response failed: %w", err)
	}

	deviceResp.ClientID = regResp.ClientID
	deviceResp.ClientSecret = regResp.ClientSecret

	logger.Info("Builder ID device auth started",
		logger.String("userCode", deviceResp.UserCode),
		logger.String("verificationUri", deviceResp.VerificationUri))

	return &deviceResp, nil
}

// PollBuilderIDToken 轮询获取 Builder ID token
func PollBuilderIDToken(deviceAuth *DeviceAuthResponse) {
	taskID := deviceAuth.DeviceCode[:8]

	pollingMutex.Lock()
	// 停止之前的任务
	for id, task := range activePollingTasks {
		task.shouldStop = true
		delete(activePollingTasks, id)
	}
	task := &pollingTask{}
	activePollingTasks[taskID] = task
	pollingMutex.Unlock()

	go func() {
		interval := deviceAuth.Interval
		if interval < 5 {
			interval = 5
		}
		maxAttempts := deviceAuth.ExpiresIn / interval
		client := &http.Client{Timeout: 30 * time.Second}

		for i := 0; i < maxAttempts; i++ {
			pollingMutex.Lock()
			shouldStop := task.shouldStop
			pollingMutex.Unlock()
			if shouldStop {
				return
			}

			time.Sleep(time.Duration(interval) * time.Second)

			tokenReq := map[string]interface{}{
				"clientId":     deviceAuth.ClientID,
				"clientSecret": deviceAuth.ClientSecret,
				"deviceCode":   deviceAuth.DeviceCode,
				"grantType":    "urn:ietf:params:oauth:grant-type:device_code",
			}
			tokenBody, _ := json.Marshal(tokenReq)

			resp, err := client.Post(
				KiroOAuthConfig.SSOOIDCEndpoint+"/token",
				"application/json",
				bytes.NewReader(tokenBody),
			)
			if err != nil {
				continue
			}

			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			var tokenResp struct {
				AccessToken  string `json:"accessToken"`
				RefreshToken string `json:"refreshToken"`
				ExpiresIn    int    `json:"expiresIn"`
				Error        string `json:"error"`
			}
			json.Unmarshal(body, &tokenResp)

			if tokenResp.AccessToken != "" {
				token := &OAuthToken{
					AccessToken:  tokenResp.AccessToken,
					RefreshToken: tokenResp.RefreshToken,
					ExpiresIn:    tokenResp.ExpiresIn,
					ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
					Provider:     "BuilderID",
					ClientID:     deviceAuth.ClientID,
					ClientSecret: deviceAuth.ClientSecret,
				}

				if err := GetOAuthTokenStore().AddToken(token); err != nil {
					logger.Error("Failed to save Builder ID token", logger.Err(err))
				} else {
					logger.Info("Builder ID token obtained and saved")
					// 重载 TokenManager
					if as := GetGlobalAuthService(); as != nil {
						if err := as.ReloadTokens(); err != nil {
							logger.Warn("ReloadTokens failed", logger.Err(err))
						}
					}
				}

				pollingMutex.Lock()
				delete(activePollingTasks, taskID)
				pollingMutex.Unlock()
				return
			}

			if tokenResp.Error != "authorization_pending" && tokenResp.Error != "slow_down" {
				logger.Error("Builder ID auth failed", logger.String("error", tokenResp.Error))
				pollingMutex.Lock()
				delete(activePollingTasks, taskID)
				pollingMutex.Unlock()
				return
			}
		}

		logger.Warn("Builder ID auth timeout")
		pollingMutex.Lock()
		delete(activePollingTasks, taskID)
		pollingMutex.Unlock()
	}()
}

// ExchangeCodeForToken 用授权码交换 token (Social Auth)
func ExchangeCodeForToken(code, state string) (*OAuthToken, error) {
	pkce, exists := GetOAuthStateManager().Get(state)
	if !exists {
		return nil, fmt.Errorf("invalid or expired state")
	}

	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {pkce.CodeVerifier},
		"redirect_uri":  {pkce.CallbackURL},
	}

	req, err := http.NewRequest("POST", KiroOAuthConfig.TokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		logger.Error("Token exchange failed",
			logger.Int("status", resp.StatusCode),
			logger.String("body", string(body)))
		return nil, fmt.Errorf("token exchange failed: %s", string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int    `json:"expiresIn"`
	}

	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse token response failed: %w", err)
	}

	token := &OAuthToken{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresIn:    tokenResp.ExpiresIn,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
		Provider:     pkce.Provider,
	}

	logger.Info("OAuth token obtained", logger.String("provider", pkce.Provider))

	return token, nil
}
