package config

import (
	"time"
)

// Config 系统配置
type Config struct {
	Redis  RedisConfig  `json:"redis"`
	Kafka  KafkaConfig  `json:"kafka"`
	MQTT   MQTTConfig   `json:"mqtt"`
	Server ServerConfig `json:"server"`
	Queue  QueueConfig  `json:"queue"`
}

// RedisConfig Redis配置
type RedisConfig struct {
	Addr         string `json:"addr"`
	Password     string `json:"password"`
	DB           int    `json:"db"`
	PoolSize     int    `json:"pool_size"`
	MinIdleConns int    `json:"min_idle_conns"`
}

// KafkaConfig Kafka配置
type KafkaConfig struct {
	Brokers      []string      `json:"brokers"`
	Topic        string        `json:"topic"`
	GroupID      string        `json:"group_id"`
	BatchSize    int           `json:"batch_size"`
	BatchTimeout time.Duration `json:"batch_timeout"`
}

// MQTTConfig MQTT配置
type MQTTConfig struct {
	Broker   string `json:"broker"`
	ClientID string `json:"client_id"`
	Username string `json:"username"`
	Password string `json:"password"`
	QoS      byte   `json:"qos"`
	Topic    string `json:"topic"`
}

// ServerConfig HTTP服务器配置
type ServerConfig struct {
	Port string `json:"port"`
	Mode string `json:"mode"` // debug, release, test
}

// QueueConfig 队列配置
type QueueConfig struct {
	DefaultTimeout      time.Duration `json:"default_timeout"`
	DefaultRetry        int           `json:"default_retry"`
	MaxConcurrency      int           `json:"max_concurrency"`
	CleanupInterval     time.Duration `json:"cleanup_interval"`
	HeartbeatInterval   time.Duration `json:"heartbeat_interval"`
	SemaphoreExpiration time.Duration `json:"semaphore_expiration"`
}

// DefaultConfig 默认配置
func DefaultConfig() *Config {
	return &Config{
		Redis: RedisConfig{
			Addr:         "localhost:6379",
			Password:     "",
			DB:           0,
			PoolSize:     10,
			MinIdleConns: 2,
		},
		Kafka: KafkaConfig{
			Brokers:      []string{"localhost:9092"},
			Topic:        "task-queue",
			GroupID:      "task-consumer",
			BatchSize:    100,
			BatchTimeout: time.Second * 5,
		},
		MQTT: MQTTConfig{
			Broker:   "tcp://localhost:1883",
			ClientID: "task-queue-notifier",
			Username: "",
			Password: "",
			QoS:      1,
			Topic:    "task/status",
		},
		Server: ServerConfig{
			Port: "8080",
			Mode: "debug",
		},
		Queue: QueueConfig{
			DefaultTimeout:      time.Minute * 10,
			DefaultRetry:        3,
			MaxConcurrency:      100,
			CleanupInterval:     time.Minute * 5,
			HeartbeatInterval:   time.Second * 30,
			SemaphoreExpiration: time.Minute * 5,
		},
	}
}
