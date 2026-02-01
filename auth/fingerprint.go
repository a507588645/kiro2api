package auth

import (
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
)

// {{RIPER-10 Action}}
// Role: LD | Time: 2025-12-14T15:38:07Z
// Principle: SOLID-O (开闭原则) - 扩展指纹维度，不修改现有接口
// Taste: 使用 osProfiles 结构体统一管理关联配置，避免多个独立数组

// Fingerprint 请求指纹，模拟真实客户端特征
type Fingerprint struct {
	// 基础信息
	SDKVersion  string
	OSType      string
	OSVersion   string
	NodeVersion string
	KiroVersion string
	KiroHash    string

	// 扩展指纹维度
	AcceptLanguage     string   // Accept-Language 头
	AcceptEncoding     string   // Accept-Encoding 头
	SecFetchMode       string   // sec-fetch-mode
	SecFetchSite       string   // sec-fetch-site
	SecFetchDest       string   // sec-fetch-dest
	HeaderOrder        []string // 请求头顺序（用于一致性）
	ConnectionBehavior string   // keep-alive 或 close

	// 时区和语言（影响某些API行为）
	Timezone string
	Locale   string

	// 新增：更多指纹维度
	ScreenResolution    string // 屏幕分辨率
	ColorDepth          int    // 颜色深度
	Platform            string // 平台标识
	DeviceMemory        int    // 设备内存 (GB)
	HardwareConcurrency int    // CPU核心数
	TimezoneOffset      int    // 时区偏移（分钟）
	DoNotTrack          string // DNT 头
	CacheControl        string // Cache-Control 头
}

// FingerprintManager 指纹管理器，每个token绑定固定指纹
type FingerprintManager struct {
	fingerprints    map[string]*Fingerprint
	bindingMachineIds map[string]string // bindingKey -> machineId 绑定
	mutex           sync.RWMutex
	rng             *rand.Rand
}

var (
	globalFingerprintManager *FingerprintManager
	fingerprintOnce          sync.Once
)

// 可选的SDK版本范围（扩展，借鉴 kiro.rs 升级到 1.0.27）
var sdkVersions = []string{
	"1.0.20", "1.0.21", "1.0.22", "1.0.23", "1.0.24",
	"1.0.25", "1.0.26", "1.0.27",
}

// 可选的操作系统配置（包含关联的语言和时区）
var osProfiles = []struct {
	osType    string
	versions  []string
	locales   []string
	timezones []string
	platform  string
}{
	{
		"darwin",
		[]string{"23.0.0", "23.1.0", "23.5.0", "24.0.0", "24.1.0", "24.5.0", "24.6.0", "25.0.0"},
		[]string{"en-US", "en-GB", "zh-CN", "zh-TW", "ja-JP", "ko-KR", "de-DE", "fr-FR"},
		[]string{"America/Los_Angeles", "America/New_York", "Europe/London", "Asia/Shanghai", "Asia/Tokyo"},
		"MacIntel",
	},
	{
		"windows",
		[]string{"10.0.19041", "10.0.19042", "10.0.19043", "10.0.22000", "10.0.22621", "10.0.22631"},
		[]string{"en-US", "en-GB", "zh-CN", "zh-TW", "ja-JP", "ko-KR", "de-DE"},
		[]string{"America/Los_Angeles", "America/New_York", "America/Chicago", "Europe/London", "Asia/Shanghai"},
		"Win32",
	},
	{
		"linux",
		[]string{"5.15.0", "5.19.0", "6.1.0", "6.2.0", "6.5.0", "6.6.0", "6.8.0"},
		[]string{"en-US", "en-GB", "zh-CN", "de-DE", "ru-RU"},
		[]string{"UTC", "America/New_York", "Europe/Berlin", "Asia/Shanghai"},
		"Linux x86_64",
	},
}

// 可选的Node.js版本（扩展）
var nodeVersions = []string{
	"18.17.0", "18.18.0", "18.19.0", "18.20.0",
	"20.10.0", "20.11.0", "20.12.0", "20.14.0", "20.15.0", "20.16.0", "20.17.0", "20.18.0",
	"22.0.0", "22.1.0", "22.2.0",
}

