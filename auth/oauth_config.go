package auth

import "os"

// AWS Region 常量
// 参考: kiro.rs 2026.1.6 - Web管理页面支持 IdC/IAM/Builder-ID 的区域 region 配置
const (
	RegionUSEast1 = "us-east-1"
	RegionUSWest2 = "us-west-2"
	RegionEUWest1 = "eu-west-1"
	RegionAPNortheast1 = "ap-northeast-1"
)

// SupportedRegions 支持的 AWS 区域列表
var SupportedRegions = []string{
	RegionUSEast1,
	RegionUSWest2,
	RegionEUWest1,
	RegionAPNortheast1,
}

// RegionConfig 区域相关的配置
type RegionConfig struct {
	AuthServiceEndpoint string
	TokenEndpoint       string
	SSOOIDCEndpoint     string
}

// GetRegionConfig 根据区域返回配置
func GetRegionConfig(region string) RegionConfig {
	if region == "" {
		region = GetDefaultRegion()
	}
	
	return RegionConfig{
		AuthServiceEndpoint: "https://prod." + region + ".auth.desktop.kiro.dev",
		TokenEndpoint:       "https://prod." + region + ".auth.desktop.kiro.dev/token",
		SSOOIDCEndpoint:     "https://oidc." + region + ".amazonaws.com",
	}
}

// GetDefaultRegion 获取默认区域
func GetDefaultRegion() string {
	if region := os.Getenv("AWS_REGION"); region != "" {
		return region
	}
	if region := os.Getenv("KIRO_REGION"); region != "" {
		return region
	}
	return RegionUSEast1 // 默认 us-east-1
}

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

// GetKiroOAuthConfigWithRegion 获取指定区域的 OAuth 配置
func GetKiroOAuthConfigWithRegion(region string) struct {
	AuthServiceEndpoint string
	TokenEndpoint       string
	SSOOIDCEndpoint     string
	BuilderIDStartURL   string
	Scopes              []string
} {
	regionConfig := GetRegionConfig(region)
	return struct {
		AuthServiceEndpoint string
		TokenEndpoint       string
		SSOOIDCEndpoint     string
		BuilderIDStartURL   string
		Scopes              []string
	}{
		AuthServiceEndpoint: regionConfig.AuthServiceEndpoint,
		TokenEndpoint:       regionConfig.TokenEndpoint,
		SSOOIDCEndpoint:     regionConfig.SSOOIDCEndpoint,
		BuilderIDStartURL:   KiroOAuthConfig.BuilderIDStartURL,
		Scopes:              KiroOAuthConfig.Scopes,
	}
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
