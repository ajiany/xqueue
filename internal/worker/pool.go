package worker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// TaskConsumer 定义任务消费者接口
type TaskConsumer interface {
	ConsumeNextTask() error
}

// WorkerPool 工作池
type WorkerPool struct {
	workerCount   int
	taskConsumer  TaskConsumer
	checkInterval time.Duration
	logger        *logrus.Logger

	// 控制协程
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// 状态管理
	running bool
	mu      sync.RWMutex
}

// NewWorkerPool 创建新的工作池
func NewWorkerPool(workerCount int, taskConsumer TaskConsumer, checkInterval time.Duration, logger *logrus.Logger) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())

	return &WorkerPool{
		workerCount:   workerCount,
		taskConsumer:  taskConsumer,
		checkInterval: checkInterval,
		logger:        logger,
		ctx:           ctx,
		cancel:        cancel,
		running:       false,
	}
}

// Start 启动工作池
func (wp *WorkerPool) Start() error {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if wp.running {
		return fmt.Errorf("worker pool is already running")
	}

	wp.logger.WithFields(logrus.Fields{
		"worker_count":   wp.workerCount,
		"check_interval": wp.checkInterval,
	}).Info("Starting worker pool")

	// 启动多个worker
	for i := 0; i < wp.workerCount; i++ {
		wp.wg.Add(1)
		go wp.runWorker(i)
	}

	wp.running = true
	wp.logger.Info("Worker pool started successfully")

	return nil
}

// Stop 停止工作池
func (wp *WorkerPool) Stop() error {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if !wp.running {
		return fmt.Errorf("worker pool is not running")
	}

	wp.logger.Info("Stopping worker pool...")

	// 取消所有worker
	wp.cancel()

	// 等待所有worker完成
	wp.wg.Wait()

	wp.running = false
	wp.logger.Info("Worker pool stopped successfully")

	return nil
}

// IsRunning 检查工作池是否运行中
func (wp *WorkerPool) IsRunning() bool {
	wp.mu.RLock()
	defer wp.mu.RUnlock()
	return wp.running
}

// GetWorkerCount 获取工作者数量
func (wp *WorkerPool) GetWorkerCount() int {
	return wp.workerCount
}

// runWorker 运行单个worker
func (wp *WorkerPool) runWorker(workerID int) {
	defer wp.wg.Done()

	workerLogger := wp.logger.WithField("worker_id", workerID)
	workerLogger.Info("Worker started")

	ticker := time.NewTicker(wp.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-wp.ctx.Done():
			workerLogger.Info("Worker stopping due to context cancellation")
			return

		case <-ticker.C:
			// 尝试消费任务
			if err := wp.taskConsumer.ConsumeNextTask(); err != nil {
				// 只在非"无任务可用"错误时记录
				if err.Error() != "no pending tasks available" &&
					err.Error() != "failed to acquire task lock" {
					workerLogger.WithError(err).Debug("Failed to consume task")
				}
			}
		}
	}
}

// WorkerPoolStats 工作池统计信息
type WorkerPoolStats struct {
	WorkerCount   int    `json:"worker_count"`
	IsRunning     bool   `json:"is_running"`
	CheckInterval string `json:"check_interval"`
}

// GetStats 获取工作池统计信息
func (wp *WorkerPool) GetStats() WorkerPoolStats {
	wp.mu.RLock()
	defer wp.mu.RUnlock()

	return WorkerPoolStats{
		WorkerCount:   wp.workerCount,
		IsRunning:     wp.running,
		CheckInterval: wp.checkInterval.String(),
	}
}
