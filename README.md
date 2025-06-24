# XQueue - 分布式任务队列系统

XQueue 是一个高性能、可扩展的分布式任务队列系统，基于 Go 语言开发，集成了 Redis、Kafka 和 MQTT 等技术栈。

## 🌟 核心特性

### 1. 分布式架构
- **基于 Kafka 实现消息队列**：保证消息可靠性和持久化
- **使用 Redis 实现分布式锁和状态管理**：支持集群部署
- **支持水平扩展**：可部署多个消费者节点

### 2. 智能并发控制
- **基于 Redis 实现分布式信号量**：精确控制并发数量
- **支持自定义并发策略**：按任务类型或业务维度控制
- **自动清理过期令牌**：防止资源泄露

### 3. 完善的任务生命周期管理
- **支持任务状态实时追踪**：pending、processing、success、failed、canceled、timeout
- **提供任务取消机制**：支持优雅取消正在执行的任务
- **智能重试机制**：可配置重试次数和重试策略

### 4. 灵活的超时控制
- **队列等待超时**：防止任务在队列中长时间等待
- **任务处理超时**：防止任务执行时间过长
- **自动超时处理**：超时任务自动标记为失败

### 5. 实时状态推送
- **MQTT 协议支持**：实时推送任务状态变更
- **WebSocket 兼容**：浏览器可直接订阅状态更新
- **可配置 QoS 等级**：保证消息传递质量

## 🚀 快速开始

### 环境要求

- Go 1.21+
- Redis 6.0+
- Kafka 2.8+ (可选)
- MQTT Broker (可选，如 Mosquitto)

### 安装依赖

```bash
# 克隆项目
git clone https://github.com/your-org/xqueue.git
cd xqueue

# 安装依赖
go mod tidy
```

### 启动服务

```bash
# 启动 Redis (Docker)
docker run -d --name redis -p 6379:6379 redis:7-alpine

# 启动 MQTT Broker (Docker)
docker run -d --name mosquitto -p 1883:1883 eclipse-mosquitto:2

# 启动 XQueue 服务
go run cmd/server/main.go
```

### 基本使用

#### 1. 提交任务

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "type": "example",
    "payload": {
      "message": "Hello, XQueue!"
    },
    "priority": 1,
    "max_retry": 3,
    "queue_timeout": 300,
    "process_timeout": 60
  }'
```

#### 2. 查询任务状态

```bash
curl http://localhost:8080/api/v1/tasks/{task_id}
```

#### 3. 获取任务列表

```bash
curl "http://localhost:8080/api/v1/tasks?status=pending&limit=10&offset=0"
```

#### 4. 取消任务

```bash
curl -X DELETE http://localhost:8080/api/v1/tasks/{task_id}
```

#### 5. 获取系统统计

```bash
curl http://localhost:8080/api/v1/stats
```

## 📊 API 文档

### 任务管理接口

| 方法 | 路径 | 描述 |
|------|------|------|
| POST | `/api/v1/tasks` | 提交新任务 |
| GET | `/api/v1/tasks` | 获取任务列表 |
| GET | `/api/v1/tasks/{id}` | 获取特定任务 |
| DELETE | `/api/v1/tasks/{id}` | 取消任务 |

### 系统接口

| 方法 | 路径 | 描述 |
|------|------|------|
| GET | `/api/v1/stats` | 获取系统统计 |
| GET | `/api/v1/health` | 健康检查 |

### 任务提交参数

```json
{
  "type": "task_type",           // 必需：任务类型
  "payload": {},                 // 可选：任务数据
  "priority": 1,                 // 可选：优先级 (数字越大优先级越高)
  "max_retry": 3,                // 可选：最大重试次数
  "queue_timeout": 300,          // 可选：队列超时时间 (秒)
  "process_timeout": 60,         // 可选：处理超时时间 (秒)
  "concurrency_key": "user_123", // 可选：并发控制键
  "max_concurrency": 5           // 可选：最大并发数
}
```

## 🔧 配置说明

### 默认配置

```go
// Redis 配置
Redis: {
    Addr:         "localhost:6379",
    Password:     "",
    DB:           0,
    PoolSize:     10,
    MinIdleConns: 2,
}

