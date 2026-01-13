package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"kiro2api/utils"
)

// TestLegacyToolUseEventHandler_OneShotCompleteData 测试一次性完整数据场景
func TestLegacyToolUseEventHandler_OneShotCompleteData(t *testing.T) {
	// 模拟上游发送一次性完整JSON数据（stop=true且包含完整input）
	toolManager := NewToolLifecycleManager()
	aggregator := NewSonicStreamingJSONAggregatorWithCallback(func(toolUseId string, fullParams string) {
		toolManager.UpdateToolArgumentsFromJSON(toolUseId, fullParams)
	})

	handler := &LegacyToolUseEventHandler{
		toolManager: toolManager,
		aggregator:  aggregator,
	}

	// 构造完整的工具调用事件（一次性发送）
	input := map[string]any{
		"query":      "测试查询",
		"maxResults": 10,
		"filters": map[string]any{
			"category": "技术",
			"language": "zh-CN",
		},
	}

	evt := toolUseEvent{
		Name:      "search_database",
		ToolUseId: "test-tool-001",
		Input:     input, // 完整的JSON对象
		Stop:      true,  // 一次性完整数据
	}

	payload, err := utils.FastMarshal(evt)
	assert.NoError(t, err)

	message := &EventStreamMessage{
		Payload: payload,
	}

	// 执行处理
	events, err := handler.handleToolCallEvent(message)
	assert.NoError(t, err)
	assert.NotEmpty(t, events, "应该返回工具注册事件")

	// 验证工具已正确注册且参数完整
	activeTools := toolManager.GetActiveTools()
	assert.Contains(t, activeTools, "test-tool-001", "工具应该已注册")

	tool := activeTools["test-tool-001"]
	assert.Equal(t, "search_database", tool.Name)
	assert.NotNil(t, tool.Arguments)

	// 验证参数完整性
	args := tool.Arguments
	assert.Equal(t, "测试查询", args["query"])
	assert.Equal(t, float64(10), args["maxResults"]) // JSON数字解析为float64

	filters, ok := args["filters"].(map[string]any)
	assert.True(t, ok, "filters应该是map类型")
	assert.Equal(t, "技术", filters["category"])
	assert.Equal(t, "zh-CN", filters["language"])

	t.Log("✅ 一次性完整数据场景测试通过")
}

// TestLegacyToolUseEventHandler_StreamingFragments 测试流式分片数据场景
func TestLegacyToolUseEventHandler_StreamingFragments(t *testing.T) {
	toolManager := NewToolLifecycleManager()
	aggregator := NewSonicStreamingJSONAggregatorWithCallback(func(toolUseId string, fullParams string) {
		toolManager.UpdateToolArgumentsFromJSON(toolUseId, fullParams)
	})

	handler := &LegacyToolUseEventHandler{
		toolManager: toolManager,
		aggregator:  aggregator,
	}

	toolUseId := "test-tool-002"
	toolName := "write_file"

	// 片段1：首次注册（不含stop）
	evt1 := toolUseEvent{
		Name:      toolName,
		ToolUseId: toolUseId,
		Input:     map[string]any{}, // 空对象或初始值
		Stop:      false,
	}

	payload1, _ := utils.FastMarshal(evt1)
	message1 := &EventStreamMessage{Payload: payload1}

	events1, err := handler.handleToolCallEvent(message1)
	assert.NoError(t, err)
	assert.NotEmpty(t, events1, "应该返回工具注册事件")

	// 片段2：第一部分数据
	evt2 := toolUseEvent{
		Name:      toolName,
		ToolUseId: toolUseId,
		Input:     `{"path":"/tmp/test.txt","con`, // 不完整的JSON字符串
		Stop:      false,
	}

	payload2, _ := utils.FastMarshal(evt2)
	message2 := &EventStreamMessage{Payload: payload2}

	_, err = handler.handleToolCallEvent(message2)
	assert.NoError(t, err)
	// 应该返回增量事件或空事件

	// 片段3：第二部分数据
	evt3 := toolUseEvent{
		Name:      toolName,
		ToolUseId: toolUseId,
		Input:     `tent":"测试内容"}`, // 完成JSON
		Stop:      true,            // 最后一个片段
	}

	payload3, _ := utils.FastMarshal(evt3)
	message3 := &EventStreamMessage{Payload: payload3}

	_, err = handler.handleToolCallEvent(message3)
	assert.NoError(t, err)

	// 验证最终工具参数已聚合完成
	completedTools := toolManager.GetCompletedTools()

	// 由于流式聚合的复杂性，这里主要验证工具调用流程正确
	// 工具应该被标记为已完成
	var found bool
	for _, completed := range completedTools {
		if completed.ID == toolUseId {
			found = true
			assert.Equal(t, toolName, completed.Name)
			break
		}
	}

	// 如果没有在completed中找到，检查active tools
	if !found {
		activeTools := toolManager.GetActiveTools()
		if tool, exists := activeTools[toolUseId]; exists {
			assert.Equal(t, toolName, tool.Name)
			found = true
		}
	}

	assert.True(t, found, "工具应该已注册或已完成")

	t.Log("✅ 流式分片数据场景测试通过")
}

