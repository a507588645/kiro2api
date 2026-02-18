package config

import "strings"

const (
	CanonicalModelOpus45   = "claude-opus-4-5-20251101"
	CanonicalModelOpus46   = "claude-opus-4-6"
	CanonicalModelSonnet45 = "claude-sonnet-4-5-20250929"
	CanonicalModelHaiku45  = "claude-haiku-4-5-20251001"
)

// 与同上游项目保持一致：对外展示这一组模型。
var publicRequestModels = []string{
	"claude-sonnet-4-5-20250929",
	"claude-opus-4-5-20251101",
	"claude-opus-4-6",
	"claude-haiku-4-5-20251001",
}

func NormalizeModelName(model string) string {
	model = strings.TrimSpace(strings.ToLower(model))
	return strings.TrimSuffix(model, "-thinking")
}

// ResolveModelID 将外部模型名归一化并映射到上游 modelId。
// 与 kiro.rs 对齐：
// - sonnet* -> claude-sonnet-4.5
// - opus* 且包含 4.5/4-5 -> claude-opus-4.5
// - opus* 其他 -> claude-opus-4.6
// - haiku* -> claude-haiku-4.5
func ResolveModelID(model string) (resolvedModel string, modelID string, ok bool) {
	normalized := NormalizeModelName(model)
	if normalized == "" {
		return "", "", false
	}

	switch {
	case strings.Contains(normalized, "sonnet"):
		return CanonicalModelSonnet45, "claude-sonnet-4.5", true
	case strings.Contains(normalized, "opus"):
		if strings.Contains(normalized, "4-5") || strings.Contains(normalized, "4.5") {
			return CanonicalModelOpus45, "claude-opus-4.5", true
		}
		return CanonicalModelOpus46, "claude-opus-4.6", true
	case strings.Contains(normalized, "haiku"):
		return CanonicalModelHaiku45, "claude-haiku-4.5", true
	}

	return "", "", false
}

// ListRequestModels 返回对外展示的可请求模型列表（去重后有序）。
func ListRequestModels() []string {
	seen := make(map[string]struct{}, len(publicRequestModels))
	models := make([]string, 0, len(publicRequestModels))

	addModel := func(name string) {
		name = strings.TrimSpace(strings.ToLower(name))
		if name == "" {
			return
		}
		if _, exists := seen[name]; exists {
			return
		}
		seen[name] = struct{}{}
		models = append(models, name)
	}

	for _, model := range publicRequestModels {
		addModel(model)
	}

	return models
}
