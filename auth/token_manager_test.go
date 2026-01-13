package auth

import (
	"fmt"
	"kiro2api/config"
	"kiro2api/types"
	"sync"
	"testing"
	"time"
)

// TestTokenManager_ConcurrentAccess 测试TokenManager的并发访问安全性
func TestTokenManager_ConcurrentAccess(t *testing.T) {
	// 创建测试配置
	configs := []AuthConfig{
		{
			AuthType:     AuthMethodSocial,
			RefreshToken: "test_token_1",
		},
		{
			AuthType:     AuthMethodSocial,
			RefreshToken: "test_token_2",
		},
		{
			AuthType:     AuthMethodSocial,
			RefreshToken: "test_token_3",
		},
	}

	// 创建TokenManager
	tm := NewTokenManager(configs)

	// 预填充缓存（模拟已刷新的token）
	tm.mutex.Lock()
	for i := range configs {
		cacheKey := fmt.Sprintf(config.TokenCacheKeyFormat, i)
		tm.cache.tokens[cacheKey] = &CachedToken{
			Token: types.TokenInfo{
				AccessToken: fmt.Sprintf("access_token_%d", i),
				ExpiresAt:   time.Now().Add(1 * time.Hour),
			},
			UsageInfo: nil,
			CachedAt:  time.Now(),
			Available: 10000.0, // 足够支持50×100=5000次调用
		}
	}
	// 关键修复：更新lastRefresh避免触发真实的token刷新
	tm.lastRefresh = time.Now()
	tm.mutex.Unlock()

	// 并发测试参数
	numGoroutines := 50
	numIterations := 100

	var wg sync.WaitGroup
	errorsChan := make(chan error, numGoroutines*numIterations)

	// 启动多个goroutine并发调用selectBestToken
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < numIterations; j++ {
				// 使用公共API getBestToken而不是内部方法
				_, err := tm.getBestToken()
				if err != nil {
					errorsChan <- fmt.Errorf("goroutine %d iteration %d: getBestToken failed: %v", id, j, err)
				}
				// 模拟一些工作
				time.Sleep(1 * time.Microsecond)
			}
		}(i)
	}

	// 等待所有goroutine完成
	wg.Wait()
	close(errorsChan)

	// 检查是否有错误
	errors := []error{}
	for err := range errorsChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		t.Errorf("并发访问测试失败，发现 %d 个错误", len(errors))
		for i, err := range errors {
			if i < 10 { // 只打印前10个错误
				t.Logf("错误 %d: %v", i+1, err)
			}
		}
	}

	t.Logf("并发测试完成: %d 个goroutine × %d 次迭代 = %d 次调用",
		numGoroutines, numIterations, numGoroutines*numIterations)
}

// TestTokenManager_ConcurrentRefresh 测试并发刷新token的安全性
func TestTokenManager_ConcurrentRefresh(t *testing.T) {
	configs := []AuthConfig{
		{
			AuthType:     AuthMethodSocial,
			RefreshToken: "test_token_1",
		},
	}

	tm := NewTokenManager(configs)

	// 预填充缓存
	tm.mutex.Lock()
	tm.cache.tokens["token_0"] = &CachedToken{
		Token: types.TokenInfo{
			AccessToken: "access_token_0",
			ExpiresAt:   time.Now().Add(1 * time.Hour),
		},
		CachedAt:  time.Now(),
		Available: 10000.0, // 足够支持20×50=1000次调用
	}
	// 关键修复：更新lastRefresh避免触发真实的token刷新
	tm.lastRefresh = time.Now()
	tm.mutex.Unlock()

	numGoroutines := 20
	var wg sync.WaitGroup

	// 并发读取token
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < 50; j++ {
				_, err := tm.getBestToken()
				if err != nil {
					t.Errorf("goroutine %d: getBestToken failed: %v", id, err)
				}
				time.Sleep(1 * time.Microsecond)
			}
		}(i)
	}

	wg.Wait()
	t.Logf("并发刷新测试完成")
}

// TestTokenManager_RaceCondition 使用race detector检测数据竞争
// 运行方式: go test -race -run TestTokenManager_RaceCondition
func TestTokenManager_RaceCondition(t *testing.T) {
	configs := []AuthConfig{
		{AuthType: AuthMethodSocial, RefreshToken: "token1"},
		{AuthType: AuthMethodSocial, RefreshToken: "token2"},
	}

	tm := NewTokenManager(configs)

	// 预填充缓存
	tm.mutex.Lock()
	for i := range configs {
		tm.cache.tokens[fmt.Sprintf(config.TokenCacheKeyFormat, i)] = &CachedToken{
			Token: types.TokenInfo{
				AccessToken: fmt.Sprintf("access_%d", i),
				ExpiresAt:   time.Now().Add(1 * time.Hour),
			},
			CachedAt:  time.Now(),
			Available: 50.0,
		}
	}
	// 关键修复：更新lastRefresh避免触发真实的token刷新
	tm.lastRefresh = time.Now()
	tm.mutex.Unlock()

	var wg sync.WaitGroup
	numGoroutines := 10

	// 并发读写测试
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, _ = tm.getBestToken()
			}
		}()
	}

	wg.Wait()
	t.Log("Race condition测试完成，使用 go test -race 运行以检测数据竞争")
}

// TestTokenManager_SequentialSelection 测试顺序选择逻辑（粘性策略）
func TestTokenManager_SequentialSelection(t *testing.T) {
	configs := []AuthConfig{
		{AuthType: AuthMethodSocial, RefreshToken: "token1"},
		{AuthType: AuthMethodSocial, RefreshToken: "token2"},
		{AuthType: AuthMethodSocial, RefreshToken: "token3"},
	}

	tm := NewTokenManager(configs)

	// 预填充缓存 - 每个token只有少量可用次数
	tm.mutex.Lock()
	for i := range configs {
		tm.cache.tokens[fmt.Sprintf(config.TokenCacheKeyFormat, i)] = &CachedToken{
			Token: types.TokenInfo{
				AccessToken: fmt.Sprintf("access_%d", i),
				ExpiresAt:   time.Now().Add(1 * time.Hour),
			},
			CachedAt:  time.Now(),
			Available: 5.0, // 每个token只有5次使用机会
		}
	}
	// 关键修复：更新lastRefresh避免触发真实的token刷新
	tm.lastRefresh = time.Now()
	tm.mutex.Unlock()

	// 验证顺序选择：使用getBestToken会递减Available
	selectedTokens := make(map[string]int)
	for i := 0; i < 15; i++ { // 15次调用会用完所有token (5+5+5)
		token, err := tm.getBestToken()
		if err == nil {
			selectedTokens[token.AccessToken]++
		}
	}

	t.Logf("Token选择分布: %v", selectedTokens)

	// 验证粘性策略：应该先用完第一个token，然后是第二个，最后是第三个
	if selectedTokens["access_0"] != 5 {
		t.Errorf("期望access_0使用5次，实际使用%d次", selectedTokens["access_0"])
	}
	if selectedTokens["access_1"] != 5 {
		t.Errorf("期望access_1使用5次，实际使用%d次", selectedTokens["access_1"])
	}
	if selectedTokens["access_2"] != 5 {
		t.Errorf("期望access_2使用5次，实际使用%d次", selectedTokens["access_2"])
	}

	// 验证所有token都被使用过
	if len(selectedTokens) != len(configs) {
		t.Errorf("期望使用 %d 个token，实际使用 %d 个", len(configs), len(selectedTokens))
	}

	t.Logf("✅ 顺序选择策略验证通过：粘性策略正确工作")
}
