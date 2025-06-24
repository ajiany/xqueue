package storage

import (
	"context"
	"fmt"
	"time"

	"xqueue/internal/models"

	"github.com/go-redis/redis/v8"
)

// RedisStorage Redis存储实现
type RedisStorage struct {
	client *redis.Client
}

// NewRedisStorage 创建Redis存储实例
func NewRedisStorage(client *redis.Client) *RedisStorage {
	return &RedisStorage{
		client: client,
	}
}

// SaveTask 保存任务状态
func (r *RedisStorage) SaveTask(ctx context.Context, task *models.Task) error {
	data, err := task.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	// 使用 Hash 存储任务详情
	taskKey := fmt.Sprintf("task:%s", task.ID)
	err = r.client.HMSet(ctx, taskKey, map[string]interface{}{
		"data":       string(data),
		"status":     string(task.Status),
		"type":       task.Type,
		"created_at": task.CreatedAt.Unix(),
		"updated_at": task.UpdatedAt.Unix(),
	}).Err()

	if err != nil {
		return fmt.Errorf("failed to save task: %w", err)
	}

	// 设置任务过期时间
	if task.QueueTimeout > 0 {
		err = r.client.Expire(ctx, taskKey, task.QueueTimeout).Err()
		if err != nil {
			return fmt.Errorf("failed to set task expiration: %w", err)
		}
	}

	// 添加到状态索引
	statusKey := fmt.Sprintf("tasks:status:%s", task.Status)
	err = r.client.ZAdd(ctx, statusKey, &redis.Z{
		Score:  float64(task.UpdatedAt.Unix()),
		Member: task.ID,
	}).Err()

	if err != nil {
		return fmt.Errorf("failed to add task to status index: %w", err)
	}

	// 添加到类型索引
	typeKey := fmt.Sprintf("tasks:type:%s", task.Type)
	err = r.client.ZAdd(ctx, typeKey, &redis.Z{
		Score:  float64(task.UpdatedAt.Unix()),
		Member: task.ID,
	}).Err()

	if err != nil {
		return fmt.Errorf("failed to add task to type index: %w", err)
	}

	return nil
}

// GetTask 获取任务
func (r *RedisStorage) GetTask(ctx context.Context, taskID string) (*models.Task, error) {
	taskKey := fmt.Sprintf("task:%s", taskID)

	data, err := r.client.HGet(ctx, taskKey, "data").Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("task not found: %s", taskID)
		}
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	task, err := models.FromJSON([]byte(data))
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal task: %w", err)
	}

	return task, nil
}

// UpdateTaskStatus 更新任务状态
func (r *RedisStorage) UpdateTaskStatus(ctx context.Context, taskID string, status models.TaskStatus, errorMsg string) error {
	task, err := r.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	// 从旧状态索引中移除
	oldStatusKey := fmt.Sprintf("tasks:status:%s", task.Status)
	err = r.client.ZRem(ctx, oldStatusKey, taskID).Err()
	if err != nil {
		return fmt.Errorf("failed to remove from old status index: %w", err)
	}

	// 更新任务状态
	task.Status = status
	task.UpdatedAt = time.Now()
	if errorMsg != "" {
		task.Error = errorMsg
	}

	if status == models.TaskStatusProcessing && task.StartedAt == nil {
		now := time.Now()
		task.StartedAt = &now
	}

	if status == models.TaskStatusSuccess || status == models.TaskStatusFailed ||
		status == models.TaskStatusCanceled || status == models.TaskStatusTimeout {
		if task.CompletedAt == nil {
			now := time.Now()
			task.CompletedAt = &now
		}
	}

	// 保存更新后的任务
	return r.SaveTask(ctx, task)
}

// GetTasksByStatus 根据状态获取任务列表
func (r *RedisStorage) GetTasksByStatus(ctx context.Context, status models.TaskStatus, limit int64, offset int64) ([]*models.Task, error) {
	statusKey := fmt.Sprintf("tasks:status:%s", status)

	// 从最新到最旧排序
	taskIDs, err := r.client.ZRevRange(ctx, statusKey, offset, offset+limit-1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get task IDs by status: %w", err)
	}

	tasks := make([]*models.Task, 0, len(taskIDs))
	for _, taskID := range taskIDs {
		task, err := r.GetTask(ctx, taskID)
		if err != nil {
			continue // 跳过获取失败的任务
		}
		tasks = append(tasks, task)
	}

	return tasks, nil
}

// DeleteTask 删除任务
func (r *RedisStorage) DeleteTask(ctx context.Context, taskID string) error {
	task, err := r.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	// 删除任务数据
	taskKey := fmt.Sprintf("task:%s", taskID)
	err = r.client.Del(ctx, taskKey).Err()
	if err != nil {
		return fmt.Errorf("failed to delete task: %w", err)
	}

	// 从索引中移除
	statusKey := fmt.Sprintf("tasks:status:%s", task.Status)
	err = r.client.ZRem(ctx, statusKey, taskID).Err()
	if err != nil {
		return fmt.Errorf("failed to remove from status index: %w", err)
	}

	typeKey := fmt.Sprintf("tasks:type:%s", task.Type)
	err = r.client.ZRem(ctx, typeKey, taskID).Err()
	if err != nil {
		return fmt.Errorf("failed to remove from type index: %w", err)
	}

	return nil
}

// CleanupExpiredTasks 清理过期任务
func (r *RedisStorage) CleanupExpiredTasks(ctx context.Context) error {
	// 获取所有待处理任务
	pendingTasks, err := r.GetTasksByStatus(ctx, models.TaskStatusPending, 1000, 0)
	if err != nil {
		return fmt.Errorf("failed to get pending tasks: %w", err)
	}

	for _, task := range pendingTasks {
		if task.IsExpired() {
			err = r.UpdateTaskStatus(ctx, task.ID, models.TaskStatusTimeout, "Queue timeout")
			if err != nil {
				continue // 继续处理其他任务
			}
		}
	}

	return nil
}

// GetTaskStats 获取任务统计信息
func (r *RedisStorage) GetTaskStats(ctx context.Context) (map[string]int64, error) {
	stats := make(map[string]int64)

	statuses := []models.TaskStatus{
		models.TaskStatusPending,
		models.TaskStatusProcessing,
		models.TaskStatusSuccess,
		models.TaskStatusFailed,
		models.TaskStatusCanceled,
		models.TaskStatusTimeout,
	}

	for _, status := range statuses {
		statusKey := fmt.Sprintf("tasks:status:%s", status)
		count, err := r.client.ZCard(ctx, statusKey).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to get status count: %w", err)
		}
		stats[string(status)] = count
	}

	return stats, nil
}
