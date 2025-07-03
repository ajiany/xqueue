package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"xqueue/internal/models"

	"github.com/go-redis/redis/v8"
	"github.com/sirupsen/logrus"
)

// StreamTaskQueue 基于Redis Stream的任务队列实现
// v0.0.5 新增：使用Redis Stream消费者组机制，彻底解决worker竞争问题
type StreamTaskQueue struct {
	client    *redis.Client
	logger    *logrus.Logger
	streamKey string
	groupName string

	// 配置参数
	maxLen       int64         // Stream最大长度
	blockTimeout time.Duration // 阻塞读取超时时间
	claimMinIdle time.Duration // 消息声明最小空闲时间
	batchSize    int64         // 批量读取大小
}

// NewStreamTaskQueue 创建新的Stream任务队列
func NewStreamTaskQueue(client *redis.Client, logger *logrus.Logger) *StreamTaskQueue {
	return &StreamTaskQueue{
		client:       client,
		logger:       logger,
		streamKey:    "xqueue:tasks",
		groupName:    "task-workers",
		maxLen:       10000,
		blockTimeout: 5 * time.Second,
		claimMinIdle: 60 * time.Second,
		batchSize:    1,
	}
}

// Initialize 初始化Stream和消费者组
func (stq *StreamTaskQueue) Initialize(ctx context.Context) error {
	// 创建消费者组（如果不存在）
	err := stq.client.XGroupCreateMkStream(ctx, stq.streamKey, stq.groupName, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return fmt.Errorf("failed to create consumer group: %w", err)
	}

	stq.logger.WithFields(logrus.Fields{
		"stream_key": stq.streamKey,
		"group_name": stq.groupName,
	}).Info("Stream task queue initialized")

	return nil
}

// AddTask 添加任务到Stream
func (stq *StreamTaskQueue) AddTask(ctx context.Context, task *models.Task) error {
	// 序列化任务数据
	payloadJSON, err := json.Marshal(task.Payload)
	if err != nil {
		return fmt.Errorf("failed to marshal task payload: %w", err)
	}

	// 构建Stream消息
	values := map[string]interface{}{
		"task_id":         task.ID,
		"type":            task.Type,
		"payload":         string(payloadJSON),
		"priority":        task.Priority,
		"max_retry":       task.MaxRetry,
		"queue_timeout":   task.QueueTimeout.Seconds(),
		"process_timeout": task.ProcessTimeout.Seconds(),
		"created_at":      task.CreatedAt.Unix(),
		"updated_at":      task.UpdatedAt.Unix(),
		"status":          string(task.Status),
	}

	// 添加到Stream
	messageID, err := stq.client.XAdd(ctx, &redis.XAddArgs{
		Stream: stq.streamKey,
		MaxLen: stq.maxLen,
		Approx: true, // 使用近似裁剪提高性能
		Values: values,
	}).Result()

	if err != nil {
		return fmt.Errorf("failed to add task to stream: %w", err)
	}

	stq.logger.WithFields(logrus.Fields{
		"task_id":    task.ID,
		"task_type":  task.Type,
		"message_id": messageID,
		"stream_key": stq.streamKey,
	}).Debug("Task added to stream")

	return nil
}

// ConsumeTask 从Stream消费任务
func (stq *StreamTaskQueue) ConsumeTask(ctx context.Context, consumerID string) (*models.Task, string, error) {
	// 首先尝试读取新消息
	streams, err := stq.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    stq.groupName,
		Consumer: consumerID,
		Streams:  []string{stq.streamKey, ">"},
		Count:    stq.batchSize,
		Block:    stq.blockTimeout,
	}).Result()

	if err != nil {
		if err == redis.Nil {
			// 没有新消息，尝试声明挂起的消息
			return stq.claimPendingMessage(ctx, consumerID)
		}
		return nil, "", fmt.Errorf("failed to read from stream: %w", err)
	}

	if len(streams) == 0 || len(streams[0].Messages) == 0 {
		return nil, "", fmt.Errorf("no messages available")
	}

	// 解析第一个消息
	message := streams[0].Messages[0]
	task, err := stq.parseMessage(message)
	if err != nil {
		// 确认消息以避免重复处理
		stq.client.XAck(ctx, stq.streamKey, stq.groupName, message.ID)
		return nil, "", fmt.Errorf("failed to parse message: %w", err)
	}

	stq.logger.WithFields(logrus.Fields{
		"task_id":     task.ID,
		"task_type":   task.Type,
		"message_id":  message.ID,
		"consumer_id": consumerID,
	}).Debug("Task consumed from stream")

	return task, message.ID, nil
}

