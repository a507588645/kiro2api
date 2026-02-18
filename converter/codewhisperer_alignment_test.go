package converter

import (
	"testing"

	"kiro2api/types"
)

func TestValidateToolPairing_FiltersDuplicateAndOrphan(t *testing.T) {
	history := []any{
		newAssistantHistoryMessage("tool-1", "read_file"),
		newUserHistoryMessageWithResults("tool-1"),
		newAssistantHistoryMessage("tool-2", "write_file"),
	}

	currentResults := []types.ToolResult{
		newToolResult("tool-1"),      // 历史已配对，应过滤
		newToolResult("tool-2"),      // 需要保留
		newToolResult("orphan-tool"), // 无对应tool_use，应过滤
	}

	filtered, orphaned := validateToolPairing(history, currentResults)

	if len(filtered) != 1 {
		t.Fatalf("expected 1 filtered result, got %d", len(filtered))
	}
	if filtered[0].ToolUseId != "tool-2" {
		t.Fatalf("unexpected tool_use_id: %s", filtered[0].ToolUseId)
	}
	if len(orphaned) != 0 {
		t.Fatalf("expected no orphaned tool_use after pairing, got %d", len(orphaned))
	}
}

func TestRemoveOrphanedToolUses(t *testing.T) {
	history := []any{
		newAssistantHistoryMessage("tool-1", "read_file"),
		newAssistantHistoryMessage("tool-2", "write_file"),
	}

	orphaned := map[string]struct{}{
		"tool-2": {},
	}
	removeOrphanedToolUses(history, orphaned)

	msg1, ok := history[1].(types.HistoryAssistantMessage)
	if !ok {
		t.Fatalf("expected assistant message at index 1")
	}
	if len(msg1.AssistantResponseMessage.ToolUses) != 0 {
		t.Fatalf("expected tool-2 to be removed")
	}
}

func TestEnsureHistoryToolsPresent(t *testing.T) {
	currentTools := []types.CodeWhispererTool{
		{
			ToolSpecification: types.ToolSpecification{
				Name: "read_file",
			},
		},
	}

	history := []any{
		newAssistantHistoryMessage("tool-1", "read_file"),
		newAssistantHistoryMessage("tool-2", "write_file"),
	}

	merged := ensureHistoryToolsPresent(currentTools, history)
	if len(merged) != 2 {
		t.Fatalf("expected 2 tools after merge, got %d", len(merged))
	}

	foundWrite := false
	for _, tool := range merged {
		if tool.ToolSpecification.Name == "write_file" {
			foundWrite = true
			if tool.ToolSpecification.Description == "" {
				t.Fatalf("expected placeholder description for write_file")
			}
		}
	}
	if !foundWrite {
		t.Fatalf("expected write_file placeholder tool to be added")
	}
}

func TestExtractSessionIDFromMetadata(t *testing.T) {
	metadata := map[string]any{
		"user_id": "user_xxx_account__session_a0662283-7fd3-4399-a7eb-52b9a717ae88",
	}

	sessionID := extractSessionIDFromMetadata(metadata)
	if sessionID != "a0662283-7fd3-4399-a7eb-52b9a717ae88" {
		t.Fatalf("unexpected session id: %s", sessionID)
	}
}

func TestIsThinkingCompatibleModel_Aliases(t *testing.T) {
	if !IsThinkingCompatibleModel("claude-opus-4.6-thinking") {
		t.Fatalf("expected opus alias to support thinking")
	}
	if !IsThinkingCompatibleModel("claude-sonnet-4.5") {
		t.Fatalf("expected sonnet alias to support thinking")
	}
	if !IsThinkingCompatibleModel("claude-haiku-4-5-20251001") {
		t.Fatalf("expected haiku model to support thinking")
	}
}

func newAssistantHistoryMessage(toolUseID, toolName string) types.HistoryAssistantMessage {
	msg := types.HistoryAssistantMessage{}
	msg.AssistantResponseMessage.Content = " "
	msg.AssistantResponseMessage.ToolUses = []types.ToolUseEntry{
		{
			ToolUseId: toolUseID,
			Name:      toolName,
			Input:     map[string]any{},
		},
	}
	return msg
}

func newUserHistoryMessageWithResults(toolUseID string) types.HistoryUserMessage {
	msg := types.HistoryUserMessage{}
	msg.UserInputMessage.Content = ""
	msg.UserInputMessage.UserInputMessageContext.ToolResults = []types.ToolResult{
		newToolResult(toolUseID),
	}
	return msg
}

func newToolResult(toolUseID string) types.ToolResult {
	return types.ToolResult{
		ToolUseId: toolUseID,
		Status:    "success",
		Content: []map[string]any{
			{"text": "ok"},
		},
	}
}
