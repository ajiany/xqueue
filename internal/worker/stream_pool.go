package worker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"xqueue/internal/concurrency"
	"xqueue/internal/models"
	"xqueue/internal/queue"

	"github.com/sirupsen/logrus"
)

// StreamWorkerPool 基于Redis Stream的Worker Pool
// v0.0.5 新增：使用Stream消费者组机制，彻底解决worker竞争问题
type StreamWorkerPool struct {
	streamQueue    *queue.StreamTaskQueue
	concurrencyMgr *concurrency.ConcurrencyManager
	logger         *logrus.Logger

	// 配置
	workerCount int
	instanceID  string

	// 运行时状态
	workers []*StreamWorker
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.RWMutex

	// 统计信息
	stats *PoolStats

	// 任务处理器
	taskProcessor TaskProcessor
}

// StreamWorker Stream消费者Worker
type StreamWorker struct {
	id     string
	pool   *StreamWorkerPool
	logger *logrus.Entry

	// 运行时状态
	ctx    context.Context
	cancel context.CancelFunc

	// 统计信息
	processedTasks int64
	failedTasks    int64
	lastActiveTime time.Time
}

// TaskProcessor 任务处理器接口
type TaskProcessor interface {
	ProcessTask(task *models.Task, messageID string) error
}

// PoolStats Worker Pool统计信息
type PoolStats struct {
	mu             sync.RWMutex
	TotalWorkers   int           `json:"total_workers"`
	ActiveWorkers  int           `json:"active_workers"`
	ProcessedTasks int64         `json:"processed_tasks"`
	FailedTasks    int64         `json:"failed_tasks"`
	Uptime         time.Duration `json:"uptime"`
	StartTime      time.Time     `json:"start_time"`
}

// NewStreamWorkerPool 创建新的Stream Worker Pool
func NewStreamWorkerPool(
	streamQueue *queue.StreamTaskQueue,
	concurrencyMgr *concurrency.ConcurrencyManager,
	taskProcessor TaskProcessor,
	workerCount int,
	instanceID string,
	logger *logrus.Logger,
) *StreamWorkerPool {
	return &StreamWorkerPool{
		streamQueue:    streamQueue,
		concurrencyMgr: concurrencyMgr,
		taskProcessor:  taskProcessor,
		workerCount:    workerCount,
		instanceID:     instanceID,
		logger:         logger,
		stats: &PoolStats{
			TotalWorkers: workerCount,
			StartTime:    time.Now(),
		},
	}
}

// Start 启动Worker Pool
func (swp *StreamWorkerPool) Start() error {
	swp.mu.Lock()
	defer swp.mu.Unlock()

	if swp.ctx != nil {
		return fmt.Errorf("worker pool already started")
	}

	// 创建上下文
	swp.ctx, swp.cancel = context.WithCancel(context.Background())

	// 初始化Stream队列
	if err := swp.streamQueue.Initialize(swp.ctx); err != nil {
		return fmt.Errorf("failed to initialize stream queue: %w", err)
	}

	// 创建并启动workers
	swp.workers = make([]*StreamWorker, swp.workerCount)
	for i := 0; i < swp.workerCount; i++ {
		worker := swp.createWorker(i)
		swp.workers[i] = worker

		swp.wg.Add(1)
		go worker.run()
	}

	swp.logger.WithFields(logrus.Fields{
		"worker_count": swp.workerCount,
		"instance_id":  swp.instanceID,
	}).Info("Stream worker pool started")

	return nil
}

// Stop 停止Worker Pool
func (swp *StreamWorkerPool) Stop() error {
	swp.mu.Lock()
	defer swp.mu.Unlock()

	if swp.cancel == nil {
		return fmt.Errorf("worker pool not started")
	}

	swp.logger.Info("Stopping stream worker pool...")

	// 取消所有worker
	swp.cancel()

	// 等待所有worker完成
	done := make(chan struct{})
	go func() {
		swp.wg.Wait()
		close(done)
	}()

	// 等待最多30秒
	select {
	case <-done:
		swp.logger.Info("All workers stopped gracefully")
	case <-time.After(30 * time.Second):
		swp.logger.Warn("Timeout waiting for workers to stop")
	}

	// 清理资源
	swp.ctx = nil
	swp.cancel = nil
	swp.workers = nil

	return nil
}

// createWorker 创建新的Worker
func (swp *StreamWorkerPool) createWorker(index int) *StreamWorker {
	workerID := fmt.Sprintf("%s-worker-%d", swp.instanceID, index)

	return &StreamWorker{
		id:             workerID,
		pool:           swp,
		logger:         swp.logger.WithField("worker_id", workerID),
		lastActiveTime: time.Now(),
	}
}

