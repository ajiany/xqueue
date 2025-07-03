package api

import (
	"net/http"
	"strconv"
	"time"

	"xqueue/internal/manager"
	"xqueue/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// StreamHandler Stream API处理器
// v0.0.5 新增：适配Stream任务管理器的HTTP接口
type StreamHandler struct {
	taskManager *manager.StreamTaskManager
	logger      *logrus.Logger
}

// NewStreamHandler 创建Stream API处理器
func NewStreamHandler(taskManager *manager.StreamTaskManager, logger *logrus.Logger) *StreamHandler {
	return &StreamHandler{
		taskManager: taskManager,
		logger:      logger,
	}
}

// 注意：SubmitTaskRequest 已在 handler.go 中定义

// SubmitTask 提交任务
func (sh *StreamHandler) SubmitTask(c *gin.Context) {
	var req SubmitTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request format",
			"details": err.Error(),
		})
		return
	}

	// 构建任务选项
	var options []manager.TaskOption

	if req.Priority != 0 {
		options = append(options, manager.WithPriority(req.Priority))
	}

	if req.MaxRetry != 0 {
		options = append(options, manager.WithMaxRetry(req.MaxRetry))
	}

	if req.QueueTimeout != 0 || req.ProcessTimeout != 0 {
		queueTimeout := time.Duration(req.QueueTimeout) * time.Second
		if queueTimeout == 0 {
			queueTimeout = 10 * time.Minute // 默认值
		}

		processTimeout := time.Duration(req.ProcessTimeout) * time.Second
		if processTimeout == 0 {
			processTimeout = 5 * time.Minute // 默认值
		}

		options = append(options, manager.WithTimeout(queueTimeout, processTimeout))
	}

	// 提交任务
	task, err := sh.taskManager.SubmitTask(c.Request.Context(), req.Type, req.Payload, options...)
	if err != nil {
		sh.logger.WithError(err).Error("Failed to submit task")
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Failed to submit task",
			"details": err.Error(),
		})
		return
	}

	sh.logger.WithFields(logrus.Fields{
		"task_id":   task.ID,
		"task_type": task.Type,
	}).Info("Task submitted via API")

	c.JSON(http.StatusCreated, gin.H{
		"task_id": task.ID,
		"status":  task.Status,
		"message": "Task submitted successfully",
		"task":    task,
	})
}

// GetTask 获取单个任务
func (sh *StreamHandler) GetTask(c *gin.Context) {
	taskID := c.Param("id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Task ID is required",
		})
		return
	}

	task, err := sh.taskManager.GetTask(c.Request.Context(), taskID)
	if err != nil {
		sh.logger.WithError(err).Warn("Failed to get task")
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "Task not found",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"task": task,
	})
}

// GetTasks 获取任务列表
func (sh *StreamHandler) GetTasks(c *gin.Context) {
	// 解析查询参数
	status := c.DefaultQuery("status", "")
	limitStr := c.DefaultQuery("limit", "10")
	offsetStr := c.DefaultQuery("offset", "0")

	limit, err := strconv.ParseInt(limitStr, 10, 64)
	if err != nil || limit <= 0 {
		limit = 10
	}

	offset, err := strconv.ParseInt(offsetStr, 10, 64)
	if err != nil || offset < 0 {
		offset = 0
	}

	var tasks []*models.Task
	if status != "" {
		taskStatus := models.TaskStatus(status)
		tasks, err = sh.taskManager.GetTasksByStatus(c.Request.Context(), taskStatus, limit, offset)
	} else {
		// 如果没有指定状态，返回所有状态的任务
		allTasks := make([]*models.Task, 0)
		statuses := []models.TaskStatus{
			models.TaskStatusPending,
			models.TaskStatusProcessing,
			models.TaskStatusSuccess,
			models.TaskStatusFailed,
			models.TaskStatusCanceled,
			models.TaskStatusTimeout,
		}

		for _, s := range statuses {
			statusTasks, _ := sh.taskManager.GetTasksByStatus(c.Request.Context(), s, limit, offset)
			allTasks = append(allTasks, statusTasks...)
		}
		tasks = allTasks
	}

	if err != nil {
		sh.logger.WithError(err).Error("Failed to get tasks")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to get tasks",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"tasks":  tasks,
		"count":  len(tasks),
		"limit":  limit,
		"offset": offset,
		"status": status,
	})
}

