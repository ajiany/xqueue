package queue

import (
	"context"
	"fmt"
	"time"

	"xqueue/internal/config"
	"xqueue/internal/models"

	"github.com/segmentio/kafka-go"
	"github.com/sirupsen/logrus"
)

// KafkaQueue Kafka消息队列
type KafkaQueue struct {
	writer *kafka.Writer
	reader *kafka.Reader
	config *config.KafkaConfig
	logger *logrus.Logger
}

// NewKafkaQueue 创建Kafka队列
func NewKafkaQueue(cfg *config.KafkaConfig, logger *logrus.Logger) *KafkaQueue {
	// 创建写入器
	writer := &kafka.Writer{
		Addr:         kafka.TCP(cfg.Brokers...),
		Topic:        cfg.Topic,
		Balancer:     &kafka.Hash{},
		RequiredAcks: kafka.RequireAll,
		Async:        false,
		BatchSize:    cfg.BatchSize,
		BatchTimeout: cfg.BatchTimeout,
		Logger:       kafka.LoggerFunc(logrus.Infof),
		ErrorLogger:  kafka.LoggerFunc(logrus.Errorf),
	}

	// 创建读取器
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     cfg.Brokers,
		Topic:       cfg.Topic,
		GroupID:     cfg.GroupID,
		MinBytes:    10e3, // 10KB
		MaxBytes:    10e6, // 10MB
		MaxWait:     1 * time.Second,
		Logger:      kafka.LoggerFunc(logrus.Infof),
		ErrorLogger: kafka.LoggerFunc(logrus.Errorf),
	})

	return &KafkaQueue{
		writer: writer,
		reader: reader,
		config: cfg,
		logger: logger,
	}
}

// PublishTask 发布任务到队列
func (kq *KafkaQueue) PublishTask(ctx context.Context, task *models.Task) error {
	data, err := task.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	message := kafka.Message{
		Key:   []byte(task.ID),
		Value: data,
		Headers: []kafka.Header{
			{Key: "task_type", Value: []byte(task.Type)},
			{Key: "priority", Value: []byte(fmt.Sprintf("%d", task.Priority))},
			{Key: "created_at", Value: []byte(task.CreatedAt.Format(time.RFC3339))},
		},
	}

	err = kq.writer.WriteMessages(ctx, message)
	if err != nil {
		return fmt.Errorf("failed to publish task to kafka: %w", err)
	}

	kq.logger.WithFields(logrus.Fields{
		"task_id":   task.ID,
		"task_type": task.Type,
		"topic":     kq.config.Topic,
	}).Info("Task published to Kafka")

	return nil
}

// ConsumeTask 从队列消费任务
func (kq *KafkaQueue) ConsumeTask(ctx context.Context) (*models.Task, error) {
	message, err := kq.reader.FetchMessage(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch message from kafka: %w", err)
	}

	task, err := models.FromJSON(message.Value)
	if err != nil {
		// 提交消息以避免重复处理
		kq.reader.CommitMessages(ctx, message)
		return nil, fmt.Errorf("failed to unmarshal task: %w", err)
	}

	// 将 Kafka 消息信息存储到任务中，用于后续提交
	task.ID = string(message.Key)

	kq.logger.WithFields(logrus.Fields{
		"task_id":   task.ID,
		"task_type": task.Type,
		"partition": message.Partition,
		"offset":    message.Offset,
	}).Debug("Task consumed from Kafka")

	return task, nil
}

// Close 关闭Kafka连接
func (kq *KafkaQueue) Close() error {
	var errs []error

	if err := kq.writer.Close(); err != nil {
		errs = append(errs, fmt.Errorf("failed to close writer: %w", err))
	}

	if err := kq.reader.Close(); err != nil {
		errs = append(errs, fmt.Errorf("failed to close reader: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing kafka queue: %v", errs)
	}

	kq.logger.Info("Kafka queue closed")
	return nil
}
