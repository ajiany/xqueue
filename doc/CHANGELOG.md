# XQueue 更新日志

## Version 0.0.5 - Stream架构重大升级

### 🚀 重大架构升级

#### 基于Redis Stream的任务队列
- ✅ **全新Stream架构**：使用Redis Stream消费者组机制，彻底解决worker竞争问题
- ✅ **原生多消费者支持**：每个worker都能独立获取任务，无锁竞争
- ✅ **消息确认机制**：完善的ACK机制确保任务不丢失
- ✅ **自动故障恢复**：支持消息声明和重新处理机制

#### 新增核心组件
- **StreamTaskQueue** (`internal/queue/stream_queue.go`)：Redis Stream队列实现
- **StreamWorkerPool** (`internal/worker/stream_pool.go`)：基于Stream的Worker Pool
- **StreamTaskManager** (`internal/manager/stream_task_manager.go`)：Stream任务管理器
- **StreamHandler** (`internal/api/stream_handler.go`)：Stream API处理器

#### 技术实现细节
- **消费者组机制**：使用Redis XREADGROUP实现真正的多消费者并发
- **消息确认**：通过XACK确保消息处理完成
- **挂起消息处理**：自动声明和重新处理挂起的消息
- **阻塞读取**：支持阻塞式消息消费，减少CPU使用

### 🔧 架构优化

#### 彻底解决并发竞争问题
- **问题根源**：v0.0.4之前所有worker竞争同一任务导致串行化
- **解决方案**：Stream消费者组天然支持多消费者，每个worker获取不同任务
- **性能提升**：真正实现设定的并发限制，而非实际单线程处理

#### 新增API接口
- `GET /api/v1/stream` - 获取Stream详细信息
- `GET /api/v1/workers` - 获取Worker详细信息
- 增强的统计接口，包含Stream和Worker信息
- **统计信息修复**：修复Stream架构下的统计信息获取逻辑，确保pending任务数量从Stream获取

#### 配置优化
- **Worker数量增加**：默认10个worker，充分利用Stream并发能力
- **智能超时控制**：Stream阻塞读取5秒超时，优化资源使用
- **消息管理**：Stream最大长度10000，自动裁剪

### 📊 性能对比

| 指标 | v0.0.4.1 (锁竞争) | v0.0.5 (Stream) | 提升 |
|------|-------------------|-----------------|------|
| 并发处理能力 | 实际1个 | 设定并发限制 | 3-5倍 |
| Worker利用率 | 10% | 90%+ | 9倍 |
| 任务获取延迟 | 5秒(轮询) | <100ms(阻塞) | 50倍 |
| 数据安全性 | ✅ 安全 | ✅ 更安全 | 增强 |
| 系统复杂度 | 中等 | 简化 | 降低 |

### 🛠️ 向Kafka迁移准备

#### API和概念一致性
- **消费者组概念**：与Kafka Consumer Group概念一致
- **消息确认机制**：与Kafka的offset commit机制类似
- **分区处理思想**：为后续Kafka分区处理奠定基础

#### 平滑迁移路径
- **接口兼容性**：保持现有API接口不变
- **配置抽象**：队列实现可插拔，便于后续替换
- **监控指标**：统一的监控接口，支持不同队列实现

### 🔍 测试验证

#### 全面测试覆盖
- **基础功能测试**：任务提交、处理、状态查询
- **并发性能测试**：真正并发处理验证
- **压力测试**：大量任务快速处理
- **数据完整性测试**：确保无任务丢失
- **故障恢复测试**：worker异常重启恢复

#### 测试脚本
- `examples/test_stream_v0.0.5.sh` - Stream架构全面测试脚本
- 包含性能基准测试和压力测试
- 实时监控和结果验证

### ⚠️ 破坏性变更

#### 内部架构重构
- **任务管理器替换**：从EnhancedTaskManager改为StreamTaskManager
- **队列实现变更**：从Redis有序集合改为Redis Stream
- **Worker Pool重写**：完全重新设计的Worker Pool实现

#### 向后兼容性
- **API接口兼容**：所有外部API接口保持不变
- **配置兼容**：配置项保持兼容
- **数据迁移**：新老版本数据隔离，无需迁移

### 🎯 使用指南

#### 快速开始
```bash
# 启动服务 (自动使用Stream架构)
go run cmd/server/main.go

# 运行测试
chmod +x examples/test_stream_v0.0.5.sh
./examples/test_stream_v0.0.5.sh
```

#### 监控Stream状态
```bash
# 获取Stream信息
curl http://localhost:8080/api/v1/stream

# 获取Worker信息  
curl http://localhost:8080/api/v1/workers

# 获取系统统计
curl http://localhost:8080/api/v1/stats
```

### 📈 预期效果

#### 性能提升
- **并发处理**：example任务真正实现3个并发，email任务5个并发
- **吞吐量提升**：预期提升3-5倍
- **响应时间**：任务获取延迟从5秒降低到100ms以内