// CancelTask 取消任务
func (sh *StreamHandler) CancelTask(c *gin.Context) {
	taskID := c.Param("id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Task ID is required",
		})
		return
	}

	err := sh.taskManager.CancelTask(c.Request.Context(), taskID)
	if err != nil {
		sh.logger.WithError(err).Warn("Failed to cancel task")
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Failed to cancel task",
			"details": err.Error(),
		})
		return
	}

	sh.logger.WithField("task_id", taskID).Info("Task canceled via API")

	c.JSON(http.StatusOK, gin.H{
		"message": "Task canceled successfully",
		"task_id": taskID,
	})
}

// GetStats 获取系统统计信息
func (sh *StreamHandler) GetStats(c *gin.Context) {
	stats, err := sh.taskManager.GetStats()
	if err != nil {
		sh.logger.WithError(err).Error("Failed to get stats")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to get stats",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// HealthCheck 健康检查
func (sh *StreamHandler) HealthCheck(c *gin.Context) {
	isRunning := sh.taskManager.IsRunning()

	status := "healthy"
	httpStatus := http.StatusOK

	if !isRunning {
		status = "unhealthy"
		httpStatus = http.StatusServiceUnavailable
	}

	c.JSON(httpStatus, gin.H{
		"status":    status,
		"timestamp": time.Now(),
		"version":   "v0.0.5-stream",
		"running":   isRunning,
	})
}

// GetHandlers 获取已注册的处理器
func (sh *StreamHandler) GetHandlers(c *gin.Context) {
	handlers := sh.taskManager.GetRegisteredHandlers()

	c.JSON(http.StatusOK, gin.H{
		"handlers": handlers,
		"count":    len(handlers),
	})
}

// SetConcurrencyRequest 设置并发限制请求
type SetConcurrencyRequest struct {
	TaskType       string `json:"task_type" binding:"required"`
	MaxConcurrency int    `json:"max_concurrency" binding:"required,min=1"`
}

// SetConcurrency 设置并发限制
func (sh *StreamHandler) SetConcurrency(c *gin.Context) {
	var req SetConcurrencyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request format",
			"details": err.Error(),
		})
		return
	}

	err := sh.taskManager.SetConcurrencyLimit(req.TaskType, req.MaxConcurrency)
	if err != nil {
		sh.logger.WithError(err).Warn("Failed to set concurrency limit")
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Failed to set concurrency limit",
			"details": err.Error(),
		})
		return
	}

	sh.logger.WithFields(logrus.Fields{
		"task_type":       req.TaskType,
		"max_concurrency": req.MaxConcurrency,
	}).Info("Concurrency limit updated via API")

	c.JSON(http.StatusOK, gin.H{
		"message":         "Concurrency limit set successfully",
		"task_type":       req.TaskType,
		"max_concurrency": req.MaxConcurrency,
	})
}

// GetConcurrency 获取并发限制配置
func (sh *StreamHandler) GetConcurrency(c *gin.Context) {
	limits := sh.taskManager.GetConcurrencyLimits()

	c.JSON(http.StatusOK, gin.H{
		"limits": limits,
		"count":  len(limits),
	})
}

// GetStreamInfo 获取Stream信息 (v0.0.5 新增)
func (sh *StreamHandler) GetStreamInfo(c *gin.Context) {
	stats, err := sh.taskManager.GetStats()
	if err != nil {
		sh.logger.WithError(err).Error("Failed to get stream info")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to get stream info",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"stream_info": stats.StreamInfo,
		"timestamp":   time.Now(),
	})
}

// GetWorkerInfo 获取Worker信息 (v0.0.5 新增)
func (sh *StreamHandler) GetWorkerInfo(c *gin.Context) {
	stats, err := sh.taskManager.GetStats()
	if err != nil {
		sh.logger.WithError(err).Error("Failed to get worker info")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to get worker info",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"total_workers":   stats.TotalWorkers,
		"active_workers":  stats.ActiveWorkers,
		"processed_tasks": stats.ProcessedTasks,
		"failed_tasks":    stats.FailedTasksCount,
		"uptime":          stats.Uptime,
		"timestamp":       time.Now(),
	})
}
