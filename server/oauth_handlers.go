package server

import (
	"fmt"
	"net/http"

	"kiro2api/auth"
	"kiro2api/logger"

	"github.com/gin-gonic/gin"
)

// RegisterOAuthRoutes 注册 OAuth 路由
func RegisterOAuthRoutes(r *gin.Engine) {
	if !auth.IsOAuthEnabled() {
		logger.Info("OAuth is disabled")
		return
	}

	r.GET("/oauth", handleOAuthPage)
	r.POST("/oauth/start", handleOAuthStart)
	r.POST("/oauth/builder-id", handleBuilderIDStart)
	r.GET("/oauth/callback", handleOAuthCallback)
	r.GET("/api/oauth/tokens", handleOAuthTokens)
	r.POST("/api/import-accounts", handleImportAccounts)

	logger.Info("OAuth routes registered")
}

// handleOAuthPage 授权入口页面
func handleOAuthPage(c *gin.Context) {
	c.File("./static/oauth.html")
}

// handleOAuthStart 启动 OAuth 流程
func handleOAuthStart(c *gin.Context) {
	var req struct {
		Provider string `json:"provider"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if req.Provider != "Google" && req.Provider != "GitHub" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid provider, must be Google or GitHub"})
		return
	}

	callbackURL := auth.GetCallbackBaseURL() + "/oauth/callback"
	authURL, pkce := auth.BuildAuthURL(req.Provider, callbackURL)

	logger.Info("OAuth started",
		logger.String("provider", req.Provider),
		logger.String("state", pkce.State))

	c.JSON(http.StatusOK, gin.H{
		"auth_url": authURL,
		"state":    pkce.State,
	})
}

// handleOAuthCallback OAuth 回调处理
func handleOAuthCallback(c *gin.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("OAuth callback panic", logger.Any("error", r))
			c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(renderCallbackPage(false, "internal error", "")))
		}
	}()

	code := c.Query("code")
	state := c.Query("state")
	errMsg := c.Query("error")
	errDesc := c.Query("error_description")

	logger.Info("OAuth callback received",
		logger.String("code_len", fmt.Sprintf("%d", len(code))),
		logger.String("state_len", fmt.Sprintf("%d", len(state))),
		logger.String("error", errMsg))

	if errMsg != "" {
		msg := errMsg
		if errDesc != "" {
			msg = errMsg + ": " + errDesc
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(renderCallbackPage(false, msg, "")))
		return
	}

	if code == "" || state == "" {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(renderCallbackPage(false, "missing code or state", "")))
		return
	}

	token, err := auth.ExchangeCodeForToken(code, state)
	if err != nil {
		logger.Error("Token exchange failed", logger.Err(err))
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(renderCallbackPage(false, err.Error(), "")))
		return
	}

	// 保存 token
	if err := auth.GetOAuthTokenStore().AddToken(token); err != nil {
		logger.Error("Failed to save token", logger.Err(err))
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(renderCallbackPage(false, "failed to save token", "")))
		return
	}

	logger.Info("OAuth completed successfully",
		logger.String("provider", token.Provider))

	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(renderCallbackPage(true, "", token.RefreshToken)))
}

// handleOAuthTokens 获取已授权的 token 列表
func handleOAuthTokens(c *gin.Context) {
	tokens := auth.GetOAuthTokenStore().GetTokens()

	// 隐藏完整 token
	result := make([]gin.H, len(tokens))
	for i, t := range tokens {
		masked := t.RefreshToken
		if len(masked) > 20 {
			masked = masked[:10] + "..." + masked[len(masked)-10:]
		}
		result[i] = gin.H{
			"id":         t.ID,
			"provider":   t.Provider,
			"token":      masked,
			"created_at": t.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, gin.H{"tokens": result, "count": len(tokens)})
}

// handleBuilderIDStart 启动 AWS Builder ID 授权
func handleBuilderIDStart(c *gin.Context) {
	deviceAuth, err := auth.StartBuilderIDAuth()
	if err != nil {
		logger.Error("Builder ID auth failed", logger.Err(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 启动后台轮询
	auth.PollBuilderIDToken(deviceAuth)

	logger.Info("Builder ID auth started",
		logger.String("userCode", deviceAuth.UserCode))

	c.JSON(http.StatusOK, gin.H{
		"auth_url":  deviceAuth.VerificationUriComplete,
		"user_code": deviceAuth.UserCode,
		"message":   "请在浏览器中打开链接并输入验证码完成授权",
	})
}

// renderCallbackPage 渲染回调结果页面
func renderCallbackPage(success bool, errMsg, token string) string {
	status := "失败"
	statusClass := "error"
	message := errMsg

	if success {
		status = "成功"
		statusClass = "success"
		message = "授权成功！RefreshToken 已保存。"
	}

	tokenSection := ""
	if success && token != "" {
		masked := token
		if len(masked) > 30 {
			masked = masked[:15] + "..." + masked[len(masked)-15:]
		}
		tokenSection = `<div class="token-info"><p>Token: <code>` + masked + `</code></p></div>`
	}

	return `<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>OAuth 授权结果</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, sans-serif; display: flex; justify-content: center; align-items: center; min-height: 100vh; margin: 0; background: #f5f5f5; }
        .container { text-align: center; padding: 40px; background: white; border-radius: 12px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); }
        .success { color: #22c55e; }
        .error { color: #ef4444; }
        h1 { font-size: 24px; margin-bottom: 16px; }
        p { color: #666; margin: 8px 0; }
        .token-info { background: #f0f0f0; padding: 12px; border-radius: 8px; margin-top: 16px; }
        code { font-size: 12px; word-break: break-all; }
        a { color: #3b82f6; text-decoration: none; }
    </style>
</head>
<body>
    <div class="container">
        <h1 class="` + statusClass + `">授权` + status + `</h1>
        <p>` + message + `</p>
        ` + tokenSection + `
        <p style="margin-top: 20px;"><a href="/">返回首页</a> | <a href="/oauth">重新授权</a></p>
    </div>
</body>
</html>`
}

// handleImportAccounts 处理账号导入
func handleImportAccounts(c *gin.Context) {
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请选择文件"})
		return
	}
	defer file.Close()

	imported, skipped, errors := auth.ImportAccountsFromReader(file)

	// 导入成功后重载 TokenManager
	if imported > 0 {
		if as := auth.GetGlobalAuthService(); as != nil {
			if err := as.ReloadTokens(); err != nil {
				logger.Warn("重载TokenManager失败", logger.Err(err))
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  len(errors) == 0,
		"imported": imported,
		"skipped":  skipped,
		"errors":   errors,
		"message":  fmt.Sprintf("成功导入 %d 个账号", imported),
	})
}
