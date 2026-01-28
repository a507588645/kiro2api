package auth

import (
	"encoding/json"
	"kiro2api/logger"
	"os"
	"sync"
	"time"

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
	bindings map[string]*MachineIdBinding // email -> binding
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
		globalMachineIdBindingManager = &MachineIdBindingManager{
			bindings: make(map[string]*MachineIdBinding),
			filePath: "machine_id_bindings.json",
		}
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
	return m.bindings[email]
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

	now := time.Now()
	if existing, exists := m.bindings[email]; exists {
		existing.MachineId = machineId
		existing.UpdatedAt = now
	} else {
		m.bindings[email] = &MachineIdBinding{
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

	delete(m.bindings, email)
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
		m.bindings = bindingData.Bindings
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
	_, err := uuid.Parse(machineId)
	return err == nil
}
