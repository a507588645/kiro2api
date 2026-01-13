package parser

import (
	"testing"
)

// TestEmptyObjectParsing 测试空对象解析（无参数工具）
func TestEmptyObjectParsing(t *testing.T) {
	// 创建聚合器
	aggregator := NewSonicStreamingJSONAggregatorWithCallback(nil)

	// 测试空对象 "{}"
	complete, result := aggregator.ProcessToolData(
		"test-001",
		"browser_press_key",
		"{}",
		true,
		-1,
	)

	if !complete {
		t.Errorf("Expected complete=true for empty object, got false")
	}

	if result != "{}" {
		t.Errorf("Expected result='{}', got '%s'", result)
	}
}

// TestEmptyObjectWithWhitespace 测试带空格的空对象
func TestEmptyObjectWithWhitespace(t *testing.T) {
	aggregator := NewSonicStreamingJSONAggregatorWithCallback(nil)

	// 测试带空格的空对象 " {} "
	complete, result := aggregator.ProcessToolData(
		"test-002",
		"browser_snapshot",
		" {} ",
		true,
		-1,
	)

	if !complete {
		t.Errorf("Expected complete=true for empty object with whitespace, got false")
	}

	if result != "{}" {
		t.Errorf("Expected result='{}', got '%s'", result)
	}
}

// TestNormalJSONParsing 测试正常JSON解析（有参数工具）
func TestNormalJSONParsing(t *testing.T) {
	aggregator := NewSonicStreamingJSONAggregatorWithCallback(nil)

	// 测试正常JSON对象
	jsonInput := `{"key":"value","number":123}`
	complete, result := aggregator.ProcessToolData(
		"test-003",
		"Read",
		jsonInput,
		true,
		-1,
	)

	if !complete {
		t.Errorf("Expected complete=true for valid JSON, got false")
	}

	if result == "" {
		t.Errorf("Expected non-empty result, got empty string")
	}
}

// TestStreamingFragments 测试流式片段聚合
func TestStreamingFragments(t *testing.T) {
	aggregator := NewSonicStreamingJSONAggregatorWithCallback(nil)

	toolID := "test-004"
	toolName := "Glob"

	// 第一个片段（未完成）
	complete, _ := aggregator.ProcessToolData(toolID, toolName, `{"pattern"`, false, -1)
	if complete {
		t.Errorf("Expected complete=false for first fragment, got true")
	}

	// 第二个片段（未完成）
	complete, _ = aggregator.ProcessToolData(toolID, toolName, `:"**/*.go"`, false, -1)
	if complete {
		t.Errorf("Expected complete=false for second fragment, got true")
	}

	// 最后一个片段（完成）
	complete, result := aggregator.ProcessToolData(toolID, toolName, `}`, true, -1)
	if !complete {
		t.Errorf("Expected complete=true for final fragment, got false")
	}

	if result == "" {
		t.Errorf("Expected non-empty result, got empty string")
	}
}
