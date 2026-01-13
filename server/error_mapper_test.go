package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// TestContentLengthExceedsStrategy_MapError 测试内容长度超限策略
func TestContentLengthExceedsStrategy_MapError(t *testing.T) {
	strategy := &ContentLengthExceedsStrategy{}

	tests := []struct {
		name         string
		statusCode   int
		responseBody []byte
		wantResponse *ClaudeErrorResponse
		wantHandled  bool
		description  string
	}{
		{
			name:       "内容长度超限错误",
			statusCode: http.StatusBadRequest,
			responseBody: []byte(`{
				"message": "Content length exceeds threshold",
				"reason": "CONTENT_LENGTH_EXCEEDS_THRESHOLD"
			}`),
			wantResponse: &ClaudeErrorResponse{
				Type:       "message_delta",
				StopReason: "max_tokens",
				Message:    "Content length exceeds threshold, response truncated",
			},
			wantHandled: true,
			description: "应该正确映射内容长度超限错误",
		},
		{
			name:         "非400状态码",
			statusCode:   http.StatusInternalServerError,
			responseBody: []byte(`{"message": "error", "reason": "CONTENT_LENGTH_EXCEEDS_THRESHOLD"}`),
			wantResponse: nil,
			wantHandled:  false,
			description:  "非400状态码应该不处理",
		},
		{
			name:       "其他错误原因",
			statusCode: http.StatusBadRequest,
			responseBody: []byte(`{
				"message": "Other error",
				"reason": "OTHER_ERROR"
			}`),
			wantResponse: nil,
			wantHandled:  false,
			description:  "其他错误原因应该不处理",
		},
		{
			name:         "无效的JSON",
			statusCode:   http.StatusBadRequest,
			responseBody: []byte(`invalid json`),
			wantResponse: nil,
			wantHandled:  false,
			description:  "无效JSON应该不处理",
		},
		{
			name:         "空响应体",
			statusCode:   http.StatusBadRequest,
			responseBody: []byte(``),
			wantResponse: nil,
			wantHandled:  false,
			description:  "空响应体应该不处理",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotResponse, gotHandled := strategy.MapError(tt.statusCode, tt.responseBody)

			assert.Equal(t, tt.wantHandled, gotHandled, tt.description)

			if tt.wantHandled {
				assert.NotNil(t, gotResponse)
				assert.Equal(t, tt.wantResponse.Type, gotResponse.Type)
				assert.Equal(t, tt.wantResponse.StopReason, gotResponse.StopReason)
				assert.Equal(t, tt.wantResponse.Message, gotResponse.Message)
			} else {
				assert.Nil(t, gotResponse)
			}
		})
	}
}

// TestContentLengthExceedsStrategy_GetErrorType 测试获取错误类型
func TestContentLengthExceedsStrategy_GetErrorType(t *testing.T) {
	strategy := &ContentLengthExceedsStrategy{}
	assert.Equal(t, "content_length_exceeds", strategy.GetErrorType())
}

// TestDefaultErrorStrategy_MapError 测试默认错误策略
func TestDefaultErrorStrategy_MapError(t *testing.T) {
	strategy := &DefaultErrorStrategy{}

	tests := []struct {
		name         string
		statusCode   int
		responseBody []byte
		wantMessage  string
		description  string
	}{
		{
			name:         "普通错误",
			statusCode:   http.StatusInternalServerError,
			responseBody: []byte(`{"error": "internal error"}`),
			wantMessage:  `Upstream error: {"error": "internal error"}`,
			description:  "应该包装上游错误消息",
		},
		{
			name:         "空响应体",
			statusCode:   http.StatusBadRequest,
			responseBody: []byte(``),
			wantMessage:  "Upstream error: ",
			description:  "空响应体应该返回空消息",
		},
		{
			name:         "纯文本错误",
			statusCode:   http.StatusForbidden,
			responseBody: []byte(`Access denied`),
			wantMessage:  "Upstream error: Access denied",
			description:  "应该处理纯文本错误",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotResponse, gotHandled := strategy.MapError(tt.statusCode, tt.responseBody)

			assert.True(t, gotHandled, "默认策略应该总是处理")
			assert.NotNil(t, gotResponse)
			assert.Equal(t, "error", gotResponse.Type)
			assert.Equal(t, tt.wantMessage, gotResponse.Message, tt.description)
		})
	}
}

