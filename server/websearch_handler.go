package server

import (
	"bytes"
	crand "crypto/rand"
	"fmt"
	"io"
	mr "math/rand"
	"net/http"
	"time"

	"kiro2api/config"
	"kiro2api/logger"
	"kiro2api/types"
	"kiro2api/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// 检测是否为纯 web_search 请求：tools 有且只有一个，且 name 为 "web_search"
func hasWebSearchTool(req types.AnthropicRequest) bool {
	if len(req.Tools) != 1 {
		return false
	}
	return req.Tools[0].Name == "web_search" || req.Tools[0].Name == "websearch"
}

// 从消息中提取搜索查询（去除前缀 "Perform a web search for the query: "）
func extractSearchQuery(req types.AnthropicRequest) string {
	if len(req.Messages) == 0 {
		return ""
	}

	last := req.Messages[len(req.Messages)-1]
	content, err := utils.GetMessageContent(last.Content)
	if err != nil {
		return ""
	}

	const prefix = "Perform a web search for the query: "
	if len(content) >= len(prefix) && content[:len(prefix)] == prefix {
		return content[len(prefix):]
	}
	return content
}

type mcpJSONRPCRequest struct {
	ID     string `json:"id"`
	JSONRPC string `json:"jsonrpc"`
	Method string `json:"method"`
	Params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"params"`
}

type mcpJSONRPCResponse struct {
	Result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"result"`
}

type mcpWebSearchPayload struct {
	Results []struct {
		Title         string `json:"title"`
		URL           string `json:"url"`
		Snippet       string `json:"snippet"`
		PublishedDate *int64 `json:"publishedDate"`
	} `json:"results"`
}

type webSearchResultItem struct {
	Type             string `json:"type"`
	Title            string `json:"title"`
	URL              string `json:"url"`
	EncryptedContent string `json:"encrypted_content"`
	PageAge          string `json:"page_age,omitempty"`
}

func getMCPURL() string {
	return fmt.Sprintf("https://q.%s.amazonaws.com/mcp", config.DefaultRegion)
}

func randLowerAlphaNum(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := 0; i < n; i++ {
		b[i] = chars[mr.Intn(len(chars))]
	}
	return string(b)
}

func randAlphaNum(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := 0; i < n; i++ {
		b[i] = chars[mr.Intn(len(chars))]
	}
	return string(b)
}

func generateToolUseID() string {
	// srvtoolu_ + UUID v4 去掉横线取前32位
	u := uuid.New().String()
	out := make([]byte, 0, 32)
	for i := 0; i < len(u) && len(out) < 32; i++ {
		if u[i] == '-' {
			continue
		}
		out = append(out, u[i])
	}
	return "srvtoolu_" + string(out)
}

func generateJSONRPCID() string {
	// id: web_search_tooluse_{22位随机字母数字}_{毫秒时间戳}_{8位随机小写字母数字}
	return fmt.Sprintf(
		"web_search_tooluse_%s_%d_%s",
		randAlphaNum(22),
		time.Now().UnixMilli(),
		randLowerAlphaNum(8),
	)
}

func callMCPWebSearch(ctx *gin.Context, query string, tokenInfo types.TokenInfo) ([]webSearchResultItem, error) {
	reqBody := mcpJSONRPCRequest{
		ID:     generateJSONRPCID(),
		JSONRPC: "2.0",
		Method: "tools/call",
	}
	reqBody.Params.Name = "web_search"
	reqBody.Params.Arguments = map[string]any{"query": query}

	b, err := utils.SafeMarshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx.Request.Context(), "POST", getMCPURL(), bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tokenInfo.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := utils.DoRequest(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var rpcResp mcpJSONRPCResponse
	if err := utils.SafeUnmarshal(body, &rpcResp); err != nil {
		return nil, err
	}

	var payloadText string
	for _, c := range rpcResp.Result.Content {
		if c.Type == "text" {
			payloadText = c.Text
			break
		}
	}
	if payloadText == "" {
		return nil, fmt.Errorf("mcp response missing text content")
	}

	var payload mcpWebSearchPayload
	if err := utils.SafeUnmarshal([]byte(payloadText), &payload); err != nil {
		return nil, err
	}

	results := make([]webSearchResultItem, 0, len(payload.Results))
	for _, r := range payload.Results {
		item := webSearchResultItem{
			Type:             "web_search_result",
			Title:            r.Title,
			URL:              r.URL,
			EncryptedContent: r.Snippet,
		}
		if r.PublishedDate != nil {
			item.PageAge = time.Unix(*r.PublishedDate/1000, 0).UTC().Format("January 2, 2006")
		}
		results = append(results, item)
	}

	return results, nil
}

func generateSearchSummary(query string, results []webSearchResultItem) string {
	s := fmt.Sprintf("Here are the search results for %q:\n\n", query)

	for i, r := range results {
		snippet := r.EncryptedContent
		if len([]rune(snippet)) > 200 {
			r := []rune(snippet)
			snippet = string(r[:200]) + "..."
		} else if snippet != "" {
			snippet = snippet + "..."
		}

		s += fmt.Sprintf("%d. **%s**\n   %s\n   Source: %s\n\n", i+1, r.Title, snippet, r.URL)
	}

	s += "Please note that these are web search results and may not be fully accurate or up-to-date."
	return s
}

// 处理 WebSearch 请求（流式 SSE 响应）
func handleWebSearchRequest(c *gin.Context, req types.AnthropicRequest, tokenInfo types.TokenInfo) {
	sender := &AnthropicStreamSender{}
	if err := initializeSSEResponse(c); err != nil {
		_ = sender.SendError(c, "连接不支持SSE刷新", err)
		return
	}

	// Seed math/rand for request-scoped IDs
	seed := make([]byte, 8)
	if _, err := crand.Read(seed); err == nil {
		var v int64
		for i := 0; i < len(seed); i++ {
			v = (v << 8) | int64(seed[i])
		}
		mr.Seed(v)
	} else {
		mr.Seed(time.Now().UnixNano())
	}

	query := extractSearchQuery(req)

	// input_tokens 估算
	inputTokens := GetTokenCalculator().CalculateInputTokens(c.Request.Context(), req)

	messageID := fmt.Sprintf(config.MessageIDFormat, time.Now().Format(config.MessageIDTimeFormat))
	srvToolUseID := generateToolUseID()

	// MCP 调用失败时优雅降级：results=空数组
	searchResults := make([]webSearchResultItem, 0)
	if results, err := callMCPWebSearch(c, query, tokenInfo); err != nil {
		logger.Warn("MCP web_search 调用失败，降级为空结果", addReqFields(c, logger.Err(err))...)
	} else {
		searchResults = results
	}

	summary := generateSearchSummary(query, searchResults)
	outputTokens := (len(summary) + 3) / 4

	events := []map[string]any{
		{
			"type": "message_start",
			"message": map[string]any{
				"id":            messageID,
				"type":          "message",
				"role":          "assistant",
				"content":       []any{},
				"model":         req.Model,
				"stop_reason":   nil,
				"stop_sequence": nil,
				"usage": map[string]any{
					"input_tokens":  inputTokens,
					"output_tokens": 0,
				},
			},
		},
		{
			"type":  "content_block_start",
			"index": 0,
			"content_block": map[string]any{
				"type": "text",
				"text": "",
			},
		},
		{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{
				"type": "text_delta",
				"text": fmt.Sprintf("I'll search for %q.", query),
			},
		},
		{
			"type":  "content_block_stop",
			"index": 0,
		},
		{
			"type":  "content_block_start",
			"index": 1,
			"content_block": map[string]any{
				"type":  "server_tool_use",
				"id":    srvToolUseID,
				"name":  "web_search",
				"input": map[string]any{"query": query},
			},
		},
		{
			"type":  "content_block_stop",
			"index": 1,
		},
		{
			"type":  "content_block_start",
			"index": 2,
			"content_block": map[string]any{
				"type":    "web_search_tool_result",
				"content": searchResults,
			},
		},
		{
			"type":  "content_block_stop",
			"index": 2,
		},
		{
			"type":  "content_block_start",
			"index": 3,
			"content_block": map[string]any{
				"type": "text",
				"text": "",
			},
		},
	}

	for _, ev := range events {
		_ = sender.SendEvent(c, ev)
	}

	// summary 按每100字符chunk发送（rune安全）
	runes := []rune(summary)
	for i := 0; i < len(runes); i += 100 {
		end := i + 100
		if end > len(runes) {
			end = len(runes)
		}
		_ = sender.SendEvent(c, map[string]any{
			"type":  "content_block_delta",
			"index": 3,
			"delta": map[string]any{
				"type": "text_delta",
				"text": string(runes[i:end]),
			},
		})
	}

	_ = sender.SendEvent(c, map[string]any{"type": "content_block_stop", "index": 3})

	_ = sender.SendEvent(c, map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   "end_turn",
			"stop_sequence": nil,
		},
		"usage": map[string]any{
			"output_tokens": outputTokens,
			"server_tool_use": map[string]any{
				"web_search_requests": 1,
			},
		},
	})

	_ = sender.SendEvent(c, map[string]any{"type": "message_stop"})
}