// 可选的Kiro版本（扩展，借鉴 kiro.rs）
var kiroVersions = []string{
	"0.3.0", "0.3.1", "0.3.2", "0.3.3",
	"0.4.0", "0.5.0", "0.6.0", "0.7.0", "0.8.0",
}

// Accept-Language 组合（基于locale生成）
var acceptLanguageTemplates = map[string][]string{
	"en-US": {"en-US,en;q=0.9", "en-US,en;q=0.9,zh-CN;q=0.8", "en-US,en;q=0.8"},
	"en-GB": {"en-GB,en;q=0.9,en-US;q=0.8", "en-GB,en;q=0.9"},
	"zh-CN": {"zh-CN,zh;q=0.9,en;q=0.8", "zh-CN,zh;q=0.9,en-US;q=0.8,en;q=0.7"},
	"zh-TW": {"zh-TW,zh;q=0.9,en;q=0.8", "zh-TW,zh-CN;q=0.9,zh;q=0.8,en;q=0.7"},
	"ja-JP": {"ja-JP,ja;q=0.9,en;q=0.8", "ja,en-US;q=0.9,en;q=0.8"},
	"ko-KR": {"ko-KR,ko;q=0.9,en;q=0.8", "ko,en-US;q=0.9,en;q=0.8"},
	"de-DE": {"de-DE,de;q=0.9,en;q=0.8", "de,en-US;q=0.9,en;q=0.8"},
	"fr-FR": {"fr-FR,fr;q=0.9,en;q=0.8", "fr,en-US;q=0.9,en;q=0.8"},
	"ru-RU": {"ru-RU,ru;q=0.9,en;q=0.8", "ru,en;q=0.9"},
}

// Accept-Encoding 组合
var acceptEncodings = []string{
	"gzip, deflate, br",
	"br, gzip, deflate",
	"gzip, deflate, br, zstd",
	"gzip, deflate",
	"br, gzip",
}

// 新增：常见屏幕分辨率
var screenResolutions = []string{
	"1920x1080", "2560x1440", "3840x2160", "1366x768",
	"1440x900", "1680x1050", "2560x1600", "3440x1440",
	"1920x1200", "2880x1800",
}

// 新增：时区偏移映射
var timezoneOffsets = map[string]int{
	"America/Los_Angeles": -480, // UTC-8
	"America/New_York":    -300, // UTC-5
	"America/Chicago":     -360, // UTC-6
	"Europe/London":       0,    // UTC+0
	"Europe/Berlin":       60,   // UTC+1
	"Asia/Shanghai":       480,  // UTC+8
	"Asia/Tokyo":          540,  // UTC+9
	"UTC":                 0,
}

// 新增：Cache-Control 变体
var cacheControlValues = []string{
	"no-cache",
	"no-store",
	"max-age=0",
	"no-cache, no-store",
}

// GetFingerprintManager 获取全局指纹管理器
func GetFingerprintManager() *FingerprintManager {
	fingerprintOnce.Do(func() {
		globalFingerprintManager = &FingerprintManager{
			fingerprints:     make(map[string]*Fingerprint),
			bindingMachineIds: make(map[string]string),
			rng:              rand.New(rand.NewSource(time.Now().UnixNano())),
		}
		// 加载已有的机器码绑定
		globalFingerprintManager.loadMachineIdBindings()
	})
	return globalFingerprintManager
}

// GetFingerprint 获取token对应的指纹，不存在则生成
func (fm *FingerprintManager) GetFingerprint(tokenKey string) *Fingerprint {
	fm.mutex.RLock()
	if fp, exists := fm.fingerprints[tokenKey]; exists {
		fm.mutex.RUnlock()
		return fp
	}
	fm.mutex.RUnlock()

	fm.mutex.Lock()
	defer fm.mutex.Unlock()

	// 双重检查
	if fp, exists := fm.fingerprints[tokenKey]; exists {
		return fp
	}

	fp := fm.generateFingerprint()
	fm.fingerprints[tokenKey] = fp
	return fp
}

