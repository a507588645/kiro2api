package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"kiro2api/types"
)

// TestHandleCountTokens_Success 测试成功的token计数
func TestHandleCountTokens_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		request       types.CountTokensRequest
		wantMinTokens int
		wantMaxTokens int
		description   string
	}{
		{
			name: "简单文本消息",
			request: types.CountTokensRequest{
				Model: "claude-sonnet-4-20250514",
				Messages: []types.AnthropicRequestMessage{
					{
						Role:    "user",
						Content: "Hello, how are you?",
					},
				},
			},
			wantMinTokens: 1,
			wantMaxTokens: 100,
			description:   "简单消息应该返回合理的token数",
		},
		{
			name: "多条消息",
			request: types.CountTokensRequest{
				Model: "claude-sonnet-4-20250514",
				Messages: []types.AnthropicRequestMessage{
					{
						Role:    "user",
						Content: "First message",
					},
					{
						Role:    "assistant",
						Content: "Response",
					},
					{
						Role:    "user",
						Content: "Second message",
					},
				},
			},
			wantMinTokens: 5,
			wantMaxTokens: 200,
			description:   "多条消息应该累加token数",
		},
		{
			name: "带工具定义的请求",
			request: types.CountTokensRequest{
				Model: "claude-sonnet-4-20250514",
				Messages: []types.AnthropicRequestMessage{
					{
						Role:    "user",
						Content: "Use a tool",
					},
				},
				Tools: []types.AnthropicTool{
					{
						Name:        "get_weather",
						Description: "Get weather information",
						InputSchema: map[string]any{
							"type": "object",
							"properties": map[string]any{
								"location": map[string]any{
									"type":        "string",
									"description": "City name",
								},
							},
						},
					},
				},
			},
			wantMinTokens: 10,
			wantMaxTokens: 500,
			description:   "带工具定义应该增加token数",
		},
		{
			name: "带系统提示的请求",
			request: types.CountTokensRequest{
				Model: "claude-sonnet-4-20250514",
				Messages: []types.AnthropicRequestMessage{
					{
						Role:    "user",
						Content: "Hello",
					},
				},
				System: []types.AnthropicSystemMessage{
					{
						Type: "text",
						Text: "You are a helpful assistant",
					},
				},
			},
			wantMinTokens: 5,
			wantMaxTokens: 200,
			description:   "带系统提示应该增加token数",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			// 构建请求
			jsonBytes, err := json.Marshal(tt.request)
			assert.NoError(t, err)

			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", bytes.NewReader(jsonBytes))
			c.Request.Header.Set("Content-Type", "application/json")

			// 调用处理器
			handleCountTokens(c)

			// 验证响应
			assert.Equal(t, http.StatusOK, w.Code, tt.description)

			// 解析响应
			var response types.CountTokensResponse
			err = json.Unmarshal(w.Body.Bytes(), &response)
			assert.NoError(t, err)

			// 验证token数在合理范围内
			assert.GreaterOrEqual(t, response.InputTokens, tt.wantMinTokens, "token数应该大于最小值")
			assert.LessOrEqual(t, response.InputTokens, tt.wantMaxTokens, "token数应该小于最大值")
			assert.Greater(t, response.InputTokens, 0, "token数应该大于0")
		})
	}
}

// TestHandleCountTokens_InvalidRequest 测试无效请求
func TestHandleCountTokens_InvalidRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		requestBody string
		wantStatus  int
		wantError   string
		description string
	}{
		{
			name:        "无效的JSON",
			requestBody: `{invalid json}`,
			wantStatus:  http.StatusBadRequest,
			wantError:   "invalid_request_error",
			description: "无效JSON应该返回400错误",
		},
		{
			name:        "空请求体",
			requestBody: ``,
			wantStatus:  http.StatusBadRequest,
			wantError:   "invalid_request_error",
			description: "空请求体应该返回400错误",
		},
		{
			name: "缺少model字段",
			requestBody: `{
				"messages": [{"role": "user", "content": "test"}]
			}`,
			wantStatus:  http.StatusBadRequest,
			wantError:   "invalid_request_error",
			description: "缺少model应该返回400错误",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", bytes.NewReader([]byte(tt.requestBody)))
			c.Request.Header.Set("Content-Type", "application/json")

			// 调用处理器
			handleCountTokens(c)

			// 验证响应
			assert.Equal(t, tt.wantStatus, w.Code, tt.description)

			// 解析错误响应
			var response map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			assert.NoError(t, err)

			// 验证错误结构
			if errorObj, ok := response["error"].(map[string]interface{}); ok {
				assert.Equal(t, tt.wantError, errorObj["type"])
				assert.NotEmpty(t, errorObj["message"])
			}
		})
	}
}

