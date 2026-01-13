package utils

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"kiro2api/config"
	"kiro2api/logger"
	"kiro2api/types"
)

// TokenRefreshStatus 表示token刷新状态
type TokenRefreshStatus int

const (
	TokenStatusIdle       TokenRefreshStatus = iota // 空闲状态
	TokenStatusRefreshing                           // 正在刷新
	TokenStatusCompleted                            // 刷新完成
	TokenStatusFailed                               // 刷新失败
)

// RefreshingToken 正在刷新的token状态
type RefreshingToken struct {
	Status    TokenRefreshStatus
	StartTime time.Time
	EndTime   time.Time
	Result    chan RefreshResult // 用于通知等待者刷新结果
	TokenInfo *types.TokenInfo   // 刷新成功后的token信息
	Error     error              // 刷新失败时的错误
}

// RefreshResult 刷新结果
type RefreshResult struct {
	TokenInfo *types.TokenInfo
	Error     error
	Success   bool
}

// TokenRefreshManager 管理token刷新的并发控制
// 防止同一token被多次并发刷新，提升系统效率并减少API调用
type TokenRefreshManager struct {
	// refreshing 跟踪每个token索引的刷新状态
	// key: token索引, value: 刷新状态信息
	refreshing sync.Map // map[int]*RefreshingToken

	// 全局统计 - 使用atomic操作，无需额外锁
	totalRefreshes     int64 // atomic counter
	duplicatePrevented int64 // atomic counter
}

// NewTokenRefreshManager 创建新的token刷新管理器
func NewTokenRefreshManager() *TokenRefreshManager {
	return &TokenRefreshManager{
		refreshing: sync.Map{},
	}
}

// StartRefresh 开始刷新指定索引的token
// 如果该token已经在刷新中，返回等待通道；否则开始新的刷新
func (trm *TokenRefreshManager) StartRefresh(tokenIdx int) (*RefreshingToken, bool) {
	// 尝试创建新的刷新任务
	newRefreshing := &RefreshingToken{
		Status:    TokenStatusRefreshing,
		StartTime: time.Now(),
		Result:    make(chan RefreshResult, 1), // 缓冲通道，避免阻塞
	}

	// 原子地尝试设置刷新状态
	actual, loaded := trm.refreshing.LoadOrStore(tokenIdx, newRefreshing)
	refreshingToken := actual.(*RefreshingToken)

	if loaded {
		// 该token已经在刷新中
		atomic.AddInt64(&trm.duplicatePrevented, 1) // 原子操作

		logger.Debug("Token正在被其他请求刷新，等待结果",
			logger.Int("token_index", tokenIdx),
			logger.String("started_at", refreshingToken.StartTime.Format(time.RFC3339)))

		return refreshingToken, false
	}

	// 这是新的刷新任务
	atomic.AddInt64(&trm.totalRefreshes, 1) // 原子操作

	logger.Debug("开始新的Token刷新任务",
		logger.Int("token_index", tokenIdx),
		logger.String("start_time", newRefreshing.StartTime.Format(time.RFC3339)))

	return newRefreshing, true
}

// CompleteRefresh 完成token刷新
func (trm *TokenRefreshManager) CompleteRefresh(tokenIdx int, tokenInfo *types.TokenInfo, err error) {
	value, exists := trm.refreshing.Load(tokenIdx)
	if !exists {
		logger.Warn("尝试完成不存在的刷新任务", logger.Int("token_index", tokenIdx))
		return
	}

	refreshingToken := value.(*RefreshingToken)
	refreshingToken.EndTime = time.Now()
	duration := refreshingToken.EndTime.Sub(refreshingToken.StartTime)

	if err != nil {
		// 刷新失败
		refreshingToken.Status = TokenStatusFailed
		refreshingToken.Error = err

		logger.Error("Token刷新失败",
			logger.Int("token_index", tokenIdx),
			logger.Err(err),
			logger.String("duration", duration.String()))

		// 通知等待者
		select {
		case refreshingToken.Result <- RefreshResult{Error: err, Success: false}:
		default:
		}
	} else {
		// 刷新成功
		refreshingToken.Status = TokenStatusCompleted
		refreshingToken.TokenInfo = tokenInfo

		logger.Info("Token刷新完成",
			logger.Int("token_index", tokenIdx),
			logger.String("duration", duration.String()),
			logger.String("expires_at", tokenInfo.ExpiresAt.Format("2006-01-02 15:04:05")))

		// 通知等待者
		select {
		case refreshingToken.Result <- RefreshResult{TokenInfo: tokenInfo, Success: true}:
		default:
		}
	}

	// 设置延迟清理，避免立即删除状态（给其他goroutine一些时间获取结果）
	go func() {
		time.Sleep(config.TokenRefreshCleanupDelay)
		trm.refreshing.Delete(tokenIdx)
		logger.Debug("清理已完成的刷新任务状态", logger.Int("token_index", tokenIdx))
	}()
}

