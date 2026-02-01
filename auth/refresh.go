package auth

import (
	"bytes"
	"fmt"
	"io"
	"kiro2api/config"
	"kiro2api/logger"
	"kiro2api/types"
	"kiro2api/utils"
	"net/http"
	"time"
)

// refreshSingleToken 刷新单个token
func (tm *TokenManager) refreshSingleToken(authConfig AuthConfig) (types.TokenInfo, error) {
	switch authConfig.AuthType {
	case AuthMethodSocial:
		return refreshSocialToken(authConfig.RefreshToken)
	case AuthMethodIdC:
		return refreshIdCToken(authConfig)
	default:
		return types.TokenInfo{}, fmt.Errorf("不支持的认证类型: %s", authConfig.AuthType)
	}
}

// refreshSocialToken 刷新Social认证token
func refreshSocialToken(refreshToken string) (types.TokenInfo, error) {
	refreshReq := types.RefreshRequest{
		RefreshToken: refreshToken,
	}

	reqBody, err := utils.FastMarshal(refreshReq)
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("序列化请求失败: %v", err)
	}

	// 调试日志：打印请求信息
	tokenLen := len(refreshToken)
	prefixLen := 20
	if tokenLen < prefixLen {
		prefixLen = tokenLen
	}
	logger.Debug("Social token 刷新请求",
		logger.String("url", config.RefreshTokenURL),
		logger.Int("token_length", tokenLen),
		logger.String("token_prefix", refreshToken[:prefixLen]))

	req, err := http.NewRequest("POST", config.RefreshTokenURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := utils.SharedHTTPClient
	resp, err := client.Do(req)
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("读取响应失败: %v", err)
	}

	// 调试日志：打印响应信息
	logger.Debug("Social token 刷新响应",
		logger.Int("status_code", resp.StatusCode),
		logger.String("response", string(body)))

	if resp.StatusCode != http.StatusOK {
		return types.TokenInfo{}, fmt.Errorf("刷新失败: 状态码 %d, 响应: %s", resp.StatusCode, string(body))
	}

	var refreshResp types.RefreshResponse
	if err := utils.SafeUnmarshal(body, &refreshResp); err != nil {
		return types.TokenInfo{}, fmt.Errorf("解析响应失败: %v", err)
	}

	var token types.Token
	token.FromRefreshResponse(refreshResp, refreshToken)

	return token, nil
}

// refreshIdCToken 刷新IdC认证token
func refreshIdCToken(authConfig AuthConfig) (types.TokenInfo, error) {
	refreshReq := types.IdcRefreshRequest{
		ClientId:     authConfig.ClientID,
		ClientSecret: authConfig.ClientSecret,
		GrantType:    "refresh_token",
		RefreshToken: authConfig.RefreshToken,
	}

	reqBody, err := utils.FastMarshal(refreshReq)
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("序列化IdC请求失败: %v", err)
	}

	req, err := http.NewRequest("POST", config.IdcRefreshTokenURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("创建IdC请求失败: %v", err)
	}

	// 设置IdC特殊headers（使用指纹随机化）
	fpManager := GetFingerprintManager()
	tokenKey := authConfig.RefreshToken
	if len(tokenKey) > 20 {
		tokenKey = tokenKey[:20]
	}
	fp := fpManager.GetFingerprint(tokenKey)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Host", "oidc.us-east-1.amazonaws.com")
	req.Header.Set("Connection", fp.ConnectionBehavior)
	req.Header.Set("x-amz-user-agent", fmt.Sprintf("aws-sdk-js/3.738.0 ua/2.1 os/%s lang/js md/browser#unknown_unknown api/sso-oidc#3.738.0 m/E KiroIDE", fp.OSType))
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", fp.AcceptLanguage)
	req.Header.Set("sec-fetch-mode", fp.SecFetchMode)
	req.Header.Set("User-Agent", "node")
	req.Header.Set("Accept-Encoding", fp.AcceptEncoding)

	client := utils.SharedHTTPClient
	resp, err := client.Do(req)
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("IdC请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return types.TokenInfo{}, fmt.Errorf("IdC刷新失败: 状态码 %d, 响应: %s", resp.StatusCode, string(body))
	}

	var refreshResp types.RefreshResponse
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return types.TokenInfo{}, fmt.Errorf("读取IdC响应失败: %v", err)
	}

	if err := utils.SafeUnmarshal(body, &refreshResp); err != nil {
		return types.TokenInfo{}, fmt.Errorf("解析IdC响应失败: %v", err)
	}

	var token types.Token
	token.AccessToken = refreshResp.AccessToken
	token.RefreshToken = authConfig.RefreshToken
	token.ExpiresIn = refreshResp.ExpiresIn
	token.ExpiresAt = time.Now().Add(time.Duration(refreshResp.ExpiresIn) * time.Second)

	return token, nil
}

// RefreshSocialToken 公开的Social token刷新函数
func RefreshSocialToken(refreshToken string) (types.TokenInfo, error) {
	return refreshSocialToken(refreshToken)
}

// RefreshIdCToken 公开的IdC token刷新函数
func RefreshIdCToken(authConfig AuthConfig) (types.TokenInfo, error) {
	return refreshIdCToken(authConfig)
}