// generateFingerprint 生成随机指纹（保持内部一致性）
func (fm *FingerprintManager) generateFingerprint() *Fingerprint {
	// 随机选择操作系统配置
	osProfile := osProfiles[fm.rng.Intn(len(osProfiles))]
	locale := osProfile.locales[fm.rng.Intn(len(osProfile.locales))]
	timezone := osProfile.timezones[fm.rng.Intn(len(osProfile.timezones))]

	// 根据locale选择Accept-Language
	acceptLangOptions := acceptLanguageTemplates[locale]
	if acceptLangOptions == nil {
		acceptLangOptions = acceptLanguageTemplates["en-US"]
	}

	// 获取时区偏移
	tzOffset := timezoneOffsets[timezone]
	if tzOffset == 0 && timezone != "UTC" && timezone != "Europe/London" {
		// 添加一些随机偏移
		tzOffset = (fm.rng.Intn(24) - 12) * 60
	}

	fp := &Fingerprint{
		SDKVersion:     sdkVersions[fm.rng.Intn(len(sdkVersions))],
		OSType:         osProfile.osType,
		OSVersion:      osProfile.versions[fm.rng.Intn(len(osProfile.versions))],
		NodeVersion:    nodeVersions[fm.rng.Intn(len(nodeVersions))],
		KiroVersion:    kiroVersions[fm.rng.Intn(len(kiroVersions))],
		KiroHash:       fm.generateHash(),
		AcceptLanguage: acceptLangOptions[fm.rng.Intn(len(acceptLangOptions))],
		AcceptEncoding: acceptEncodings[fm.rng.Intn(len(acceptEncodings))],
		SecFetchMode:   "cors",
		SecFetchSite:   "cross-site",
		SecFetchDest:   "empty",
		Timezone:       timezone,
		Locale:         locale,

		// 新增指纹维度
		ScreenResolution:    screenResolutions[fm.rng.Intn(len(screenResolutions))],
		ColorDepth:          fm.randomColorDepth(),
		Platform:            osProfile.platform,
		DeviceMemory:        fm.randomDeviceMemory(),
		HardwareConcurrency: fm.randomCPUCores(),
		TimezoneOffset:      tzOffset,
		DoNotTrack:          fm.randomDNT(),
		CacheControl:        cacheControlValues[fm.rng.Intn(len(cacheControlValues))],
	}

	// 生成一致的请求头顺序
	fp.HeaderOrder = fm.generateHeaderOrder()

	// 80%概率使用keep-alive
	if fm.rng.Float64() < 0.8 {
		fp.ConnectionBehavior = "keep-alive"
	} else {
		fp.ConnectionBehavior = "close"
	}

	return fp
}

// randomColorDepth 随机颜色深度
func (fm *FingerprintManager) randomColorDepth() int {
	depths := []int{24, 32, 30}
	return depths[fm.rng.Intn(len(depths))]
}

// randomDeviceMemory 随机设备内存
func (fm *FingerprintManager) randomDeviceMemory() int {
	memories := []int{4, 8, 16, 32, 64}
	return memories[fm.rng.Intn(len(memories))]
}

// randomCPUCores 随机CPU核心数
func (fm *FingerprintManager) randomCPUCores() int {
	cores := []int{4, 6, 8, 10, 12, 16, 24, 32}
	return cores[fm.rng.Intn(len(cores))]
}

// randomDNT 随机DNT设置
func (fm *FingerprintManager) randomDNT() string {
	// 大多数用户不设置DNT
	if fm.rng.Float64() < 0.7 {
		return ""
	}
	if fm.rng.Float64() < 0.5 {
		return "1"
	}
	return "0"
}

// generateHash 生成随机的Kiro hash
func (fm *FingerprintManager) generateHash() string {
	const chars = "0123456789abcdef"
	hash := make([]byte, 64)
	for i := range hash {
		hash[i] = chars[fm.rng.Intn(len(chars))]
	}
	return string(hash)
}

// generateHeaderOrder 生成请求头顺序（模拟不同客户端行为）
func (fm *FingerprintManager) generateHeaderOrder() []string {
	// 几种常见的请求头顺序模式
	patterns := [][]string{
		{"Host", "Connection", "Content-Type", "Authorization", "Accept", "Accept-Language", "Accept-Encoding", "User-Agent"},
		{"Host", "User-Agent", "Accept", "Accept-Language", "Accept-Encoding", "Connection", "Content-Type", "Authorization"},
		{"Authorization", "Content-Type", "Accept", "Accept-Encoding", "Accept-Language", "User-Agent", "Host", "Connection"},
		{"Content-Type", "Authorization", "User-Agent", "Accept", "Accept-Language", "Accept-Encoding", "Host", "Connection"},
		{"User-Agent", "Accept", "Accept-Language", "Accept-Encoding", "Authorization", "Content-Type", "Host", "Connection"},
	}
	return patterns[fm.rng.Intn(len(patterns))]
}

