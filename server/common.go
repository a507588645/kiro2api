package server

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"kiro2api/auth"
	"kiro2api/config"
	"kiro2api/converter"
	"kiro2api/logger"
	"kiro2api/types"
	"kiro2api/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AuthServiceWithFingerprint 带指纹的认证服务接口
type AuthServiceWithFingerprint interface {
	GetToken() (types.TokenInfo, error)
	GetTokenWithFingerprint() (types.TokenInfo, *auth.Fingerprint, error)
	MarkTokenFailed()
}

// AuthServiceWithSession 带会话绑定的认证服务接口
type AuthServiceWithSession interface {
	GetToken() (types.TokenInfo, error)
	GetTokenWithFingerprint() (types.TokenInfo, *auth.Fingerprint, error)
	GetTokenWithFingerprintForSession(sessionID string) (types.TokenInfo, *auth.Fingerprint, string, error)
	MarkTokenFailed()
}

// AuthServiceWithModel 支持按模型获取 token
type AuthServiceWithModel interface {
	GetTokenForModel(model string) (types.TokenInfo, error)
}

// AuthServiceWithFingerprintForModel 支持按模型获取带指纹 token
type AuthServiceWithFingerprintForModel interface {
	GetTokenWithFingerprintForModel(model string) (types.TokenInfo, *auth.Fingerprint, error)
}

// AuthServiceWithSessionForModel 支持按模型获取会话绑定 token
type AuthServiceWithSessionForModel interface {
	GetTokenWithFingerprintForSessionAndModel(sessionID string, model string) (types.TokenInfo, *auth.Fingerprint, string, error)
}

// getRequestFingerprint 从上下文获取请求指纹
func getRequestFingerprint(c *gin.Context) *auth.Fingerprint {
	if fp, exists := c.Get("request_fingerprint"); exists {
		if fingerprint, ok := fp.(*auth.Fingerprint); ok {
			return fingerprint
		}
	}
	return nil
}

// respondErrorWithCode 标准化的错误响应结构
// 统一返回: {"error": {"message": string, "code": string}}
func respondErrorWithCode(c *gin.Context, statusCode int, code string, format string, args ...any) {
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"message": fmt.Sprintf(format, args...),
			"code":    code,
		},
	})
}

// respondError 简化封装，依据statusCode映射默认code
func respondError(c *gin.Context, statusCode int, format string, args ...any) {
	var code string
	switch statusCode {
	case http.StatusBadRequest:
		code = "bad_request"
	case http.StatusUnauthorized:
		code = "unauthorized"
	case http.StatusForbidden:
		code = "forbidden"
	case http.StatusNotFound:
		code = "not_found"
	case http.StatusTooManyRequests:
		code = "rate_limited"
	default:
		code = "internal_error"
	}
	respondErrorWithCode(c, statusCode, code, format, args...)
}

// 通用请求处理错误函数
func handleRequestBuildError(c *gin.Context, err error) {
	logger.Error("构建请求失败", addReqFields(c, logger.Err(err))...)
	respondError(c, http.StatusInternalServerError, "构建请求失败: %v", err)
}

func handleRequestSendError(c *gin.Context, err error) {
	logger.Error("发送请求失败", addReqFields(c, logger.Err(err))...)
	respondError(c, http.StatusInternalServerError, "发送请求失败: %v", err)
}

func handleResponseReadError(c *gin.Context, err error) {
	logger.Error("读取响应体失败", addReqFields(c, logger.Err(err))...)
	respondError(c, http.StatusInternalServerError, "读取响应体失败: %v", err)
}

// 通用请求执行函数
func executeCodeWhispererRequest(c *gin.Context, anthropicReq types.AnthropicRequest, tokenInfo types.TokenInfo, isStream bool) (*http.Response, error) {
	req, err := buildCodeWhispererRequest(c, anthropicReq, tokenInfo, isStream)
	if err != nil {
		// 检查是否是模型未找到错误，如果是，则响应已经发送，不需要再次处理
		if _, ok := err.(*types.ModelNotFoundErrorType); ok {
			return nil, err
		}
		handleRequestBuildError(c, err)
		return nil, err
	}

	resp, err := utils.DoRequest(req)
	if err != nil {
		handleRequestSendError(c, err)
		return nil, err
	}

	if handleCodeWhispererError(c, resp) {
		resp.Body.Close()
		return nil, fmt.Errorf("CodeWhisperer API error")
	}

	// 上游响应成功，记录方向与会话
	logger.Debug("上游响应成功",
		addReqFields(c,
			logger.String("direction", "upstream_response"),
			logger.Int("status_code", resp.StatusCode),
		)...)

	return resp, nil
}

