package config

import "testing"

func TestResolveModelID_DirectModel(t *testing.T) {
	resolvedModel, modelID, ok := ResolveModelID("claude-sonnet-4-5-20250929")
	if !ok {
		t.Fatalf("expected model to resolve")
	}
	if resolvedModel != "claude-sonnet-4-5-20250929" {
		t.Fatalf("unexpected resolved model: %s", resolvedModel)
	}
	if modelID != "claude-sonnet-4.5" {
		t.Fatalf("unexpected model id: %s", modelID)
	}
}

func TestResolveModelID_Sonnet46(t *testing.T) {
	tests := []struct {
		input         string
		wantResolved  string
		wantModelID   string
	}{
		{"claude-sonnet-4-6", CanonicalModelSonnet46, "claude-sonnet-4.6"},
		{"claude-sonnet-4.6", CanonicalModelSonnet46, "claude-sonnet-4.6"},
		{"claude-sonnet-4-6-thinking", CanonicalModelSonnet46, "claude-sonnet-4.6"},
	}
	for _, tt := range tests {
		resolved, modelID, ok := ResolveModelID(tt.input)
		if !ok {
			t.Fatalf("expected model %q to resolve", tt.input)
		}
		if resolved != tt.wantResolved {
			t.Fatalf("input %q: unexpected resolved model: %s, want %s", tt.input, resolved, tt.wantResolved)
		}
		if modelID != tt.wantModelID {
			t.Fatalf("input %q: unexpected model id: %s, want %s", tt.input, modelID, tt.wantModelID)
		}
	}
}

func TestResolveModelID_AliasAndThinkingSuffix(t *testing.T) {
	resolvedModel, modelID, ok := ResolveModelID("claude-opus-4.6-thinking")
	if !ok {
		t.Fatalf("expected alias model to resolve")
	}
	if resolvedModel != CanonicalModelOpus46 {
		t.Fatalf("unexpected resolved model: %s", resolvedModel)
	}
	if modelID != "claude-opus-4.6" {
		t.Fatalf("unexpected model id: %s", modelID)
	}
}

func TestResolveModelID_FamilyFallback(t *testing.T) {
	resolvedModel, _, ok := ResolveModelID("my-custom-sonnet-model")
	if !ok {
		t.Fatalf("expected sonnet-family model to resolve")
	}
	if resolvedModel != CanonicalModelSonnet45 {
		t.Fatalf("unexpected resolved model: %s", resolvedModel)
	}
}

func TestResolveModelID_UnknownModel(t *testing.T) {
	_, _, ok := ResolveModelID("definitely-unknown-model")
	if ok {
		t.Fatalf("expected unknown model to be rejected")
	}
}

func TestListRequestModels_ContainsExpectedEntries(t *testing.T) {
	models := ListRequestModels()
	if len(models) == 0 {
		t.Fatalf("expected non-empty model list")
	}
	if len(models) != 5 {
		t.Fatalf("expected 5 base models, got %d", len(models))
	}

	required := map[string]bool{
		"claude-sonnet-4-5-20250929": false,
		"claude-sonnet-4-6":          false,
		"claude-opus-4-5-20251101":   false,
		"claude-opus-4-6":            false,
		"claude-haiku-4-5-20251001":  false,
	}

	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		if _, exists := seen[model]; exists {
			t.Fatalf("duplicate model in list: %s", model)
		}
		seen[model] = struct{}{}
		if _, exists := required[model]; exists {
			required[model] = true
		}
	}

	for model, ok := range required {
		if !ok {
			t.Fatalf("expected model %s in list", model)
		}
	}
}
