package models

import (
	"encoding/json"
	"time"
)

// TaskStatus 任务状态枚举
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"    // 等待中
	TaskStatusProcessing TaskStatus = "processing" // 处理中
	TaskStatusSuccess    TaskStatus = "success"    // 成功
	TaskStatusFailed     TaskStatus = "failed"     // 失败
	TaskStatusCanceled   TaskStatus = "canceled"   // 已取消
	TaskStatusTimeout    TaskStatus = "timeout"    // 队列超时
)

// Task 任务结构体
type Task struct {
	ID             string                 `json:"id"`              // 任务唯一标识
	Type           string                 `json:"type"`            // 任务类型
	Payload        map[string]interface{} `json:"payload"`         // 任务负载数据
	Priority       int                    `json:"priority"`        // 优先级 (数字越大优先级越高)
	Status         TaskStatus             `json:"status"`          // 任务状态
	Retry          int                    `json:"retry"`           // 重试次数
	MaxRetry       int                    `json:"max_retry"`       // 最大重试次数
	QueueTimeout   time.Duration          `json:"queue_timeout"`   // 队列等待超时时间
	ProcessTimeout time.Duration          `json:"process_timeout"` // 处理超时时间
	CreatedAt      time.Time              `json:"created_at"`      // 创建时间
	UpdatedAt      time.Time              `json:"updated_at"`      // 更新时间
	StartedAt      *time.Time             `json:"started_at"`      // 开始处理时间
	CompletedAt    *time.Time             `json:"completed_at"`    // 完成时间
	Error          string                 `json:"error,omitempty"` // 错误信息
	WorkerID       string                 `json:"worker_id"`       // 处理该任务的工作者ID
	ConcurrencyKey string                 `json:"concurrency_key"` // 并发控制key
	MaxConcurrency int                    `json:"max_concurrency"` // 最大并发数
}

// ToJSON 将任务转换为JSON字符串
func (t *Task) ToJSON() ([]byte, error) {
	return json.Marshal(t)
}

// FromJSON 从JSON字符串解析任务
func FromJSON(data []byte) (*Task, error) {
	var task Task
	err := json.Unmarshal(data, &task)
	return &task, err
}

// IsExpired 检查任务是否已过期
func (t *Task) IsExpired() bool {
	if t.QueueTimeout <= 0 {
		return false
	}
	return time.Since(t.CreatedAt) > t.QueueTimeout
}

// CanRetry 检查任务是否可以重试
func (t *Task) CanRetry() bool {
	return t.Retry < t.MaxRetry
}

// TaskHandler 任务处理器接口
type TaskHandler interface {
	Handle(task *Task) error
	GetType() string
}

// TaskMessage MQTT消息结构
type TaskMessage struct {
	TaskID    string     `json:"task_id"`
	Status    TaskStatus `json:"status"`
	Error     string     `json:"error,omitempty"`
	Timestamp time.Time  `json:"timestamp"`
}
