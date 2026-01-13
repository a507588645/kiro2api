package auth

import (
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"sync"
	"time"

	"kiro2api/logger"
)

// ProxyInfo 代理信息
type ProxyInfo struct {
	URL           string    // 代理URL，如 http://127.0.0.1:40000
	UseCount      int       // 使用次数
	FailCount     int       // 连续失败次数
	LastUsed      time.Time // 最后使用时间
	LastCheck     time.Time // 最后健康检查时间
	IsHealthy     bool      // 是否健康
	CurrentIP     string    // 当前出口IP
	ResponseTime  int64     // 响应时间(ms)
}

// ProxyPool 代理池
type ProxyPool struct {
	proxies       []*ProxyInfo
	mutex         sync.RWMutex
	rng           *rand.Rand
	
	// 配置
	maxUseCount       int           // 单个代理最大使用次数
	maxFailCount      int           // 最大连续失败次数
	healthCheckInterval time.Duration // 健康检查间隔
	cooldownDuration  time.Duration // 失败后冷却时间
	
	// 状态
	currentIndex int
	enabled      bool
}

// ProxyPoolConfig 代理池配置
type ProxyPoolConfig struct {
	Proxies             []string      // 代理URL列表
	MaxUseCount         int           // 单个代理最大使用次数（默认10）
	MaxFailCount        int           // 最大连续失败次数（默认3）
	HealthCheckInterval time.Duration // 健康检查间隔（默认5分钟）
	CooldownDuration    time.Duration // 失败后冷却时间（默认60秒）
}

// DefaultProxyPoolConfig 默认配置
func DefaultProxyPoolConfig() ProxyPoolConfig {
	return ProxyPoolConfig{
		Proxies:             []string{},
		MaxUseCount:         10,
		MaxFailCount:        3,
		HealthCheckInterval: 5 * time.Minute,
		CooldownDuration:    60 * time.Second,
	}
}

var (
	globalProxyPool *ProxyPool
	proxyPoolOnce   sync.Once
)

// GetProxyPool 获取全局代理池
func GetProxyPool() *ProxyPool {
	proxyPoolOnce.Do(func() {
		globalProxyPool = NewProxyPool(DefaultProxyPoolConfig())
	})
	return globalProxyPool
}

// InitProxyPool 初始化代理池（带配置）
func InitProxyPool(cfg ProxyPoolConfig) *ProxyPool {
	proxyPoolOnce.Do(func() {
		globalProxyPool = NewProxyPool(cfg)
	})
	return globalProxyPool
}

// NewProxyPool 创建代理池
func NewProxyPool(cfg ProxyPoolConfig) *ProxyPool {
	pool := &ProxyPool{
		proxies:             make([]*ProxyInfo, 0),
		rng:                 rand.New(rand.NewSource(time.Now().UnixNano())),
		maxUseCount:         cfg.MaxUseCount,
		maxFailCount:        cfg.MaxFailCount,
		healthCheckInterval: cfg.HealthCheckInterval,
		cooldownDuration:    cfg.CooldownDuration,
		enabled:             len(cfg.Proxies) > 0,
	}

	// 初始化代理列表
	for _, proxyURL := range cfg.Proxies {
		if proxyURL != "" {
			pool.proxies = append(pool.proxies, &ProxyInfo{
				URL:       proxyURL,
				IsHealthy: true, // 初始假设健康
			})
		}
	}

	if pool.enabled {
		logger.Info("代理池初始化完成",
			logger.Int("proxy_count", len(pool.proxies)),
			logger.Int("max_use_count", pool.maxUseCount))
		
		// 启动后台健康检查
		go pool.backgroundHealthCheck()
	}

	return pool
}