// claimPendingMessage 声明挂起的消息
func (stq *StreamTaskQueue) claimPendingMessage(ctx context.Context, consumerID string) (*models.Task, string, error) {
	// 获取挂起的消息
	pending, err := stq.client.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: stq.streamKey,
		Group:  stq.groupName,
		Start:  "-",
		End:    "+",
		Count:  1,
	}).Result()

	if err != nil || len(pending) == 0 {
		return nil, "", fmt.Errorf("no pending messages available")
	}

	// 检查消息是否空闲足够长时间
	msg := pending[0]
	if msg.Idle < stq.claimMinIdle {
		return nil, "", fmt.Errorf("no claimable messages available")
	}

	// 声明消息
	claimed, err := stq.client.XClaim(ctx, &redis.XClaimArgs{
		Stream:   stq.streamKey,
		Group:    stq.groupName,
		Consumer: consumerID,
		MinIdle:  stq.claimMinIdle,
		Messages: []string{msg.ID},
	}).Result()

	if err != nil || len(claimed) == 0 {
		return nil, "", fmt.Errorf("failed to claim message: %w", err)
	}

	// 解析声明的消息
	claimedMsg := claimed[0]
	task, err := stq.parseMessage(claimedMsg)
	if err != nil {
		// 确认消息以避免重复处理
		stq.client.XAck(ctx, stq.streamKey, stq.groupName, claimedMsg.ID)
		return nil, "", fmt.Errorf("failed to parse claimed message: %w", err)
	}

	stq.logger.WithFields(logrus.Fields{
		"task_id":     task.ID,
		"task_type":   task.Type,
		"message_id":  claimedMsg.ID,
		"consumer_id": consumerID,
		"idle_time":   msg.Idle,
	}).Info("Claimed pending message from stream")

	return task, claimedMsg.ID, nil
}

// parseMessage 解析Stream消息为Task对象
func (stq *StreamTaskQueue) parseMessage(message redis.XMessage) (*models.Task, error) {
	values := message.Values

	// 解析基本字段
	taskID, ok := values["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("missing task_id in message")
	}

	taskType, ok := values["type"].(string)
	if !ok {
		return nil, fmt.Errorf("missing type in message")
	}

	payloadStr, ok := values["payload"].(string)
	if !ok {
		return nil, fmt.Errorf("missing payload in message")
	}

	// 解析payload
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(payloadStr), &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	// 解析其他字段
	priority := stq.parseInt64Field(values, "priority", 0)
	maxRetry := stq.parseInt64Field(values, "max_retry", 3)
	queueTimeoutSecs := stq.parseInt64Field(values, "queue_timeout", 600)
	processTimeoutSecs := stq.parseInt64Field(values, "process_timeout", 300)
	createdAtUnix := stq.parseInt64Field(values, "created_at", time.Now().Unix())
	updatedAtUnix := stq.parseInt64Field(values, "updated_at", time.Now().Unix())

	statusStr, ok := values["status"].(string)
	if !ok {
		statusStr = string(models.TaskStatusPending)
	}

	// 构建Task对象
	task := &models.Task{
		ID:             taskID,
		Type:           taskType,
		Payload:        payload,
		Priority:       int(priority),
		Status:         models.TaskStatus(statusStr),
		MaxRetry:       int(maxRetry),
		Retry:          0,
		QueueTimeout:   time.Duration(queueTimeoutSecs) * time.Second,
		ProcessTimeout: time.Duration(processTimeoutSecs) * time.Second,
		CreatedAt:      time.Unix(createdAtUnix, 0),
		UpdatedAt:      time.Unix(updatedAtUnix, 0),
	}

	return task, nil
}

