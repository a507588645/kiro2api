package utils

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"kiro2api/logger"
	"kiro2api/types"
)

type anthropicCountTokensResponse struct {
	InputTokens int `json:"input_tokens"`
}

// TokenCounter provides "best effort" token counting:
// 1) If CLAUDE_API_KEY is configured, uses Anthropic official /v1/messages/count_tokens.
// 2) Otherwise falls back to local tiktoken-based estimation.
//
// This mirrors the strategy used in b4u2cc.
type TokenCounter struct {
	claudeAPIKey      string
	anthropicVersion  string
	countTokensURL    string
	localEncodingName string
}

func NewTokenCounterFromEnv() *TokenCounter {
	v := strings.TrimSpace(os.Getenv("ANTHROPIC_VERSION"))
	if v == "" {
		v = "2023-06-01"
	}

	url := strings.TrimSpace(os.Getenv("ANTHROPIC_COUNT_TOKENS_URL"))
	if url == "" {
		url = "https://api.anthropic.com/v1/messages/count_tokens"
	}

	// For Claude models we intentionally use cl100k_base as a practical approximation.
	return &TokenCounter{
		claudeAPIKey:      strings.TrimSpace(os.Getenv("CLAUDE_API_KEY")),
		anthropicVersion:  v,
		countTokensURL:    url,
		localEncodingName: "cl100k_base",
	}
}

func (tc *TokenCounter) CountInputTokens(ctx context.Context, req *types.CountTokensRequest) (int, error) {
	if req == nil {
		return 0, fmt.Errorf("nil request")
	}

	if tc.claudeAPIKey != "" {
		tokens, err := tc.countViaAnthropicAPI(ctx, req)
		if err == nil && tokens > 0 {
			return tokens, nil
		}
		if err != nil {
			logger.Warn("Anthropic官方count_tokens失败，回退到本地估算", logger.Err(err))
		}
	}

	// Local fallback
	return tc.countLocally(req), nil
}

func (tc *TokenCounter) countLocally(req *types.CountTokensRequest) int {
	tokens := 0

	// System prompts
	if len(req.System) > 0 {
		for _, s := range req.System {
			if strings.TrimSpace(s.Text) == "" {
				continue
			}
			tokens += CountTokensWithTiktoken(s.Text, tc.localEncodingName)
		}
	}

	// Messages (including multimodal content blocks)
	tokens += countTokensFromAnthropicMessages(req.Messages, tc.localEncodingName)

	// Tools schema
	if len(req.Tools) > 0 {
		if b, err := SafeMarshal(req.Tools); err == nil {
			tokens += CountTokensWithTiktoken(string(b), tc.localEncodingName)
		}
	}

	if tokens < 1 {
		return 1
	}
	return tokens
}

func (tc *TokenCounter) countViaAnthropicAPI(ctx context.Context, req *types.CountTokensRequest) (int, error) {
	// Small timeout to avoid blocking request processing.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	body, err := SafeMarshal(req)
	if err != nil {
		return 0, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, tc.countTokensURL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", tc.claudeAPIKey)
	httpReq.Header.Set("anthropic-version", tc.anthropicVersion)

	resp, err := DoRequest(httpReq)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	respBody, err := ReadHTTPResponse(resp.Body)
	if err != nil {
		return 0, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Don't log full body (may contain details); just return it for upstream log.
		return 0, fmt.Errorf("anthropic count_tokens status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var out anthropicCountTokensResponse
	if err := SafeUnmarshal(respBody, &out); err != nil {
		return 0, err
	}
	if out.InputTokens <= 0 {
		return 0, fmt.Errorf("invalid input_tokens: %d", out.InputTokens)
	}
	return out.InputTokens, nil
}

const localImageTokenEstimate = 1500

func countTokensFromAnthropicMessages(messages []types.AnthropicRequestMessage, encodingName string) int {
	total := 0
	for _, m := range messages {
		total += countTokensFromAnthropicMessageContent(m.Content, encodingName)
	}
	return total
}

func countTokensFromAnthropicMessageContent(content any, encodingName string) int {
	switch v := content.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return 0
		}
		return CountTokensWithTiktoken(v, encodingName)

	case []any:
		total := 0
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			t, _ := m["type"].(string)
			switch t {
			case "text":
				if s, ok := m["text"].(string); ok {
					total += CountTokensWithTiktoken(s, encodingName)
				}

			case "image":
				// No official tokenizer offline for vision payload; use a conservative fixed estimate.
				total += localImageTokenEstimate

			case "tool_use":
				name, _ := m["name"].(string)
				input := m["input"]
				inputJSON, _ := SafeMarshal(input)
				total += CountTokensWithTiktoken("<invoke name=\""+name+"\">"+string(inputJSON)+"</invoke>", encodingName)

			case "tool_result":
				// content may be string/array/object; stringify conservatively
				var contentStr string
				switch c := m["content"].(type) {
				case string:
					contentStr = c
				default:
					if jb, err := SafeMarshal(c); err == nil {
						contentStr = string(jb)
					}
				}
				total += CountTokensWithTiktoken("<tool_result>"+contentStr+"</tool_result>", encodingName)

			default:
				if jb, err := SafeMarshal(m); err == nil {
					total += CountTokensWithTiktoken(string(jb), encodingName)
				}
			}
		}
		return total

	case []types.ContentBlock:
		total := 0
		for _, block := range v {
			switch block.Type {
			case "text":
				if block.Text != nil {
					total += CountTokensWithTiktoken(*block.Text, encodingName)
				}

			case "image":
				total += localImageTokenEstimate

			case "tool_use":
				name := ""
				if block.Name != nil {
					name = *block.Name
				}
				var inputJSON []byte
				if block.Input != nil {
					inputJSON, _ = SafeMarshal(*block.Input)
				}
				total += CountTokensWithTiktoken("<invoke name=\""+name+"\">"+string(inputJSON)+"</invoke>", encodingName)

			case "tool_result":
				contentStr := ""
				switch c := block.Content.(type) {
				case string:
					contentStr = c
				default:
					if jb, err := SafeMarshal(c); err == nil {
						contentStr = string(jb)
					}
				}
				total += CountTokensWithTiktoken("<tool_result>"+contentStr+"</tool_result>", encodingName)

			default:
				if jb, err := SafeMarshal(block); err == nil {
					total += CountTokensWithTiktoken(string(jb), encodingName)
				}
			}
		}
		return total

	default:
		if jb, err := SafeMarshal(v); err == nil {
			return CountTokensWithTiktoken(string(jb), encodingName)
		}
		return 0
	}
}
