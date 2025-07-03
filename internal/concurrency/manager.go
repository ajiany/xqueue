package concurrency

import (
	"context"
	"fmt"
	"sync"
	"time"

	"xqueue/internal/semaphore"

	"github.com/go-redis/redis/v8"
	"github.com/sirupsen/logrus"
)

// ConcurrencyManager 并发控制管理器
type ConcurrencyManager struct {
	redisClient *redis.Client
	logger      *logrus.Logger

	// 并发限制配置
	limits map[string]int // 任务类型 -> 最大并发数
	mu     sync.RWMutex

	// 信号量缓存
	semaphores map[string]*semaphore.RedisSemaphore
	semMu      sync.RWMutex

	// 配置
	semaphoreExpiration time.Duration
}

// NewConcurrencyManager 创建并发控制管理器
func NewConcurrencyManager(redisClient *redis.Client, logger *logrus.Logger, semaphoreExpiration time.Duration) *ConcurrencyManager {
	cm := &ConcurrencyManager{
		redisClient:         redisClient,
		logger:              logger,
		limits:              make(map[string]int),
		semaphores:          make(map[string]*semaphore.RedisSemaphore),
		semaphoreExpiration: semaphoreExpiration,
	}

	// 启动时清理可能泄漏的令牌
	go cm.cleanupLeakedTokens()

	return cm
}

// SetConcurrencyLimit 设置任务类型的并发限制
func (cm *ConcurrencyManager) SetConcurrencyLimit(taskType string, maxConcurrency int) error {
	if maxConcurrency <= 0 {
		return fmt.Errorf("max concurrency must be greater than 0")
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	// 更新配置
	oldLimit := cm.limits[taskType]
	cm.limits[taskType] = maxConcurrency

	// 如果限制发生变化，需要重新创建信号量
	if oldLimit != maxConcurrency {
		cm.semMu.Lock()
		delete(cm.semaphores, taskType) // 删除旧的信号量，让其重新创建
		cm.semMu.Unlock()
	}

	cm.logger.WithFields(logrus.Fields{
		"task_type":       taskType,
		"max_concurrency": maxConcurrency,
		"old_limit":       oldLimit,
	}).Info("Updated concurrency limit for task type")

	return nil
}

// GetConcurrencyLimit 获取任务类型的并发限制
func (cm *ConcurrencyManager) GetConcurrencyLimit(taskType string) (int, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	limit, exists := cm.limits[taskType]
	return limit, exists
}

// AcquireToken 为任务类型获取并发令牌
func (cm *ConcurrencyManager) AcquireToken(ctx context.Context, taskType string) (string, error) {
	// 检查是否有并发限制
	limit, exists := cm.GetConcurrencyLimit(taskType)
	if !exists {
		// 没有并发限制，直接返回空令牌
		return "", nil
	}

	// 获取或创建信号量
	sem := cm.getSemaphore(taskType, limit)

	// 获取令牌
	token, err := sem.Acquire(ctx)
	if err != nil {
		cm.logger.WithFields(logrus.Fields{
			"task_type": taskType,
			"limit":     limit,
			"error":     err,
		}).Warn("Failed to acquire concurrency token")
		return "", fmt.Errorf("failed to acquire concurrency token for task type %s: %w", taskType, err)
	}

	cm.logger.WithFields(logrus.Fields{
		"task_type": taskType,
		"token":     token,
		"limit":     limit,
	}).Debug("Acquired concurrency token")

	return token, nil
}

// ReleaseToken 释放并发令牌
func (cm *ConcurrencyManager) ReleaseToken(ctx context.Context, taskType string, token string) error {
	// 如果没有令牌，说明没有并发限制，直接返回
	if token == "" {
		return nil
	}

	// 获取信号量
	limit, exists := cm.GetConcurrencyLimit(taskType)
	if !exists {
		// 并发限制已被移除，但令牌可能还存在，尝试清理
		cm.logger.WithFields(logrus.Fields{
			"task_type": taskType,
			"token":     token,
		}).Warn("Concurrency limit removed but token still exists")
		return nil
	}

	sem := cm.getSemaphore(taskType, limit)

	// 释放令牌
	err := sem.Release(ctx, token)
	if err != nil {
		cm.logger.WithFields(logrus.Fields{
			"task_type": taskType,
			"token":     token,
			"error":     err,
		}).Warn("Failed to release concurrency token")
		return fmt.Errorf("failed to release concurrency token for task type %s: %w", taskType, err)
	}

	cm.logger.WithFields(logrus.Fields{
		"task_type": taskType,
		"token":     token,
	}).Debug("Released concurrency token")

	return nil
}

// getSemaphore 获取或创建信号量
func (cm *ConcurrencyManager) getSemaphore(taskType string, limit int) *semaphore.RedisSemaphore {
	cm.semMu.RLock()
	sem, exists := cm.semaphores[taskType]
	cm.semMu.RUnlock()

	if exists {
		return sem
	}

	// 创建新的信号量
	cm.semMu.Lock()
	defer cm.semMu.Unlock()

	// 双重检查
	if sem, exists := cm.semaphores[taskType]; exists {
		return sem
	}

	// 创建信号量
	semaphoreKey := fmt.Sprintf("concurrency:%s", taskType)
	sem = semaphore.NewRedisSemaphore(cm.redisClient, semaphoreKey, int64(limit), cm.semaphoreExpiration)
	cm.semaphores[taskType] = sem

	cm.logger.WithFields(logrus.Fields{
		"task_type":     taskType,
		"limit":         limit,
		"semaphore_key": semaphoreKey,
		"expiration":    cm.semaphoreExpiration,
	}).Info("Created new semaphore for task type")

	return sem
}

// GetAllLimits 获取所有并发限制配置
func (cm *ConcurrencyManager) GetAllLimits() map[string]int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	result := make(map[string]int)
	for taskType, limit := range cm.limits {
		result[taskType] = limit
	}

	return result
}

// cleanupLeakedTokens 清理可能泄漏的令牌
func (cm *ConcurrencyManager) cleanupLeakedTokens() {
	ctx := context.Background()

	// 等待一小段时间让系统启动完成
	time.Sleep(5 * time.Second)

	// 扫描所有可能的信号量键
	pattern := "concurrency:*"
	keys, err := cm.redisClient.Keys(ctx, pattern).Result()
	if err != nil {
		cm.logger.WithError(err).Warn("Failed to scan for semaphore keys during cleanup")
		return
	}

	cleanedCount := 0
	for _, key := range keys {
		// 清理过期令牌
		now := time.Now().Unix()
		expiredTime := now - int64(cm.semaphoreExpiration.Seconds())

		removed, err := cm.redisClient.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", expiredTime)).Result()
		if err != nil {
			cm.logger.WithFields(logrus.Fields{
				"key":   key,
				"error": err,
			}).Warn("Failed to cleanup tokens for key")
			continue
		}

		if removed > 0 {
			cleanedCount += int(removed)
			cm.logger.WithFields(logrus.Fields{
				"key":     key,
				"removed": removed,
			}).Info("Cleaned up leaked tokens")
		}
	}

	if cleanedCount > 0 {
		cm.logger.WithField("total_cleaned", cleanedCount).Info("Token cleanup completed")
	} else {
		cm.logger.Debug("No leaked tokens found during startup cleanup")
	}
}