// TestDefaultErrorStrategy_GetErrorType 测试获取默认错误类型
func TestDefaultErrorStrategy_GetErrorType(t *testing.T) {
	strategy := &DefaultErrorStrategy{}
	assert.Equal(t, "default", strategy.GetErrorType())
}

// TestNewErrorMapper 测试创建错误映射器
func TestNewErrorMapper(t *testing.T) {
	mapper := NewErrorMapper()

	assert.NotNil(t, mapper)
	assert.NotNil(t, mapper.strategies)
	assert.Len(t, mapper.strategies, 2, "应该有2个策略")

	// 验证策略顺序
	assert.IsType(t, &ContentLengthExceedsStrategy{}, mapper.strategies[0], "第一个应该是ContentLengthExceedsStrategy")
	assert.IsType(t, &DefaultErrorStrategy{}, mapper.strategies[1], "第二个应该是DefaultErrorStrategy")
}

// TestErrorMapper_MapCodeWhispererError 测试映射CodeWhisperer错误
func TestErrorMapper_MapCodeWhispererError(t *testing.T) {
	mapper := NewErrorMapper()

	tests := []struct {
		name                string
		statusCode          int
		responseBody        []byte
		wantType            string
		wantStopReason      string
		wantMessageContains string
		description         string
	}{
		{
			name:                "内容长度超限错误",
			statusCode:          http.StatusBadRequest,
			responseBody:        []byte(`{"message": "error", "reason": "CONTENT_LENGTH_EXCEEDS_THRESHOLD"}`),
			wantType:            "message_delta",
			wantStopReason:      "max_tokens",
			wantMessageContains: "Content length exceeds",
			description:         "应该映射为max_tokens",
		},
		{
			name:                "普通错误",
			statusCode:          http.StatusInternalServerError,
			responseBody:        []byte(`{"error": "server error"}`),
			wantType:            "error",
			wantStopReason:      "",
			wantMessageContains: "Upstream error",
			description:         "应该使用默认策略",
		},
		{
			name:                "未知错误",
			statusCode:          http.StatusBadRequest,
			responseBody:        []byte(`{"reason": "UNKNOWN"}`),
			wantType:            "error",
			wantStopReason:      "",
			wantMessageContains: "Upstream error",
			description:         "未知错误应该使用默认策略",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapper.MapCodeWhispererError(tt.statusCode, tt.responseBody)

			assert.NotNil(t, result)
			assert.Equal(t, tt.wantType, result.Type, tt.description)
			assert.Equal(t, tt.wantStopReason, result.StopReason)
			assert.Contains(t, result.Message, tt.wantMessageContains)
		})
	}
}

// TestErrorMapper_MapCodeWhispererError_EmptyStrategies 测试空策略列表
func TestErrorMapper_MapCodeWhispererError_EmptyStrategies(t *testing.T) {
	// 创建一个没有策略的映射器
	mapper := &ErrorMapper{
		strategies: []ErrorMappingStrategy{},
	}

	result := mapper.MapCodeWhispererError(http.StatusInternalServerError, []byte(`error`))

	assert.NotNil(t, result)
	assert.Equal(t, "error", result.Type)
	assert.Equal(t, "Unknown error", result.Message, "空策略列表应该返回Unknown error")
}