// GetProxy 获取一个可用代理
func (pp *ProxyPool) GetProxy() *ProxyInfo {
	if !pp.enabled || len(pp.proxies) == 0 {
		return nil
	}

	pp.mutex.Lock()
	defer pp.mutex.Unlock()

	// 收集可用代理
	var available []*ProxyInfo
	for _, proxy := range pp.proxies {
		if pp.isProxyAvailable(proxy) {
			available = append(available, proxy)
		}
	}

	if len(available) == 0 {
		logger.Warn("没有可用代理，尝试重置所有代理状态")
		pp.resetAllProxies()
		return nil
	}

	// 随机选择策略（避免总是用同一个）
	var selected *ProxyInfo
	if pp.rng.Float64() < 0.7 {
		// 70% 概率选择使用次数最少的
		selected = available[0]
		for _, proxy := range available {
			if proxy.UseCount < selected.UseCount {
				selected = proxy
			}
		}
	} else {
		// 30% 概率随机选择
		selected = available[pp.rng.Intn(len(available))]
	}

	return selected
}

// isProxyAvailable 检查代理是否可用
func (pp *ProxyPool) isProxyAvailable(proxy *ProxyInfo) bool {
	// 不健康的代理需要检查冷却时间
	if !proxy.IsHealthy {
		if time.Since(proxy.LastUsed) < pp.cooldownDuration {
			return false
		}
		// 冷却期过后重置状态
		proxy.IsHealthy = true
		proxy.FailCount = 0
	}

	// 使用次数超限
	if proxy.UseCount >= pp.maxUseCount {
		// 检查是否可以重置
		if time.Since(proxy.LastUsed) > pp.cooldownDuration {
			proxy.UseCount = 0
			return true
		}
		return false
	}

	return true
}

// RecordUse 记录代理使用
func (pp *ProxyPool) RecordUse(proxy *ProxyInfo) {
	if proxy == nil {
		return
	}

	pp.mutex.Lock()
	defer pp.mutex.Unlock()

	proxy.UseCount++
	proxy.LastUsed = time.Now()

	logger.Debug("代理使用记录",
		logger.String("proxy", proxy.URL),
		logger.Int("use_count", proxy.UseCount))
}

// RecordSuccess 记录成功
func (pp *ProxyPool) RecordSuccess(proxy *ProxyInfo, responseTime int64) {
	if proxy == nil {
		return
	}

	pp.mutex.Lock()
	defer pp.mutex.Unlock()

	proxy.FailCount = 0
	proxy.IsHealthy = true
	proxy.ResponseTime = responseTime
}

// RecordFailure 记录失败
func (pp *ProxyPool) RecordFailure(proxy *ProxyInfo) {
	if proxy == nil {
		return
	}

	pp.mutex.Lock()
	defer pp.mutex.Unlock()

	proxy.FailCount++
	if proxy.FailCount >= pp.maxFailCount {
		proxy.IsHealthy = false
		logger.Warn("代理标记为不健康",
			logger.String("proxy", proxy.URL),
			logger.Int("fail_count", proxy.FailCount))
	}
}

// resetAllProxies 重置所有代理状态
func (pp *ProxyPool) resetAllProxies() {
	for _, proxy := range pp.proxies {
		proxy.UseCount = 0
		proxy.FailCount = 0
		proxy.IsHealthy = true
	}
	logger.Info("所有代理状态已重置")
}

// backgroundHealthCheck 后台健康检查
func (pp *ProxyPool) backgroundHealthCheck() {
	ticker := time.NewTicker(pp.healthCheckInterval)
	defer ticker.Stop()

	for range ticker.C {
		pp.checkAllProxies()
	}
}

// checkAllProxies 检查所有代理健康状态
func (pp *ProxyPool) checkAllProxies() {
	pp.mutex.RLock()
	proxiesToCheck := make([]*ProxyInfo, len(pp.proxies))
	copy(proxiesToCheck, pp.proxies)
	pp.mutex.RUnlock()

	for _, proxy := range proxiesToCheck {
		go pp.checkProxyHealth(proxy)
	}
}

