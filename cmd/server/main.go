package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"xqueue/internal/api"
	"xqueue/internal/config"
	"xqueue/internal/lock"
	"xqueue/internal/models"
	"xqueue/internal/notifier"
	"xqueue/internal/storage"
	"xqueue/internal/worker"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// EnhancedTaskManager 增强版任务管理器
type EnhancedTaskManager struct {
	storage    *storage.RedisStorage
	notifier   *notifier.MQTTNotifier
	lock       *lock.RedisLock
	logger     *logrus.Logger
	workerPool *worker.WorkerPool

	// 处理器管理
	handlers map[string]models.TaskHandler
	mu       sync.RWMutex

	// 实例标识
	instanceID string
}

// WorkerAdapter 适配器，实现WorkerPool的TaskConsumer接口
type WorkerAdapter struct {
	taskManager *EnhancedTaskManager
}

// ConsumeNextTask 实现worker.TaskConsumer接口
func (wa *WorkerAdapter) ConsumeNextTask() error {
	_, err := wa.taskManager.ConsumeNextTask()
	return err
}

// NewEnhancedTaskManager 创建任务管理器
func NewEnhancedTaskManager(storage *storage.RedisStorage, notifier *notifier.MQTTNotifier,
	redisClient *redis.Client, logger *logrus.Logger) *EnhancedTaskManager {

	taskManager := &EnhancedTaskManager{
		storage:    storage,
		notifier:   notifier,
		lock:       lock.NewRedisLock(redisClient, logger),
		logger:     logger,
		handlers:   make(map[string]models.TaskHandler),
		instanceID: uuid.New().String(),
	}

	// 创建worker池适配器
	workerAdapter := &WorkerAdapter{taskManager: taskManager}
	taskManager.workerPool = worker.NewWorkerPool(3, workerAdapter, time.Second, logger)

	return taskManager
}

// RegisterHandler 注册任务处理器
func (tm *EnhancedTaskManager) RegisterHandler(handler models.TaskHandler) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	taskType := handler.GetType()
	if _, exists := tm.handlers[taskType]; exists {
		return fmt.Errorf("handler for task type '%s' already registered", taskType)
	}

	tm.handlers[taskType] = handler
	tm.logger.WithField("task_type", taskType).Info("Task handler registered")

	return nil
}

// UnregisterHandler 注销任务处理器
func (tm *EnhancedTaskManager) UnregisterHandler(taskType string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if _, exists := tm.handlers[taskType]; !exists {
		return fmt.Errorf("handler for task type '%s' not found", taskType)
	}

	delete(tm.handlers, taskType)
	tm.logger.WithField("task_type", taskType).Info("Task handler unregistered")

	return nil
}

// IsHandlerRegistered 检查处理器是否已注册
func (tm *EnhancedTaskManager) IsHandlerRegistered(taskType string) bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	_, exists := tm.handlers[taskType]
	return exists
}

// GetRegisteredHandlers 获取已注册的处理器列表
func (tm *EnhancedTaskManager) GetRegisteredHandlers() []string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	handlers := make([]string, 0, len(tm.handlers))
	for taskType := range tm.handlers {
		handlers = append(handlers, taskType)
	}

	return handlers
}

