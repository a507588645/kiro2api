package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEventStreamMessage_GetMessageType(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]HeaderValue
		expected string
	}{
		{
			name: "有效的message-type",
			headers: map[string]HeaderValue{
				":message-type": {
					Type:  ValueType_STRING,
					Value: "exception",
				},
			},
			expected: "exception",
		},
		{
			name:     "空headers",
			headers:  map[string]HeaderValue{},
			expected: "event", // 默认值
		},
		{
			name: "message-type不是字符串",
			headers: map[string]HeaderValue{
				":message-type": {
					Type:  ValueType_INTEGER,
					Value: 123,
				},
			},
			expected: "event",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &EventStreamMessage{
				Headers: tt.headers,
			}

			result := msg.GetMessageType()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEventStreamMessage_GetEventType(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]HeaderValue
		expected string
	}{
		{
			name: "有效的event-type",
			headers: map[string]HeaderValue{
				":event-type": {
					Type:  ValueType_STRING,
					Value: "contentBlockStart",
				},
			},
			expected: "contentBlockStart",
		},
		{
			name:     "空headers",
			headers:  map[string]HeaderValue{},
			expected: "",
		},
		{
			name: "event-type不是字符串",
			headers: map[string]HeaderValue{
				":event-type": {
					Type:  ValueType_BOOL_TRUE,
					Value: true,
				},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &EventStreamMessage{
				Headers: tt.headers,
			}

			result := msg.GetEventType()
			assert.Equal(t, tt.expected, result)
		})
	}
}
