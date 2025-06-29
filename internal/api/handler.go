package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"xqueue/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// TaskManager 任务管理器接口
type TaskManager interface {
	SubmitTask(taskType string, payload map[string]interface{}, options ...TaskOption) (*models.Task, error)
	GetTask(taskID string) (*models.Task, error)
	GetTasksByStatus(status models.TaskStatus, limit, offset int64) ([]*models.Task, error)
	CancelTask(taskID string) error
	GetStats() (map[string]interface{}, error)
	ProcessTask(taskID string) error
	ConsumeNextTask() (*models.Task, error)

	// v0.0.2
	RegisterHandler(handler models.TaskHandler) error
	UnregisterHandler(taskType string) error
	IsHandlerRegistered(taskType string) bool
	GetRegisteredHandlers() []string

	// v0.0.3 并发控制管理
	SetConcurrencyLimit(taskType string, maxConcurrency int) error
	GetConcurrencyLimit(taskType string) (int, bool)
	GetAllConcurrencyLimits() map[string]int
}

// TaskOption 任务选项
type TaskOption func(*models.Task)

// Handler API处理器
type Handler struct {
	taskManager TaskManager
	logger      *logrus.Logger
}

// NewHandler 创建新的处理器
func NewHandler(taskManager TaskManager, logger *logrus.Logger) *Handler {
	return &Handler{
		taskManager: taskManager,
		logger:      logger,
	}
}

// SetupRoutes 设置路由
func (h *Handler) SetupRoutes(r *gin.Engine) {
	api := r.Group("/api/v1")
	{
		// 任务相关接口
		tasks := api.Group("/tasks")
		{
			tasks.POST("", h.SubmitTask)
			tasks.GET("", h.GetTasks)
			tasks.GET("/:id", h.GetTask)
			tasks.DELETE("/:id", h.CancelTask)
			tasks.POST("/:id/process", h.ProcessTask)
			tasks.POST("/consume", h.ConsumeTask)
		}

		// 统计接口
		api.GET("/stats", h.GetStats)

		// 健康检查
		api.GET("/health", h.Health)

		// v0.0.2
		handlers := api.Group("/handlers")
		{
			handlers.GET("", h.GetHandlers)
		}

		// v0.0.3 并发控制管理
		concurrency := api.Group("/concurrency")
		{
			concurrency.GET("", h.GetConcurrencyLimits)
			concurrency.POST("", h.SetConcurrencyLimit)
		}
	}
}

// SubmitTaskRequest 提交任务请求
type SubmitTaskRequest struct {
	Type           string                 `json:"type" binding:"required"`
	Payload        map[string]interface{} `json:"payload"`
	Priority       int                    `json:"priority"`
	MaxRetry       int                    `json:"max_retry"`
	QueueTimeout   int                    `json:"queue_timeout"`   // 秒
	ProcessTimeout int                    `json:"process_timeout"` // 秒
}

// SubmitTask 提交任务
func (h *Handler) SubmitTask(c *gin.Context) {
	var req SubmitTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.WithError(err).Error("Invalid request")
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 构建任务选项
	var options []TaskOption

	if req.Priority != 0 {
		options = append(options, WithPriority(req.Priority))
	}

	if req.MaxRetry != 0 {
		options = append(options, WithRetry(req.MaxRetry))
	}

	if req.QueueTimeout != 0 || req.ProcessTimeout != 0 {
		queueTimeout := time.Duration(req.QueueTimeout) * time.Second
		processTimeout := time.Duration(req.ProcessTimeout) * time.Second
		options = append(options, WithTimeout(queueTimeout, processTimeout))
	}

	// 提交任务
	task, err := h.taskManager.SubmitTask(req.Type, req.Payload, options...)
	if err != nil {
		h.logger.WithError(err).Error("Failed to submit task")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, task)
}

// GetTasks 获取任务列表
func (h *Handler) GetTasks(c *gin.Context) {
	// 获取查询参数
	status := c.Query("status")
	limitStr := c.DefaultQuery("limit", "10")
	offsetStr := c.DefaultQuery("offset", "0")

	limit, err := strconv.ParseInt(limitStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid limit parameter"})
		return
	}

	offset, err := strconv.ParseInt(offsetStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid offset parameter"})
		return
	}

	// 默认获取所有待处理任务
	taskStatus := models.TaskStatusPending
	if status != "" {
		taskStatus = models.TaskStatus(status)
	}

	tasks, err := h.taskManager.GetTasksByStatus(taskStatus, limit, offset)
	if err != nil {
		h.logger.WithError(err).Error("Failed to get tasks")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"tasks":  tasks,
		"limit":  limit,
		"offset": offset,
		"count":  len(tasks),
	})
}

