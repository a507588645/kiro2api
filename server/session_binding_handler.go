package server

import (
	"net/http"

	"kiro2api/auth"

	"github.com/gin-gonic/gin"
)

// handleSessionBindingStatus 处理会话绑定状态查询
func handleSessionBindingStatus(c *gin.Context) {
	sessionManager := auth.GetSessionTokenBindingManager()
	stats := sessionManager.GetAllStats()

	c.JSON(http.StatusOK, stats)
}

// handleSessionBindingDetail 处理单个会话详情查询
func handleSessionBindingDetail(c *gin.Context) {
	sessionID := c.Param("session_id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "session_id is required",
		})
		return
	}

	sessionManager := auth.GetSessionTokenBindingManager()
	stats := sessionManager.GetSessionStats(sessionID)

	c.JSON(http.StatusOK, stats)
}
