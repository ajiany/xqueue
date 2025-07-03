package manager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"xqueue/internal/concurrency"
	"xqueue/internal/models"
	"xqueue/internal/queue"
	"xqueue/internal/storage"
	"xqueue/internal/worker"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// TaskNotifier 任务通知接口
type TaskNotifier interface {
	NotifyTaskStatus(taskID string, message *models.TaskMessage) error
}

// StreamTaskManager 基于Redis Stream的任务管理器
// v0.0.5 新增：使用Stream消费者组机制，彻底解决并发竞争问题
type StreamTaskManager struct {
	// 核心组件
	storage        *storage.RedisStorage
	streamQueue    *queue.StreamTaskQueue
	workerPool     *worker.StreamWorkerPool
	concurrencyMgr *concurrency.ConcurrencyManager
	notifier       TaskNotifier
	logger         *logrus.Logger

	// 配置
	instanceID  string
	workerCount int

	// 任务处理器注册
	handlers   map[string]models.TaskHandler
	handlersMu sync.RWMutex

	// 运行时状态
	ctx       context.Context
	cancel    context.CancelFunc
	isRunning bool
	mu        sync.RWMutex

	// 统计信息缓存
	statsCache      *ManagerStats
	statsMu         sync.RWMutex
	lastStatsUpdate time.Time
}

// NewStreamTaskManager 创建新的Stream任务管理器
func NewStreamTaskManager(
	redisClient *redis.Client,
	notifier TaskNotifier,
	workerCount int,
	logger *logrus.Logger,
) *StreamTaskManager {
	instanceID := fmt.Sprintf("xqueue-%s", uuid.New().String()[:8])

	// 创建核心组件
	taskStorage := storage.NewRedisStorage(redisClient)
	streamQueue := queue.NewStreamTaskQueue(redisClient, logger)
	concurrencyMgr := concurrency.NewConcurrencyManager(redisClient, logger, 5*time.Minute)

	stm := &StreamTaskManager{
		storage:        taskStorage,
		streamQueue:    streamQueue,
		concurrencyMgr: concurrencyMgr,
		notifier:       notifier,
		logger:         logger,
		instanceID:     instanceID,
		workerCount:    workerCount,
		handlers:       make(map[string]models.TaskHandler),
	}

	// 创建Worker Pool
	stm.workerPool = worker.NewStreamWorkerPool(
		streamQueue,
		concurrencyMgr,
		stm, // StreamTaskManager实现TaskProcessor接口
		workerCount,
		instanceID,
		logger,
	)

	return stm
}

// ProcessTask 实现TaskProcessor接口
func (stm *StreamTaskManager) ProcessTask(task *models.Task, messageID string) error {
	ctx := context.Background()

	// 更新任务状态为processing
	task.Status = models.TaskStatusProcessing
	task.UpdatedAt = time.Now()
	startedAt := time.Now()
	task.StartedAt = &startedAt

	if err := stm.storage.SaveTaskForStream(ctx, task); err != nil {
		stm.logger.WithError(err).Error("Failed to update task status to processing")
	}

	// 发送任务状态变更通知
	stm.notifyTaskStatus(task, "")

	// 获取任务处理器
	stm.handlersMu.RLock()
	handler, exists := stm.handlers[task.Type]
	stm.handlersMu.RUnlock()

	if !exists {
		err := fmt.Errorf("no handler registered for task type: %s", task.Type)
		stm.handleTaskFailure(task, err)
		return err
	}

	// 处理任务
	err := handler.Handle(task)

	if err != nil {
		stm.handleTaskFailure(task, err)
		return err
	}

	// 任务成功完成
	stm.handleTaskSuccess(task)
	return nil
}

// handleTaskSuccess 处理任务成功
func (stm *StreamTaskManager) handleTaskSuccess(task *models.Task) {
	ctx := context.Background()

	task.Status = models.TaskStatusSuccess
	task.UpdatedAt = time.Now()
	completedAt := time.Now()
	task.CompletedAt = &completedAt
	task.Error = ""

	if err := stm.storage.SaveTaskForStream(ctx, task); err != nil {
		stm.logger.WithError(err).Error("Failed to update task status to success")
	}

	stm.notifyTaskStatus(task, "")

	stm.logger.WithFields(logrus.Fields{
		"task_id":   task.ID,
		"task_type": task.Type,
		"duration":  time.Since(*task.StartedAt),
	}).Info("Task completed successfully")
}