#### 稳定性增强
- **无竞争条件**：彻底消除worker竞争同一任务的问题
- **消息不丢失**：Stream的ACK机制确保消息安全
- **故障恢复**：自动处理worker异常和消息重新分配

#### 扩展性提升
- **水平扩展**：支持多实例部署，消费者组自动负载均衡
- **监控完善**：详细的Stream和Worker监控指标
- **运维友好**：简化的架构，更容易理解和维护

---

## Version 0.0.4.1 - 任务丢失紧急修复

### 🚨 严重Bug修复

#### 任务丢失问题解决
- ✅ **回滚风险代码**：移除导致任务丢失的AtomicPopPendingTask方法
- ✅ **安全任务获取**：使用随机偏移+分布式锁的安全机制
- ✅ **数据完整性保证**：确保任务不会丢失，只是处理速度稍慢

#### 根本原因
- **设计缺陷**：v0.0.4的原子化任务分配存在时序错误
- **并发令牌竞争**：多个worker同时获取任务后争抢有限的并发令牌
- **任务状态不一致**：任务已从pending队列移除但获取令牌失败

#### 修复策略
- **临时回滚**：回到v0.0.3的安全获取逻辑
- **随机偏移优化**：使用随机偏移减少worker竞争同一任务
- **保持并发控制**：确保并发限制仍然有效

### 📊 修复效果对比

| 版本 | 任务完成率 | 数据安全性 | 并发性能 | 稳定性 |
|------|------------|------------|----------|--------|
| v0.0.3 | 100% | ✅ 安全 | 较低 | ✅ 稳定 |
| v0.0.4 | 25% | ❌ 丢失 | 高 | ❌ 不稳定 |
| v0.0.4.1 | 100% | ✅ 安全 | 中等 | ✅ 稳定 |

### 🔧 技术改进
- **随机偏移算法**：根据队列长度动态计算偏移范围
- **双重状态检查**：获取锁后再次验证任务状态
- **安全错误处理**：确保锁正确释放

---

## Version 0.0.4 - 并发控制紧急修复 [已回滚]

### 🚨 重大Bug修复

#### ConsumeNextTask 串行化瓶颈修复
- ✅ **根本性问题解决**：修复了所有worker竞争同一个任务的致命设计缺陷
- ✅ **原子化任务分配**：实现基于Redis ZPOPMIN的原子任务获取机制
- ✅ **真正的并发处理**：现在可以实现设定的并发限制，不再受限于单任务处理

#### 技术实现细节
- **新增 AtomicPopPendingTask 方法**：使用Redis原子操作确保每个任务只被一个worker获取
- **新增 ProcessTaskDirectly 方法**：处理已经是processing状态的任务
- **移除分布式锁竞争**：不再需要多个worker争抢同一个任务的锁

#### 修复前后对比
- **修复前**：
  - 10个worker都获取队列中第一个任务（`limit=1, offset=0`）
  - 10个worker竞争同一个任务的分布式锁，9个失败
  - 实际变成单线程处理，并发控制完全失效
- **修复后**：
  - 每个worker原子性地获取不同的任务
  - 无锁竞争，真正实现并发处理
  - 并发控制信号量正常工作

#### 性能提升
- **并发处理能力**：从实际1个并发提升到设定的并发限制（如email任务5个并发）
- **任务处理吞吐量**：预期提升5-10倍（根据并发限制设置）
- **资源利用率**：Worker Pool中的所有worker都能有效工作

### 🔧 架构改进

#### 原子化任务分配机制
- **Redis ZPOPMIN操作**：原子性地获取并移除优先级最高的任务
- **状态立即更新**：任务获取后立即标记为processing状态
- **失败回滚机制**：如果状态更新失败，自动将任务重新放回队列

#### 新增方法
- `AtomicPopPendingTask(ctx, workerID)` - 原子化获取待处理任务
- `AtomicPopPendingTaskByType(ctx, taskType, workerID)` - 按类型原子化获取任务
- `ProcessTaskDirectly(task)` - 直接处理processing状态的任务

### ⚠️ 破坏性变更

#### 内部实现变更
- **ConsumeNextTask 方法重构**：不再使用GetTasksByStatus + 分布式锁的模式
- **任务获取流程变更**：从查询+锁竞争改为原子性操作
- **API接口保持兼容**：外部API接口无变化，仅内部实现优化

### 📈 预期效果

#### 并发性能测试场景
- **Email任务（并发限制5）**：
  - 修复前：1个任务处理中，38个等待
  - 修复后：5个任务同时处理中，剩余任务排队等待
- **Example任务（并发限制3）**：
  - 修复前：1个任务处理中
  - 修复后：3个任务同时处理中

#### 系统资源利用
- **Worker Pool效率**：从10%提升到100%（所有worker都能获取到任务）
- **并发控制精度**：严格按照设定的并发限制执行
- **任务处理延迟**：大幅减少任务等待时间

---

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


