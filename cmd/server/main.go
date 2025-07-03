package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"xqueue/internal/api"
	"xqueue/internal/config"
	"xqueue/internal/manager"
	"xqueue/internal/models"
	"xqueue/internal/notifier"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/sirupsen/logrus"
)

// ExampleTaskHandler 示例任务处理器
type ExampleTaskHandler struct {
	logger *logrus.Logger
}

func (h *ExampleTaskHandler) Handle(task *models.Task) error {
	h.logger.WithField("task_id", task.ID).Info("Processing example task")

	// 从payload中获取duration参数，默认5秒
	duration := 5
	if durationValue, exists := task.Payload["duration"]; exists {
		if durationFloat, ok := durationValue.(float64); ok {
			duration = int(durationFloat)
		} else if durationInt, ok := durationValue.(int); ok {
			duration = durationInt
		}
	}

	// 模拟任务处理
	time.Sleep(time.Duration(duration) * time.Second)

	h.logger.WithField("task_id", task.ID).Info("Example task completed")
	return nil
}

func (h *ExampleTaskHandler) GetType() string {
	return "example"
}

// EmailTaskHandler 邮件任务处理器
type EmailTaskHandler struct {
	logger *logrus.Logger
}

func (h *EmailTaskHandler) Handle(task *models.Task) error {
	h.logger.WithField("task_id", task.ID).Info("Processing email task")

	// 模拟邮件发送
	duration := 1
	if durationValue, exists := task.Payload["duration"]; exists {
		if durationFloat, ok := durationValue.(float64); ok {
			duration = int(durationFloat)
		} else if durationInt, ok := durationValue.(int); ok {
			duration = durationInt
		}
	}
	// 模拟任务处理
	time.Sleep(time.Duration(duration) * time.Second)

	h.logger.WithField("task_id", task.ID).Info("Email task completed")
	return nil
}

func (h *EmailTaskHandler) GetType() string {
	return "email"
}

// MQTTNotifierAdapter MQTT通知器适配器
type MQTTNotifierAdapter struct {
	notifier *notifier.MQTTNotifier
}

func (m *MQTTNotifierAdapter) NotifyTaskStatus(taskID string, message *models.TaskMessage) error {
	return m.notifier.NotifyTaskStatus(taskID, message.Status, message.Error)
}