// parseInt64Field 安全解析int64字段
func (stq *StreamTaskQueue) parseInt64Field(values map[string]interface{}, key string, defaultValue int64) int64 {
	if val, ok := values[key]; ok {
		switch v := val.(type) {
		case string:
			if parsed, err := json.Number(v).Int64(); err == nil {
				return parsed
			}
		case int64:
			return v
		case int:
			return int64(v)
		case float64:
			return int64(v)
		}
	}
	return defaultValue
}

// AckTask 确认任务处理完成
func (stq *StreamTaskQueue) AckTask(ctx context.Context, messageID string) error {
	err := stq.client.XAck(ctx, stq.streamKey, stq.groupName, messageID).Err()
	if err != nil {
		return fmt.Errorf("failed to ack message: %w", err)
	}

	stq.logger.WithFields(logrus.Fields{
		"message_id": messageID,
		"stream_key": stq.streamKey,
		"group_name": stq.groupName,
	}).Debug("Message acknowledged")

	return nil
}

// GetStreamInfo 获取Stream信息
func (stq *StreamTaskQueue) GetStreamInfo(ctx context.Context) (*StreamInfo, error) {
	// 使用简单的方法获取Stream长度，避免版本兼容性问题
	length, err := stq.client.XLen(ctx, stq.streamKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get stream length: %w", err)
	}

	// 获取消费者组信息
	groupInfo, err := stq.client.XInfoGroups(ctx, stq.streamKey).Result()
	if err != nil {
		// 如果没有消费者组，返回基本信息
		return &StreamInfo{
			Length:          length,
			RadixTreeKeys:   0,
			RadixTreeNodes:  0,
			Groups:          0,
			LastGeneratedID: "",
			Consumers:       []ConsumerInfo{},
		}, nil
	}

	// 获取消费者信息
	var consumers []ConsumerInfo
	if len(groupInfo) > 0 {
		// 查找我们的消费者组
		for _, group := range groupInfo {
			if group.Name == stq.groupName {
				// 使用单独的上下文获取消费者信息，避免超时
				consumerCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				defer cancel()

				consumerInfos, err := stq.client.XInfoConsumers(consumerCtx, stq.streamKey, stq.groupName).Result()
				if err == nil {
					for _, ci := range consumerInfos {
						consumers = append(consumers, ConsumerInfo{
							Name:    ci.Name,
							Pending: ci.Pending,
							Idle:    time.Duration(ci.Idle) * time.Millisecond,
						})
					}
				} else {
					stq.logger.WithError(err).Warn("Failed to get consumer info, skipping")
				}
				break
			}
		}
	}

	return &StreamInfo{
		Length:          length,
		RadixTreeKeys:   0, // 简化，不获取这些详细信息
		RadixTreeNodes:  0, // 简化，不获取这些详细信息
		Groups:          int64(len(groupInfo)),
		LastGeneratedID: "", // 简化，不获取这个信息
		Consumers:       consumers,
	}, nil
}

// StreamInfo Stream信息
type StreamInfo struct {
	Length          int64          `json:"length"`
	RadixTreeKeys   int64          `json:"radix_tree_keys"`
	RadixTreeNodes  int64          `json:"radix_tree_nodes"`
	Groups          int64          `json:"groups"`
	LastGeneratedID string         `json:"last_generated_id"`
	Consumers       []ConsumerInfo `json:"consumers"`
}

// ConsumerInfo 消费者信息
type ConsumerInfo struct {
	Name    string        `json:"name"`
	Pending int64         `json:"pending"`
	Idle    time.Duration `json:"idle"`
}

// Close 关闭Stream队列
func (stq *StreamTaskQueue) Close() error {
	stq.logger.Info("Stream task queue closed")
	return nil
}