// BuildUserAgent 构建User-Agent头
func (fp *Fingerprint) BuildUserAgent() string {
	return fmt.Sprintf(
		"aws-sdk-js/%s ua/2.1 os/%s#%s lang/js md/nodejs#%s api/codewhispererstreaming#%s m/E KiroIDE-%s-%s",
		fp.SDKVersion, fp.OSType, fp.OSVersion, fp.NodeVersion, fp.SDKVersion, fp.KiroVersion, fp.KiroHash,
	)
}

// BuildAmzUserAgent 构建x-amz-user-agent头
func (fp *Fingerprint) BuildAmzUserAgent() string {
	return fmt.Sprintf(
		"aws-sdk-js/%s KiroIDE-%s-%s",
		fp.SDKVersion, fp.KiroVersion, fp.KiroHash,
	)
}

// ApplyToRequest 将指纹应用到HTTP请求
func (fp *Fingerprint) ApplyToRequest(req *http.Request) {
	// 核心UA头
	req.Header.Set("User-Agent", fp.BuildUserAgent())
	req.Header.Set("x-amz-user-agent", fp.BuildAmzUserAgent())

	// 扩展指纹头
	req.Header.Set("Accept-Language", fp.AcceptLanguage)
	req.Header.Set("Accept-Encoding", fp.AcceptEncoding)
	req.Header.Set("Connection", fp.ConnectionBehavior)

	// Sec-Fetch 系列（模拟现代浏览器/Electron行为）
	req.Header.Set("sec-fetch-mode", fp.SecFetchMode)
	req.Header.Set("sec-fetch-site", fp.SecFetchSite)
	req.Header.Set("sec-fetch-dest", fp.SecFetchDest)

	// 新增：Cache-Control
	if fp.CacheControl != "" {
		req.Header.Set("Cache-Control", fp.CacheControl)
	}

	// 新增：DNT
	if fp.DoNotTrack != "" {
		req.Header.Set("DNT", fp.DoNotTrack)
	}
}

// GetInfo 获取指纹摘要信息（用于日志）
func (fp *Fingerprint) GetInfo() map[string]string {
	return map[string]string{
		"os":       fp.OSType,
		"os_ver":   fp.OSVersion,
		"node":     fp.NodeVersion,
		"kiro":     fp.KiroVersion,
		"locale":   fp.Locale,
		"timezone": fp.Timezone,
		"screen":   fp.ScreenResolution,
		"platform": fp.Platform,
	}
}

// GetStats 获取指纹管理器统计信息
func (fm *FingerprintManager) GetStats() map[string]any {
	fm.mutex.RLock()
	defer fm.mutex.RUnlock()

	fingerprintDetails := make(map[string]any)
	osCounts := make(map[string]int)
	localeCounts := make(map[string]int)
	screenCounts := make(map[string]int)

	for key, fp := range fm.fingerprints {
		// 统计OS分布
		osCounts[fp.OSType]++
		localeCounts[fp.Locale]++
		screenCounts[fp.ScreenResolution]++

		// 记录每个token的指纹摘要
		fingerprintDetails[key] = fp.GetInfo()
	}

	return map[string]any{
		"total_fingerprints":  len(fm.fingerprints),
		"os_distribution":     osCounts,
		"locale_distribution": localeCounts,
		"screen_distribution": screenCounts,
		"details":             fingerprintDetails,
	}
}

// loadMachineIdBindings 从绑定管理器加载机器码绑定
func (fm *FingerprintManager) loadMachineIdBindings() {
	bindingManager := GetMachineIdBindingManager()
	bindings := bindingManager.GetAllBindings()

	fm.mutex.Lock()
	defer fm.mutex.Unlock()

	for key, binding := range bindings {
		fm.bindingMachineIds[key] = binding.MachineId
	}
}

