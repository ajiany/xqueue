# features and bug todo fixed
## v0.0.3
1. **并发控制功能设计和实现都存在根本性错误**
   - 核心问题：当前并发控制基于单个任务实例，无法实现任务类型级别的全局并发控制
   - 设计层面错误：
     - `WithConcurrency(key, maxConcurrency)` 将并发配置存储在每个 Task 实例中
     - 每个任务都携带自己的 `ConcurrencyKey` 和 `MaxConcurrency` 字段
     - API 接口设计为每个任务单独设置并发参数
   - 实现层面缺失：
     - 即使按错误设计，在 `ProcessTask` 方法中也完全没有使用这些并发控制字段
     - 已有的 `internal/semaphore/redis_semaphore.go` 模块未被集成到任务处理流程中
   - 问题场景：
     - 用户提交100个 "email" 类型任务，每个都设置 `max_concurrency: 5`
     - 期望：全局最多5个邮件任务并发处理
     - 实际：100个任务可能同时运行，完全没有并发限制
   - 正确设计应该是：
     - 并发控制在 TaskManager 层面管理：`taskManager.SetConcurrencyLimit("email", 5)`
     - 全局并发配置管理器，按任务类型存储并发限制
     - 处理任务时根据任务类型获取对应的信号量进行控制
   - 修复建议：
     - 重新设计并发控制架构，从 Task 级别提升到 TaskManager 级别
     - 实现 ConcurrencyManager 组件管理任务类型的全局并发限制
     - 重构 API 接口，支持任务类型级别的并发配置
     - 在任务处理流程中正确集成信号量控制