package converter

import (
	"net/http/httptest"
	"strings"
	"testing"

	"kiro2api/types"

	"github.com/gin-gonic/gin"
)

func TestBuildCodeWhispererRequest_MergeConsecutiveAssistantMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	c.Request.Header.Set("User-Agent", "test")

	anthropicReq := types.AnthropicRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []types.AnthropicRequestMessage{
			{
				Role:    "user",
				Content: "Read the config file",
			},
			{
				Role: "assistant",
				Content: []any{
					map[string]any{"type": "thinking", "thinking": "I need to read the file..."},
					map[string]any{"type": "text", "text": " "},
				},
			},
			{
				Role: "assistant",
				Content: []any{
					map[string]any{"type": "thinking", "thinking": "Let me read the config."},
					map[string]any{"type": "text", "text": "I'll read the config file for you."},
					map[string]any{
						"type":  "tool_use",
						"id":    "toolu_01XYZ",
						"name":  "read_file",
						"input": map[string]any{"path": "/config.json"},
					},
				},
			},
			{
				Role: "user",
				Content: []any{
					map[string]any{
						"type":        "tool_result",
						"tool_use_id": "toolu_01XYZ",
						"content":     "{\"key\":\"value\"}",
					},
				},
			},
		},
		Stream: false,
	}

	cwReq, err := BuildCodeWhispererRequest(anthropicReq, c)
	if err != nil {
		t.Fatalf("BuildCodeWhispererRequest failed: %v", err)
	}

	history := cwReq.ConversationState.History
	if len(history) != 2 {
		t.Fatalf("expected 2 history messages (user + merged assistant), got %d", len(history))
	}

	assistantCount := 0
	foundToolUse := false
	foundThinking := false
	foundText := false

	for i, h := range history {
		if i > 0 {
			_, prevIsAssistant := history[i-1].(types.HistoryAssistantMessage)
			_, curIsAssistant := h.(types.HistoryAssistantMessage)
			if prevIsAssistant && curIsAssistant {
				t.Fatalf("found consecutive assistant messages at history index %d", i)
			}
		}

		if am, ok := h.(types.HistoryAssistantMessage); ok {
			assistantCount++
			if strings.Contains(am.AssistantResponseMessage.Content, "<thinking>") {
				foundThinking = true
			}
			if strings.Contains(am.AssistantResponseMessage.Content, "I'll read the config file for you.") {
				foundText = true
			}
			for _, tu := range am.AssistantResponseMessage.ToolUses {
				if tu.ToolUseId == "toolu_01XYZ" {
					foundToolUse = true
					break
				}
			}
		}
	}

	if assistantCount != 1 {
		t.Fatalf("expected 1 merged assistant message, got %d", assistantCount)
	}
	if !foundThinking {
		t.Fatalf("expected merged assistant content to include <thinking> tag")
	}
	if !foundText {
		t.Fatalf("expected merged assistant content to include the second assistant's text")
	}
	if !foundToolUse {
		t.Fatalf("expected merged assistant message to include tool_use_id toolu_01XYZ")
	}
}
