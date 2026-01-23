package utils

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"kiro2api/config"
)

var (
	// SharedHTTPClient 共享的HTTP客户端实例，优化了连接池和性能配置
	SharedHTTPClient *http.Client
)

func init() {
	// 检查TLS配置并记录日志
	skipTLS := shouldSkipTLSVerify()
	if skipTLS {
		os.Stderr.WriteString("[WARNING] TLS证书验证已禁用 - 仅适用于开发/调试环境\n")
	}

	// 创建统一的HTTP客户端
	SharedHTTPClient = &http.Client{
		Transport: &http.Transport{
			// {{RIPER-10 Action}}
			// Role: LD | Time: 2025-12-14T13:54:45Z
			// Principle: SOLID-O (开闭原则) - 通过环境变量扩展功能，不修改现有逻辑
			// Taste: 使用标准库 http.ProxyFromEnvironment，自动读取 HTTP_PROXY/HTTPS_PROXY/NO_PROXY
			Proxy: http.ProxyFromEnvironment,

			// 连接池配置
			MaxIdleConns:        200,
			MaxIdleConnsPerHost: 100,
			MaxConnsPerHost:     100,
			IdleConnTimeout:     120 * time.Second,

			// 连接建立配置
			DialContext: (&net.Dialer{
				Timeout:   15 * time.Second,
				KeepAlive: config.HTTPClientKeepAlive,
				DualStack: true,
			}).DialContext,

			// TLS配置
			// 参考: kiro.rs 2026.1.6 - 现在支持通过配置文件切换两种tls后端了
			TLSHandshakeTimeout: config.HTTPClientTLSHandshakeTimeout,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: skipTLS,
				MinVersion:         getTLSMinVersionInternal(), // 使用可配置的最低版本
				MaxVersion:         tls.VersionTLS13,
				CipherSuites: []uint16{
					tls.TLS_AES_256_GCM_SHA384,
					tls.TLS_CHACHA20_POLY1305_SHA256,
					tls.TLS_AES_128_GCM_SHA256,
				},
			},

			// HTTP配置
			ForceAttemptHTTP2:     false,
			DisableCompression:    false,
			WriteBufferSize:       32 * 1024,
			ReadBufferSize:        32 * 1024,
			ResponseHeaderTimeout: 60 * time.Second,
		},
	}
}

// shouldSkipTLSVerify 根据环境变量决定是否跳过TLS证书验证
// 参考: kiro.rs 2026.1.6 - 现在支持通过配置文件切换两种tls后端了
// 支持的环境变量:
//   - TLS_SKIP_VERIFY=true: 显式跳过TLS验证
//   - GIN_MODE=debug: 开发模式自动跳过
//   - TLS_INSECURE=true: 与TLS_SKIP_VERIFY相同
func shouldSkipTLSVerify() bool {
	// 显式配置优先
	if os.Getenv("TLS_SKIP_VERIFY") == "true" || os.Getenv("TLS_INSECURE") == "true" {
		return true
	}
	// 开发模式
	return os.Getenv("GIN_MODE") == "debug"
}

// getTLSMinVersionInternal 内部函数，用于 init() 阶段
func getTLSMinVersionInternal() uint16 {
	switch os.Getenv("TLS_MIN_VERSION") {
	case "1.3":
		return tls.VersionTLS13
	default:
		return tls.VersionTLS12
	}
}

// GetTLSMinVersion 获取最低 TLS 版本
// 支持的环境变量:
//   - TLS_MIN_VERSION: 可选值 "1.2", "1.3"
func GetTLSMinVersion() uint16 {
	return getTLSMinVersionInternal()
}

// DoRequest 执行HTTP请求
func DoRequest(req *http.Request) (*http.Response, error) {
	return SharedHTTPClient.Do(req)
}

// ProxyAwareClient 支持代理池的HTTP客户端
type ProxyAwareClient struct {
	baseTransport *http.Transport
}

// NewProxyAwareClient 创建支持代理池的客户端
func NewProxyAwareClient() *ProxyAwareClient {
	return &ProxyAwareClient{
		baseTransport: SharedHTTPClient.Transport.(*http.Transport).Clone(),
	}
}

// DoWithProxy 使用指定代理执行请求
func (c *ProxyAwareClient) DoWithProxy(req *http.Request, proxyURL string) (*http.Response, error) {
	if proxyURL == "" {
		return SharedHTTPClient.Do(req)
	}

	// 解析代理URL
	proxy, err := parseProxyURL(proxyURL)
	if err != nil {
		return nil, err
	}

	// 克隆transport并设置代理
	transport := c.baseTransport.Clone()
	transport.Proxy = http.ProxyURL(proxy)

	client := &http.Client{
		Transport: transport,
		Timeout:   60 * time.Second,
	}

	return client.Do(req)
}

// parseProxyURL 解析代理URL
func parseProxyURL(proxyURL string) (*url.URL, error) {
	return url.Parse(proxyURL)
}
