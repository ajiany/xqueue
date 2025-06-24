package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"xqueue/internal/api"
	"xqueue/internal/config"
	"xqueue/internal/models"
	"xqueue/internal/notifier"
	"xqueue/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/sirupsen/logrus"
)

// SimpleTaskManager 简化版任务管理器
type SimpleTaskManager struct {
	storage  *storage.RedisStorage
	notifier *notifier.MQTTNotifier
	logger   *logrus.Logger
	handlers map[string]models.TaskHandler
}

// NewSimpleTaskManager 创建简化版任务管理器
func NewSimpleTaskManager(storage *storage.RedisStorage, notifier *notifier.MQTTNotifier, logger *logrus.Logger) *SimpleTaskManager {
	return &SimpleTaskManager{
		storage:  storage,
		notifier: notifier,
		logger:   logger,
		handlers: make(map[string]models.TaskHandler),
	}
}

// SubmitTask 提交任务
func (tm *SimpleTaskManager) SubmitTask(taskType string, payload map[string]interface{}, options ...api.TaskOption) (*models.Task, error) {
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
func (tm *SimpleTaskManager) GetTask(taskID string) (*models.Task, error) {
	return tm.storage.GetTask(context.Background(), taskID)
}

// GetTasksByStatus 根据状态获取任务
func (tm *SimpleTaskManager) GetTasksByStatus(status models.TaskStatus, limit, offset int64) ([]*models.Task, error) {
	return tm.storage.GetTasksByStatus(context.Background(), status, limit, offset)
}

// CancelTask 取消任务
func (tm *SimpleTaskManager) CancelTask(taskID string) error {
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

// GetStats 获取统计信息
func (tm *SimpleTaskManager) GetStats() (map[string]interface{}, error) {
	taskStats, err := tm.storage.GetTaskStats(context.Background())
	if err != nil {
		return nil, err
	}

	stats := map[string]interface{}{
		"tasks": taskStats,
	}

	return stats, nil
}

// ProcessTask 处理单个任务
func (tm *SimpleTaskManager) ProcessTask(taskID string) error {
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
	handler, exists := tm.handlers[task.Type]
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
func (tm *SimpleTaskManager) ConsumeNextTask() (*models.Task, error) {
	// 获取下一个待处理任务
	tasks, err := tm.storage.GetTasksByStatus(context.Background(), models.TaskStatusPending, 1, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending tasks: %w", err)
	}

	if len(tasks) == 0 {
		return nil, fmt.Errorf("no pending tasks available")
	}

	task := tasks[0]

	// 处理任务
	if err := tm.ProcessTask(task.ID); err != nil {
		return nil, err
	}

	return task, nil
}

// StartTaskConsumer 启动任务消费者
func (tm *SimpleTaskManager) StartTaskConsumer(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_, err := tm.ConsumeNextTask()
				if err != nil {
					if err.Error() != "no pending tasks available" {
						tm.logger.WithError(err).Debug("Failed to consume task")
					}
				}
			}
		}
	}()
	tm.logger.WithField("interval", interval).Info("Task consumer started")
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

func main() {
	// 创建日志器
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)
	logger.SetFormatter(&logrus.JSONFormatter{})

	logger.Info("Starting XQueue Server")

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

	// 创建任务管理器
	taskManager := NewSimpleTaskManager(storage, mqttNotifier, logger)

	// 注册示例任务处理器
	exampleHandler := &ExampleTaskHandler{logger: logger}
	taskManager.handlers[exampleHandler.GetType()] = exampleHandler

	// 启动任务消费者（每5秒检查一次）
	taskManager.StartTaskConsumer(5 * time.Second)

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

	if err := server.Shutdown(ctx); err != nil {
		logger.WithError(err).Error("Server forced to shutdown")
	}

	// 关闭连接
	mqttNotifier.Disconnect()
	redisClient.Close()

	logger.Info("Server exited")
}
