# XQueue 更新日志

## Version 0.0.3

### 🔄 重大架构重构

#### 并发控制架构重新设计
- ✅ **修复根本性设计缺陷**：从单任务级别的并发控制重构为任务类型级别的全局并发控制
- ✅ **新增 ConcurrencyManager 组件**：集中管理所有任务类型的并发限制配置
- ✅ **信号量令牌机制**：基于 Redis 实现分布式并发控制，确保任务类型级别的并发限制
- ✅ **动态配置管理**：支持运行时调整任务类型的并发限制

#### API 接口重构
- ✅ **新增并发控制管理接口**：
  - `GET /api/v1/concurrency` - 获取所有任务类型的并发限制配置
  - `POST /api/v1/concurrency` - 设置特定任务类型的并发限制
- ✅ **简化任务提交参数**：移除不再需要的 `concurrency_key` 和 `max_concurrency` 参数
- ✅ **增强任务处理流程**：集成并发令牌获取和释放机制

### 🛠️ 实现细节

#### 核心组件
- **ConcurrencyManager** (`internal/concurrency/manager.go`)：
  - 管理任务类型到并发限制的映射
  - 缓存和管理 Redis 信号量实例
  - 提供令牌获取、释放和统计功能
- **TaskManager 增强**：
  - 集成 ConcurrencyManager
  - 在任务处理过程中自动应用并发控制
  - 确保令牌的正确获取和释放

#### 数据模型优化
- **Task 结构体简化**：移除 `ConcurrencyKey` 和 `MaxConcurrency` 字段
- **配置集中化**：并发限制配置从任务实例转移到 TaskManager 层面

### 🔧 改进优化

#### 性能优化
- **信号量缓存机制**：避免重复创建 Redis 信号量实例
- **双重检查锁定**：确保并发环境下信号量创建的线程安全
- **自动令牌清理**：定期清理过期的并发令牌

#### 错误处理增强
- **令牌获取失败处理**：任务状态更新失败时自动释放已获取的令牌
- **Panic 恢复机制**：确保即使任务处理发生 panic 也能正确释放令牌
- **并发限制验证**：设置并发限制时验证任务类型是否已注册

### 📝 文档更新
- 更新 README.md 包含 v0.0.3 新功能说明
- 重写并发控制使用示例
- 更新 API 文档和参数说明
- 创建 v0.0.3 功能测试脚本

### 🎯 使用场景示例

#### 设置并发限制
```bash
# 设置邮件任务最多 5 个并发
curl -X POST http://localhost:8080/api/v1/concurrency \
  -H "Content-Type: application/json" \
  -d '{"task_type": "email", "max_concurrency": 5}'
```

#### 查看并发配置
```bash
# 获取所有并发限制配置
curl http://localhost:8080/api/v1/concurrency
```

#### 任务提交（自动应用并发控制）
```bash
# 提交邮件任务，系统自动应用 email 类型的并发限制
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{"type": "email", "payload": {"to": "test@example.com"}}'
```

### ⚠️ 破坏性变更

#### API 变更
- **移除任务提交参数**：`concurrency_key` 和 `max_concurrency` 不再支持
- **并发控制方式变更**：从任务级别改为任务类型级别的全局控制

#### 迁移指南
- **v0.0.2 → v0.0.3**：
  1. 移除任务提交时的并发控制参数
  2. 使用新的并发控制管理 API 设置任务类型级别的限制
  3. 更新客户端代码以适应新的 API 接口

---

## Version 0.0.2

### 🆕 新功能

#### 任务类型验证
- ✅ 提交任务时验证任务类型是否已注册
- ✅ 防止未注册任务类型导致的任务堆积问题
- ✅ 返回友好的错误信息提示用户注册处理器

#### 多Worker消费者支持
- ✅ 支持启动多个Worker消费者并发处理任务
- ✅ 实现WorkerPool管理多个消费者协程
- ✅ 避免协程泄露，支持优雅关闭
- ✅ 可配置Worker数量和处理间隔