// handleTaskFailure 处理任务失败
func (stm *StreamTaskManager) handleTaskFailure(task *models.Task, err error) {
	ctx := context.Background()

	task.Retry++
	task.Error = err.Error()
	task.UpdatedAt = time.Now()

	if task.CanRetry() {
		// 可以重试，重新提交到队列
		task.Status = models.TaskStatusPending
		task.StartedAt = nil

		if updateErr := stm.storage.SaveTaskForStream(ctx, task); updateErr != nil {
			stm.logger.WithError(updateErr).Error("Failed to update task for retry")
		}

		// 重新添加到Stream
		if addErr := stm.streamQueue.AddTask(ctx, task); addErr != nil {
			stm.logger.WithError(addErr).Error("Failed to re-add task to stream for retry")
		}

		stm.logger.WithFields(logrus.Fields{
			"task_id":     task.ID,
			"task_type":   task.Type,
			"retry_count": task.Retry,
			"max_retry":   task.MaxRetry,
			"error":       err.Error(),
		}).Warn("Task failed, retrying")

	} else {
		// 重试次数已达上限，标记为失败
		task.Status = models.TaskStatusFailed
		completedAt := time.Now()
		task.CompletedAt = &completedAt

		if updateErr := stm.storage.SaveTaskForStream(ctx, task); updateErr != nil {
			stm.logger.WithError(updateErr).Error("Failed to update task status to failed")
		}

		stm.logger.WithFields(logrus.Fields{
			"task_id":     task.ID,
			"task_type":   task.Type,
			"retry_count": task.Retry,
			"max_retry":   task.MaxRetry,
			"error":       err.Error(),
		}).Error("Task failed permanently")
	}

	stm.notifyTaskStatus(task, err.Error())
}

// notifyTaskStatus 发送任务状态通知
func (stm *StreamTaskManager) notifyTaskStatus(task *models.Task, errorMsg string) {
	if stm.notifier == nil {
		return
	}

	message := &models.TaskMessage{
		TaskID:    task.ID,
		Status:    task.Status,
		Error:     errorMsg,
		Timestamp: time.Now(),
	}

	if err := stm.notifier.NotifyTaskStatus(task.ID, message); err != nil {
		stm.logger.WithError(err).Warn("Failed to send task status notification")
	}
}

// Start 启动任务管理器
func (stm *StreamTaskManager) Start() error {
	stm.mu.Lock()
	defer stm.mu.Unlock()

	if stm.isRunning {
		return fmt.Errorf("task manager already running")
	}

	stm.ctx, stm.cancel = context.WithCancel(context.Background())

	// 初始化Stream队列（创建消费者组）
	if err := stm.streamQueue.Initialize(stm.ctx); err != nil {
		return fmt.Errorf("failed to initialize stream queue: %w", err)
	}

	// 启动Worker Pool
	if err := stm.workerPool.Start(); err != nil {
		return fmt.Errorf("failed to start worker pool: %w", err)
	}

	stm.isRunning = true

	stm.logger.WithFields(logrus.Fields{
		"instance_id":  stm.instanceID,
		"worker_count": stm.workerCount,
	}).Info("Stream task manager started")

	return nil
}

// Stop 停止任务管理器
func (stm *StreamTaskManager) Stop() error {
	stm.mu.Lock()
	defer stm.mu.Unlock()

	if !stm.isRunning {
		return fmt.Errorf("task manager not running")
	}

	stm.logger.Info("Stopping stream task manager...")

	// 停止Worker Pool
	if err := stm.workerPool.Stop(); err != nil {
		stm.logger.WithError(err).Warn("Error stopping worker pool")
	}

	// 取消上下文
	if stm.cancel != nil {
		stm.cancel()
	}

	stm.isRunning = false

	stm.logger.Info("Stream task manager stopped")
	return nil
}

// SubmitTask 提交任务
func (stm *StreamTaskManager) SubmitTask(ctx context.Context, taskType string, payload map[string]interface{}, options ...TaskOption) (*models.Task, error) {
	// 检查任务类型是否已注册
	stm.handlersMu.RLock()
	_, exists := stm.handlers[taskType]
	stm.handlersMu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("task type '%s' not registered", taskType)
	}

	// 创建任务
	task := &models.Task{
		ID:             uuid.New().String(),
		Type:           taskType,
		Payload:        payload,
		Status:         models.TaskStatusPending,
		Priority:       0,
		MaxRetry:       3,
		Retry:          0,
		QueueTimeout:   10 * time.Minute,
		ProcessTimeout: 5 * time.Minute,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// 应用选项
	for _, option := range options {
		option(task)
	}

	// 保存到存储 - 使用Stream架构专用的保存方法
	if err := stm.storage.SaveTaskForStream(ctx, task); err != nil {
		return nil, fmt.Errorf("failed to save task: %w", err)
	}

	// 添加到Stream队列
	if err := stm.streamQueue.AddTask(ctx, task); err != nil {
		return nil, fmt.Errorf("failed to add task to stream: %w", err)
	}

	stm.logger.WithFields(logrus.Fields{
		"task_id":   task.ID,
		"task_type": task.Type,
		"priority":  task.Priority,
	}).Info("Task submitted to stream")

	// 发送任务创建通知
	stm.notifyTaskStatus(task, "")

	return task, nil
}