// GetTask 获取单个任务
func (h *Handler) GetTask(c *gin.Context) {
	taskID := c.Param("id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Task ID is required"})
		return
	}

	task, err := h.taskManager.GetTask(taskID)
	if err != nil {
		h.logger.WithError(err).Error("Failed to get task")
		c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
		return
	}

	c.JSON(http.StatusOK, task)
}

// CancelTask 取消任务
func (h *Handler) CancelTask(c *gin.Context) {
	taskID := c.Param("id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Task ID is required"})
		return
	}

	err := h.taskManager.CancelTask(taskID)
	if err != nil {
		h.logger.WithError(err).Error("Failed to cancel task")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Task canceled successfully"})
}

// GetStats 获取统计信息
func (h *Handler) GetStats(c *gin.Context) {
	stats, err := h.taskManager.GetStats()
	if err != nil {
		h.logger.WithError(err).Error("Failed to get stats")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// Health 健康检查
func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "ok",
		"timestamp": time.Now().Unix(),
		"version":   "1.0.0",
	})
}

// ProcessTask 处理指定任务
func (h *Handler) ProcessTask(c *gin.Context) {
	taskID := c.Param("id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Task ID is required"})
		return
	}

	err := h.taskManager.ProcessTask(taskID)
	if err != nil {
		h.logger.WithError(err).Error("Failed to process task")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Task processing started",
		"task_id": taskID,
	})
}

// ConsumeTask 消费下一个待处理任务
func (h *Handler) ConsumeTask(c *gin.Context) {
	task, err := h.taskManager.ConsumeNextTask()
	if err != nil {
		h.logger.WithError(err).Error("Failed to consume task")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Task consumption started",
		"task":    task,
	})
}

// 任务选项实现
func WithPriority(priority int) TaskOption {
	return func(task *models.Task) {
		task.Priority = priority
	}
}

func WithRetry(maxRetry int) TaskOption {
	return func(task *models.Task) {
		task.MaxRetry = maxRetry
	}
}

func WithTimeout(queueTimeout, processTimeout time.Duration) TaskOption {
	return func(task *models.Task) {
		task.QueueTimeout = queueTimeout
		task.ProcessTimeout = processTimeout
	}
}

// GetHandlers 获取已注册的处理器列表
func (h *Handler) GetHandlers(c *gin.Context) {
	handlers := h.taskManager.GetRegisteredHandlers()

	c.JSON(http.StatusOK, gin.H{
		"handlers": handlers,
		"count":    len(handlers),
	})
}

// GetConcurrencyLimits 获取所有并发限制配置
func (h *Handler) GetConcurrencyLimits(c *gin.Context) {
	limits := h.taskManager.GetAllConcurrencyLimits()

	c.JSON(http.StatusOK, gin.H{
		"limits": limits,
		"count":  len(limits),
	})
}

// SetConcurrencyLimitRequest 设置并发限制请求
type SetConcurrencyLimitRequest struct {
	TaskType       string `json:"task_type" binding:"required"`
	MaxConcurrency int    `json:"max_concurrency" binding:"required,min=1"`
}

// SetConcurrencyLimit 设置任务类型的并发限制
func (h *Handler) SetConcurrencyLimit(c *gin.Context) {
	var req SetConcurrencyLimitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.WithError(err).Error("Invalid request")
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 检查任务类型是否已注册
	if !h.taskManager.IsHandlerRegistered(req.TaskType) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("task type '%s' is not registered", req.TaskType),
		})
		return
	}

	err := h.taskManager.SetConcurrencyLimit(req.TaskType, req.MaxConcurrency)
	if err != nil {
		h.logger.WithError(err).Error("Failed to set concurrency limit")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	h.logger.WithFields(logrus.Fields{
		"task_type":       req.TaskType,
		"max_concurrency": req.MaxConcurrency,
	}).Info("Concurrency limit set successfully")

	c.JSON(http.StatusOK, gin.H{
		"message":         "Concurrency limit set successfully",
		"task_type":       req.TaskType,
		"max_concurrency": req.MaxConcurrency,
	})
}
