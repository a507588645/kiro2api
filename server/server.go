package server

import (
	"net/http"
	"os"
	"strings"

	"kiro2api/auth"
	"kiro2api/config"
	"kiro2api/converter"
	"kiro2api/logger"
	"kiro2api/types"
	"kiro2api/utils"

	"github.com/gin-gonic/gin"
)

// 移除全局httpClient，使用utils包中的共享客户端

// StartServer 启动HTTP代理服务器
func StartServer(port string, authToken string, authService *auth.AuthService) {
	// 设置 gin 模式
	ginMode := os.Getenv("GIN_MODE")
	if ginMode == "" {
		ginMode = gin.ReleaseMode
	}
	gin.SetMode(ginMode)

	r := gin.New()

	// 设置请求体大小限制（借鉴 kiro.rs 2026.1.6: 解决图片上传问题）
	r.MaxMultipartMemory = config.MaxMultipartMemory

	// 添加中间件
	r.Use(gin.Logger())
	r.Use(gin.Recovery())
	// 注入请求ID，便于日志追踪
	r.Use(RequestIDMiddleware())
	r.Use(corsMiddleware())
	// 请求体大小限制中间件（100MB，支持大图片上传）
	r.Use(MaxBodySizeMiddleware())
	// 注入AuthService到上下文，供错误处理时使用
	r.Use(func(c *gin.Context) {
		c.Set("auth_service", authService)
		c.Next()
	})
	// 只对 /v1 开头的端点进行认证
	r.Use(PathBasedAuthMiddleware(authToken, []string{"/v1"}))
	uiPassword := strings.TrimSpace(os.Getenv("KIRO_UI_PASSWORD"))
	if uiPassword != "" {
		logger.Info("UI 认证已启用")
	} else {
		logger.Info("UI 认证未启用")
	}
	// 仅保护 Web UI 与管理端点
	r.Use(UIAuthMiddleware(uiPassword, []string{"/static", "/oauth", "/api"}))

	// 静态资源服务 - 前后端完全分离
	r.Static("/static", "./static")
	r.GET("/", func(c *gin.Context) {
		c.File("./static/index.html")
	})

	// 注册 OAuth 路由
	RegisterOAuthRoutes(r)

	// 注册机器码管理路由
	RegisterMachineIdRoutes(r)

	// API端点 - 纯数据服务
	r.GET("/api/tokens", handleTokenPoolAPI)
	r.GET("/api/anti-ban/status", handleAntiBanStatus)
	r.GET("/api/session-binding/status", handleSessionBindingStatus)
	r.GET("/api/session-binding/:session_id", handleSessionBindingDetail)

	// GET /v1/models 端点
	r.GET("/v1/models", func(c *gin.Context) {
		requestModels := config.ListRequestModels()
		if modelProvider, ok := any(authService).(interface{ GetAvailableModels() []string }); ok {
			if models := modelProvider.GetAvailableModels(); len(models) > 0 {
				requestModels = models
			}
		}

		// 构建模型列表
		models := []types.Model{}
		for _, anthropicModel := range requestModels {
			isThinkingVariant := strings.HasSuffix(anthropicModel, "-thinking")
			baseModel := strings.TrimSuffix(anthropicModel, "-thinking")
			supportsThinking := converter.IsThinkingCompatibleModel(baseModel)

			// 添加原始模型
			model := types.Model{
				ID:               anthropicModel,
				Object:           "model",
				Created:          1234567890,
				OwnedBy:          "anthropic",
				DisplayName:      anthropicModel,
				Type:             "text",
				MaxTokens:        200000,
				SupportsThinking: supportsThinking,
			}
			models = append(models, model)

			// 为支持 thinking 的模型添加 -thinking 后缀版本
			if supportsThinking && !isThinkingVariant {
				thinkingModel := types.Model{
					ID:               baseModel + "-thinking",
					Object:           "model",
					Created:          1234567890,
					OwnedBy:          "anthropic",
					DisplayName:      baseModel + " (Thinking)",
					Type:             "text",
					MaxTokens:        200000,
					SupportsThinking: true,
				}
				models = append(models, thinkingModel)
			}
		}

		response := types.ModelsResponse{
			Object: "list",
			Data:   models,
		}

		c.JSON(http.StatusOK, response)
	})

	r.POST("/v1/messages", func(c *gin.Context) {
		// 使用RequestContext统一处理token获取和请求体读取
		reqCtx := &RequestContext{
			GinContext:  c,
			AuthService: authService,
			RequestType: "Anthropic",
		}

		tokenInfo, body, err := reqCtx.GetTokenAndBody()
		if err != nil {
			return // 错误已在GetTokenAndBody中处理
		}

		// 先解析为通用map以便处理工具格式
		var rawReq map[string]any
		if err := utils.SafeUnmarshal(body, &rawReq); err != nil {
			logger.Error("解析请求体失败", logger.Err(err))
			respondError(c, http.StatusBadRequest, "解析请求体失败: %v", err)
			return
		}

		// 标准化工具格式处理
		if tools, exists := rawReq["tools"]; exists && tools != nil {
			if toolsArray, ok := tools.([]any); ok {
				normalizedTools := make([]map[string]any, 0, len(toolsArray))
				for _, tool := range toolsArray {
					if toolMap, ok := tool.(map[string]any); ok {
						// 检查是否是简化的工具格式（直接包含name, description, input_schema）
						if name, hasName := toolMap["name"]; hasName {
							if description, hasDesc := toolMap["description"]; hasDesc {
								if inputSchema, hasSchema := toolMap["input_schema"]; hasSchema {
									// 转换为标准Anthropic工具格式
									normalizedTool := map[string]any{
										"name":         name,
										"description":  description,
										"input_schema": inputSchema,
									}
									normalizedTools = append(normalizedTools, normalizedTool)
									continue
								}
							}
						}
						// 如果不是简化格式，保持原样
						normalizedTools = append(normalizedTools, toolMap)
					}
				}
				rawReq["tools"] = normalizedTools
			}
		}

		// 重新序列化并解析为AnthropicRequest
		normalizedBody, err := utils.SafeMarshal(rawReq)
		if err != nil {
			logger.Error("重新序列化请求失败", logger.Err(err))
			respondError(c, http.StatusBadRequest, "处理请求格式失败: %v", err)
			return
		}

		var anthropicReq types.AnthropicRequest
		if err := utils.SafeUnmarshal(normalizedBody, &anthropicReq); err != nil {
			logger.Error("解析标准化请求体失败", logger.Err(err))
			respondError(c, http.StatusBadRequest, "解析请求体失败: %v", err)
			return
		}

		// 检测 -thinking 后缀，自动开启思考模式（与 kiro.rs 对齐）
		if strings.HasSuffix(anthropicReq.Model, "-thinking") {
			anthropicReq.Model = strings.TrimSuffix(anthropicReq.Model, "-thinking")
			if anthropicReq.Thinking == nil {
				// 与 kiro.rs 对齐：Opus 4.6 使用 adaptive 模式，其他使用 enabled
				modelLower := strings.ToLower(anthropicReq.Model)
				isOpus46 := strings.Contains(modelLower, "opus") &&
					(strings.Contains(modelLower, "4-6") || strings.Contains(modelLower, "4.6"))

				budgetTokens := 20000 // 与 kiro.rs 对齐
				if isOpus46 {
					anthropicReq.Thinking = &types.Thinking{
						Type:         "adaptive",
						BudgetTokens: budgetTokens,
					}
					anthropicReq.OutputConfig = &types.OutputConfig{
						Effort: "high",
					}
				} else {
					anthropicReq.Thinking = &types.Thinking{
						Type:         "enabled",
						BudgetTokens: budgetTokens,
					}
				}
				// 确保 max_tokens > budget_tokens（官方 API 要求）
				if anthropicReq.MaxTokens <= budgetTokens {
					anthropicReq.MaxTokens = budgetTokens + 4096
				}
			}
		}

		// 验证请求的有效性
		if len(anthropicReq.Messages) == 0 {
			logger.Error("请求中没有消息")
			respondError(c, http.StatusBadRequest, "%s", "messages 数组不能为空")
			return
		}

		// 静默丢弃 assistant prefill（参考 kiro.rs fix #72）
		if anthropicReq.Messages[len(anthropicReq.Messages)-1].Role == "assistant" {
			logger.Debug("静默丢弃 assistant prefill 消息")
			anthropicReq.Messages = anthropicReq.Messages[:len(anthropicReq.Messages)-1]
			if len(anthropicReq.Messages) == 0 {
				respondError(c, http.StatusBadRequest, "%s", "messages 数组不能为空")
				return
			}
		}

		// 验证最后一条消息有有效内容
		lastMsg := anthropicReq.Messages[len(anthropicReq.Messages)-1]
		content, err := utils.GetMessageContent(lastMsg.Content)
		if err != nil || strings.TrimSpace(content) == "" || strings.TrimSpace(content) == "answer for user question" {
			respondError(c, http.StatusBadRequest, "%s", "消息内容不能为空")
			return
		}

		if anthropicReq.Stream {
			// 检测纯 WebSearch 请求（参考 kiro.rs）
			if hasWebSearchTool(anthropicReq) {
				handleWebSearchRequest(c, anthropicReq, tokenInfo)
				return
			}
			// 当启用会话池时，使用带重试的处理器
			if config.SessionPoolEnabled {
				handleStreamRequestWithRetry(c, anthropicReq)
			} else {
				handleStreamRequest(c, anthropicReq, tokenInfo)
			}
			return
		}

		handleNonStreamRequest(c, anthropicReq, tokenInfo)
	})

	// Token计数端点
	r.POST("/v1/messages/count_tokens", handleCountTokens)

	// 新增：OpenAI兼容的 /v1/chat/completions 端点
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		// 使用RequestContext统一处理token获取和请求体读取
		reqCtx := &RequestContext{
			GinContext:  c,
			AuthService: authService,
			RequestType: "OpenAI",
		}

		tokenInfo, body, err := reqCtx.GetTokenAndBody()
		if err != nil {
			return // 错误已在GetTokenAndBody中处理
		}

		var openaiReq types.OpenAIRequest
		if err := utils.SafeUnmarshal(body, &openaiReq); err != nil {
			logger.Error("解析OpenAI请求体失败", logger.Err(err))
			respondError(c, http.StatusBadRequest, "解析请求体失败: %v", err)
			return
		}

		logger.Debug("OpenAI请求解析成功",
			logger.String("model", openaiReq.Model),
			logger.Bool("stream", openaiReq.Stream != nil && *openaiReq.Stream),
			logger.Int("max_tokens", func() int {
				if openaiReq.MaxTokens != nil {
					return *openaiReq.MaxTokens
				}
				return 16384
			}()))

		// 转换为Anthropic格式
		anthropicReq := converter.ConvertOpenAIToAnthropic(openaiReq)

		if anthropicReq.Stream {
			// 当启用会话池时，使用带重试的处理器
			if config.SessionPoolEnabled {
				handleOpenAIStreamRequestWithRetry(c, anthropicReq)
			} else {
				handleOpenAIStreamRequest(c, anthropicReq, tokenInfo)
			}
			return
		}
		handleOpenAINonStreamRequest(c, anthropicReq, tokenInfo)
	})

	r.NoRoute(func(c *gin.Context) {
		logger.Warn("访问未知端点",
			logger.String("path", c.Request.URL.Path),
			logger.String("method", c.Request.Method))
		respondError(c, http.StatusNotFound, "%s", "404 未找到")
	})

	logger.Info("启动Anthropic API代理服务器",
		logger.String("port", port),
		logger.String("auth_token", "***"))
	logger.Info("AuthToken 验证已启用")
	logger.Info("可用端点:")
	logger.Info("  GET  /                          - 重定向到静态Dashboard")
	logger.Info("  GET  /static/*                  - 静态资源服务")
	logger.Info("  GET  /api/tokens                - Token池状态API")
	logger.Info("  GET  /v1/models                 - 模型列表")
	logger.Info("  POST /v1/messages               - Anthropic API代理")
	logger.Info("  POST /v1/messages/count_tokens  - Token计数接口")
	logger.Info("  POST /v1/chat/completions       - OpenAI API代理")
	logger.Info("按Ctrl+C停止服务器")

	// 创建自定义HTTP服务器以支持长时间请求
	server := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	logger.Info("启动HTTP服务器", logger.String("port", port))

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("启动服务器失败", logger.Err(err), logger.String("port", port))
		os.Exit(1)
	}
}

// corsMiddleware CORS中间件
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, x-api-key")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusOK)
			return
		}

		c.Next()
	}
}