// SubmitTask 提交任务 (增强版，包含任务类型验证)
func (tm *EnhancedTaskManager) SubmitTask(taskType string, payload map[string]interface{}, options ...api.TaskOption) (*models.Task, error) {
	// 验证任务类型是否已注册
	if !tm.IsHandlerRegistered(taskType) {
		return nil, fmt.Errorf("task type '%s' is not registered. Please register a handler for this task type first. Registered types: %v",
			taskType, tm.GetRegisteredHandlers())
	}

	task := &models.Task{
		ID:             generateTaskID(),
		Type:           taskType,
		Payload:        payload,
		Status:         models.TaskStatusPending,
		Priority:       0,
		Retry:          0,
		MaxRetry:       3,
		QueueTimeout:   time.Minute * 10,
		ProcessTimeout: time.Minute * 5,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// 应用选项
	for _, option := range options {
		option(task)
	}

	// 保存任务到存储
	if err := tm.storage.SaveTask(context.Background(), task); err != nil {
		return nil, fmt.Errorf("failed to save task: %w", err)
	}

	// 发送创建通知
	if err := tm.notifier.NotifyTaskCreated(task); err != nil {
		tm.logger.WithError(err).Warn("Failed to send task creation notification")
	}

	tm.logger.WithFields(logrus.Fields{
		"task_id":   task.ID,
		"task_type": task.Type,
	}).Info("Task submitted successfully")

	return task, nil
}

// GetTask 获取任务
func (tm *EnhancedTaskManager) GetTask(taskID string) (*models.Task, error) {
	return tm.storage.GetTask(context.Background(), taskID)
}

// GetTasksByStatus 根据状态获取任务
func (tm *EnhancedTaskManager) GetTasksByStatus(status models.TaskStatus, limit, offset int64) ([]*models.Task, error) {
	return tm.storage.GetTasksByStatus(context.Background(), status, limit, offset)
}

// CancelTask 取消任务
func (tm *EnhancedTaskManager) CancelTask(taskID string) error {
	err := tm.storage.UpdateTaskStatus(context.Background(), taskID, models.TaskStatusCanceled, "")
	if err != nil {
		return err
	}

	// 发送取消通知
	if err := tm.notifier.NotifyTaskStatus(taskID, models.TaskStatusCanceled, ""); err != nil {
		tm.logger.WithError(err).Warn("Failed to send task cancellation notification")
	}

	return nil
}

// GetStats 获取统计信息 (增强版，包含worker池信息)
func (tm *EnhancedTaskManager) GetStats() (map[string]interface{}, error) {
	taskStats, err := tm.storage.GetTaskStats(context.Background())
	if err != nil {
		return nil, err
	}

	stats := map[string]interface{}{
		"tasks":       taskStats,
		"worker_pool": tm.workerPool.GetStats(),
		"handlers":    tm.GetRegisteredHandlers(),
		"instance_id": tm.instanceID,
	}

	return stats, nil
}

// ProcessTask 处理单个任务
func (tm *EnhancedTaskManager) ProcessTask(taskID string) error {
	// 获取任务
	task, err := tm.storage.GetTask(context.Background(), taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	// 检查任务状态
	if task.Status != models.TaskStatusPending {
		return fmt.Errorf("task %s is not in pending status, current status: %s", taskID, task.Status)
	}

	// 查找处理器
	tm.mu.RLock()
	handler, exists := tm.handlers[task.Type]
	tm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no handler found for task type: %s", task.Type)
	}

	// 更新任务状态为处理中
	if err := tm.storage.UpdateTaskStatus(context.Background(), taskID, models.TaskStatusProcessing, ""); err != nil {
		return fmt.Errorf("failed to update task status to processing: %w", err)
	}

	// 发送处理开始通知
	if err := tm.notifier.NotifyTaskStatus(taskID, models.TaskStatusProcessing, ""); err != nil {
		tm.logger.WithError(err).Warn("Failed to send task processing notification")
	}

	tm.logger.WithFields(logrus.Fields{
		"task_id":   taskID,
		"task_type": task.Type,
	}).Info("Started processing task")

	// 在 goroutine 中处理任务
	go func() {
		defer func() {
			if r := recover(); r != nil {
				tm.logger.WithFields(logrus.Fields{
					"task_id": taskID,
					"panic":   r,
				}).Error("Task processing panicked")
				tm.storage.UpdateTaskStatus(context.Background(), taskID, models.TaskStatusFailed, fmt.Sprintf("panic: %v", r))
				tm.notifier.NotifyTaskStatus(taskID, models.TaskStatusFailed, fmt.Sprintf("panic: %v", r))
			}
		}()

		// 处理任务
		if err := handler.Handle(task); err != nil {
			tm.logger.WithFields(logrus.Fields{
				"task_id": taskID,
				"error":   err,
			}).Error("Task processing failed")

			// 更新任务状态为失败
			tm.storage.UpdateTaskStatus(context.Background(), taskID, models.TaskStatusFailed, err.Error())
			tm.notifier.NotifyTaskStatus(taskID, models.TaskStatusFailed, err.Error())
		} else {
			tm.logger.WithField("task_id", taskID).Info("Task processing completed successfully")

			// 更新任务状态为成功
			tm.storage.UpdateTaskStatus(context.Background(), taskID, models.TaskStatusSuccess, "")
			tm.notifier.NotifyTaskStatus(taskID, models.TaskStatusSuccess, "")
		}
	}()

	return nil
}

// ConsumeNextTask 消费下一个待处理任务
func (tm *EnhancedTaskManager) ConsumeNextTask() (*models.Task, error) {
	ctx := context.Background()

	// 获取下一个待处理任务
	tasks, err := tm.storage.GetTasksByStatus(ctx, models.TaskStatusPending, 1, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending tasks: %w", err)
	}

	if len(tasks) == 0 {
		return nil, fmt.Errorf("no pending tasks available")
	}

	task := tasks[0]

	// 尝试获取任务锁
	lockKey := fmt.Sprintf("task:%s", task.ID)
	lockValue := fmt.Sprintf("%s:%d", tm.instanceID, time.Now().UnixNano())
	lockExpiration := 5 * time.Minute // 锁过期时间

	acquired, err := tm.lock.AcquireLock(ctx, lockKey, lockValue, lockExpiration)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire task lock: %w", err)
	}

	if !acquired {
		return nil, fmt.Errorf("failed to acquire task lock")
	}

	// 确保在函数结束时释放锁
	defer func() {
		if err := tm.lock.ReleaseLock(ctx, lockKey, lockValue); err != nil {
			tm.logger.WithError(err).WithField("task_id", task.ID).Warn("Failed to release task lock")
		}
	}()

	// 再次检查任务状态（防止在获取锁期间任务状态发生变化）
	task, err = tm.storage.GetTask(ctx, task.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get task after lock: %w", err)
	}

	if task.Status != models.TaskStatusPending {
		return nil, fmt.Errorf("task %s is no longer pending, current status: %s", task.ID, task.Status)
	}

	// 处理任务
	if err := tm.ProcessTask(task.ID); err != nil {
		return nil, err
	}

	return task, nil
}

