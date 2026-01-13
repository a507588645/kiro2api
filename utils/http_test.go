package utils

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReadHTTPResponse_Success(t *testing.T) {
	testData := "Hello, World!"
	reader := strings.NewReader(testData)

	result, err := ReadHTTPResponse(reader)

	assert.NoError(t, err)
	assert.Equal(t, []byte(testData), result)
}

func TestReadHTTPResponse_EmptyBody(t *testing.T) {
	reader := strings.NewReader("")

	result, err := ReadHTTPResponse(reader)

	assert.NoError(t, err)
	assert.Equal(t, []byte{}, result)
}

func TestReadHTTPResponse_LargeBody(t *testing.T) {
	// 创建大于1024字节的数据
	testData := strings.Repeat("A", 5000)
	reader := strings.NewReader(testData)

	result, err := ReadHTTPResponse(reader)

	assert.NoError(t, err)
	assert.Equal(t, 5000, len(result))
	assert.Equal(t, testData, string(result))
}

func TestReadHTTPResponse_MultipleChunks(t *testing.T) {
	testData := strings.Repeat("B", 2048)
	reader := strings.NewReader(testData)

	result, err := ReadHTTPResponse(reader)

	assert.NoError(t, err)
	assert.Equal(t, 2048, len(result))
}

// ErrorReader 模拟读取错误的Reader
type ErrorReader struct {
	readCount int
}

func (e *ErrorReader) Read(p []byte) (int, error) {
	e.readCount++
	if e.readCount == 1 {
		// 第一次读取返回一些数据
		copy(p, []byte("partial data"))
		return 12, nil
	}
	// 第二次读取返回错误
	return 0, errors.New("read error")
}

func TestReadHTTPResponse_ReadError(t *testing.T) {
	errorReader := &ErrorReader{}

	result, err := ReadHTTPResponse(errorReader)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "read error")
	assert.Equal(t, []byte("partial data"), result)
}

func TestReadHTTPResponse_JSONResponse(t *testing.T) {
	jsonData := `{"message":"success","data":{"id":123}}`
	reader := strings.NewReader(jsonData)

	result, err := ReadHTTPResponse(reader)

	assert.NoError(t, err)
	assert.Equal(t, jsonData, string(result))
}

func TestReadHTTPResponse_BinaryData(t *testing.T) {
	binaryData := []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE, 0xFD}
	reader := bytes.NewReader(binaryData)

	result, err := ReadHTTPResponse(reader)

	assert.NoError(t, err)
	assert.Equal(t, binaryData, result)
}

func TestReadHTTPResponse_UnicodeContent(t *testing.T) {
	unicodeData := "你好世界 🌍 Hello World"
	reader := strings.NewReader(unicodeData)

	result, err := ReadHTTPResponse(reader)

	assert.NoError(t, err)
	assert.Equal(t, unicodeData, string(result))
}

func TestReadHTTPResponse_ExactlyOneKB(t *testing.T) {
	testData := strings.Repeat("X", 1024)
	reader := strings.NewReader(testData)

	result, err := ReadHTTPResponse(reader)

	assert.NoError(t, err)
	assert.Equal(t, 1024, len(result))
	assert.Equal(t, testData, string(result))
}