func main() {
	// 初始化日志
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)
	logger.SetFormatter(&logrus.JSONFormatter{})

	// 加载配置
	cfg := config.DefaultConfig()

	// 创建Redis客户端
	redisClient := redis.NewClient(&redis.Options{
		Addr:         cfg.Redis.Addr,
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		PoolSize:     cfg.Redis.PoolSize,
		MinIdleConns: cfg.Redis.MinIdleConns,
		// 增加超时配置以避免连接池超时
		DialTimeout:  10 * time.Second, // 连接超时
		ReadTimeout:  5 * time.Second,  // 读取超时
		WriteTimeout: 5 * time.Second,  // 写入超时
		PoolTimeout:  10 * time.Second, // 连接池超时
		IdleTimeout:  5 * time.Minute,  // 空闲连接超时
		// 连接检查
		IdleCheckFrequency: 1 * time.Minute,
		MaxRetries:         3, // 最大重试次数
		MinRetryBackoff:    8 * time.Millisecond,
		MaxRetryBackoff:    512 * time.Millisecond,
	})

	// 测试Redis连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	logger.Info("Connected to Redis successfully")

	// 创建MQTT通知器
	var taskNotifier manager.TaskNotifier
	if cfg.MQTT.Broker != "" {
		mqttNotifier, err := notifier.NewMQTTNotifier(&cfg.MQTT, logger)
		if err != nil {
			logger.WithError(err).Warn("Failed to create MQTT notifier, continuing without notifications")
		} else {
			if err := mqttNotifier.Connect(); err != nil {
				logger.WithError(err).Warn("Failed to connect to MQTT broker, continuing without notifications")
			} else {
				taskNotifier = &MQTTNotifierAdapter{notifier: mqttNotifier}
				logger.Info("MQTT notifier connected successfully")
			}
		}
	}

	// 创建Stream任务管理器 (v0.0.5 新增)
	workerCount := 10 // 增加worker数量以充分利用Stream的并发能力
	streamTaskManager := manager.NewStreamTaskManager(
		redisClient,
		taskNotifier,
		workerCount,
		logger,
	)

	// 注册任务处理器
	exampleHandler := &ExampleTaskHandler{logger: logger}
	if err := streamTaskManager.RegisterHandler(exampleHandler); err != nil {
		log.Fatalf("Failed to register example handler: %v", err)
	}

	emailHandler := &EmailTaskHandler{logger: logger}
	if err := streamTaskManager.RegisterHandler(emailHandler); err != nil {
		log.Fatalf("Failed to register email handler: %v", err)
	}

	// 设置并发限制
	if err := streamTaskManager.SetConcurrencyLimit("example", 3); err != nil {
		log.Fatalf("Failed to set concurrency limit for example tasks: %v", err)
	}

	if err := streamTaskManager.SetConcurrencyLimit("email", 5); err != nil {
		log.Fatalf("Failed to set concurrency limit for email tasks: %v", err)
	}

	logger.WithFields(logrus.Fields{
		"example_concurrency": 3,
		"email_concurrency":   5,
		"worker_count":        workerCount,
	}).Info("Concurrency limits configured")

	// 启动任务管理器
	if err := streamTaskManager.Start(); err != nil {
		log.Fatalf("Failed to start stream task manager: %v", err)
	}
	logger.Info("Stream task manager started successfully")

	// 创建HTTP处理器 - 需要适配新的接口
	httpHandler := api.NewStreamHandler(streamTaskManager, logger)

	// 设置Gin路由
	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.Default()

	// 添加CORS中间件
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

	// API路由
	v1 := router.Group("/api/v1")
	{
		// 任务管理
		v1.POST("/tasks", httpHandler.SubmitTask)
		v1.GET("/tasks", httpHandler.GetTasks)
		v1.GET("/tasks/:id", httpHandler.GetTask)
		v1.DELETE("/tasks/:id", httpHandler.CancelTask)

		// 系统信息
		v1.GET("/stats", httpHandler.GetStats)
		v1.GET("/health", httpHandler.HealthCheck)
		v1.GET("/handlers", httpHandler.GetHandlers)

		// 并发控制 (v0.0.3+)
		v1.GET("/concurrency", httpHandler.GetConcurrency)
		v1.POST("/concurrency", httpHandler.SetConcurrency)

		// Stream信息 (v0.0.5 新增)
		v1.GET("/stream", httpHandler.GetStreamInfo)
		v1.GET("/workers", httpHandler.GetWorkerInfo)
	}

	// 创建HTTP服务器
	server := &http.Server{
		Addr:    ":" + cfg.Server.Port,
		Handler: router,
	}

	// 启动服务器
	go func() {
		logger.WithFields(logrus.Fields{
			"port":    cfg.Server.Port,
			"mode":    cfg.Server.Mode,
			"version": "v0.0.5-stream",
		}).Info("Starting XQueue server")

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
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

	// 停止任务管理器
	if err := streamTaskManager.Stop(); err != nil {
		logger.WithError(err).Error("Error stopping stream task manager")
	}

	// 停止HTTP服务器
	if err := server.Shutdown(ctx); err != nil {
		logger.WithError(err).Error("Error shutting down server")
	}

	// 关闭MQTT连接
	if taskNotifier != nil {
		if adapter, ok := taskNotifier.(*MQTTNotifierAdapter); ok {
			adapter.notifier.Disconnect()
		}
	}

	// 关闭Redis连接
	if err := redisClient.Close(); err != nil {
		logger.WithError(err).Error("Error closing Redis connection")
	}

	logger.Info("Server shutdown completed")
}
