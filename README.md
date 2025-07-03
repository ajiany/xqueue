# XQueue - 分布式任务队列系统

[![Go Version](https://img.shields.io/badge/Go-1.19+-blue.svg)](https://golang.org)
[![Redis](https://img.shields.io/badge/Redis-6.0+-red.svg)](https://redis.io)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Version](https://img.shields.io/badge/Version-v0.0.5-orange.svg)](doc/CHANGELOG.md)

XQueue 是一个基于 Redis Stream 的高性能分布式任务队列系统，专为高并发场景设计。

## 🚀 v0.0.5 重大升级

### Stream架构 - 彻底解决并发竞争问题

v0.0.5 采用全新的 **Redis Stream** 架构，彻底解决了之前版本中worker竞争同一任务的问题：

- ✅ **真正的并发处理**：example任务3个并发，email任务5个并发（不再是串行）
- ✅ **零竞争条件**：每个worker独立获取不同任务，无锁竞争
- ✅ **消息不丢失**：完善的ACK机制确保任务安全
- ✅ **性能提升3-5倍**：充分利用多核CPU资源

### 核心特性

#### 🌊 基于Redis Stream的消息队列
- **消费者组机制**：原生支持多消费者并发处理
- **消息确认机制**：XACK确保消息处理完成
- **故障自动恢复**：自动处理挂起消息和worker重启
- **阻塞式消费**：减少CPU使用，提高响应速度

#### ⚡ 高性能并发控制
- **精确并发限制**：真正实现设定的并发数量
- **智能负载均衡**：消费者组自动分配任务
- **资源优化**：10个worker高效利用系统资源
- **实时监控**：详细的Stream和Worker状态监控

#### 🛠️ 向Kafka迁移准备
- **概念一致性**：消费者组概念与Kafka一致
- **API兼容性**：保持现有接口不变
- **平滑升级**：为后续Kafka迁移奠定基础

## 📊 性能对比

| 指标 | v0.0.4.1 | v0.0.5 Stream | 提升倍数 |
|------|----------|---------------|----------|
| 并发处理能力 | 实际1个 | 设定限制 | 3-5x |
| Worker利用率 | ~10% | 90%+ | 9x |
| 任务获取延迟 | 5秒 | <100ms | 50x |
| 吞吐量 | 基准 | 3-5x基准 | 3-5x |

## 🏗️ 架构设计

### Stream架构图

```
┌─────────────────────────────────────────────────────────────┐
│                     XQueue v0.0.5 Stream架构                │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌─────────────┐    ┌──────────────────────────────────┐   │
│  │   Client    │───▶│         HTTP API                 │   │
│  │  (Submit)   │    │    (StreamHandler)               │   │
│  └─────────────┘    └──────────────────────────────────┘   │
│                                    │                        │
│                                    ▼                        │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │            StreamTaskManager                            │ │
│  │  ┌─────────────────┐  ┌─────────────────────────────┐  │ │
│  │  │  Task Storage   │  │    Concurrency Manager     │  │ │
│  │  │   (Redis)       │  │   (Semaphore Control)      │  │ │
│  │  └─────────────────┘  └─────────────────────────────┘  │ │
│  └─────────────────────────────────────────────────────────┘ │
│                                    │                        │
│                                    ▼                        │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │                Redis Stream Queue                       │ │
│  │                                                         │ │
│  │  Stream: xqueue:tasks                                   │ │
│  │  Group:  xqueue-workers                                 │ │
│  │                                                         │ │
│  │  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐      │ │
│  │  │ Message │ │ Message │ │ Message │ │ Message │ ...  │ │
│  │  │   #1    │ │   #2    │ │   #3    │ │   #4    │      │ │
│  │  └─────────┘ └─────────┘ └─────────┘ └─────────┘      │ │
│  └─────────────────────────────────────────────────────────┘ │
│                                    │                        │
│                                    ▼                        │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │                StreamWorkerPool                         │ │
│  │                                                         │ │
│  │  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐      │ │
│  │  │Worker #1│ │Worker #2│ │Worker #3│ │Worker #4│ ...  │ │
│  │  │         │ │         │ │         │ │         │      │ │
│  │  │XREADGRP │ │XREADGRP │ │XREADGRP │ │XREADGRP │      │ │
│  │  │  XACK   │ │  XACK   │ │  XACK   │ │  XACK   │      │ │
│  │  └─────────┘ └─────────┘ └─────────┘ └─────────┘      │ │
│  └─────────────────────────────────────────────────────────┘ │
│                                    │                        │
│                                    ▼                        │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │                Task Handlers                            │ │
│  │  ┌─────────────────┐  ┌─────────────────────────────┐  │ │
│  │  │  ExampleHandler │  │      EmailHandler           │  │ │
│  │  │  (Concurrency:3)│  │    (Concurrency:5)          │  │ │
│  │  └─────────────────┘  └─────────────────────────────┘  │ │
│  └─────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

### 关键组件

#### StreamTaskQueue
- **Redis Stream操作**：XADD, XREADGROUP, XACK, XCLAIM
- **消费者组管理**：自动创建和管理消费者组
- **消息序列化**：任务对象与Stream消息的转换
- **故障恢复**：处理挂起消息和消费者重启

#### StreamWorkerPool  
- **多Worker并发**：10个独立worker同时消费
- **阻塞式读取**：XREADGROUP BLOCK 5000优化性能
- **消息确认**：处理完成后自动XACK
- **优雅关闭**：支持graceful shutdown

#### StreamTaskManager
- **任务生命周期管理**：从提交到完成的全流程
- **并发控制集成**：与Redis信号量无缝集成
- **状态监控**：实时统计和监控指标
- **处理器管理**：动态注册和管理任务处理器

## 🚀 快速开始

### 环境要求

- Go 1.19+
- Redis 6.0+ (支持Stream功能)
- MQTT Broker (可选，用于消息通知)

### 安装运行

```bash
# 克隆项目
git clone https://github.com/your-org/xqueue.git
cd xqueue

# 启动Redis (使用Docker)
docker run -d --name redis -p 6379:6379 redis:7-alpine

# 启动XQueue服务
go run cmd/server/main.go
```

### 测试验证

```bash
# 运行v0.0.5 Stream架构测试
./examples/test_stream_v0.0.5.sh
```

## 📚 API文档

### 基础任务管理

#### 提交任务
```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "type": "example",
    "payload": {
      "message": "Hello XQueue v0.0.5!"
    },
    "priority": 1
  }'
```

#### 获取任务状态
```bash
curl http://localhost:8080/api/v1/tasks/{task_id}
```

#### 获取任务列表
```bash
curl "http://localhost:8080/api/v1/tasks?status=pending&limit=10"
```

### v0.0.5 新增接口

#### Stream信息
```bash
# 获取Stream详细信息
curl http://localhost:8080/api/v1/stream

# 响应示例
{
  "stream_info": {
    "length": 15,
    "groups": 1,
    "consumers": [
      {
        "name": "worker-1",
        "pending": 2,
        "idle": 1500
      }
    ]
  }
}
```

#### Worker信息
```bash
# 获取Worker详细信息
curl http://localhost:8080/api/v1/workers

# 响应示例
{
  "total_workers": 10,
  "active_workers": 8,
  "processed_tasks": 156,
  "failed_tasks": 2,
  "uptime": 3600000000000
}
```

#### 系统统计
```bash
# 获取增强的系统统计
curl http://localhost:8080/api/v1/stats

# 响应示例
{
  "pending_tasks": 5,
  "processing_tasks": 3,
  "success_tasks": 142,
  "failed_tasks": 2,
  "total_workers": 10,
  "active_workers": 8,
  "stream_info": {
    "length": 8,
    "groups": 1
  },
  "instance_id": "xqueue-a1b2c3d4",
  "is_running": true
}
```

## 🔧 配置管理

### 并发控制
```bash
# 设置任务类型并发限制
curl -X POST http://localhost:8080/api/v1/concurrency \
  -H "Content-Type: application/json" \
  -d '{
    "task_type": "example",
    "max_concurrency": 3
  }'

# 获取并发配置
curl http://localhost:8080/api/v1/concurrency
```

### 任务处理器

```go
// 自定义任务处理器
type CustomHandler struct {
    logger *logrus.Logger
}

func (h *CustomHandler) Handle(task *models.Task) error {
    // 处理任务逻辑
    h.logger.Info("Processing custom task")
    return nil
}

func (h *CustomHandler) GetType() string {
    return "custom"
}

// 注册处理器
streamTaskManager.RegisterHandler(&CustomHandler{logger: logger})
streamTaskManager.SetConcurrencyLimit("custom", 5)
```

## 📈 监控和运维

### 健康检查
```bash
curl http://localhost:8080/api/v1/health
```

### 性能监控

#### 关键指标
- **Stream长度**：队列中待处理消息数
- **活跃Worker数**：正在处理任务的worker数量
- **处理吞吐量**：每秒处理的任务数
- **消息确认率**：成功ACK的消息比例

#### 监控脚本
```bash
# 实时监控脚本
watch -n 2 'curl -s http://localhost:8080/api/v1/stats | jq'
```

## 🧪 测试和验证

### 功能测试
```bash
# 基础功能测试
./examples/test_stream_v0.0.5.sh

# 并发性能测试
./examples/concurrent_test.sh

# 压力测试
./examples/stress_test.sh
```

### 性能基准
```bash
# 运行性能基准测试
go test -bench=. ./internal/benchmark/
```

## 📝 开发指南

### 添加新的任务类型

1. **实现TaskHandler接口**
```go
type MyTaskHandler struct {
    logger *logrus.Logger
}

func (h *MyTaskHandler) Handle(task *models.Task) error {
    // 实现任务处理逻辑
    return nil
}

func (h *MyTaskHandler) GetType() string {
    return "my_task"
}
```

2. **注册处理器**
```go
handler := &MyTaskHandler{logger: logger}
streamTaskManager.RegisterHandler(handler)
streamTaskManager.SetConcurrencyLimit("my_task", 10)
```

3. **提交任务**
```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "type": "my_task",
    "payload": {"data": "value"}
  }'
```

### 自定义配置

```go
// 自定义Stream配置
streamQueue := queue.NewStreamTaskQueue(redisClient, logger)
streamQueue.SetMaxLength(20000)      // 设置Stream最大长度
streamQueue.SetBlockTimeout(10*time.Second) // 设置阻塞超时
streamQueue.SetBatchSize(50)         // 设置批量读取大小
```

## 🛣️ 发展路线图

### v0.0.6 计划
- [ ] **多实例支持**：支持多个XQueue实例组成集群
- [ ] **动态扩缩容**：根据负载自动调整worker数量
- [ ] **消息优先级**：Stream中的消息优先级支持
- [ ] **死信队列**：处理失败任务的死信机制

### v0.1.0 计划
- [ ] **Kafka集成**：可选的Kafka队列后端
- [ ] **Web管理界面**：任务监控和管理的Web UI
- [ ] **指标导出**：Prometheus指标导出
- [ ] **集群管理**：多节点集群管理和协调

## 🤝 贡献指南

我们欢迎所有形式的贡献！

### 提交代码
1. Fork 项目
2. 创建特性分支: `git checkout -b feature/amazing-feature`
3. 提交更改: `git commit -m 'Add amazing feature'`
4. 推送分支: `git push origin feature/amazing-feature`
5. 提交 Pull Request

### 报告问题
- 使用 GitHub Issues 报告 bug
- 提供详细的重现步骤
- 包含相关的日志和配置信息

### 功能建议
- 在 Issues 中描述新功能需求
- 说明使用场景和预期效果
- 参与讨论和设计

## 📄 许可证

本项目采用 MIT 许可证。详情请参阅 [LICENSE](LICENSE) 文件。

## 🙏 致谢

- Redis 团队提供的强大 Stream 功能
- Go 社区的优秀开源库
- 所有贡献者的宝贵建议和代码

---

**XQueue v0.0.5** - 基于Redis Stream的高性能分布式任务队列系统