package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateUUID(t *testing.T) {
	uuid1 := GenerateUUID()
	uuid2 := GenerateUUID()

	// 验证UUID不为空
	assert.NotEmpty(t, uuid1)
	assert.NotEmpty(t, uuid2)

	// 验证两次生成的UUID不同
	assert.NotEqual(t, uuid1, uuid2)

	// 验证UUID格式（应该是36个字符，包含破折号）
	assert.Len(t, uuid1, 36)
	assert.Contains(t, uuid1, "-")
}

func TestGenerateUUID_Format(t *testing.T) {
	uuid := GenerateUUID()

	// UUID格式应该是 xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
	// 验证长度
	assert.Len(t, uuid, 36)

	// 验证破折号位置
	assert.Equal(t, "-", string(uuid[8]))
	assert.Equal(t, "-", string(uuid[13]))
	assert.Equal(t, "-", string(uuid[18]))
	assert.Equal(t, "-", string(uuid[23]))
}

func TestGenerateUUID_Uniqueness(t *testing.T) {
	// 生成多个UUID，验证唯一性
	uuids := make(map[string]bool)
	count := 100

	for i := 0; i < count; i++ {
		uuid := GenerateUUID()
		assert.False(t, uuids[uuid], "UUID应该是唯一的")
		uuids[uuid] = true
	}

	assert.Len(t, uuids, count, "应该生成100个不同的UUID")
}
