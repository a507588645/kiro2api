package utils

import (
	"sync"
	"testing"
	"time"

	"kiro2api/types"

	"github.com/stretchr/testify/assert"
)

func TestNewTokenRefreshManager(t *testing.T) {
	manager := NewTokenRefreshManager()

	assert.NotNil(t, manager)
	stats := manager.GetStats()
	assert.Equal(t, int64(0), stats["total_refreshes"])
	assert.Equal(t, int64(0), stats["duplicate_prevented"])
}

func TestStartRefresh_NewRefresh(t *testing.T) {
	manager := NewTokenRefreshManager()

	refreshToken, isNew := manager.StartRefresh(0)

	assert.True(t, isNew, "第一次刷新应该返回true")
	assert.NotNil(t, refreshToken)
	assert.Equal(t, TokenStatusRefreshing, refreshToken.Status)
	assert.NotNil(t, refreshToken.Result)
	assert.False(t, refreshToken.StartTime.IsZero())

	stats := manager.GetStats()
	assert.Equal(t, int64(1), stats["total_refreshes"])
	assert.Equal(t, int64(0), stats["duplicate_prevented"])
}

func TestStartRefresh_DuplicateRefresh(t *testing.T) {
	manager := NewTokenRefreshManager()

	// 第一次刷新
	refreshToken1, isNew1 := manager.StartRefresh(0)
	assert.True(t, isNew1)

	// 第二次刷新同一个token（应该被阻止）
	refreshToken2, isNew2 := manager.StartRefresh(0)
	assert.False(t, isNew2, "重复刷新应该返回false")
	assert.Equal(t, refreshToken1, refreshToken2, "应该返回相同的RefreshingToken")

	stats := manager.GetStats()
	assert.Equal(t, int64(1), stats["total_refreshes"], "总刷新次数应该为1")
	assert.Equal(t, int64(1), stats["duplicate_prevented"], "应该阻止1次重复刷新")
}

func TestCompleteRefresh_Success(t *testing.T) {
	manager := NewTokenRefreshManager()

	// 开始刷新
	refreshToken, _ := manager.StartRefresh(0)

	// 完成刷新
	tokenInfo := &types.TokenInfo{
		AccessToken: "test-token",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}
	manager.CompleteRefresh(0, tokenInfo, nil)

	// 验证状态
	status, _, _ := manager.GetRefreshStatus(0)
	assert.Equal(t, TokenStatusCompleted, status)

	// 验证结果通道
	select {
	case result := <-refreshToken.Result:
		assert.True(t, result.Success)
		assert.Equal(t, tokenInfo, result.TokenInfo)
		assert.NoError(t, result.Error)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("超时等待刷新结果")
	}
}

func TestCompleteRefresh_Failure(t *testing.T) {
	manager := NewTokenRefreshManager()

	// 开始刷新
	refreshToken, _ := manager.StartRefresh(0)

	// 完成刷新（失败）
	testErr := assert.AnError
	manager.CompleteRefresh(0, nil, testErr)

	// 验证状态
	status, _, _ := manager.GetRefreshStatus(0)
	assert.Equal(t, TokenStatusFailed, status)

	// 验证结果通道
	select {
	case result := <-refreshToken.Result:
		assert.False(t, result.Success)
		assert.Nil(t, result.TokenInfo)
		assert.Equal(t, testErr, result.Error)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("超时等待刷新结果")
	}
}

func TestWaitForRefresh_Success(t *testing.T) {
	manager := NewTokenRefreshManager()

	// 开始刷新
	manager.StartRefresh(0)

	// 在另一个goroutine中完成刷新
	tokenInfo := &types.TokenInfo{
		AccessToken: "test-token",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		manager.CompleteRefresh(0, tokenInfo, nil)
	}()

	// 等待刷新完成
	result, err := manager.WaitForRefresh(0, 1*time.Second)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, tokenInfo, result)
}

func TestWaitForRefresh_Timeout(t *testing.T) {
	manager := NewTokenRefreshManager()

	// 开始刷新但不完成
	manager.StartRefresh(0)

	// 等待刷新（应该超时）
	result, err := manager.WaitForRefresh(0, 100*time.Millisecond)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "超时")
}

func TestWaitForRefresh_ContextCanceled(t *testing.T) {
	manager := NewTokenRefreshManager()

	// 开始刷新但不完成
	manager.StartRefresh(0)

	// 等待刷新（应该超时，因为WaitForRefresh内部使用context）
	result, err := manager.WaitForRefresh(0, 50*time.Millisecond)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "超时")
}

