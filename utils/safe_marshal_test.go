package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSafeMarshal_BasicTypes(t *testing.T) {
	tests := []struct {
		name  string
		input any
	}{
		{"string", "hello"},
		{"int", 123},
		{"float", 45.67},
		{"bool", true},
		{"nil", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := SafeMarshal(tt.input)
			assert.NoError(t, err)
			assert.NotNil(t, result)
		})
	}
}

func TestSafeMarshal_ComplexTypes(t *testing.T) {
	testMap := map[string]any{
		"key1": "value1",
		"key2": 123,
		"key3": []string{"a", "b", "c"},
	}

	result, err := SafeMarshal(testMap)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Contains(t, string(result), "key1")
	assert.Contains(t, string(result), "value1")
}

func TestSafeMarshal_Array(t *testing.T) {
	testArray := []int{1, 2, 3, 4, 5}

	result, err := SafeMarshal(testArray)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Contains(t, string(result), "1")
	assert.Contains(t, string(result), "5")
}