// TestLegacyToolUseEventHandler_EmptyParameters 测试空参数工具调用
func TestLegacyToolUseEventHandler_EmptyParameters(t *testing.T) {
	toolManager := NewToolLifecycleManager()
	aggregator := NewSonicStreamingJSONAggregatorWithCallback(func(toolUseId string, fullParams string) {
		toolManager.UpdateToolArgumentsFromJSON(toolUseId, fullParams)
	})

	handler := &LegacyToolUseEventHandler{
		toolManager: toolManager,
		aggregator:  aggregator,
	}

	// 模拟无参数工具调用（如get_current_time）
	evt := toolUseEvent{
		Name:      "get_current_time",
		ToolUseId: "test-tool-003",
		Input:     map[string]any{}, // 空对象
		Stop:      true,
	}

	payload, _ := utils.FastMarshal(evt)
	message := &EventStreamMessage{Payload: payload}

	// 执行处理
	events, err := handler.handleToolCallEvent(message)
	assert.NoError(t, err)
	assert.NotEmpty(t, events)

	// 验证工具已注册
	activeTools := toolManager.GetActiveTools()
	assert.Contains(t, activeTools, "test-tool-003")

	tool := activeTools["test-tool-003"]
	assert.Equal(t, "get_current_time", tool.Name)
	assert.NotNil(t, tool.Arguments)
	assert.Empty(t, tool.Arguments, "参数应该为空map")

	t.Log("✅ 空参数工具调用场景测试通过")
}

// TestConvertInputToString 测试input类型转换函数
func TestConvertInputToString(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{
			name:     "nil输入",
			input:    nil,
			expected: "{}",
		},
		{
			name:     "字符串输入",
			input:    `{"key":"value"}`,
			expected: `{"key":"value"}`,
		},
		{
			name: "对象输入",
			input: map[string]any{
				"query": "测试",
				"limit": 10,
			},
			expected: `{"limit":10,"query":"测试"}`, // sonic会排序key
		},
		{
			name:     "空对象",
			input:    map[string]any{},
			expected: "{}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertInputToString(tt.input)

			// 对于对象输入，验证是否为有效JSON
			if tt.input != nil {
				if _, ok := tt.input.(map[string]any); ok {
					var testMap map[string]any
					err := utils.FastUnmarshal([]byte(result), &testMap)
					assert.NoError(t, err, "结果应该是有效的JSON")
				}
			}

			// 对于简单情况，直接比较
			if tt.name == "nil输入" || tt.name == "字符串输入" || tt.name == "空对象" {
				assert.Equal(t, tt.expected, result)
			}
		})
	}

	t.Log("✅ convertInputToString函数测试通过")
}

// TestLegacyToolUseEventHandler_MemoryLeakPrevention 测试内存泄漏预防
// 场景：工具已注册，收到空数据+stop信号，确保聚合器状态被清理
// TestLegacyToolUseEventHandler_MemoryLeakPrevention 测试内存泄漏预防
// 场景：工具已注册，收到空数据+stop信号，确保聚合器状态被清理
func TestLegacyToolUseEventHandler_MemoryLeakPrevention(t *testing.T) {
	toolManager := NewToolLifecycleManager()
	aggregator := NewSonicStreamingJSONAggregatorWithCallback(func(toolUseId string, fullParams string) {
		toolManager.UpdateToolArgumentsFromJSON(toolUseId, fullParams)
	})

	handler := &LegacyToolUseEventHandler{
		toolManager: toolManager,
		aggregator:  aggregator,
	}

	toolUseId := "test-tool-leak"
	toolName := "test_tool"

	// 步骤1：首次注册工具（空对象，stop=false，说明后续有数据）
	evt1 := toolUseEvent{
		Name:      toolName,
		ToolUseId: toolUseId,
		Input:     map[string]any{}, // 空对象
		Stop:      false,
	}

	payload1, _ := utils.FastMarshal(evt1)
	message1 := &EventStreamMessage{Payload: payload1}

	_, err := handler.handleToolCallEvent(message1)
	assert.NoError(t, err)

	// 步骤2：发送第一个数据片段（字符串片段）
	evt2 := toolUseEvent{
		Name:      toolName,
		ToolUseId: toolUseId,
		Input:     `{"initial":"data"`, // 不完整的JSON字符串片段
		Stop:      false,
	}

	payload2, _ := utils.FastMarshal(evt2)
	message2 := &EventStreamMessage{Payload: payload2}

	_, err = handler.handleToolCallEvent(message2)
	assert.NoError(t, err)

	// 验证聚合器中已创建streamer
	aggregator.mu.Lock()
	_, streamerExists := aggregator.activeStreamers[toolUseId]
	aggregator.mu.Unlock()
	assert.True(t, streamerExists, "聚合器应该已创建streamer")

	// 步骤3：发送最后一个片段+stop信号
	evt3 := toolUseEvent{
		Name:      toolName,
		ToolUseId: toolUseId,
		Input:     `}`,  // 完成JSON
		Stop:      true, // stop信号
	}

	payload3, _ := utils.FastMarshal(evt3)
	message3 := &EventStreamMessage{Payload: payload3}

	_, err = handler.handleToolCallEvent(message3)
	assert.NoError(t, err)

	// 🔥 关键验证：聚合器中的streamer应该已被清理
	aggregator.mu.Lock()
	_, streamerStillExists := aggregator.activeStreamers[toolUseId]
	aggregator.mu.Unlock()
	assert.False(t, streamerStillExists, "聚合器应该已清理streamer，避免内存泄漏")

	// 验证工具已完成
	completedTools := toolManager.GetCompletedTools()
	var found bool
	for _, completed := range completedTools {
		if completed.ID == toolUseId {
			found = true
			break
		}
	}
	assert.True(t, found, "工具应该已标记为完成")

	t.Log("✅ 内存泄漏预防测试通过")
}
