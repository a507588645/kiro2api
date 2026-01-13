package utils

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsDebugMode_Debug(t *testing.T) {
	os.Setenv("DEBUG", "true")
	defer os.Unsetenv("DEBUG")

	assert.True(t, IsDebugMode())
}

func TestIsDebugMode_Debug1(t *testing.T) {
	os.Setenv("DEBUG", "1")
	defer os.Unsetenv("DEBUG")

	assert.True(t, IsDebugMode())
}

func TestIsDebugMode_LogLevel(t *testing.T) {
	os.Setenv("LOG_LEVEL", "debug")
	defer os.Unsetenv("LOG_LEVEL")

	assert.True(t, IsDebugMode())
}

func TestIsDebugMode_LogLevelUpperCase(t *testing.T) {
	os.Setenv("LOG_LEVEL", "DEBUG")
	defer os.Unsetenv("LOG_LEVEL")

	assert.True(t, IsDebugMode())
}

func TestIsDebugMode_GinMode(t *testing.T) {
	os.Setenv("GIN_MODE", "debug")
	defer os.Unsetenv("GIN_MODE")

	assert.True(t, IsDebugMode())
}

func TestIsDebugMode_False(t *testing.T) {
	os.Unsetenv("DEBUG")
	os.Unsetenv("LOG_LEVEL")
	os.Unsetenv("GIN_MODE")

	assert.False(t, IsDebugMode())
}

func TestGetEnvWithDefault_Exists(t *testing.T) {
	os.Setenv("TEST_KEY", "test_value")
	defer os.Unsetenv("TEST_KEY")

	result := GetEnvWithDefault("TEST_KEY", "default")
	assert.Equal(t, "test_value", result)
}

func TestGetEnvWithDefault_NotExists(t *testing.T) {
	os.Unsetenv("TEST_KEY")

	result := GetEnvWithDefault("TEST_KEY", "default")
	assert.Equal(t, "default", result)
}

func TestGetEnvWithDefault_Empty(t *testing.T) {
	os.Setenv("TEST_KEY", "")
	defer os.Unsetenv("TEST_KEY")

	result := GetEnvWithDefault("TEST_KEY", "default")
	assert.Equal(t, "default", result)
}

func TestGetEnvBool_True(t *testing.T) {
	testCases := []string{"true", "1", "yes", "on", "TRUE", "YES", "ON"}

	for _, value := range testCases {
		os.Setenv("TEST_BOOL", value)
		assert.True(t, GetEnvBool("TEST_BOOL"), "Value: %s", value)
		os.Unsetenv("TEST_BOOL")
	}
}

func TestGetEnvBool_False(t *testing.T) {
	testCases := []string{"false", "0", "no", "off", "invalid", ""}

	for _, value := range testCases {
		os.Setenv("TEST_BOOL", value)
		assert.False(t, GetEnvBool("TEST_BOOL"), "Value: %s", value)
		os.Unsetenv("TEST_BOOL")
	}
}

func TestGetEnvBool_WithWhitespace(t *testing.T) {
	os.Setenv("TEST_BOOL", "  true  ")
	defer os.Unsetenv("TEST_BOOL")

	assert.True(t, GetEnvBool("TEST_BOOL"))
}

func TestGetEnvBoolWithDefault_Exists(t *testing.T) {
	os.Setenv("TEST_BOOL", "true")
	defer os.Unsetenv("TEST_BOOL")

	result := GetEnvBoolWithDefault("TEST_BOOL", false)
	assert.True(t, result)
}

func TestGetEnvBoolWithDefault_NotExists(t *testing.T) {
	os.Unsetenv("TEST_BOOL")

	result := GetEnvBoolWithDefault("TEST_BOOL", true)
	assert.True(t, result)

	result2 := GetEnvBoolWithDefault("TEST_BOOL", false)
	assert.False(t, result2)
}

func TestGetEnvBoolWithDefault_Empty(t *testing.T) {
	os.Setenv("TEST_BOOL", "")
	defer os.Unsetenv("TEST_BOOL")

	result := GetEnvBoolWithDefault("TEST_BOOL", true)
	assert.True(t, result)
}

func TestMultipleEnvChecks(t *testing.T) {
	// 测试多个环境变量同时存在的情况
	os.Setenv("DEBUG", "true")
	os.Setenv("LOG_LEVEL", "info")
	os.Setenv("GIN_MODE", "release")
	defer func() {
		os.Unsetenv("DEBUG")
		os.Unsetenv("LOG_LEVEL")
		os.Unsetenv("GIN_MODE")
	}()

	assert.True(t, IsDebugMode()) // DEBUG=true 优先级最高
}
