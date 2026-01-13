package auth

import "os"

// KiroOAuthConfig Kiro OAuth 配置
var KiroOAuthConfig = struct {
	AuthServiceEndpoint string
	TokenEndpoint       string
	SSOOIDCEndpoint     string
	BuilderIDStartURL   string
	Scopes              []string
}{
	AuthServiceEndpoint: "https://prod.us-east-1.auth.desktop.kiro.dev",
	TokenEndpoint:       "https://prod.us-east-1.auth.desktop.kiro.dev/token",
	SSOOIDCEndpoint:     "https://oidc.us-east-1.amazonaws.com",
	BuilderIDStartURL:   "https://view.awsapps.com/start",
	Scopes: []string{
		"codewhisperer:completions",
		"codewhisperer:analysis",
		"codewhisperer:conversations",
		"codewhisperer:transformations",
		"codewhisperer:taskassist",
	},
}

// GetCallbackBaseURL 获取回调基础URL（支持云平台自动检测）
func GetCallbackBaseURL() string {
	if url := os.Getenv("OAUTH_CALLBACK_BASE_URL"); url != "" {
		return url
	}
	if url := os.Getenv("ZEABUR_WEB_URL"); url != "" {
		return url
	}
	if url := os.Getenv("RAILWAY_PUBLIC_DOMAIN"); url != "" {
		return "https://" + url
	}
	if url := os.Getenv("RENDER_EXTERNAL_URL"); url != "" {
		return url
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	return "http://localhost:" + port
}

// IsOAuthEnabled 检查是否启用 OAuth
func IsOAuthEnabled() bool {
	return os.Getenv("OAUTH_ENABLED") == "true"
}