func TestWaitForRefresh_NotRefreshing(t *testing.T) {
	manager := NewTokenRefreshManager()

	// 不开始刷新，直接等待
	result, err := manager.WaitForRefresh(0, 100*time.Millisecond)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "没有找到token")
}

func TestIsRefreshing(t *testing.T) {
	manager := NewTokenRefreshManager()

	// 初始状态
	assert.False(t, manager.IsRefreshing(0))

	// 开始刷新
	manager.StartRefresh(0)
	assert.True(t, manager.IsRefreshing(0))

	// 完成刷新
	manager.CompleteRefresh(0, &types.TokenInfo{}, nil)
	assert.False(t, manager.IsRefreshing(0))
}

func TestGetRefreshStatus(t *testing.T) {
	manager := NewTokenRefreshManager()

	// 未刷新状态
	status, _, _ := manager.GetRefreshStatus(0)
	assert.Equal(t, TokenStatusIdle, status)

	// 刷新中状态
	manager.StartRefresh(0)
	status, _, _ = manager.GetRefreshStatus(0)
	assert.Equal(t, TokenStatusRefreshing, status)

	// 完成状态
	manager.CompleteRefresh(0, &types.TokenInfo{}, nil)
	status, _, _ = manager.GetRefreshStatus(0)
	assert.Equal(t, TokenStatusCompleted, status)
}

func TestConcurrentRefresh(t *testing.T) {
	manager := NewTokenRefreshManager()
	tokenIdx := 0

	// 并发启动多个刷新请求
	concurrency := 10
	var wg sync.WaitGroup
	newRefreshCount := 0
	duplicateCount := 0
	var mu sync.Mutex

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, isNew := manager.StartRefresh(tokenIdx)
			mu.Lock()
			if isNew {
				newRefreshCount++
			} else {
				duplicateCount++
			}
			mu.Unlock()
		}()
	}

	wg.Wait()

	// 验证只有一个新刷新，其他都是重复
	assert.Equal(t, 1, newRefreshCount, "应该只有一个新刷新")
	assert.Equal(t, concurrency-1, duplicateCount, "其他应该都是重复刷新")

	stats := manager.GetStats()
	assert.Equal(t, int64(1), stats["total_refreshes"])
	assert.Equal(t, int64(concurrency-1), stats["duplicate_prevented"])
}

func TestClearExpiredRefreshes(t *testing.T) {
	manager := NewTokenRefreshManager()

	// 创建一个已完成的刷新
	manager.StartRefresh(0)
	manager.CompleteRefresh(0, &types.TokenInfo{}, nil)

	// 等待一段时间
	time.Sleep(10 * time.Millisecond)

	// 清理过期的刷新（1毫秒过期时间）
	cleared := manager.ClearExpiredRefreshes(1 * time.Millisecond)
	assert.Equal(t, 1, cleared, "应该清理1个过期刷新")

	// 验证已被清理
	status, _, _ := manager.GetRefreshStatus(0)
	assert.Equal(t, TokenStatusIdle, status)
}

func TestForceCancel(t *testing.T) {
	manager := NewTokenRefreshManager()

	// 开始刷新
	refreshToken, _ := manager.StartRefresh(0)

	// 强制取消
	cancelled := manager.ForceCancel(0)
	assert.True(t, cancelled, "应该成功取消刷新")

	// 验证状态已被删除（ForceCancel会删除状态）
	status, _, _ := manager.GetRefreshStatus(0)
	assert.Equal(t, TokenStatusIdle, status, "取消后状态应该被清理")

	// 验证结果通道
	select {
	case result := <-refreshToken.Result:
		assert.False(t, result.Success)
		assert.Error(t, result.Error)
		assert.Contains(t, result.Error.Error(), "强制取消")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("超时等待取消结果")
	}
}

func TestMultipleTokenRefresh(t *testing.T) {
	manager := NewTokenRefreshManager()

	// 同时刷新多个不同的token
	token0, isNew0 := manager.StartRefresh(0)
	token1, isNew1 := manager.StartRefresh(1)
	token2, isNew2 := manager.StartRefresh(2)

	assert.True(t, isNew0)
	assert.True(t, isNew1)
	assert.True(t, isNew2)
	assert.NotEqual(t, token0, token1)
	assert.NotEqual(t, token1, token2)

	stats := manager.GetStats()
	assert.Equal(t, int64(3), stats["total_refreshes"])
	assert.Equal(t, int64(0), stats["duplicate_prevented"])
}