// SetMachineIdForBindingKey 为指定绑定key设置机器码
func (fm *FingerprintManager) SetMachineIdForBindingKey(bindingKey, machineId string) {
	fm.mutex.Lock()
	defer fm.mutex.Unlock()
	if bindingKey == "" {
		return
	}
	fm.bindingMachineIds[bindingKey] = machineId
	// 清除缓存的指纹，确保使用新机器码
	fpKey := "binding:" + bindingKey
	delete(fm.fingerprints, fpKey)
}

// RemoveMachineIdForBindingKey 移除指定绑定key的机器码绑定
func (fm *FingerprintManager) RemoveMachineIdForBindingKey(bindingKey string) {
	fm.mutex.Lock()
	defer fm.mutex.Unlock()
	if bindingKey == "" {
		return
	}
	delete(fm.bindingMachineIds, bindingKey)
	// 清除缓存的指纹
	fpKey := "binding:" + bindingKey
	delete(fm.fingerprints, fpKey)
}

// GetMachineIdForBindingKey 获取指定绑定key的机器码
func (fm *FingerprintManager) GetMachineIdForBindingKey(bindingKey string) string {
	fm.mutex.RLock()
	defer fm.mutex.RUnlock()
	if bindingKey == "" {
		return ""
	}
	return fm.bindingMachineIds[bindingKey]
}

// GetFingerprintForBindingKey 获取指定绑定key对应的指纹（使用绑定的机器码）
func (fm *FingerprintManager) GetFingerprintForBindingKey(bindingKey, tokenKey string) *Fingerprint {
	fm.mutex.RLock()
	machineId := fm.bindingMachineIds[bindingKey]
	fm.mutex.RUnlock()

	// 如果有绑定的机器码，使用它来生成/获取指纹
	if machineId != "" {
		// 使用 bindingKey 作为 key 来保持指纹一致性
		fpKey := "binding:" + bindingKey
		fm.mutex.RLock()
		if fp, exists := fm.fingerprints[fpKey]; exists {
			fm.mutex.RUnlock()
			return fp
		}
		fm.mutex.RUnlock()

		fm.mutex.Lock()
		defer fm.mutex.Unlock()

		// 双重检查
		if fp, exists := fm.fingerprints[fpKey]; exists {
			return fp
		}

		// 生成新指纹，使用绑定的机器码作为 KiroHash
		fp := fm.generateFingerprint()
		// 允许 UUID 或 64位HEX
		cleanMachineId := strings.ReplaceAll(machineId, "-", "")
		if len(cleanMachineId) == 64 {
			fp.KiroHash = strings.ToLower(cleanMachineId)
		} else {
			// UUID 去掉连字符后为32位，重复以保证64位
			if len(cleanMachineId) < 64 {
				cleanMachineId = cleanMachineId + cleanMachineId
			}
			fp.KiroHash = strings.ToLower(cleanMachineId[:64])
		}
		fm.fingerprints[fpKey] = fp
		return fp
	}

	// 没有绑定机器码，使用默认的 tokenKey 获取指纹
	return fm.GetFingerprint(tokenKey)
}

// SetMachineIdForEmail 为指定邮箱设置机器码（兼容旧接口）
func (fm *FingerprintManager) SetMachineIdForEmail(email, machineId string) {
	key := NormalizeBindingKey(email)
	fm.SetMachineIdForBindingKey(key, machineId)
}

// RemoveMachineIdForEmail 移除指定邮箱的机器码绑定（兼容旧接口）
func (fm *FingerprintManager) RemoveMachineIdForEmail(email string) {
	key := NormalizeBindingKey(email)
	fm.RemoveMachineIdForBindingKey(key)
}

// GetMachineIdForEmail 获取指定邮箱绑定的机器码（兼容旧接口）
func (fm *FingerprintManager) GetMachineIdForEmail(email string) string {
	key := NormalizeBindingKey(email)
	return fm.GetMachineIdForBindingKey(key)
}

// GetFingerprintForEmail 获取指定邮箱对应的指纹（兼容旧接口）
func (fm *FingerprintManager) GetFingerprintForEmail(email, tokenKey string) *Fingerprint {
	key := NormalizeBindingKey(email)
	return fm.GetFingerprintForBindingKey(key, tokenKey)
}
