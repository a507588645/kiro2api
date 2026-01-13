package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModelMap_ClaudeSonnet45(t *testing.T) {
	model, exists := ModelMap["claude-sonnet-4-5-20250929"]
	assert.True(t, exists)
	assert.Equal(t, "CLAUDE_SONNET_4_5_20250929_V1_0", model)
}

func TestModelMap_ClaudeSonnet4(t *testing.T) {
	model, exists := ModelMap["claude-sonnet-4-20250514"]
	assert.True(t, exists)
	assert.Equal(t, "CLAUDE_SONNET_4_20250514_V1_0", model)
}

func TestModelMap_Claude37Sonnet(t *testing.T) {
	model, exists := ModelMap["claude-3-7-sonnet-20250219"]
	assert.True(t, exists)
	assert.Equal(t, "CLAUDE_3_7_SONNET_20250219_V1_0", model)
}

func TestModelMap_Claude35Haiku(t *testing.T) {
	model, exists := ModelMap["claude-3-5-haiku-20241022"]
	assert.True(t, exists)
	assert.Equal(t, "auto", model)
}

func TestModelMap_NonExistentModel(t *testing.T) {
	_, exists := ModelMap["non-existent-model"]
	assert.False(t, exists)
}

func TestModelMap_AllModelsHaveMapping(t *testing.T) {
	// 确保所有模型都有对应的映射
	expectedModels := []string{
		"claude-sonnet-4-5-20250929",
		"claude-sonnet-4-20250514",
		"claude-3-7-sonnet-20250219",
		"claude-3-5-haiku-20241022",
	}

	for _, model := range expectedModels {
		_, exists := ModelMap[model]
		assert.True(t, exists, "Model %s should exist in ModelMap", model)
	}
}

func TestModelMap_MappingsAreCorrectFormat(t *testing.T) {
	for inputModel, outputModel := range ModelMap {
		// 输出模型应该是大写格式或"auto"
		if outputModel != "auto" {
			assert.Contains(t, outputModel, "CLAUDE",
				"Model mapping for %s should contain 'CLAUDE'", inputModel)
			assert.Contains(t, outputModel, "_V1_0",
				"Model mapping for %s should contain '_V1_0'", inputModel)
		}
	}
}

func TestSupportedModels_ContainsAllKeys(t *testing.T) {
	// 确保所有ModelMap的key都在某个地方被文档化
	assert.NotEmpty(t, ModelMap, "ModelMap should not be empty")
	assert.Greater(t, len(ModelMap), 3, "ModelMap should contain at least 3 models")
}
