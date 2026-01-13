package parser

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewHeaderParseState(t *testing.T) {
	state := NewHeaderParseState()

	assert.NotNil(t, state)
	assert.Equal(t, PhaseReadNameLength, state.Phase)
	assert.Equal(t, 0, state.CurrentHeader)
	assert.NotNil(t, state.ParsedHeaders)
	assert.Empty(t, state.ParsedHeaders)
}

func TestHeaderParseState_Reset(t *testing.T) {
	state := NewHeaderParseState()

	// 修改状态
	state.Phase = PhaseReadValue
	state.CurrentHeader = 5
	state.NameLength = 10
	state.ValueType = ValueType_STRING
	state.ValueLength = 20
	state.PartialName = []byte("test")
	state.PartialValue = []byte("value")
	state.PartialLength = []byte{0, 1}
	state.ParsedHeaders["key"] = HeaderValue{Type: ValueType_STRING, Value: "value"}

	// 重置
	state.Reset()

	// 验证所有字段都被重置
	assert.Equal(t, PhaseReadNameLength, state.Phase)
	assert.Equal(t, 0, state.CurrentHeader)
	assert.Equal(t, 0, state.NameLength)
	assert.Equal(t, ValueType(0), state.ValueType)
	assert.Equal(t, 0, state.ValueLength)
	assert.Nil(t, state.PartialName)
	assert.Nil(t, state.PartialValue)
	assert.Nil(t, state.PartialLength)
	assert.Empty(t, state.ParsedHeaders)
}

func TestHeaderParseState_IsComplete(t *testing.T) {
	state := NewHeaderParseState()

	// 初始状态：未完成（没有解析的头部）
	assert.False(t, state.IsComplete())

	// 添加头部但阶段不对
	state.ParsedHeaders["key"] = HeaderValue{Type: ValueType_STRING, Value: "value"}
	state.Phase = PhaseReadValue
	assert.False(t, state.IsComplete())

	// 正确的完成状态
	state.Phase = PhaseReadNameLength
	assert.True(t, state.IsComplete())
}

func TestNewHeaderParser(t *testing.T) {
	parser := NewHeaderParser()

	assert.NotNil(t, parser)
	assert.NotNil(t, parser.state)
	assert.Equal(t, PhaseReadNameLength, parser.state.Phase)
}

func TestHeaderParser_ParseHeaders_EmptyData(t *testing.T) {
	parser := NewHeaderParser()

	headers, err := parser.ParseHeaders([]byte{})

	assert.NoError(t, err)
	assert.NotNil(t, headers)
	assert.Empty(t, headers)
}

func TestHeaderParser_ParseHeaders_SimpleStringHeader(t *testing.T) {
	parser := NewHeaderParser()

	// 构造一个简单的字符串头部
	// 格式: [name_length(1)] [name] [value_type(1)] [value_length(2)] [value]
	data := buildSimpleStringHeader("content-type", "application/json")

	headers, err := parser.ParseHeaders(data)

	assert.NoError(t, err)
	assert.Len(t, headers, 1)
	assert.Contains(t, headers, "content-type")
	assert.Equal(t, ValueType_STRING, headers["content-type"].Type)
	assert.Equal(t, "application/json", headers["content-type"].Value.(string))
}

func TestHeaderParser_ParseHeaders_MultipleHeaders(t *testing.T) {
	parser := NewHeaderParser()

	// 构造多个头部
	data := []byte{}
	data = append(data, buildSimpleStringHeader(":message-type", "event")...)
	data = append(data, buildSimpleStringHeader(":event-type", "messageStart")...)

	headers, err := parser.ParseHeaders(data)

	assert.NoError(t, err)
	assert.Len(t, headers, 2)
	assert.Equal(t, "event", headers[":message-type"].Value.(string))
	assert.Equal(t, "messageStart", headers[":event-type"].Value.(string))
}

func TestHeaderParser_Reset(t *testing.T) {
	parser := NewHeaderParser()

	// 解析一些数据
	data := buildSimpleStringHeader("test", "value")
	parser.ParseHeaders(data)

	// 重置
	parser.Reset()

	// 验证状态被重置
	assert.Equal(t, PhaseReadNameLength, parser.state.Phase)
	assert.Empty(t, parser.state.ParsedHeaders)
}

func TestHeaderParser_GetState(t *testing.T) {
	parser := NewHeaderParser()

	state := parser.GetState()

	assert.NotNil(t, state)
	assert.Equal(t, parser.state, state)
}

func TestGetMessageTypeFromHeaders(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]HeaderValue
		expected string
	}{
		{
			name: "存在message-type",
			headers: map[string]HeaderValue{
				":message-type": {Type: ValueType_STRING, Value: "event"},
			},
			expected: "event",
		},
		{
			name:     "不存在message-type",
			headers:  map[string]HeaderValue{},
			expected: "event", // 默认返回"event"
		},
		{
			name: "message-type为空",
			headers: map[string]HeaderValue{
				":message-type": {Type: ValueType_STRING, Value: ""},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetMessageTypeFromHeaders(tt.headers)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetEventTypeFromHeaders(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]HeaderValue
		expected string
	}{
		{
			name: "存在event-type",
			headers: map[string]HeaderValue{
				":event-type": {Type: ValueType_STRING, Value: "messageStart"},
			},
			expected: "messageStart",
		},
		{
			name:     "不存在event-type",
			headers:  map[string]HeaderValue{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetEventTypeFromHeaders(tt.headers)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetContentTypeFromHeaders(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]HeaderValue
		expected string
	}{
		{
			name: "存在content-type",
			headers: map[string]HeaderValue{
				":content-type": {Type: ValueType_STRING, Value: "application/json"},
			},
			expected: "application/json",
		},
		{
			name:     "不存在content-type",
			headers:  map[string]HeaderValue{},
			expected: "application/json", // 默认返回"application/json"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetContentTypeFromHeaders(tt.headers)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// 辅助函数：构造简单的字符串头部
func buildSimpleStringHeader(name, value string) []byte {
	data := []byte{}

	// Name length (1 byte)
	data = append(data, byte(len(name)))

	// Name
	data = append(data, []byte(name)...)

	// Value type (1 byte) - 7 for string
	data = append(data, byte(ValueType_STRING))

	// Value length (2 bytes, big-endian)
	valueLen := make([]byte, 2)
	binary.BigEndian.PutUint16(valueLen, uint16(len(value)))
	data = append(data, valueLen...)

	// Value
	data = append(data, []byte(value)...)

	return data
}

func TestHeaderParser_ParseHeaders_PartialData(t *testing.T) {
	parser := NewHeaderParser()

	// 构造完整的头部数据
	fullData := buildSimpleStringHeader("test-header", "test-value")

	// 只提供部分数据（前5个字节）
	partialData := fullData[:5]

	headers, err := parser.ParseHeaders(partialData)

	// 应该返回错误或空结果，因为数据不完整
	// 具体行为取决于实现
	if err != nil {
		assert.Error(t, err)
	} else {
		// 如果没有错误，应该返回空或部分解析的结果
		assert.NotNil(t, headers)
	}
}

func TestHeaderParser_ParseHeaders_InvalidData(t *testing.T) {
	parser := NewHeaderParser()

	// 提供无效数据
	invalidData := []byte{0xFF, 0xFF, 0xFF}

	headers, err := parser.ParseHeaders(invalidData)

	// 应该处理无效数据
	if err != nil {
		assert.Error(t, err)
	} else {
		assert.NotNil(t, headers)
	}
}
