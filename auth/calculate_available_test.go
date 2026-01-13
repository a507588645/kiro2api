package auth

import (
	"kiro2api/types"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCalculateAvailableCount_WithFreeTrialActive(t *testing.T) {
	usage := &types.UsageLimits{
		UsageBreakdownList: []types.UsageBreakdown{
			{
				ResourceType:              "CREDIT",
				UsageLimitWithPrecision:   1000.0,
				CurrentUsageWithPrecision: 200.0,
				FreeTrialInfo: &types.FreeTrialInfo{
					FreeTrialStatus:           "ACTIVE",
					UsageLimitWithPrecision:   500.0,
					CurrentUsageWithPrecision: 100.0,
				},
			},
		},
	}

	available := CalculateAvailableCount(usage)

	// 免费试用可用: 500 - 100 = 400
	// 基础额度可用: 1000 - 200 = 800
	// 总计: 400 + 800 = 1200
	assert.Equal(t, 1200.0, available)
}

func TestCalculateAvailableCount_WithFreeTrialInactive(t *testing.T) {
	usage := &types.UsageLimits{
		UsageBreakdownList: []types.UsageBreakdown{
			{
				ResourceType:              "CREDIT",
				UsageLimitWithPrecision:   1000.0,
				CurrentUsageWithPrecision: 200.0,
				FreeTrialInfo: &types.FreeTrialInfo{
					FreeTrialStatus:           "EXPIRED",
					UsageLimitWithPrecision:   500.0,
					CurrentUsageWithPrecision: 500.0,
				},
			},
		},
	}

	available := CalculateAvailableCount(usage)

	// 免费试用已过期，不计入
	// 基础额度可用: 1000 - 200 = 800
	assert.Equal(t, 800.0, available)
}

func TestCalculateAvailableCount_NoFreeTrialInfo(t *testing.T) {
	usage := &types.UsageLimits{
		UsageBreakdownList: []types.UsageBreakdown{
			{
				ResourceType:              "CREDIT",
				UsageLimitWithPrecision:   1000.0,
				CurrentUsageWithPrecision: 300.0,
				FreeTrialInfo:             nil,
			},
		},
	}

	available := CalculateAvailableCount(usage)

	// 只有基础额度: 1000 - 300 = 700
	assert.Equal(t, 700.0, available)
}

func TestCalculateAvailableCount_NegativeAvailable(t *testing.T) {
	usage := &types.UsageLimits{
		UsageBreakdownList: []types.UsageBreakdown{
			{
				ResourceType:              "CREDIT",
				UsageLimitWithPrecision:   1000.0,
				CurrentUsageWithPrecision: 1200.0, // 超额使用
				FreeTrialInfo:             nil,
			},
		},
	}

	available := CalculateAvailableCount(usage)

	// 负数应该返回0
	assert.Equal(t, 0.0, available)
}

func TestCalculateAvailableCount_NoCredit(t *testing.T) {
	usage := &types.UsageLimits{
		UsageBreakdownList: []types.UsageBreakdown{
			{
				ResourceType:              "OTHER",
				UsageLimitWithPrecision:   1000.0,
				CurrentUsageWithPrecision: 200.0,
			},
		},
	}

	available := CalculateAvailableCount(usage)

	// 没有CREDIT类型的资源，返回0
	assert.Equal(t, 0.0, available)
}

func TestCalculateAvailableCount_EmptyBreakdownList(t *testing.T) {
	usage := &types.UsageLimits{
		UsageBreakdownList: []types.UsageBreakdown{},
	}

	available := CalculateAvailableCount(usage)

	// 空列表返回0
	assert.Equal(t, 0.0, available)
}

func TestCalculateAvailableCount_MultipleBreakdowns(t *testing.T) {
	usage := &types.UsageLimits{
		UsageBreakdownList: []types.UsageBreakdown{
			{
				ResourceType:              "OTHER",
				UsageLimitWithPrecision:   500.0,
				CurrentUsageWithPrecision: 100.0,
			},
			{
				ResourceType:              "CREDIT",
				UsageLimitWithPrecision:   1000.0,
				CurrentUsageWithPrecision: 250.0,
				FreeTrialInfo: &types.FreeTrialInfo{
					FreeTrialStatus:           "ACTIVE",
					UsageLimitWithPrecision:   300.0,
					CurrentUsageWithPrecision: 50.0,
				},
			},
			{
				ResourceType:              "ANOTHER",
				UsageLimitWithPrecision:   2000.0,
				CurrentUsageWithPrecision: 500.0,
			},
		},
	}

	available := CalculateAvailableCount(usage)

	// 只计算第一个CREDIT类型
	// 免费试用: 300 - 50 = 250
	// 基础额度: 1000 - 250 = 750
	// 总计: 250 + 750 = 1000
	assert.Equal(t, 1000.0, available)
}

func TestCalculateAvailableCount_ZeroUsage(t *testing.T) {
	usage := &types.UsageLimits{
		UsageBreakdownList: []types.UsageBreakdown{
			{
				ResourceType:              "CREDIT",
				UsageLimitWithPrecision:   1000.0,
				CurrentUsageWithPrecision: 0.0,
				FreeTrialInfo: &types.FreeTrialInfo{
					FreeTrialStatus:           "ACTIVE",
					UsageLimitWithPrecision:   500.0,
					CurrentUsageWithPrecision: 0.0,
				},
			},
		},
	}

	available := CalculateAvailableCount(usage)

	// 免费试用: 500 - 0 = 500
	// 基础额度: 1000 - 0 = 1000
	// 总计: 1500
	assert.Equal(t, 1500.0, available)
}

func TestCalculateAvailableCount_FreeTrialExhausted(t *testing.T) {
	usage := &types.UsageLimits{
		UsageBreakdownList: []types.UsageBreakdown{
			{
				ResourceType:              "CREDIT",
				UsageLimitWithPrecision:   1000.0,
				CurrentUsageWithPrecision: 100.0,
				FreeTrialInfo: &types.FreeTrialInfo{
					FreeTrialStatus:           "ACTIVE",
					UsageLimitWithPrecision:   500.0,
					CurrentUsageWithPrecision: 500.0, // 已用完
				},
			},
		},
	}

	available := CalculateAvailableCount(usage)

	// 免费试用: 500 - 500 = 0
	// 基础额度: 1000 - 100 = 900
	// 总计: 900
	assert.Equal(t, 900.0, available)
}