// TestErrorMapper_SendClaudeError_MaxTokens 测试发送max_tokens错误
func TestErrorMapper_SendClaudeError_MaxTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mapper := NewErrorMapper()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	claudeError := &ClaudeErrorResponse{
		Type:       "message_delta",
		StopReason: "max_tokens",
		Message:    "Content length exceeds threshold",
	}

	mapper.SendClaudeError(c, claudeError)

	// 验证响应
	assert.Equal(t, http.StatusOK, w.Code)

	// 验证响应体包含SSE格式
	body := w.Body.String()
	assert.Contains(t, body, "data: ", "应该包含SSE data前缀")

	// 解析JSON验证结构
	// SSE格式: data: {json}\n\n
	if len(body) > 6 {
		jsonStr := body[6:] // 跳过 "data: "
		var response map[string]interface{}
		err := json.Unmarshal([]byte(jsonStr), &response)
		if err == nil {
			assert.Equal(t, "message_delta", response["type"])
			if delta, ok := response["delta"].(map[string]interface{}); ok {
				assert.Equal(t, "max_tokens", delta["stop_reason"])
			}
		}
	}
}

// TestErrorMapper_SendClaudeError_StandardError 测试发送标准错误
func TestErrorMapper_SendClaudeError_StandardError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mapper := NewErrorMapper()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	claudeError := &ClaudeErrorResponse{
		Type:    "error",
		Message: "Test error message",
	}

	mapper.SendClaudeError(c, claudeError)

	// 验证响应
	assert.Equal(t, http.StatusOK, w.Code)

	// 验证响应体包含SSE格式
	body := w.Body.String()
	assert.Contains(t, body, "data: ", "应该包含SSE data前缀")

	// 解析JSON验证结构
	if len(body) > 6 {
		jsonStr := body[6:]
		var response map[string]interface{}
		err := json.Unmarshal([]byte(jsonStr), &response)
		if err == nil {
			assert.Equal(t, "error", response["type"])
			if errorObj, ok := response["error"].(map[string]interface{}); ok {
				assert.Equal(t, "overloaded_error", errorObj["type"])
				assert.Equal(t, "Test error message", errorObj["message"])
			}
		}
	}
}

// TestClaudeErrorResponse_JSON 测试ClaudeErrorResponse JSON序列化
func TestClaudeErrorResponse_JSON(t *testing.T) {
	tests := []struct {
		name     string
		response ClaudeErrorResponse
		wantJSON string
	}{
		{
			name: "完整响应",
			response: ClaudeErrorResponse{
				Type:       "message_delta",
				Message:    "test message",
				StopReason: "max_tokens",
			},
			wantJSON: `{"type":"message_delta","message":"test message","stop_reason":"max_tokens"}`,
		},
		{
			name: "无StopReason",
			response: ClaudeErrorResponse{
				Type:    "error",
				Message: "error message",
			},
			wantJSON: `{"type":"error","message":"error message"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonBytes, err := json.Marshal(tt.response)
			assert.NoError(t, err)
			assert.JSONEq(t, tt.wantJSON, string(jsonBytes))
		})
	}
}

// TestCodeWhispererErrorBody_JSON 测试CodeWhispererErrorBody JSON反序列化
func TestCodeWhispererErrorBody_JSON(t *testing.T) {
	jsonStr := `{"message":"test error","reason":"TEST_REASON"}`

	var errorBody CodeWhispererErrorBody
	err := json.Unmarshal([]byte(jsonStr), &errorBody)

	assert.NoError(t, err)
	assert.Equal(t, "test error", errorBody.Message)
	assert.Equal(t, "TEST_REASON", errorBody.Reason)
}

// BenchmarkErrorMapper_MapCodeWhispererError 基准测试错误映射性能
func BenchmarkErrorMapper_MapCodeWhispererError(b *testing.B) {
	mapper := NewErrorMapper()
	responseBody := []byte(`{"message": "error", "reason": "CONTENT_LENGTH_EXCEEDS_THRESHOLD"}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mapper.MapCodeWhispererError(http.StatusBadRequest, responseBody)
	}
}

// BenchmarkContentLengthExceedsStrategy_MapError 基准测试内容长度超限策略性能
func BenchmarkContentLengthExceedsStrategy_MapError(b *testing.B) {
	strategy := &ContentLengthExceedsStrategy{}
	responseBody := []byte(`{"message": "error", "reason": "CONTENT_LENGTH_EXCEEDS_THRESHOLD"}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		strategy.MapError(http.StatusBadRequest, responseBody)
	}
}