// TestHandleCountTokens_InvalidModel 测试无效模型
func TestHandleCountTokens_InvalidModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name            string
		model           string
		wantStatus      int
		wantErrContains string
		description     string
	}{
		{
			name:            "无效的模型名称",
			model:           "invalid-model",
			wantStatus:      http.StatusBadRequest,
			wantErrContains: "Invalid model",
			description:     "无效模型应该返回错误",
		},
		{
			name:            "空模型名称",
			model:           "",
			wantStatus:      http.StatusBadRequest,
			wantErrContains: "required",
			description:     "空模型名称应该返回验证错误",
		},
		{
			name:            "不支持的模型前缀",
			model:           "llama-2",
			wantStatus:      http.StatusBadRequest,
			wantErrContains: "Invalid model",
			description:     "不支持的模型前缀应该返回错误",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			request := types.CountTokensRequest{
				Model: tt.model,
				Messages: []types.AnthropicRequestMessage{
					{
						Role:    "user",
						Content: "test",
					},
				},
			}

			jsonBytes, err := json.Marshal(request)
			assert.NoError(t, err)

			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", bytes.NewReader(jsonBytes))
			c.Request.Header.Set("Content-Type", "application/json")

			// 调用处理器
			handleCountTokens(c)

			// 验证响应状态码
			assert.Equal(t, tt.wantStatus, w.Code, tt.description)

			// 解析错误响应
			var response map[string]interface{}
			err = json.Unmarshal(w.Body.Bytes(), &response)
			assert.NoError(t, err)

			// 验证错误结构
			if errorObj, ok := response["error"].(map[string]interface{}); ok {
				assert.Equal(t, "invalid_request_error", errorObj["type"])
				assert.Contains(t, errorObj["message"], tt.wantErrContains)
			}
		})
	}
}

// TestHandleCountTokens_ComplexContent 测试复杂内容的token计数
func TestHandleCountTokens_ComplexContent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		messages    []types.AnthropicRequestMessage
		minTokens   int
		description string
	}{
		{
			name: "长文本消息",
			messages: []types.AnthropicRequestMessage{
				{
					Role:    "user",
					Content: "This is a very long message that contains multiple sentences and should result in a higher token count. " + string(make([]byte, 1000)),
				},
			},
			minTokens:   100,
			description: "长文本应该有更多token",
		},
		{
			name: "多模态内容（文本+图片）",
			messages: []types.AnthropicRequestMessage{
				{
					Role: "user",
					Content: []map[string]any{
						{
							"type": "text",
							"text": "Describe this image",
						},
						{
							"type": "image",
							"source": map[string]any{
								"type":       "base64",
								"media_type": "image/jpeg",
								"data":       "base64data...",
							},
						},
					},
				},
			},
			minTokens:   10,
			description: "多模态内容应该计算所有部分",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			request := types.CountTokensRequest{
				Model:    "claude-sonnet-4-20250514",
				Messages: tt.messages,
			}

			jsonBytes, err := json.Marshal(request)
			assert.NoError(t, err)

			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", bytes.NewReader(jsonBytes))
			c.Request.Header.Set("Content-Type", "application/json")

			// 调用处理器
			handleCountTokens(c)

			// 验证响应
			assert.Equal(t, http.StatusOK, w.Code)

			// 解析响应
			var response types.CountTokensResponse
			err = json.Unmarshal(w.Body.Bytes(), &response)
			assert.NoError(t, err)

			assert.GreaterOrEqual(t, response.InputTokens, tt.minTokens, tt.description)
		})
	}
}

// BenchmarkHandleCountTokens 基准测试token计数性能
func BenchmarkHandleCountTokens(b *testing.B) {
	gin.SetMode(gin.TestMode)

	request := types.CountTokensRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []types.AnthropicRequestMessage{
			{
				Role:    "user",
				Content: "Hello, how are you today?",
			},
		},
	}

	jsonBytes, _ := json.Marshal(request)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", bytes.NewReader(jsonBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		handleCountTokens(c)
	}
}
