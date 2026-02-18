package auth

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"kiro2api/config"
	"kiro2api/types"
)

func TestDetectAccountLevelFromUsage(t *testing.T) {
	tests := []struct {
		name  string
		usage *types.UsageLimits
		want  AccountLevel
	}{
		{
			name: "free subscription",
			usage: &types.UsageLimits{
				SubscriptionInfo: types.SubscriptionInfo{
					Type:              "FREE",
					SubscriptionTitle: "Free Trial",
				},
			},
			want: AccountLevelFree,
		},
		{
			name: "pro subscription",
			usage: &types.UsageLimits{
				SubscriptionInfo: types.SubscriptionInfo{
					Type:              "PRO",
					SubscriptionTitle: "Kiro Pro",
				},
			},
			want: AccountLevelPro,
		},
		{
			name: "enterprise subscription",
			usage: &types.UsageLimits{
				SubscriptionInfo: types.SubscriptionInfo{
					Type:              "ENTERPRISE",
					SubscriptionTitle: "Enterprise Plan",
				},
			},
			want: AccountLevelEnterprise,
		},
		{
			name: "unknown subscription",
			usage: &types.UsageLimits{
				SubscriptionInfo: types.SubscriptionInfo{
					Type:              "CUSTOM",
					SubscriptionTitle: "Special",
				},
			},
			want: AccountLevelUnknown,
		},
		{
			name:  "nil usage",
			usage: nil,
			want:  AccountLevelUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectAccountLevelFromUsage(tt.usage)
			if got != tt.want {
				t.Fatalf("DetectAccountLevelFromUsage() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestIsModelAllowedForLevel(t *testing.T) {
	origEnabled := config.ModelAccessControlEnabled
	origUnknownAllowed := config.ModelAccessUnknownAllowed
	defer func() {
		config.ModelAccessControlEnabled = origEnabled
		config.ModelAccessUnknownAllowed = origUnknownAllowed
	}()

	config.ModelAccessControlEnabled = true
	config.ModelAccessUnknownAllowed = true

	if !IsModelAllowedForLevel(AccountLevelFree, config.CanonicalModelSonnet45) {
		t.Fatalf("free level should allow sonnet")
	}
	if IsModelAllowedForLevel(AccountLevelFree, config.CanonicalModelOpus46) {
		t.Fatalf("free level should not allow opus 4.6")
	}
	if !IsModelAllowedForLevel(AccountLevelUnknown, config.CanonicalModelOpus46) {
		t.Fatalf("unknown level should allow all models when ModelAccessUnknownAllowed=true")
	}

	config.ModelAccessUnknownAllowed = false
	if IsModelAllowedForLevel(AccountLevelUnknown, config.CanonicalModelOpus46) {
		t.Fatalf("unknown level should not allow opus 4.6 when ModelAccessUnknownAllowed=false")
	}
}

func TestTokenManager_GetBestTokenForModel_SelectsSupportedToken(t *testing.T) {
	origEnabled := config.ModelAccessControlEnabled
	defer func() {
		config.ModelAccessControlEnabled = origEnabled
	}()
	config.ModelAccessControlEnabled = true

	configs := []AuthConfig{
		{AuthType: AuthMethodSocial, RefreshToken: "token_free"},
		{AuthType: AuthMethodSocial, RefreshToken: "token_enterprise"},
	}
	tm := NewTokenManager(configs)

	now := time.Now()
	tm.mutex.Lock()
	tm.cache.tokens[fmt.Sprintf(config.TokenCacheKeyFormat, 0)] = &CachedToken{
		Token: types.TokenInfo{
			AccessToken: "access_free",
			ExpiresAt:   now.Add(1 * time.Hour),
		},
		CachedAt:     now,
		Available:    10,
		UsageInfo:    buildUsageForPlan("FREE"),
		AccountLevel: AccountLevelFree,
	}
	tm.cache.tokens[fmt.Sprintf(config.TokenCacheKeyFormat, 1)] = &CachedToken{
		Token: types.TokenInfo{
			AccessToken: "access_enterprise",
			ExpiresAt:   now.Add(1 * time.Hour),
		},
		CachedAt:     now,
		Available:    10,
		UsageInfo:    buildUsageForPlan("ENTERPRISE"),
		AccountLevel: AccountLevelEnterprise,
	}
	tm.lastRefresh = now
	tm.mutex.Unlock()

	token, err := tm.getBestTokenForModel("claude-opus-4-6")
	if err != nil {
		t.Fatalf("getBestTokenForModel() unexpected error: %v", err)
	}
	if token.AccessToken != "access_enterprise" {
		t.Fatalf("expected enterprise token, got %s", token.AccessToken)
	}
}

func TestTokenManager_GetBestTokenForModel_ModelNotFoundWhenNoAccountSupports(t *testing.T) {
	origEnabled := config.ModelAccessControlEnabled
	origUnknownAllowed := config.ModelAccessUnknownAllowed
	defer func() {
		config.ModelAccessControlEnabled = origEnabled
		config.ModelAccessUnknownAllowed = origUnknownAllowed
	}()
	config.ModelAccessControlEnabled = true
	config.ModelAccessUnknownAllowed = false

	configs := []AuthConfig{
		{AuthType: AuthMethodSocial, RefreshToken: "token_free"},
	}
	tm := NewTokenManager(configs)

	now := time.Now()
	tm.mutex.Lock()
	tm.cache.tokens[fmt.Sprintf(config.TokenCacheKeyFormat, 0)] = &CachedToken{
		Token: types.TokenInfo{
			AccessToken: "access_free",
			ExpiresAt:   now.Add(1 * time.Hour),
		},
		CachedAt:     now,
		Available:    10,
		UsageInfo:    buildUsageForPlan("FREE"),
		AccountLevel: AccountLevelFree,
	}
	tm.lastRefresh = now
	tm.mutex.Unlock()

	_, err := tm.getBestTokenForModel("claude-opus-4-6")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	var modelErr *types.ModelNotFoundErrorType
	if !errors.As(err, &modelErr) {
		t.Fatalf("expected ModelNotFoundErrorType, got %T", err)
	}
}

func buildUsageForPlan(plan string) *types.UsageLimits {
	return &types.UsageLimits{
		SubscriptionInfo: types.SubscriptionInfo{
			Type:              plan,
			SubscriptionTitle: plan,
		},
		UsageBreakdownList: []types.UsageBreakdown{
			{
				ResourceType:              "CREDIT",
				UsageLimitWithPrecision:   100,
				CurrentUsageWithPrecision: 0,
			},
		},
	}
}
