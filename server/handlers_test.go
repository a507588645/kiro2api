package server

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestExtractRelevantHeaders(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		expected map[string]string
	}{
		{
			name: "提取基本头部",
			headers: map[string]string{
				"Content-Type": "application/json",
				"Accept":       "application/json",
			},
			expected: map[string]string{
				"Content-Type": "application/json",
				"Accept":       "application/json",
			},
		},
		{
			name: "Authorization头部脱敏",
			headers: map[string]string{
				"Authorization": "Bearer 1234567890abcdefghijklmnopqrstuvwxyz",
			},
			expected: map[string]string{
				"Authorization": "Bearer 123***tuvwxyz",
			},
		},
		{
			name: "X-API-Key头部脱敏",
			headers: map[string]string{
				"X-API-Key": "sk-1234567890abcdef",
			},
			expected: map[string]string{
				"X-API-Key": "sk-12***def",
			},
		},
		{
			name: "短Authorization不脱敏",
			headers: map[string]string{
				"Authorization": "Bearer short",
			},
			expected: map[string]string{
				"Authorization": "Bearer short",
			},
		},
		{
			name: "短X-API-Key不脱敏",
			headers: map[string]string{
				"X-API-Key": "short",
			},
			expected: map[string]string{
				"X-API-Key": "short",
			},
		},
		{
			name: "混合头部",
			headers: map[string]string{
				"Content-Type":  "application/json",
				"Authorization": "Bearer 1234567890abcdefghijklmnopqrstuvwxyz",
				"X-Request-ID":  "req-123",
				"Accept":        "*/*",
			},
			expected: map[string]string{
				"Content-Type":  "application/json",
				"Authorization": "Bearer 123***tuvwxyz",
				"X-Request-ID":  "req-123",
				"Accept":        "*/*",
			},
		},
		{
			name:     "空头部",
			headers:  map[string]string{},
			expected: map[string]string{},
		},
		{
			name: "忽略不相关头部",
			headers: map[string]string{
				"Content-Type":    "application/json",
				"X-Custom-Header": "custom-value",
			},
			expected: map[string]string{
				"Content-Type": "application/json",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/", nil)

			// 设置请求头
			for key, value := range tt.headers {
				c.Request.Header.Set(key, value)
			}

			result := extractRelevantHeaders(c)

			assert.Equal(t, len(tt.expected), len(result), "头部数量应该匹配")
			for key, expectedValue := range tt.expected {
				assert.Equal(t, expectedValue, result[key], "头部 %s 的值应该匹配", key)
			}
		})
	}
}

func TestExtractRelevantHeaders_AllSupportedHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)

	// 设置所有支持的头部
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Authorization", "Bearer 1234567890abcdefghijklmnopqrstuvwxyz")
	c.Request.Header.Set("X-API-Key", "sk-1234567890abcdef")
	c.Request.Header.Set("X-Request-ID", "req-123")
	c.Request.Header.Set("X-Forwarded-For", "192.168.1.1")
	c.Request.Header.Set("Accept", "application/json")
	c.Request.Header.Set("Accept-Encoding", "gzip, deflate")

	result := extractRelevantHeaders(c)

	assert.Equal(t, 7, len(result), "应该提取所有7个支持的头部")
	assert.Equal(t, "application/json", result["Content-Type"])
	assert.Equal(t, "Bearer 123***tuvwxyz", result["Authorization"])
	assert.Equal(t, "sk-12***def", result["X-API-Key"])
	assert.Equal(t, "req-123", result["X-Request-ID"])
	assert.Equal(t, "192.168.1.1", result["X-Forwarded-For"])
	assert.Equal(t, "application/json", result["Accept"])
	assert.Equal(t, "gzip, deflate", result["Accept-Encoding"])
}

func TestCreateTokenPreview(t *testing.T) {
	tests := []struct {
		name        string
		accessToken string
		expected    string
	}{
		{
			name:        "正常长度token",
			accessToken: "1234567890abcdefghijklmnopqrstuvwxyz",
			expected:    "***qrstuvwxyz", // *** + 后10位
		},
		{
			name:        "短token(<=10)",
			accessToken: "short",
			expected:    "*****", // 全部用*代替
		},
		{
			name:        "空token",
			accessToken: "",
			expected:    "",
		},
		{
			name:        "恰好10字符",
			accessToken: "1234567890",
			expected:    "**********",
		},
		{
			name:        "11字符",
			accessToken: "12345678901",
			expected:    "***2345678901", // *** + 后10位
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			preview := createTokenPreview(tt.accessToken)
			assert.Equal(t, tt.expected, preview)
		})
	}
}

func TestCreateTokenPreview_EdgeCases(t *testing.T) {
	// 测试边界情况
	tests := []struct {
		name        string
		accessToken string
		expected    string
	}{
		{"恰好11字符", "12345678901", "***2345678901"},
		{"12字符", "123456789012", "***3456789012"},
		{"13字符", "1234567890123", "***4567890123"},
		{"非常长的token", "1234567890abcdefghijklmnopqrstuvwxyz1234567890", "***1234567890"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			preview := createTokenPreview(tt.accessToken)
			assert.Equal(t, tt.expected, preview)
			assert.Equal(t, 13, len(preview), "预览长度应该是13 (*** + 10字符)")
		})
	}
}