// executeCodeWhispererRequestWithRetry 带429重试的请求执行函数
// 当启用会话池时，遇到429会自动切换Token重试
func executeCodeWhispererRequestWithRetry(c *gin.Context, anthropicReq types.AnthropicRequest, isStream bool) (*http.Response, error) {
	sessionID, _ := c.Get("session_id")
	sessionIDStr, _ := sessionID.(string)
	if sessionIDStr == "" {
		sessionIDStr = auth.ExtractSessionID(map[string]string{
			"X-Session-ID": c.GetHeader("X-Session-ID"),
			"X-Request-ID": c.GetHeader("X-Request-ID"),
		})
	}

	poolManager := auth.GetSessionTokenPoolManager()
	maxRetries := config.SessionPoolMaxRetries

	var lastResp *http.Response
	var currentTokenKey string

	for retry := 0; retry <= maxRetries; retry++ {
		// 获取Token
		var token types.TokenInfo
		var fingerprint *auth.Fingerprint
		var err error

		if retry == 0 {
			token, fingerprint, currentTokenKey, err = poolManager.GetAvailableTokenForModel(sessionIDStr, anthropicReq.Model)
		} else {
			token, fingerprint, currentTokenKey, err = poolManager.GetNextAvailableTokenForModel(sessionIDStr, currentTokenKey, anthropicReq.Model)
		}

		if err != nil {
			var modelNotFoundErr *types.ModelNotFoundErrorType
			if errors.As(err, &modelNotFoundErr) {
				c.JSON(http.StatusBadRequest, modelNotFoundErr.ErrorData)
				return nil, err
			}

			if retry == 0 {
				logger.Error("获取Token失败", logger.Err(err))
				respondError(c, http.StatusInternalServerError, "获取token失败: %v", err)
			}
			return nil, err
		}

		// 设置指纹到上下文
		if fingerprint != nil {
			c.Set("request_fingerprint", fingerprint)
		}
		c.Set("token_key", currentTokenKey)

		// 构建并执行请求
		req, err := buildCodeWhispererRequest(c, anthropicReq, token, isStream)
		if err != nil {
			if _, ok := err.(*types.ModelNotFoundErrorType); ok {
				return nil, err
			}
			handleRequestBuildError(c, err)
			return nil, err
		}

		resp, err := utils.DoRequest(req)
		if err != nil {
			handleRequestSendError(c, err)
			return nil, err
		}

		// 检查是否为429
		if resp.StatusCode == http.StatusTooManyRequests {
			logger.Warn("收到429错误，尝试切换Token重试",
				logger.String("session_id", sessionIDStr),
				logger.String("token_key", currentTokenKey),
				logger.Int("retry", retry),
				logger.Int("max_retries", maxRetries))

			// 读取响应体以获取冷却时间
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			cooldown := auth.CalculateCooldownDuration(body, config.SessionPoolCooldown)
			poolManager.MarkTokenCooldown(sessionIDStr, currentTokenKey, cooldown)

			if retry >= maxRetries {
				logger.Error("达到最大重试次数",
					logger.String("session_id", sessionIDStr),
					logger.Int("retries", retry))
				respondErrorWithCode(c, http.StatusTooManyRequests, "rate_limited", "请求过于频繁，请稍后重试")
				return nil, fmt.Errorf("max retries exceeded")
			}

			// 等待重试间隔
			select {
			case <-c.Request.Context().Done():
				return nil, c.Request.Context().Err()
			case <-time.After(config.SessionPoolRetryInterval):
			}
			continue
		}

		// 非429错误或成功
		if handleCodeWhispererError(c, resp) {
			resp.Body.Close()
			return nil, fmt.Errorf("CodeWhisperer API error")
		}

		// 成功
		poolManager.MarkTokenSuccess(sessionIDStr, currentTokenKey)
		logger.Debug("上游响应成功",
			addReqFields(c,
				logger.String("direction", "upstream_response"),
				logger.Int("status_code", resp.StatusCode),
				logger.Int("retries", retry),
			)...)

		return resp, nil
	}

	return lastResp, fmt.Errorf("unexpected retry loop exit")
}

// execCWRequest 供测试覆盖的请求执行入口（可在测试中替换）
var execCWRequest = executeCodeWhispererRequest

