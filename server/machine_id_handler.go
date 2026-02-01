package server

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"kiro2api/auth"
	"kiro2api/logger"

	"github.com/gin-gonic/gin"
)

// MachineIdBindingResponse 机器码绑定响应
type MachineIdBindingResponse struct {
	BindingKey string    `json:"binding_key"`
	Email      string    `json:"email,omitempty"`
	MachineId  string    `json:"machine_id"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// RegisterMachineIdRoutes 注册机器码管理路由
func RegisterMachineIdRoutes(r *gin.Engine) {
	r.GET("/api/machine-ids", handleGetAllMachineIds)
	r.GET("/api/machine-ids/:email", handleGetMachineId)
	r.PUT("/api/machine-ids/:email", handleSetMachineId)
	r.DELETE("/api/machine-ids/:email", handleDeleteMachineId)
	r.POST("/api/machine-ids/:email/generate", handleGenerateMachineId)

	logger.Info("Machine ID routes registered")
}

// handleGetAllMachineIds 获取所有机器码绑定
func handleGetAllMachineIds(c *gin.Context) {
	manager := auth.GetMachineIdBindingManager()
	bindings := manager.GetAllBindings()

	result := make([]MachineIdBindingResponse, 0, len(bindings))
	for key, binding := range bindings {
		email := ""
		if strings.HasPrefix(key, "email:") {
			email = strings.TrimPrefix(key, "email:")
		}
		result = append(result, MachineIdBindingResponse{
			BindingKey: key,
			Email:      email,
			MachineId:  binding.MachineId,
			CreatedAt:  binding.CreatedAt,
			UpdatedAt:  binding.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"bindings": result,
		"count":    len(result),
	})
}

// handleGetMachineId 获取指定账号的机器码
func handleGetMachineId(c *gin.Context) {
	rawKey, err := url.QueryUnescape(c.Param("email"))
	if err != nil || rawKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的绑定Key"})
		return
	}
	bindingKey := auth.NormalizeBindingKey(rawKey)
	if bindingKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的绑定Key"})
		return
	}

	manager := auth.GetMachineIdBindingManager()
	binding := manager.GetBinding(bindingKey)

	if binding == nil {
		c.JSON(http.StatusOK, gin.H{
			"success":     true,
			"binding_key": bindingKey,
			"email":       strings.TrimPrefix(bindingKey, "email:"),
			"machine_id":  "",
			"bound":       false,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"binding_key": bindingKey,
		"email":       strings.TrimPrefix(bindingKey, "email:"),
		"machine_id":  binding.MachineId,
		"created_at":  binding.CreatedAt,
		"updated_at":  binding.UpdatedAt,
		"bound":       true,
	})
}

// handleSetMachineId 设置/更新账号的机器码
func handleSetMachineId(c *gin.Context) {
	rawKey, err := url.QueryUnescape(c.Param("email"))
	if err != nil || rawKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的绑定Key"})
		return
	}
	bindingKey := auth.NormalizeBindingKey(rawKey)
	if bindingKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的绑定Key"})
		return
	}

	var req struct {
		MachineId string `json:"machine_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请提供机器码"})
		return
	}

	// 验证机器码格式
	normalizedMachineId, ok := auth.NormalizeMachineId(req.MachineId)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的机器码格式，请使用UUID或64位HEX格式"})
		return
	}

	manager := auth.GetMachineIdBindingManager()
	if err := manager.SetBinding(bindingKey, normalizedMachineId); err != nil {
		logger.Error("设置机器码失败", logger.Err(err), logger.String("binding_key", bindingKey))
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "保存失败"})
		return
	}

	// 更新指纹管理器中的绑定
	fingerprintManager := auth.GetFingerprintManager()
	fingerprintManager.SetMachineIdForBindingKey(bindingKey, normalizedMachineId)

	logger.Info("机器码绑定成功",
		logger.String("binding_key", bindingKey),
		logger.String("machine_id", req.MachineId[:8]+"..."))

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"message":     "机器码绑定成功",
		"binding_key": bindingKey,
		"email":       strings.TrimPrefix(bindingKey, "email:"),
		"machine_id":  normalizedMachineId,
	})
}

// handleDeleteMachineId 删除账号的机器码绑定
func handleDeleteMachineId(c *gin.Context) {
	rawKey, err := url.QueryUnescape(c.Param("email"))
	if err != nil || rawKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的绑定Key"})
		return
	}
	bindingKey := auth.NormalizeBindingKey(rawKey)
	if bindingKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的绑定Key"})
		return
	}

	manager := auth.GetMachineIdBindingManager()
	if err := manager.DeleteBinding(bindingKey); err != nil {
		logger.Error("删除机器码绑定失败", logger.Err(err), logger.String("binding_key", bindingKey))
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "删除失败"})
		return
	}

	// 从指纹管理器中移除绑定
	fingerprintManager := auth.GetFingerprintManager()
	fingerprintManager.RemoveMachineIdForBindingKey(bindingKey)

	logger.Info("机器码绑定删除成功", logger.String("binding_key", bindingKey))

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"message":     "机器码绑定已删除",
		"binding_key": bindingKey,
		"email":       strings.TrimPrefix(bindingKey, "email:"),
	})
}

// handleGenerateMachineId 为账号生成随机机器码
func handleGenerateMachineId(c *gin.Context) {
	rawKey, err := url.QueryUnescape(c.Param("email"))
	if err != nil || rawKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的绑定Key"})
		return
	}
	bindingKey := auth.NormalizeBindingKey(rawKey)
	if bindingKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的绑定Key"})
		return
	}

	// 生成随机机器码
	machineId := auth.GenerateRandomMachineId()

	manager := auth.GetMachineIdBindingManager()
	if err := manager.SetBinding(bindingKey, machineId); err != nil {
		logger.Error("生成机器码失败", logger.Err(err), logger.String("binding_key", bindingKey))
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "保存失败"})
		return
	}

	// 更新指纹管理器中的绑定
	fingerprintManager := auth.GetFingerprintManager()
	fingerprintManager.SetMachineIdForBindingKey(bindingKey, machineId)

	logger.Info("随机机器码生成成功",
		logger.String("binding_key", bindingKey),
		logger.String("machine_id", machineId[:8]+"..."))

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"message":     "随机机器码生成成功",
		"binding_key": bindingKey,
		"email":       strings.TrimPrefix(bindingKey, "email:"),
		"machine_id":  machineId,
	})
}