// checkProxyHealth 检查单个代理健康状态
func (pp *ProxyPool) checkProxyHealth(proxy *ProxyInfo) {
	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		pp.RecordFailure(proxy)
		return
	}

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
		Timeout: 10 * time.Second,
	}

	start := time.Now()
	resp, err := client.Get("https://api.ipify.org")
	responseTime := time.Since(start).Milliseconds()

	if err != nil {
		pp.RecordFailure(proxy)
		logger.Debug("代理健康检查失败",
			logger.String("proxy", proxy.URL),
			logger.Err(err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		ip := string(body)

		pp.mutex.Lock()
		proxy.CurrentIP = ip
		proxy.LastCheck = time.Now()
		proxy.ResponseTime = responseTime
		proxy.IsHealthy = true
		proxy.FailCount = 0
		pp.mutex.Unlock()

		logger.Debug("代理健康检查成功",
			logger.String("proxy", proxy.URL),
			logger.String("ip", ip),
			logger.Int64("response_time_ms", responseTime))
	} else {
		pp.RecordFailure(proxy)
	}
}

// GetProxyURL 获取代理URL用于http.Transport
func (pp *ProxyPool) GetProxyURL() (*url.URL, *ProxyInfo) {
	proxy := pp.GetProxy()
	if proxy == nil {
		return nil, nil
	}

	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		logger.Error("解析代理URL失败",
			logger.String("proxy", proxy.URL),
			logger.Err(err))
		return nil, nil
	}

	return proxyURL, proxy
}

// IsEnabled 检查代理池是否启用
func (pp *ProxyPool) IsEnabled() bool {
	return pp.enabled
}

// GetStats 获取代理池统计信息
func (pp *ProxyPool) GetStats() map[string]any {
	pp.mutex.RLock()
	defer pp.mutex.RUnlock()

	proxyStats := make([]map[string]any, 0)
	healthyCount := 0
	totalUseCount := 0

	for _, proxy := range pp.proxies {
		if proxy.IsHealthy {
			healthyCount++
		}
		totalUseCount += proxy.UseCount

		proxyStats = append(proxyStats, map[string]any{
			"url":           maskProxyURL(proxy.URL),
			"use_count":     proxy.UseCount,
			"fail_count":    proxy.FailCount,
			"is_healthy":    proxy.IsHealthy,
			"current_ip":    proxy.CurrentIP,
			"response_time": proxy.ResponseTime,
			"last_used":     proxy.LastUsed.Format(time.RFC3339),
			"last_check":    proxy.LastCheck.Format(time.RFC3339),
		})
	}

	return map[string]any{
		"enabled":        pp.enabled,
		"total_proxies":  len(pp.proxies),
		"healthy_proxies": healthyCount,
		"total_use_count": totalUseCount,
		"config": map[string]any{
			"max_use_count":          pp.maxUseCount,
			"max_fail_count":         pp.maxFailCount,
			"health_check_interval":  pp.healthCheckInterval.String(),
			"cooldown_duration":      pp.cooldownDuration.String(),
		},
		"proxies": proxyStats,
	}
}

// maskProxyURL 脱敏代理URL
func maskProxyURL(proxyURL string) string {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return "***"
	}
	// 隐藏密码
	if u.User != nil {
		u.User = url.UserPassword("***", "***")
	}
	return u.String()
}

// AddProxy 动态添加代理
func (pp *ProxyPool) AddProxy(proxyURL string) error {
	if proxyURL == "" {
		return fmt.Errorf("代理URL不能为空")
	}

	// 验证URL格式
	if _, err := url.Parse(proxyURL); err != nil {
		return fmt.Errorf("无效的代理URL: %v", err)
	}

	pp.mutex.Lock()
	defer pp.mutex.Unlock()

	// 检查是否已存在
	for _, proxy := range pp.proxies {
		if proxy.URL == proxyURL {
			return fmt.Errorf("代理已存在")
		}
	}

	pp.proxies = append(pp.proxies, &ProxyInfo{
		URL:       proxyURL,
		IsHealthy: true,
	})
	pp.enabled = true

	logger.Info("添加新代理", logger.String("proxy", maskProxyURL(proxyURL)))
	return nil
}

// RemoveProxy 移除代理
func (pp *ProxyPool) RemoveProxy(proxyURL string) {
	pp.mutex.Lock()
	defer pp.mutex.Unlock()

	for i, proxy := range pp.proxies {
		if proxy.URL == proxyURL {
			pp.proxies = append(pp.proxies[:i], pp.proxies[i+1:]...)
			logger.Info("移除代理", logger.String("proxy", maskProxyURL(proxyURL)))
			break
		}
	}

	if len(pp.proxies) == 0 {
		pp.enabled = false
	}
}