// buildCodeWhispererRequest 构建通用的CodeWhisperer请求
func buildCodeWhispererRequest(c *gin.Context, anthropicReq types.AnthropicRequest, tokenInfo types.TokenInfo, isStream bool) (*http.Request, error) {
	cwReq, err := converter.BuildCodeWhispererRequest(anthropicReq, c)
	if err != nil {
		// 检查是否是模型未找到错误
		if modelNotFoundErr, ok := err.(*types.ModelNotFoundErrorType); ok {
			// 直接返回用户期望的JSON格式
			c.JSON(http.StatusBadRequest, modelNotFoundErr.ErrorData)
			return nil, err
		}
		return nil, fmt.Errorf("构建CodeWhisperer请求失败: %v", err)
	}

	cwReqBody, err := utils.SafeMarshal(cwReq)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %v", err)
	}

	// 临时调试：记录发送给CodeWhisperer的请求内容
	// 补充：当工具直传启用时输出工具名称预览
	var toolNamesPreview string
	if len(cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools) > 0 {
		names := make([]string, 0, len(cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools))
		for _, t := range cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools {
			if t.ToolSpecification.Name != "" {
				names = append(names, t.ToolSpecification.Name)
			}
		}
		toolNamesPreview = strings.Join(names, ",")
	}

	logger.Debug("发送给CodeWhisperer的请求",
		logger.String("direction", "upstream_request"),
		logger.Int("request_size", len(cwReqBody)),
		logger.String("request_body", string(cwReqBody)),
		logger.Int("tools_count", len(cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools)),
		logger.String("tools_names", toolNamesPreview))

	req, err := http.NewRequest("POST", config.GetCodeWhispererURL(), bytes.NewReader(cwReqBody))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Authorization", "Bearer "+tokenInfo.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	if isStream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "*/*")
	}

	// 添加上游请求必需的header（借鉴 kiro.rs）
	req.Header.Set("x-amzn-kiro-agent-mode", "vibe")             // kiro.rs 使用 "vibe"
	req.Header.Set("x-amzn-codewhisperer-optout", "true")        // 借鉴 kiro.rs
	req.Header.Set("amz-sdk-invocation-id", uuid.New().String()) // 借鉴 kiro.rs：请求追踪ID
	req.Header.Set("amz-sdk-request", "attempt=1; max=3")        // 借鉴 kiro.rs：重试配置
	req.Header.Set("Host", config.GetCodeWhispererHost())         // 与 kiro.rs 对齐：设置 Host 头

	// 使用指纹管理器获取随机化的请求头
	fingerprint := getRequestFingerprint(c)
	if fingerprint != nil {
		// 应用完整指纹（包括UA、Accept-Language、Sec-Fetch等）
		fingerprint.ApplyToRequest(req)

		logger.Debug("应用请求指纹",
			logger.String("os", fingerprint.OSType),
			logger.String("locale", fingerprint.Locale),
			logger.String("sdk", fingerprint.SDKVersion))
	} else {
		// 降级到默认值（与 kiro.rs 0.9.2 对齐）
		req.Header.Set("x-amz-user-agent", "aws-sdk-js/1.0.27 KiroIDE-0.9.2-66c23a8c5d15afabec89ef9954ef52a119f10d369df04d548fc6c1eac694b0d1")
		req.Header.Set("user-agent", "aws-sdk-js/1.0.27 ua/2.1 os/darwin#24.6.0 lang/js md/nodejs#22.21.1 api/codewhispererstreaming#1.0.27 m/E KiroIDE-0.9.2-66c23a8c5d15afabec89ef9954ef52a119f10d369df04d548fc6c1eac694b0d1")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		req.Header.Set("Accept-Encoding", "gzip, deflate, br")
		req.Header.Set("Connection", "close") // 借鉴 kiro.rs 使用 close
	}

	return req, nil
}

// handleCodeWhispererError 处理 CodeWhisperer API 错误响应
// 使用统一的 ErrorMapper 处理所有错误类型
func handleCodeWhispererError(c *gin.Context, resp *http.Response) bool {
	if resp.StatusCode == http.StatusOK {
		return false
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("读取错误响应失败",
			addReqFields(c,
				logger.String("direction", "upstream_response"),
				logger.Err(err),
			)...)
		respondError(c, http.StatusInternalServerError, "%s", "读取响应失败")
		return true
	}

	logger.Error("上游响应错误",
		addReqFields(c,
			logger.String("direction", "upstream_response"),
			logger.Int("status_code", resp.StatusCode),
			logger.Int("response_len", len(body)),
			logger.String("response_body", string(body)),
		)...)

	// 使用统一的错误映射器处理所有错误
	errorMapper := NewErrorMapper()
	result := errorMapper.MapCodeWhispererError(resp.StatusCode, body)

	// 如果策略要求标记 token 失败，执行标记
	if result.ShouldMarkTokenFail {
		markTokenFailed(c)
	}

	// 发送符合 Claude 规范的错误响应
	errorMapper.SendClaudeError(c, result)

	return true
}

// markTokenFailed 标记当前 token 失败
func markTokenFailed(c *gin.Context) {
	if authService, exists := c.Get("auth_service"); exists {
		if as, ok := authService.(AuthServiceWithSession); ok {
			as.MarkTokenFailed()
			logger.Debug("已标记 token 失败（会话绑定模式）")
		} else if as, ok := authService.(AuthServiceWithFingerprint); ok {
			as.MarkTokenFailed()
			logger.Debug("已标记 token 失败（指纹模式）")
		}
	}
}

// StreamEventSender 统一的流事件发送接口
type StreamEventSender interface {
	SendEvent(c *gin.Context, data any) error
	SendError(c *gin.Context, message string, err error) error
}

// AnthropicStreamSender Anthropic格式的流事件发送器
type AnthropicStreamSender struct{}

func (s *AnthropicStreamSender) SendEvent(c *gin.Context, data any) error {
	var eventType string

	if dataMap, ok := data.(map[string]any); ok {
		if t, exists := dataMap["type"]; exists {
			eventType = t.(string)
		}

	}

	json, err := utils.SafeMarshal(data)
	if err != nil {
		return err
	}

	// 压缩日志：仅记录事件类型与负载长度
	logger.Debug("发送SSE事件",
		addReqFields(c,
			logger.String("direction", "downstream_send"),
			logger.String("event", eventType),
			logger.Int("payload_len", len(json)),
			logger.String("payload_preview", string(json)),
		)...)

	fmt.Fprintf(c.Writer, "event: %s\n", eventType)
	fmt.Fprintf(c.Writer, "data: %s\n\n", string(json))
	c.Writer.Flush()
	return nil
}

func (s *AnthropicStreamSender) SendError(c *gin.Context, message string, _ error) error {
	errorResp := map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    "overloaded_error",
			"message": message,
		},
	}
	return s.SendEvent(c, errorResp)
}

// OpenAIStreamSender OpenAI格式的流事件发送器
type OpenAIStreamSender struct{}

func (s *OpenAIStreamSender) SendEvent(c *gin.Context, data any) error {

	json, err := utils.SafeMarshal(data)
	if err != nil {
		return err
	}

	// 压缩日志：记录负载长度
	logger.Debug("发送OpenAI SSE事件",
		addReqFields(c,
			logger.String("direction", "downstream_send"),
			logger.Int("payload_len", len(json)),
		)...)

	fmt.Fprintf(c.Writer, "data: %s\n\n", string(json))
	c.Writer.Flush()
	return nil
}

func (s *OpenAIStreamSender) SendError(c *gin.Context, message string, _ error) error {
	errorResp := map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "server_error",
			"code":    "internal_error",
		},
	}

	json, err := utils.FastMarshal(errorResp)
	if err != nil {
		return err
	}

	fmt.Fprintf(c.Writer, "data: %s\n\n", string(json))
	c.Writer.Flush()
	return nil
}

// RequestContext 请求处理上下文，封装通用的请求处理逻辑
type RequestContext struct {
	GinContext  *gin.Context
	AuthService interface {
		GetToken() (types.TokenInfo, error)
	}
	RequestType string // "anthropic" 或 "openai"
}

// GetTokenAndBody 通用的token获取和请求体读取
// 返回: tokenInfo, requestBody, error
func (rc *RequestContext) GetTokenAndBody() (types.TokenInfo, []byte, error) {
	var tokenInfo types.TokenInfo
	var err error

	// 先读取请求体，以便提取 model 并做模型级 token 选择
	body, err := rc.GinContext.GetRawData()
	if err != nil {
		logger.Error("读取请求体失败", logger.Err(err))
		respondError(rc.GinContext, http.StatusBadRequest, "读取请求体失败: %v", err)
		return types.TokenInfo{}, nil, err
	}

	requestedModel := extractRequestedModel(body)
	rc.GinContext.Set("requested_model", requestedModel)

	// 提取会话 ID
	sessionID := auth.ExtractSessionID(map[string]string{
		"X-Session-ID": rc.GinContext.GetHeader("X-Session-ID"),
		"X-Request-ID": rc.GinContext.GetHeader("X-Request-ID"),
	})

	// 将会话 ID 存入上下文
	rc.GinContext.Set("session_id", sessionID)

	// 尝试使用会话绑定获取 token
	if authWithSessionModel, ok := rc.AuthService.(AuthServiceWithSessionForModel); ok {
		var fingerprint *auth.Fingerprint
		var tokenKey string
		tokenInfo, fingerprint, tokenKey, err = authWithSessionModel.GetTokenWithFingerprintForSessionAndModel(sessionID, requestedModel)
		if err == nil {
			if fingerprint != nil {
				rc.GinContext.Set("request_fingerprint", fingerprint)
				logger.Debug("使用会话绑定的指纹化token",
					logger.String("session_id", sessionID),
					logger.String("token_key", tokenKey),
					logger.String("os", fingerprint.OSType),
					logger.String("sdk_version", fingerprint.SDKVersion),
					logger.String("requested_model", requestedModel))
			}
			rc.GinContext.Set("token_key", tokenKey)
		}
	} else if authWithSession, ok := rc.AuthService.(AuthServiceWithSession); ok {
		var fingerprint *auth.Fingerprint
		var tokenKey string
		tokenInfo, fingerprint, tokenKey, err = authWithSession.GetTokenWithFingerprintForSession(sessionID)
		if err == nil {
			// 将指纹和 token key 存入上下文
			if fingerprint != nil {
				rc.GinContext.Set("request_fingerprint", fingerprint)
				logger.Debug("使用会话绑定的指纹化token",
					logger.String("session_id", sessionID),
					logger.String("token_key", tokenKey),
					logger.String("os", fingerprint.OSType),
					logger.String("sdk_version", fingerprint.SDKVersion))
			}
			rc.GinContext.Set("token_key", tokenKey)
		}
	} else if authWithFpModel, ok := rc.AuthService.(AuthServiceWithFingerprintForModel); ok {
		var fingerprint *auth.Fingerprint
		tokenInfo, fingerprint, err = authWithFpModel.GetTokenWithFingerprintForModel(requestedModel)
		if err == nil && fingerprint != nil {
			rc.GinContext.Set("request_fingerprint", fingerprint)
			logger.Debug("使用模型过滤后的指纹化token",
				logger.String("os", fingerprint.OSType),
				logger.String("sdk_version", fingerprint.SDKVersion),
				logger.String("requested_model", requestedModel))
		}
	} else if authWithFp, ok := rc.AuthService.(AuthServiceWithFingerprint); ok {
		// 降级到带指纹的方法
		var fingerprint *auth.Fingerprint
		tokenInfo, fingerprint, err = authWithFp.GetTokenWithFingerprint()
		if err == nil && fingerprint != nil {
			// 将指纹存入上下文，供后续请求使用
			rc.GinContext.Set("request_fingerprint", fingerprint)
			logger.Debug("使用指纹化token",
				logger.String("os", fingerprint.OSType),
				logger.String("sdk_version", fingerprint.SDKVersion))
		}
	} else if authWithModel, ok := rc.AuthService.(AuthServiceWithModel); ok {
		tokenInfo, err = authWithModel.GetTokenForModel(requestedModel)
	} else {
		// 降级到普通方法
		tokenInfo, err = rc.AuthService.GetToken()
	}

	if err != nil {
		var modelNotFoundErr *types.ModelNotFoundErrorType
		if errors.As(err, &modelNotFoundErr) {
			rc.GinContext.JSON(http.StatusBadRequest, modelNotFoundErr.ErrorData)
			return types.TokenInfo{}, nil, err
		}

		logger.Error("获取token失败", logger.Err(err))
		respondError(rc.GinContext, http.StatusInternalServerError, "获取token失败: %v", err)
		return types.TokenInfo{}, nil, err
	}

	// 记录请求日志
	logger.Debug(fmt.Sprintf("收到%s请求", rc.RequestType),
		addReqFields(rc.GinContext,
			logger.String("direction", "client_request"),
			logger.String("session_id", sessionID),
			logger.String("model", requestedModel),
			logger.String("body", string(body)),
			logger.Int("body_size", len(body)),
			logger.String("remote_addr", rc.GinContext.ClientIP()),
			logger.String("user_agent", rc.GinContext.GetHeader("User-Agent")),
		)...)

	return tokenInfo, body, nil
}

func extractRequestedModel(body []byte) string {
	var req struct {
		Model string `json:"model"`
	}
	if err := utils.SafeUnmarshal(body, &req); err != nil {
		return ""
	}
	return strings.TrimSpace(req.Model)
}
