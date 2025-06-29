# XQueue - 分布式任务队列

XQueue 是一个高性能、可扩展的分布式任务队列系统，基于 Go 语言开发，集成了 Redis、Kafka 和 MQTT 等技术栈。

> **当前版本：v0.0.3** - 重新设计并发控制架构，实现任务类型级别的全局并发控制

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

### 6. 任务类型验证 (v0.0.2 新增)
- **处理器注册机制**：只允许提交已注册处理器的任务类型
- **类型安全保证**：防止未知任务类型导致的任务堆积
- **动态处理器管理**：支持运行时注册和注销处理器

### 7. 多Worker并发处理 (v0.0.2 新增)
- **WorkerPool 架构**：支持多个Worker并发处理任务
- **可配置Worker数量**：根据系统负载调整并发度
- **优雅关闭机制**：避免协程泄露，支持平滑重启

### 8. 分布式任务锁 (v0.0.2 新增)
- **Redis分布式锁**：防止多个Worker重复处理同一任务
- **锁自动过期**：避免死锁，支持锁超时清理
- **竞争处理优化**：高并发场景下的任务分配优化

### 9. 全局并发控制 (v0.0.3 重构)
- **任务类型级别控制**：按任务类型设置全局并发限制
- **ConcurrencyManager**：集中管理所有任务类型的并发配置
- **信号量令牌机制**：基于Redis实现分布式并发控制
- **动态配置调整**：运行时调整任务类型的并发限制

## 🚀 快速开始

### 环境要求

- Go 1.21+
- Redis 6.0+
- Kafka 2.8+ (可选)
- MQTT Broker (可选，如 Mosquitto)

### 安装依赖

```bash
# 克隆项目
git clone git@github.com:ajiany/xqueue.git
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

#### 5. 获取已注册的处理器 (v0.0.2 新增)

```bash
curl http://localhost:8080/api/v1/handlers
```

#### 6. 获取系统统计

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
| GET | `/api/v1/handlers` | 获取已注册的处理器 (v0.0.2) |
| GET | `/api/v1/concurrency` | 获取并发限制配置 (v0.0.3) |
| POST | `/api/v1/concurrency` | 设置并发限制 (v0.0.3) |

### 任务提交参数

```json
{
  "type": "task_type",           // 必需：任务类型 (v0.0.2: 必须是已注册的处理器类型)
  "payload": {},                 // 可选：任务数据
  "priority": 1,                 // 可选：优先级 (数字越大优先级越高)
  "max_retry": 3,                // 可选：最大重试次数
  "queue_timeout": 300,          // 可选：队列超时时间 (秒)
  "process_timeout": 60          // 可选：处理超时时间 (秒)
}
```

### v0.0.3 并发控制配置

#### 设置并发限制
```bash
curl -X POST http://localhost:8080/api/v1/concurrency \
  -H "Content-Type: application/json" \
  -d '{
    "task_type": "email",
    "max_concurrency": 5
  }'
```

#### 获取并发限制配置
```bash
curl http://localhost:8080/api/v1/concurrency

# 响应示例
{
  "limits": {
    "email": 5,
    "example": 3
  },
  "count": 2
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
// v0.0.3 新设计：任务类型级别的全局并发控制

// 设置邮件任务全局最多 5 个并发
taskManager.SetConcurrencyLimit("email", 5)

// 设置图片处理任务全局最多 3 个并发
taskManager.SetConcurrencyLimit("image_process", 3)

// 提交任务时不需要指定并发参数，系统会自动应用对应类型的并发限制
taskManager.SubmitTask("email", emailPayload)
taskManager.SubmitTask("image_process", imagePayload)

// 获取当前并发限制
limit, exists := taskManager.GetConcurrencyLimit("email")
if exists {
    fmt.Printf("邮件任务并发限制: %d\n", limit)
}
```

## 🏗️ 架构设计

```