// StartWorkerPool 启动工作池
func (tm *EnhancedTaskManager) StartWorkerPool() error {
	return tm.workerPool.Start()
}

// StopWorkerPool 停止工作池
func (tm *EnhancedTaskManager) StopWorkerPool() error {
	return tm.workerPool.Stop()
}

// generateTaskID 生成任务ID
func generateTaskID() string {
	return fmt.Sprintf("task_%d", time.Now().UnixNano())
}

// ExampleTaskHandler 示例任务处理器
type ExampleTaskHandler struct {
	logger *logrus.Logger
}

func (h *ExampleTaskHandler) Handle(task *models.Task) error {
	h.logger.WithField("task_id", task.ID).Info("Processing example task")

	// 模拟处理时间
	time.Sleep(time.Second * 2)

	// 模拟处理逻辑
	if message, ok := task.Payload["message"].(string); ok {
		h.logger.WithFields(logrus.Fields{
			"task_id": task.ID,
			"message": message,
		}).Info("Example task processed")
	}

	return nil
}

func (h *ExampleTaskHandler) GetType() string {
	return "example"
}

// EmailTaskHandler 邮件任务处理器示例
type EmailTaskHandler struct {
	logger *logrus.Logger
}

func (h *EmailTaskHandler) Handle(task *models.Task) error {
	h.logger.WithField("task_id", task.ID).Info("Processing email task")

	// 模拟邮件发送
	time.Sleep(time.Second * 1)

	if to, ok := task.Payload["to"].(string); ok {
		if subject, ok := task.Payload["subject"].(string); ok {
			h.logger.WithFields(logrus.Fields{
				"task_id": task.ID,
				"to":      to,
				"subject": subject,
			}).Info("Email sent successfully")
		}
	}

	return nil
}

func (h *EmailTaskHandler) GetType() string {
	return "email"
}

func main() {
	// 创建日志器
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)
	logger.SetFormatter(&logrus.JSONFormatter{})

	logger.Info("Starting XQueue Server v0.0.2")

	// 加载配置
	cfg := config.DefaultConfig()

	// 创建 Redis 客户端
	redisClient := redis.NewClient(&redis.Options{
		Addr:         cfg.Redis.Addr,
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		PoolSize:     cfg.Redis.PoolSize,
		MinIdleConns: cfg.Redis.MinIdleConns,
	})

	// 测试 Redis 连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.WithError(err).Fatal("Failed to connect to Redis")
	}
	logger.Info("Connected to Redis successfully")

	// 创建存储
	storage := storage.NewRedisStorage(redisClient)

	// 创建 MQTT 通知器
	mqttNotifier, err := notifier.NewMQTTNotifier(&cfg.MQTT, logger)
	if err != nil {
		logger.WithError(err).Fatal("Failed to create MQTT notifier")
	}

	// 连接 MQTT
	if err := mqttNotifier.Connect(); err != nil {
		logger.WithError(err).Warn("Failed to connect to MQTT broker")
	} else {
		logger.Info("Connected to MQTT broker successfully")
	}

	// 创建增强版任务管理器
	taskManager := NewEnhancedTaskManager(storage, mqttNotifier, redisClient, logger)

	// 注册任务处理器
	exampleHandler := &ExampleTaskHandler{logger: logger}
	if err := taskManager.RegisterHandler(exampleHandler); err != nil {
		logger.WithError(err).Fatal("Failed to register example handler")
	}

	emailHandler := &EmailTaskHandler{logger: logger}
	if err := taskManager.RegisterHandler(emailHandler); err != nil {
		logger.WithError(err).Fatal("Failed to register email handler")
	}

	// 启动工作池
	if err := taskManager.StartWorkerPool(); err != nil {
		logger.WithError(err).Fatal("Failed to start worker pool")
	}

	// 设置 Gin 模式
	gin.SetMode(cfg.Server.Mode)

	// 创建 HTTP 服务器
	router := gin.New()
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	// 设置 CORS
	router.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// 设置API路由
	apiHandler := api.NewHandler(taskManager, logger)
	apiHandler.SetupRoutes(router)

	// 启动HTTP服务器
	server := &http.Server{
		Addr:    ":" + cfg.Server.Port,
		Handler: router,
	}

	// 在 goroutine 中启动服务器
	go func() {
		logger.WithField("port", cfg.Server.Port).Info("Starting HTTP server")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.WithError(err).Fatal("Failed to start HTTP server")
		}
	}()

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	// 优雅关闭
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 停止工作池
	if err := taskManager.StopWorkerPool(); err != nil {
		logger.WithError(err).Error("Failed to stop worker pool")
	}

	if err := server.Shutdown(ctx); err != nil {
		logger.WithError(err).Error("Server forced to shutdown")
	}

	// 关闭连接
	mqttNotifier.Disconnect()
	redisClient.Close()

	logger.Info("Server exited")
}
