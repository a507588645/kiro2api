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
			TLSHandshakeTimeout: config.HTTPClientTLSHandshakeTimeout,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: skipTLS,
				MinVersion:         tls.VersionTLS12,
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

// shouldSkipTLSVerify 根据GIN_MODE决定是否跳过TLS证书验证
func shouldSkipTLSVerify() bool {
	return os.Getenv("GIN_MODE") == "debug"
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
