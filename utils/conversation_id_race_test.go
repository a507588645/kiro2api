package utils

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestConversationIDManagerConcurrency 测试并发场景下的线程安全性
func TestConversationIDManagerConcurrency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := NewConversationIDManager()

	const numGoroutines = 100
	const numRequests = 50

	var wg sync.WaitGroup
	results := make(chan string, numGoroutines*numRequests)

	// 模拟100个并发goroutine，每个发送50个请求
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < numRequests; j++ {
				// 创建模拟的HTTP请求上下文
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request, _ = http.NewRequest("GET", "/test", nil)
				c.Request.Header.Set("User-Agent", fmt.Sprintf("TestAgent-%d", workerID))
				c.Request.RemoteAddr = fmt.Sprintf("192.168.1.%d:12345", workerID%255+1)

				// 调用会话ID生成（这里会并发访问cache map）
				convID := manager.GenerateConversationID(c)
				results <- convID
			}
		}(i)
	}

	// 等待所有goroutine完成
	wg.Wait()
	close(results)

	// 验证结果
	uniqueIDs := make(map[string]int)
	totalResults := 0
	for id := range results {
		uniqueIDs[id]++
		totalResults++
	}

	expectedTotal := numGoroutines * numRequests
	if totalResults != expectedTotal {
		t.Errorf("期望收到 %d 个结果，实际收到 %d", expectedTotal, totalResults)
	}

	t.Logf("并发测试完成: %d 个goroutine × %d 个请求 = %d 个总调用",
		numGoroutines, numRequests, totalResults)
	t.Logf("生成了 %d 个唯一的会话ID", len(uniqueIDs))
}

// TestConversationIDManagerInvalidateRace 测试清理缓存时的并发安全性
func TestConversationIDManagerInvalidateRace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := NewConversationIDManager()

	const numReaders = 50
	const numInvalidators = 5
	const iterations = 100

	var wg sync.WaitGroup

	// 启动多个读取goroutine
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request, _ = http.NewRequest("GET", "/test", nil)
				c.Request.Header.Set("User-Agent", fmt.Sprintf("Reader-%d", workerID))
				c.Request.RemoteAddr = "127.0.0.1:12345"

				// 并发读取
				_ = manager.GenerateConversationID(c)
			}
		}(i)
	}

	// 启动多个清理goroutine
	for i := 0; i < numInvalidators; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				// 并发清理缓存
				manager.InvalidateOldSessions()
			}
		}(i)
	}

	wg.Wait()
	t.Log("并发读写和清理测试完成，无data race")
}

// TestGlobalConversationIDManagerConcurrency 测试全局实例的并发安全性
func TestGlobalConversationIDManagerConcurrency(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const numGoroutines = 50
	var wg sync.WaitGroup

	// 模拟多个HTTP请求同时访问全局manager（真实场景）
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request, _ = http.NewRequest("POST", "/v1/messages", nil)
			c.Request.Header.Set("User-Agent", "claude-cli/2.0.9")
			c.Request.RemoteAddr = "::1:56789"

			// 使用全局函数（这是实际代码中的调用方式）
			convID := GenerateStableConversationID(c)
			if convID == "" {
				t.Errorf("Worker %d 收到空的会话ID", workerID)
			}
		}(i)
	}

	wg.Wait()
	t.Log("全局实例并发测试完成")
}

// BenchmarkConversationIDGenerationConcurrent 并发场景下的性能基准测试
func BenchmarkConversationIDGenerationConcurrent(b *testing.B) {
	gin.SetMode(gin.TestMode)
	manager := NewConversationIDManager()

	b.RunParallel(func(pb *testing.PB) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/test", nil)
		c.Request.Header.Set("User-Agent", "BenchAgent")
		c.Request.RemoteAddr = "127.0.0.1:12345"

		for pb.Next() {
			_ = manager.GenerateConversationID(c)
		}
	})
}
