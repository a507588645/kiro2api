package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectImageFormat_JPEG(t *testing.T) {
	// JPEG magic bytes: FF D8
	jpegData := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01}

	format, err := DetectImageFormat(jpegData)

	assert.NoError(t, err)
	assert.Equal(t, "image/jpeg", format)
}

func TestDetectImageFormat_PNG(t *testing.T) {
	// PNG magic bytes: 89 50 4E 47 0D 0A 1A 0A
	pngData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D}

	format, err := DetectImageFormat(pngData)

	assert.NoError(t, err)
	assert.Equal(t, "image/png", format)
}

func TestDetectImageFormat_TooSmall(t *testing.T) {
	smallData := []byte{0x01, 0x02}

	_, err := DetectImageFormat(smallData)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "文件太小")
}

func TestDetectImageFormat_Unknown(t *testing.T) {
	unknownData := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}

	format, err := DetectImageFormat(unknownData)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不支持的图片格式")
	assert.Empty(t, format)
}

func TestSupportedImageFormats(t *testing.T) {
	expectedFormats := []string{
		".jpg",
		".jpeg",
		".png",
		".gif",
		".webp",
		".bmp",
	}

	for _, ext := range expectedFormats {
		format, exists := SupportedImageFormats[ext]
		assert.True(t, exists, "Extension %s should be supported", ext)
		assert.NotEmpty(t, format)
	}
}

func TestMaxImageSize(t *testing.T) {
	assert.Equal(t, 20*1024*1024, MaxImageSize)
}