// TaskOption 任务选项
type TaskOption func(*models.Task)

// WithPriority 设置任务优先级
func WithPriority(priority int) TaskOption {
	return func(task *models.Task) {
		task.Priority = priority
	}
}

// WithMaxRetry 设置最大重试次数
func WithMaxRetry(maxRetry int) TaskOption {
	return func(task *models.Task) {
		task.MaxRetry = maxRetry
	}
}

// WithTimeout 设置超时时间
func WithTimeout(queueTimeout, processTimeout time.Duration) TaskOption {
	return func(task *models.Task) {
		task.QueueTimeout = queueTimeout
		task.ProcessTimeout = processTimeout
	}
}

// RegisterHandler 注册任务处理器
func (stm *StreamTaskManager) RegisterHandler(handler models.TaskHandler) error {
	taskType := handler.GetType()

	stm.handlersMu.Lock()
	defer stm.handlersMu.Unlock()

	if _, exists := stm.handlers[taskType]; exists {
		return fmt.Errorf("handler for task type '%s' already registered", taskType)
	}

	stm.handlers[taskType] = handler

	stm.logger.WithField("task_type", taskType).Info("Task handler registered")
	return nil
}

// GetRegisteredHandlers 获取已注册的处理器类型
func (stm *StreamTaskManager) GetRegisteredHandlers() []string {
	stm.handlersMu.RLock()
	defer stm.handlersMu.RUnlock()

	handlers := make([]string, 0, len(stm.handlers))
	for taskType := range stm.handlers {
		handlers = append(handlers, taskType)
	}

	return handlers
}

// SetConcurrencyLimit 设置并发限制
func (stm *StreamTaskManager) SetConcurrencyLimit(taskType string, limit int) error {
	// 检查任务类型是否已注册
	stm.handlersMu.RLock()
	_, exists := stm.handlers[taskType]
	stm.handlersMu.RUnlock()

	if !exists {
		return fmt.Errorf("task type '%s' not registered", taskType)
	}

	return stm.concurrencyMgr.SetConcurrencyLimit(taskType, limit)
}

// GetConcurrencyLimit 获取并发限制
func (stm *StreamTaskManager) GetConcurrencyLimit(taskType string) (int, bool) {
	return stm.concurrencyMgr.GetConcurrencyLimit(taskType)
}

// GetConcurrencyLimits 获取所有并发限制
func (stm *StreamTaskManager) GetConcurrencyLimits() map[string]int {
	return stm.concurrencyMgr.GetAllLimits()
}

// GetTask 获取任务
func (stm *StreamTaskManager) GetTask(ctx context.Context, taskID string) (*models.Task, error) {
	return stm.storage.GetTask(ctx, taskID)
}

// GetTasksByStatus 按状态获取任务列表
func (stm *StreamTaskManager) GetTasksByStatus(ctx context.Context, status models.TaskStatus, limit, offset int64) ([]*models.Task, error) {
	return stm.storage.GetTasksByStatus(ctx, status, limit, offset)
}

// CancelTask 取消任务
func (stm *StreamTaskManager) CancelTask(ctx context.Context, taskID string) error {
	task, err := stm.storage.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	if task.Status != models.TaskStatusPending {
		return fmt.Errorf("task %s is not in pending status, current status: %s", taskID, task.Status)
	}

	task.Status = models.TaskStatusCanceled
	task.UpdatedAt = time.Now()
	completedAt := time.Now()
	task.CompletedAt = &completedAt

	if err := stm.storage.SaveTask(ctx, task); err != nil {
		return fmt.Errorf("failed to update task status: %w", err)
	}

	stm.notifyTaskStatus(task, "")

	stm.logger.WithField("task_id", taskID).Info("Task canceled")
	return nil
}

