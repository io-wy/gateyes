package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
)

type AdminHandler struct {
	userRepo    *repository.UserRepository
	providerMgr *provider.Manager
	adminKey    string
}

func NewAdminHandler(userRepo *repository.UserRepository, providerMgr *provider.Manager, adminKey string) *AdminHandler {
	return &AdminHandler{
		userRepo:    userRepo,
		providerMgr: providerMgr,
		adminKey:    adminKey,
	}
}

// ============ Middleware ============

func (h *AdminHandler) AdminAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 简单的管理员验证，实际应该用更安全的方式
		adminKey := c.GetHeader("X-Admin-Key")
		if adminKey == "" || adminKey != h.adminKey {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "invalid admin key",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

// ============ Provider Status ============

func (h *AdminHandler) GetProviders(c *gin.Context) {
	stats := h.providerMgr.Stats.List()
	c.JSON(http.StatusOK, gin.H{
		"data": stats,
	})
}

func (h *AdminHandler) GetProvider(c *gin.Context) {
	name := c.Param("name")

	stats, ok := h.providerMgr.Stats.Get(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "provider not found",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": stats,
	})
}

func (h *AdminHandler) GetProviderStats(c *gin.Context) {
	totalReq, successReq, failedReq, totalTokens, avgLatency := h.providerMgr.Stats.GlobalStats()

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"total_requests":    totalReq,
			"success_requests":  successReq,
			"failed_requests":   failedReq,
			"total_tokens":      totalTokens,
			"avg_latency_ms":    avgLatency,
			"error_rate":        func() float64 {
				if totalReq > 0 {
					return float64(failedReq) / float64(totalReq)
				}
				return 0
			}(),
		},
	})
}

// ============ User Management ============

type CreateUserRequest struct {
	Name   string   `json:"name" binding:"required"`
	Email  string   `json:"email"`
	Quota  int      `json:"quota"` // -1 for unlimited
	QPS    int      `json:"qps"`
	Models []string `json:"models"`
}

func (h *AdminHandler) CreateUser(c *gin.Context) {
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// -1 表示无限额
	quota := req.Quota
	if quota == 0 {
		quota = -1
	}

	user := h.userRepo.Create(req.Name, req.Email, quota, req.QPS, req.Models)

	c.JSON(http.StatusCreated, gin.H{
		"data": gin.H{
			"id":         user.ID,
			"api_key":    user.APIKey,
			"name":       user.Name,
			"email":      user.Email,
			"quota":      user.Quota,
			"used":       user.Used,
			"remaining":  user.Quota - user.Used,
			"qps":        user.QPS,
			"models":     user.Models,
			"status":     user.Status,
			"created_at": user.CreatedAt,
		},
	})
}

func (h *AdminHandler) ListUsers(c *gin.Context) {
	users := h.userRepo.List()

	var result []gin.H
	for _, u := range users {
		result = append(result, gin.H{
			"id":         u.ID,
			"api_key":    u.APIKey,
			"name":       u.Name,
			"email":      u.Email,
			"quota":      u.Quota,
			"used":       u.Used,
			"remaining":  u.Quota - u.Used,
			"qps":        u.QPS,
			"models":     u.Models,
			"status":     u.Status,
			"created_at": u.CreatedAt,
			"updated_at": u.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (h *AdminHandler) GetUser(c *gin.Context) {
	id := c.Param("id")

	user, ok := h.userRepo.Get(id)
	if !ok {
		// 尝试用 apiKey 查找
		user, ok = h.userRepo.GetByAPIKey(id)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"id":         user.ID,
			"api_key":    user.APIKey,
			"name":       user.Name,
			"email":      user.Email,
			"quota":      user.Quota,
			"used":       user.Used,
			"remaining":  user.Quota - user.Used,
			"qps":        user.QPS,
			"models":     user.Models,
			"status":     user.Status,
			"created_at": user.CreatedAt,
			"updated_at": user.UpdatedAt,
		},
	})
}

type UpdateUserRequest struct {
	Quota  *int      `json:"quota"`
	QPS    *int      `json:"qps"`
	Models *[]string `json:"models"`
	Status *string   `json:"status"`
}

func (h *AdminHandler) UpdateUser(c *gin.Context) {
	id := c.Param("id")

	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 检查用户是否存在
	user, ok := h.userRepo.Get(id)
	if !ok {
		// 尝试用 apiKey 查找
		user, ok = h.userRepo.GetByAPIKey(id)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
	}

	quota := -1
	if req.Quota != nil {
		quota = *req.Quota
	}

	qps := -1
	if req.QPS != nil {
		qps = *req.QPS
	}

	var models []string
	if req.Models != nil {
		models = *req.Models
	}

	status := ""
	if req.Status != nil {
		status = *req.Status
	}

	updated := h.userRepo.Update(user.ID, quota, qps, models, status)
	if !updated {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "update failed"})
		return
	}

	user, _ = h.userRepo.Get(user.ID)

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"id":         user.ID,
			"api_key":    user.APIKey,
			"name":       user.Name,
			"email":      user.Email,
			"quota":      user.Quota,
			"used":       user.Used,
			"remaining":  user.Quota - user.Used,
			"qps":        user.QPS,
			"models":     user.Models,
			"status":     user.Status,
			"updated_at": user.UpdatedAt,
		},
	})
}

func (h *AdminHandler) DeleteUser(c *gin.Context) {
	id := c.Param("id")

	// 先获取用户
	user, ok := h.userRepo.Get(id)
	if !ok {
		user, ok = h.userRepo.GetByAPIKey(id)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
	}

	deleted := h.userRepo.Delete(user.ID)
	if !deleted {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "user deleted"})
}

func (h *AdminHandler) ResetUserUsage(c *gin.Context) {
	id := c.Param("id")

	user, ok := h.userRepo.Get(id)
	if !ok {
		user, ok = h.userRepo.GetByAPIKey(id)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
	}

	// 重置使用量
	h.userRepo.Update(user.ID, -1, -1, nil, "")
	updated, _ := h.userRepo.Get(user.ID)

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"id":        updated.ID,
			"used":      0,
			"remaining": updated.Quota,
		},
	})
}

// ============ Dashboard ============

func (h *AdminHandler) Dashboard(c *gin.Context) {
	// Provider stats
	providerStats := h.providerMgr.Stats.List()
	totalReq, successReq, failedReq, totalTokens, avgLatency := h.providerMgr.Stats.GlobalStats()

	// User stats
	totalUsers, activeUsers, totalQuota, totalUsed := h.userRepo.Stats()

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"providers": gin.H{
				"list":              providerStats,
				"total_requests":    totalReq,
				"success_requests":  successReq,
				"failed_requests":   failedReq,
				"total_tokens":      totalTokens,
				"avg_latency_ms":   avgLatency,
				"error_rate":        func() float64 {
					if totalReq > 0 {
						return float64(failedReq) / float64(totalReq)
					}
					return 0
				}(),
			},
			"users": gin.H{
				"total_users":     totalUsers,
				"active_users":    activeUsers,
				"total_quota":     totalQuota,
				"total_used":      totalUsed,
				"usage_percent":   func() float64 {
					if totalQuota > 0 {
						return float64(totalUsed) / float64(totalQuota) * 100
					}
					return 0
				}(),
			},
			"uptime": time.Since(time.Now()).String(), // 需要单独记录启动时间
		},
	})
}