// WaitForRefresh 等待指定token的刷新完成
func (trm *TokenRefreshManager) WaitForRefresh(tokenIdx int, timeout time.Duration) (*types.TokenInfo, error) {
	value, exists := trm.refreshing.Load(tokenIdx)
	if !exists {
		return nil, fmt.Errorf("没有找到token %d的刷新任务", tokenIdx)
	}

	refreshingToken := value.(*RefreshingToken)

	// 使用context实现超时控制
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	select {
	case result := <-refreshingToken.Result:
		if result.Success {
			logger.Debug("等待的Token刷新成功完成",
				logger.Int("token_index", tokenIdx))
			return result.TokenInfo, nil
		}
		return nil, result.Error

	case <-ctx.Done():
		logger.Warn("等待Token刷新超时",
			logger.Int("token_index", tokenIdx),
			logger.String("timeout", timeout.String()))
		return nil, fmt.Errorf("等待token %d刷新超时: %v", tokenIdx, ctx.Err())
	}
}

// IsRefreshing 检查指定token是否正在刷新
func (trm *TokenRefreshManager) IsRefreshing(tokenIdx int) bool {
	value, exists := trm.refreshing.Load(tokenIdx)
	if !exists {
		return false
	}

	refreshingToken := value.(*RefreshingToken)
	return refreshingToken.Status == TokenStatusRefreshing
}

// GetRefreshStatus 获取token刷新状态
func (trm *TokenRefreshManager) GetRefreshStatus(tokenIdx int) (TokenRefreshStatus, time.Time, error) {
	value, exists := trm.refreshing.Load(tokenIdx)
	if !exists {
		return TokenStatusIdle, time.Time{}, nil
	}

	refreshingToken := value.(*RefreshingToken)
	return refreshingToken.Status, refreshingToken.StartTime, refreshingToken.Error
}

// GetStats 获取刷新管理器统计信息
func (trm *TokenRefreshManager) GetStats() map[string]any {
	activeRefreshes := 0
	trm.refreshing.Range(func(key, value any) bool {
		refreshingToken := value.(*RefreshingToken)
		if refreshingToken.Status == TokenStatusRefreshing {
			activeRefreshes++
		}
		return true
	})

	totalRefreshes := atomic.LoadInt64(&trm.totalRefreshes)
	duplicatePrevented := atomic.LoadInt64(&trm.duplicatePrevented)

	efficiencyRate := float64(0)
	if totalRefreshes > 0 {
		efficiencyRate = float64(duplicatePrevented) / float64(totalRefreshes) * 100
	}

	return map[string]any{
		"total_refreshes":     totalRefreshes,
		"duplicate_prevented": duplicatePrevented,
		"active_refreshes":    activeRefreshes,
		"efficiency_rate":     fmt.Sprintf("%.2f%%", efficiencyRate),
	}
}

// ClearExpiredRefreshes 清理过期的刷新状态（用于定期维护）
func (trm *TokenRefreshManager) ClearExpiredRefreshes(maxAge time.Duration) int {
	cleared := 0
	now := time.Now()

	trm.refreshing.Range(func(key, value any) bool {
		refreshingToken := value.(*RefreshingToken)

		// 如果刷新任务已经完成且超过指定时间，则清理
		if refreshingToken.Status != TokenStatusRefreshing &&
			!refreshingToken.EndTime.IsZero() &&
			now.Sub(refreshingToken.EndTime) > maxAge {

			trm.refreshing.Delete(key)
			cleared++

			logger.Debug("清理过期的刷新状态",
				logger.Any("token_index", key),
				logger.String("age", now.Sub(refreshingToken.EndTime).String()))
		}

		return true
	})

	return cleared
}

// ForceCancel 强制取消指定token的刷新（用于紧急情况）
func (trm *TokenRefreshManager) ForceCancel(tokenIdx int) bool {
	value, exists := trm.refreshing.Load(tokenIdx)
	if !exists {
		return false
	}

	refreshingToken := value.(*RefreshingToken)
	if refreshingToken.Status == TokenStatusRefreshing {
		refreshingToken.Status = TokenStatusFailed
		refreshingToken.Error = fmt.Errorf("刷新被强制取消")
		refreshingToken.EndTime = time.Now()

		// 通知等待者
		select {
		case refreshingToken.Result <- RefreshResult{
			Error:   refreshingToken.Error,
			Success: false,
		}:
		default:
		}

		trm.refreshing.Delete(tokenIdx)
		logger.Warn("强制取消Token刷新", logger.Int("token_index", tokenIdx))
		return true
	}

	return false
}
