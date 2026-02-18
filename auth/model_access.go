package auth

import (
	"strings"

	"kiro2api/config"
	"kiro2api/types"
)

// AccountLevel 账号等级
type AccountLevel string

const (
	AccountLevelUnknown    AccountLevel = "unknown"
	AccountLevelFree       AccountLevel = "free"
	AccountLevelPro        AccountLevel = "pro"
	AccountLevelEnterprise AccountLevel = "enterprise"
)

var modelAccessByLevel = map[AccountLevel][]string{
	AccountLevelFree: {
		config.CanonicalModelSonnet45,
		config.CanonicalModelSonnet46,
		config.CanonicalModelHaiku45,
	},
	AccountLevelPro: {
		config.CanonicalModelSonnet45,
		config.CanonicalModelSonnet46,
		config.CanonicalModelHaiku45,
		config.CanonicalModelOpus45,
	},
	AccountLevelEnterprise: {
		config.CanonicalModelSonnet45,
		config.CanonicalModelSonnet46,
		config.CanonicalModelHaiku45,
		config.CanonicalModelOpus45,
		config.CanonicalModelOpus46,
	},
}

// DetectAccountLevelFromUsage 从 usage 信息中识别账号等级
func DetectAccountLevelFromUsage(usage *types.UsageLimits) AccountLevel {
	if usage == nil {
		return AccountLevelUnknown
	}

	candidates := []string{
		usage.SubscriptionInfo.Type,
		usage.SubscriptionInfo.SubscriptionTitle,
		usage.SubscriptionInfo.OverageCapability,
		usage.SubscriptionInfo.UpgradeCapability,
		usage.SubscriptionInfo.SubscriptionManagementTarget,
	}
	raw := strings.ToLower(strings.Join(candidates, " "))

	switch {
	case strings.Contains(raw, "enterprise"),
		strings.Contains(raw, "business"):
		return AccountLevelEnterprise
	case strings.Contains(raw, "team"),
		strings.Contains(raw, "pro"),
		strings.Contains(raw, "paid"):
		return AccountLevelPro
	case strings.Contains(raw, "free"),
		strings.Contains(raw, "trial"),
		strings.Contains(raw, "basic"):
		return AccountLevelFree
	default:
		return AccountLevelUnknown
	}
}

// AllowedModelsForLevel 返回该等级可用的模型列表（去重、有序）
func AllowedModelsForLevel(level AccountLevel) []string {
	if level == AccountLevelUnknown {
		return config.ListRequestModels()
	}

	raw, ok := modelAccessByLevel[level]
	if !ok {
		return config.ListRequestModels()
	}

	seen := make(map[string]struct{}, len(raw))
	models := make([]string, 0, len(raw))
	for _, model := range raw {
		model = strings.TrimSpace(strings.ToLower(model))
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		models = append(models, model)
	}
	return models
}

// AllowedModelsForUsage 返回 usage 对应账号的可用模型
func AllowedModelsForUsage(usage *types.UsageLimits) []string {
	level := DetectAccountLevelFromUsage(usage)
	return AllowedModelsForLevel(level)
}

// IsModelAllowedForLevel 判断账号等级是否允许请求该模型
func IsModelAllowedForLevel(level AccountLevel, requestedModel string) bool {
	if !config.ModelAccessControlEnabled {
		return true
	}

	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return true
	}

	resolvedModel, _, ok := config.ResolveModelID(requestedModel)
	if !ok {
		// 未识别模型交给后续标准校验逻辑处理
		return true
	}

	effectiveLevel := level
	if level == AccountLevelUnknown {
		if config.ModelAccessUnknownAllowed {
			return true
		}
		// 严格模式下，未知等级按最低权限处理
		effectiveLevel = AccountLevelFree
	}

	allowedModels := AllowedModelsForLevel(effectiveLevel)
	for _, allowed := range allowedModels {
		if allowed == resolvedModel {
			return true
		}
	}

	return false
}

// IsModelAllowedForUsage 判断 usage 对应账号是否允许请求该模型
func IsModelAllowedForUsage(usage *types.UsageLimits, requestedModel string) bool {
	level := DetectAccountLevelFromUsage(usage)
	return IsModelAllowedForLevel(level, requestedModel)
}
