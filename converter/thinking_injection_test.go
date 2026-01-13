package converter

import (
	"testing"

	"kiro2api/types"
)

func TestGenerateThinkingPrefix(t *testing.T) {
	tests := []struct {
		name     string
		thinking *types.Thinking
		want     string
	}{
		{
			name:     "nil thinking",
			thinking: nil,
			want:     "",
		},
		{
			name: "disabled thinking",
			thinking: &types.Thinking{
				Type:         "disabled",
				BudgetTokens: 10000,
			},
			want: "",
		},
		{
			name: "enabled thinking with default budget",
			thinking: &types.Thinking{
				Type:         "enabled",
				BudgetTokens: 20000,
			},
			want: "<thinking_mode>enabled</thinking_mode><max_thinking_length>20000</max_thinking_length>",
		},
		{
			name: "enabled thinking with custom budget",
			thinking: &types.Thinking{
				Type:         "enabled",
				BudgetTokens: 15000,
			},
			want: "<thinking_mode>enabled</thinking_mode><max_thinking_length>15000</max_thinking_length>",
		},
		{
			name: "enabled thinking with over-max budget (should be normalized)",
			thinking: &types.Thinking{
				Type:         "enabled",
				BudgetTokens: 50000, // Over max, will be normalized to 24576
			},
			want: "<thinking_mode>enabled</thinking_mode><max_thinking_length>24576</max_thinking_length>",
		},
		{
			name: "enabled thinking with under-min budget (should be normalized)",
			thinking: &types.Thinking{
				Type:         "enabled",
				BudgetTokens: 500, // Under min, will be normalized to 1024
			},
			want: "<thinking_mode>enabled</thinking_mode><max_thinking_length>1024</max_thinking_length>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateThinkingPrefix(tt.thinking)
			if got != tt.want {
				t.Errorf("generateThinkingPrefix() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHasThinkingTags(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "no tags",
			content: "This is a normal system message",
			want:    false,
		},
		{
			name:    "has thinking_mode tag",
			content: "<thinking_mode>enabled</thinking_mode> Some content",
			want:    true,
		},
		{
			name:    "has max_thinking_length tag",
			content: "Some content <max_thinking_length>20000</max_thinking_length>",
			want:    true,
		},
		{
			name:    "has both tags",
			content: "<thinking_mode>enabled</thinking_mode><max_thinking_length>20000</max_thinking_length>\nSystem prompt",
			want:    true,
		},
		{
			name:    "empty content",
			content: "",
			want:    false,
		},
		{
			name:    "similar but not exact tag",
			content: "thinking_mode is not a tag",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasThinkingTags(tt.content)
			if got != tt.want {
				t.Errorf("hasThinkingTags(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}