// run Worker运行循环
func (sw *StreamWorker) run() {
	defer sw.pool.wg.Done()

	sw.logger.Info("Stream worker started")
	defer sw.logger.Info("Stream worker stopped")

	for {
		select {
		case <-sw.pool.ctx.Done():
			return
		default:
			sw.processNextTask()
		}
	}
}

// processNextTask 处理下一个任务
func (sw *StreamWorker) processNextTask() {
	ctx := sw.pool.ctx

	// 从Stream消费任务
	task, messageID, err := sw.pool.streamQueue.ConsumeTask(ctx, sw.id)
	if err != nil {
		// 没有任务可处理，短暂休眠
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
			return
		}
	}

	sw.lastActiveTime = time.Now()

	// 获取并发控制令牌
	var token string
	var tokenAcquired bool

	// 重试获取令牌直到成功
	backoffTime := 200 * time.Millisecond
	for !tokenAcquired {
		var err error
		token, err = sw.pool.concurrencyMgr.AcquireToken(ctx, task.Type)
		if err == nil {
			tokenAcquired = true
			break
		}

		limit, _ := sw.pool.concurrencyMgr.GetConcurrencyLimit(task.Type)
		sw.logger.WithFields(logrus.Fields{
			"task_id":   task.ID,
			"task_type": task.Type,
			"error":     err,
			"limit":     limit,
		}).Debug("Failed to acquire concurrency token, will retry")

		// 使用指数退避重试
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoffTime):
			// 指数退避，但最大不超过5秒
			backoffTime = backoffTime * 2
			if backoffTime > 5*time.Second {
				backoffTime = 5 * time.Second
			}
		}
	}

	// 确保释放令牌
	defer func() {
		if tokenAcquired && token != "" {
			if err := sw.pool.concurrencyMgr.ReleaseToken(ctx, task.Type, token); err != nil {
				sw.logger.WithError(err).Warn("Failed to release concurrency token")
			}
		}
	}()

	// 处理任务
	if err := sw.pool.taskProcessor.ProcessTask(task, messageID); err != nil {
		sw.logger.WithFields(logrus.Fields{
			"task_id": task.ID,
			"error":   err,
		}).Error("Task processing failed")
		sw.failedTasks++
		sw.pool.updateStats(0, 1)
	} else {
		sw.logger.WithField("task_id", task.ID).Info("Task processing completed")
		sw.processedTasks++
		sw.pool.updateStats(1, 0)
	}

	// 确认消息处理完成
	if err := sw.pool.streamQueue.AckTask(ctx, messageID); err != nil {
		sw.logger.WithError(err).Warn("Failed to acknowledge message")
	}
}

// updateStats 更新统计信息
func (swp *StreamWorkerPool) updateStats(processed, failed int64) {
	swp.stats.mu.Lock()
	defer swp.stats.mu.Unlock()

	swp.stats.ProcessedTasks += processed
	swp.stats.FailedTasks += failed
	swp.stats.Uptime = time.Since(swp.stats.StartTime)
}

// GetStats 获取统计信息
func (swp *StreamWorkerPool) GetStats() *PoolStats {
	swp.stats.mu.RLock()
	defer swp.stats.mu.RUnlock()

	// 计算活跃worker数量
	activeWorkers := 0
	swp.mu.RLock()
	for _, worker := range swp.workers {
		if time.Since(worker.lastActiveTime) < time.Minute {
			activeWorkers++
		}
	}
	swp.mu.RUnlock()

	stats := *swp.stats
	stats.ActiveWorkers = activeWorkers
	stats.Uptime = time.Since(stats.StartTime)

	return &stats
}

// GetWorkerInfo 获取Worker信息
func (swp *StreamWorkerPool) GetWorkerInfo() []WorkerInfo {
	swp.mu.RLock()
	defer swp.mu.RUnlock()

	workers := make([]WorkerInfo, len(swp.workers))
	for i, worker := range swp.workers {
		workers[i] = WorkerInfo{
			ID:             worker.id,
			ProcessedTasks: worker.processedTasks,
			FailedTasks:    worker.failedTasks,
			LastActiveTime: worker.lastActiveTime,
			IsActive:       time.Since(worker.lastActiveTime) < time.Minute,
		}
	}

	return workers
}

// WorkerInfo Worker信息
type WorkerInfo struct {
	ID             string    `json:"id"`
	ProcessedTasks int64     `json:"processed_tasks"`
	FailedTasks    int64     `json:"failed_tasks"`
	LastActiveTime time.Time `json:"last_active_time"`
	IsActive       bool      `json:"is_active"`
}

// IsRunning 检查Worker Pool是否正在运行
func (swp *StreamWorkerPool) IsRunning() bool {
	swp.mu.RLock()
	defer swp.mu.RUnlock()
	return swp.ctx != nil
}
