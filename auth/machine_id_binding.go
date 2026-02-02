package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"kiro2api/logger"
	"kiro2api/types"

	"github.com/google/uuid"
)

// MachineIdBinding 机器码绑定信息
type MachineIdBinding struct {
	MachineId string    `json:"machine_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MachineIdBindingData 持久化数据结构
type MachineIdBindingData struct {
	Bindings map[string]*MachineIdBinding `json:"bindings"`
}

// MachineIdBindingManager 机器码绑定管理器
type MachineIdBindingManager struct {
	bindings map[string]*MachineIdBinding // bindingKey -> binding
	mutex    sync.RWMutex
	filePath string
}

var (
	globalMachineIdBindingManager *MachineIdBindingManager
	machineIdBindingOnce          sync.Once
)

// GetMachineIdBindingManager 获取全局机器码绑定管理器
func GetMachineIdBindingManager() *MachineIdBindingManager {
	machineIdBindingOnce.Do(func() {
		filePath := os.Getenv("MACHINE_ID_BINDING_FILE")
		if filePath == "" {
			// 如果设置了OAUTH_TOKEN_FILE，使用相同目录
			oauthFile := os.Getenv("OAUTH_TOKEN_FILE")
			if oauthFile != "" {
				dir := filepath.Dir(oauthFile)
				filePath = filepath.Join(dir, "machine_id_bindings.json")
			} else {
				filePath = "machine_id_bindings.json"
			}
		}
		globalMachineIdBindingManager = &MachineIdBindingManager{
			bindings: make(map[string]*MachineIdBinding),
			filePath: filePath,
		}
		logger.Info("机器码绑定文件路径", logger.String("path", filePath))
		// 启动时加载已有绑定
		if err := globalMachineIdBindingManager.LoadFromFile(); err != nil {
			logger.Warn("加载机器码绑定失败，将使用空绑定", logger.Err(err))
		}
	})
	return globalMachineIdBindingManager
}

// GetBinding 获取账号绑定的机器码
func (m *MachineIdBindingManager) GetBinding(email string) *MachineIdBinding {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	key := NormalizeBindingKey(email)
	if key == "" {
		return nil
	}
	if binding, exists := m.bindings[key]; exists {
		return binding
	}
	return nil
}

// GetMachineId 获取账号绑定的机器码字符串，不存在返回空字符串
func (m *MachineIdBindingManager) GetMachineId(email string) string {
	binding := m.GetBinding(email)
	if binding != nil {
		return binding.MachineId
	}
	return ""
}

// SetBinding 设置/更新账号的机器码绑定
func (m *MachineIdBindingManager) SetBinding(email, machineId string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	key := NormalizeBindingKey(email)
	if key == "" {
		return nil
	}

	now := time.Now()
	if existing, exists := m.bindings[key]; exists {
		existing.MachineId = machineId
		existing.UpdatedAt = now
	} else {
		m.bindings[key] = &MachineIdBinding{
			MachineId: machineId,
			CreatedAt: now,
			UpdatedAt: now,
		}
	}

	// 持久化到文件
	return m.saveToFileUnlocked()
}

// DeleteBinding 删除账号的机器码绑定
func (m *MachineIdBindingManager) DeleteBinding(email string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	key := NormalizeBindingKey(email)
	if key != "" {
		delete(m.bindings, key)
	}
	return m.saveToFileUnlocked()
}

// GetAllBindings 获取所有绑定
func (m *MachineIdBindingManager) GetAllBindings() map[string]*MachineIdBinding {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	// 返回副本
	result := make(map[string]*MachineIdBinding)
	for k, v := range m.bindings {
		result[k] = &MachineIdBinding{
			MachineId: v.MachineId,
			CreatedAt: v.CreatedAt,
			UpdatedAt: v.UpdatedAt,
		}
	}
	return result
}

// GenerateRandomMachineId 生成随机机器码（UUID格式）
func GenerateRandomMachineId() string {
	return uuid.New().String()
}

var hex64Regex = regexp.MustCompile("^[0-9a-fA-F]{64}$")

const (
	bindingKeyEmailPrefix   = "email:"
	bindingKeyOAuthPrefix   = "oauth:"
	bindingKeyRefreshPrefix = "refresh:"
)

// NormalizeMachineId 标准化机器码格式（UUID 或 64位HEX）
// 返回标准化字符串及是否有效
func NormalizeMachineId(machineId string) (string, bool) {
	trimmed := strings.TrimSpace(machineId)
	if trimmed == "" {
		return "", false
	}

	if parsed, err := uuid.Parse(trimmed); err == nil {
		return parsed.String(), true
	}

	if hex64Regex.MatchString(trimmed) {
		return strings.ToLower(trimmed), true
	}

	return "", false
}

// NormalizeBindingKey 标准化绑定key（无前缀视为email）
func NormalizeBindingKey(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, ":") {
		return trimmed
	}
	return bindingKeyEmailPrefix + trimmed
}

// BuildMachineIdBindingKey 基于AuthConfig生成绑定key
func BuildMachineIdBindingKey(authConfig AuthConfig) string {
	if authConfig.OAuthID != "" {
		return bindingKeyOAuthPrefix + authConfig.OAuthID
	}
	if authConfig.RefreshToken != "" {
		hash := sha256.Sum256([]byte(authConfig.RefreshToken))
		return bindingKeyRefreshPrefix + hex.EncodeToString(hash[:])
	}
	return ""
}

// GenerateStableMachineIdFromSeed 根据固定种子生成稳定机器码（64位HEX）
func GenerateStableMachineIdFromSeed(seed string) string {
	if strings.TrimSpace(seed) == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(hash[:])
}

// EnsureAutoMachineIdBinding 自动生成并绑定机器码（优先profileArn，否则refreshToken）
// 若已存在绑定则不覆盖，返回是否新增绑定
func EnsureAutoMachineIdBinding(authConfig AuthConfig, token types.TokenInfo) (string, bool) {
	bindingKey := BuildMachineIdBindingKey(authConfig)
	if bindingKey == "" {
		return "", false
	}

	manager := GetMachineIdBindingManager()
	if existing := manager.GetBinding(bindingKey); existing != nil && existing.MachineId != "" {
		return existing.MachineId, false
	}

	seed := strings.TrimSpace(token.ProfileArn)
	if seed == "" {
		seed = strings.TrimSpace(authConfig.RefreshToken)
	}
	if seed == "" {
		return "", false
	}

	machineId := GenerateStableMachineIdFromSeed(seed)
	if machineId == "" {
		return "", false
	}

	if err := manager.SetBinding(bindingKey, machineId); err != nil {
		logger.Warn("自动绑定机器码失败", logger.String("binding_key", bindingKey), logger.Err(err))
		return "", false
	}

	// 同步到指纹管理器，确保立即生效
	fpManager := GetFingerprintManager()
	fpManager.SetMachineIdForBindingKey(bindingKey, machineId)

	logger.Info("自动生成机器码绑定成功",
		logger.String("binding_key", bindingKey),
		logger.String("machine_id", machineId[:8]+"..."))
	return machineId, true
}

// LoadFromFile 从文件加载绑定数据
func (m *MachineIdBindingManager) LoadFromFile() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	data, err := os.ReadFile(m.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// 文件不存在，使用空绑定
			return nil
		}
		return err
	}

	var bindingData MachineIdBindingData
	if err := json.Unmarshal(data, &bindingData); err != nil {
		return err
	}

	if bindingData.Bindings != nil {
		normalized := make(map[string]*MachineIdBinding)
		for key, binding := range bindingData.Bindings {
			normalizedKey := NormalizeBindingKey(key)
			if normalizedKey == "" {
				continue
			}
			normalized[normalizedKey] = binding
		}
		m.bindings = normalized
	}

	logger.Info("加载机器码绑定成功", logger.Int("count", len(m.bindings)))
	return nil
}

// saveToFileUnlocked 保存绑定数据到文件（调用者必须持有锁）
func (m *MachineIdBindingManager) saveToFileUnlocked() error {
	bindingData := MachineIdBindingData{
		Bindings: m.bindings,
	}

	data, err := json.MarshalIndent(bindingData, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.filePath, data, 0644)
}

// SaveToFile 保存绑定数据到文件
func (m *MachineIdBindingManager) SaveToFile() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.saveToFileUnlocked()
}

// IsValidMachineId 验证机器码格式（UUID格式）
func IsValidMachineId(machineId string) bool {
	_, ok := NormalizeMachineId(machineId)
	return ok
}
