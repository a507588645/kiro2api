package utils

import (
	"bytes"
	"io"
)

// ReadHTTPResponse 通用的HTTP响应体读取函数（使用对象池优化）
func ReadHTTPResponse(body io.Reader) ([]byte, error) {
	// 直接分配缓冲区，Go GC会自动管理
	buffer := bytes.NewBuffer(nil)
	buf := make([]byte, 1024)

	for {
		n, err := body.Read(buf)
		if n > 0 {
			buffer.Write(buf[:n])
		}
		if err != nil {
			result := buffer.Bytes()
			// 确保空body返回空切片而不是nil，保持向后兼容
			if result == nil {
				result = []byte{}
			}
			if err == io.EOF {
				return result, nil
			}
			return result, err
		}
	}
}
