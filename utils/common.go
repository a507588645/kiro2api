package utils

// IntMin 返回两个整数的最小值
// 遵循KISS原则，简单而高效
func IntMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// IntMax 返回两个整数的最大值
// 遵循KISS原则，简单而高效
func IntMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}