// Kafka 配置
Kafka: {
    Brokers:      []string{"localhost:9092"},
    Topic:        "task-queue",
    GroupID:      "task-consumer",
    BatchSize:    100,
    BatchTimeout: 5 * time.Second,
}

// MQTT 配置
MQTT: {
    Broker:   "tcp://localhost:1883",
    ClientID: "task-queue-notifier",
    QoS:      1,
    Topic:    "task/status",
}

// 服务器配置
Server: {
    Port: "8080",
    Mode: "debug",
}

// 队列配置
Queue: {
    DefaultTimeout:      10 * time.Minute,
    DefaultRetry:        3,
    MaxConcurrency:      100,
    CleanupInterval:     5 * time.Minute,
    HeartbeatInterval:   30 * time.Second,
    SemaphoreExpiration: 5 * time.Minute,
}
```

## 📈 监控和观察

### MQTT 主题结构

- `task/status/{task_id}` - 特定任务状态变更
- `task/status/status/{status}` - 按状态分类的任务通知
- `task/progress/{task_id}` - 任务进度更新

### 任务状态

| 状态 | 描述 |
|------|------|
| `pending` | 等待处理 |
| `processing` | 正在处理 |
| `success` | 处理成功 |
| `failed` | 处理失败 |
| `canceled` | 已取消 |
| `timeout` | 队列超时 |

## 🔨 开发指南

### 自定义任务处理器

```go
type MyTaskHandler struct {
    logger *logrus.Logger
}

func (h *MyTaskHandler) Handle(task *models.Task) error {
    // 实现你的业务逻辑
    h.logger.WithField("task_id", task.ID).Info("Processing custom task")
    
    // 获取任务数据
    data := task.Payload["data"].(string)
    
    // 处理逻辑
    result := processData(data)
    
    // 可以更新任务进度
    // notifier.NotifyTaskProgress(task.ID, map[string]interface{}{
    //     "progress": 50,
    //     "message": "Half way done"
    // })
    
    return nil
}

func (h *MyTaskHandler) GetType() string {
    return "my_custom_task"
}

// 注册处理器
taskManager.RegisterHandler(&MyTaskHandler{logger: logger})
```

### 并发控制示例

```go
// 限制每个用户同时只能有 3 个任务在处理
taskManager.SubmitTask("user_task", payload, 
    WithConcurrency("user_123", 3))

// 限制图片处理任务全局最多 10 个并发
taskManager.SubmitTask("image_process", payload, 
    WithConcurrency("image_process_global", 10))
```

## 🏗️ 架构设计

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   HTTP Client   │    │   MQTT Client   │    │   Web Console   │
└─────────┬───────┘    └─────────┬───────┘    └─────────┬───────┘
          │                      │                      │
          ▼                      ▼                      ▼
┌─────────────────────────────────────────────────────────────────┐
│                      XQueue API Gateway                        │
├─────────────────────────────────────────────────────────────────┤
│                     Task Manager                               │
├─────────────────┬───────────────────┬───────────────────────────┤
│   Redis Storage │    MQTT Notifier  │      Worker Pool          │
└─────────────────┴───────────────────┴───────────────────────────┘
          │                      │                      │
          ▼                      ▼                      ▼
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│      Redis      │    │   MQTT Broker   │    │   Kafka Queue   │
└─────────────────┘    └─────────────────┘    └─────────────────┘
```

## 📝 许可证

MIT License

## 🤝 贡献

欢迎提交 Issue 和 Pull Request！

## 📞 联系我们

- 邮箱: your-email@example.com
- 项目地址: https://github.com/your-org/xqueue