// GetStats 获取统计信息
func (stm *StreamTaskManager) GetStats() (*ManagerStats, error) {
	// 检查缓存是否有效（缓存时间1秒）
	stm.statsMu.RLock()
	if stm.statsCache != nil && time.Since(stm.lastStatsUpdate) < 1*time.Second {
		cachedStats := *stm.statsCache // 复制结构体
		stm.statsMu.RUnlock()
		return &cachedStats, nil
	}
	stm.statsMu.RUnlock()

	// 需要更新统计信息
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// 获取Stream信息
	streamInfo, err := stm.streamQueue.GetStreamInfo(ctx)
	if err != nil {
		stm.logger.WithError(err).Warn("Failed to get stream info")
		streamInfo = &queue.StreamInfo{}
	}

	// 获取各状态任务数量 - 使用高效的计数方法
	taskStats, err := stm.getTaskStatsCounts(ctx)
	if err != nil {
		stm.logger.WithError(err).Warn("Failed to get task stats, using fallback")
		taskStats = map[string]int64{}
	}

	// 获取Worker Pool统计
	poolStats := stm.workerPool.GetStats()

	// 修复统计逻辑：
	// 1. Stream长度 = 所有提交的任务总数
	// 2. 使用任务状态索引来统计各种状态的任务
	// 3. pending = Stream总数 - (processing + success + failed + canceled)

	streamLength := int64(streamInfo.Length)
	processingTasks := taskStats["processing"]
	successTasks := taskStats["success"]
	failedTasks := taskStats["failed"]
	canceledTasks := taskStats["canceled"]

	// 已处理完成的任务数
	completedTasks := successTasks + failedTasks + canceledTasks

	// 等待处理的任务数 = Stream总数 - 正在处理数 - 已完成数
	pendingTasks := streamLength - processingTasks - completedTasks
	if pendingTasks < 0 {
		pendingTasks = 0
	}

	stm.logger.WithFields(logrus.Fields{
		"stream_length":    streamLength,
		"processing_tasks": processingTasks,
		"success_tasks":    successTasks,
		"failed_tasks":     failedTasks,
		"canceled_tasks":   canceledTasks,
		"completed_tasks":  completedTasks,
		"pending_tasks":    pendingTasks,
	}).Debug("Calculated task statistics")

	stats := &ManagerStats{
		InstanceID:        stm.instanceID,
		IsRunning:         stm.isRunning,
		PendingTasks:      pendingTasks,
		ProcessingTasks:   processingTasks,
		SuccessTasks:      successTasks,
		FailedTasks:       failedTasks,
		TotalWorkers:      poolStats.TotalWorkers,
		ActiveWorkers:     poolStats.ActiveWorkers,
		ProcessedTasks:    poolStats.ProcessedTasks,
		FailedTasksCount:  poolStats.FailedTasks,
		Uptime:            poolStats.Uptime,
		StreamInfo:        streamInfo,
		ConcurrencyLimits: stm.GetConcurrencyLimits(),
	}

	// 更新缓存
	stm.statsMu.Lock()
	stm.statsCache = stats
	stm.lastStatsUpdate = time.Now()
	stm.statsMu.Unlock()

	return stats, nil
}

// getTaskStatsCounts 获取任务状态统计计数（高效版本）
func (stm *StreamTaskManager) getTaskStatsCounts(ctx context.Context) (map[string]int64, error) {
	// 使用storage的GetTaskStats方法获取统计信息
	taskStats, err := stm.storage.GetTaskStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get task stats: %w", err)
	}

	// 转换为字符串键的map
	stats := make(map[string]int64)
	stats["pending"] = taskStats[string(models.TaskStatusPending)]
	stats["processing"] = taskStats[string(models.TaskStatusProcessing)]
	stats["success"] = taskStats[string(models.TaskStatusSuccess)]
	stats["failed"] = taskStats[string(models.TaskStatusFailed)]
	stats["canceled"] = taskStats[string(models.TaskStatusCanceled)]
	stats["timeout"] = taskStats[string(models.TaskStatusTimeout)]

	return stats, nil
}

// ManagerStats 管理器统计信息
type ManagerStats struct {
	InstanceID        string            `json:"instance_id"`
	IsRunning         bool              `json:"is_running"`
	PendingTasks      int64             `json:"pending_tasks"`
	ProcessingTasks   int64             `json:"processing_tasks"`
	SuccessTasks      int64             `json:"success_tasks"`
	FailedTasks       int64             `json:"failed_tasks"`
	TotalWorkers      int               `json:"total_workers"`
	ActiveWorkers     int               `json:"active_workers"`
	ProcessedTasks    int64             `json:"processed_tasks"`
	FailedTasksCount  int64             `json:"failed_tasks_count"`
	Uptime            time.Duration     `json:"uptime"`
	StreamInfo        *queue.StreamInfo `json:"stream_info"`
	ConcurrencyLimits map[string]int    `json:"concurrency_limits"`
}

// IsRunning 检查管理器是否正在运行
func (stm *StreamTaskManager) IsRunning() bool {
	stm.mu.RLock()
	defer stm.mu.RUnlock()
	return stm.isRunning
}