#### 分布式任务竞争处理
- ✅ 基于Redis分布式锁防止任务重复处理
- ✅ 任务获取时自动加锁，处理完成后释放锁
- ✅ 支持锁超时和自动清理机制

### 🔧 改进优化
- 增强任务管理器接口，支持注册和验证处理器
- 优化任务消费逻辑，提高并发处理能力
- 改进错误处理和日志记录

### 📝 文档更新
- 更新README.md包含新功能说明
- 更新examples测试脚本
- 增加多Worker配置示例

---

## Version 0.0.1

### 🌟 核心功能

#### 分布式架构
- ✅ 基于 Kafka 实现消息队列，保证消息可靠性和持久化
- ✅ 使用 Redis 实现分布式锁和状态管理
- ✅ 支持水平扩展，可部署多个消费者节点

#### 智能并发控制
- ✅ 基于 Redis 实现分布式信号量
- ✅ 支持精确的并发数量控制
- ✅ 自动清理过期的并发令牌

#### 完善的任务生命周期管理
- ✅ 支持任务状态实时追踪（pending、processing、success、failed、canceled、timeout）
- ✅ 提供任务取消机制
- ✅ 支持任务重试和超时控制

#### 灵活的超时控制
- ✅ 支持队列等待超时设置
- ✅ 支持任务处理超时控制
- ✅ 自动处理超时任务

#### 实时状态推送
- ✅ 支持通过 MQTT 协议推送任务状态变更
- ✅ 浏览器可直接订阅任务状态更新
- ✅ 可配置消息质量（QoS）等级

#### RESTful API
- ✅ 完整的任务管理API（提交、查询、取消、统计）
- ✅ 支持任务优先级、重试、超时配置
- ✅ 提供健康检查和统计接口

#### 部署支持
- ✅ Docker容器化部署
- ✅ Docker Compose一键启动开发环境
- ✅ 完整的配置管理系统

### 🏗️ 技术架构

#### 核心组件
- **任务模型** (`internal/models/task.go`)：完整的任务结构体定义
- **配置管理** (`internal/config/config.go`)：统一配置管理
- **Redis存储** (`internal/storage/redis_storage.go`)：任务状态持久化
- **MQTT通知器** (`internal/notifier/mqtt_notifier.go`)：实时消息推送
- **Kafka队列** (`internal/queue/kafka_queue.go`)：消息队列实现
- **Redis信号量** (`internal/semaphore/redis_semaphore.go`)：并发控制
- **HTTP API** (`internal/api/handler.go`)：REST接口

#### 依赖技术栈
- **Go 1.21+**：主要开发语言
- **Redis**：状态存储和分布式锁
- **MQTT (Eclipse Mosquitto)**：实时消息推送
- **Apache Kafka**：消息队列
- **Gin**：HTTP框架
- **Docker & Docker Compose**：容器化部署

### 📚 文档和示例

#### 核心文档
- **README.md** (8397字节)：完整的项目介绍和使用指南
- **features_todo.md** (3497字节)：功能规划和版本路线图
- **examples/quick_start.md**：快速开始指南
- **doc/architecture.drawio**：系统架构图
- **doc/sequence_diagram.drawio**：时序图

#### 开发工具和示例
- **examples/test_api.sh**：完整的API测试脚本
- **examples/docker-compose.yml**：开发环境一键启动
- **examples/mqtt_client_example.html**：实时监控界面
- **examples/mosquitto.conf**：MQTT服务器配置

### 🎯 项目特点
- **企业级架构**：支持高并发、高可用部署
- **开箱即用**：完整的Docker环境和测试脚本
- **扩展性强**：模块化设计，易于定制和扩展
- **监控友好**：实时状态推送和详细日志记录
- **文档完善**：详细的使用指南和架构说明


