# XQueue 快速开始指南

这个指南将帮助您在几分钟内启动并运行 XQueue 分布式任务队列系统。

## 📋 前置要求

- Docker 和 Docker Compose
- Go 1.21+ (可选，如果要从源码构建)
- curl 和 jq (用于 API 测试)

## 🚀 Step 1: 启动依赖服务

```bash
# 进入 examples 目录
cd examples

# 启动 Redis 和 MQTT 服务
docker-compose up -d redis mosquitto

# 检查服务状态
docker-compose ps
```

您应该看到类似下面的输出：
```
NAME               IMAGE                 COMMAND                  SERVICE      CREATED      STATUS       PORTS
xqueue-mosquitto   eclipse-mosquitto:2   "/docker-entrypoint.…"   mosquitto    5 seconds ago   Up 4 seconds   0.0.0.0:1883->1883/tcp, 0.0.0.0:9001->9001/tcp
xqueue-redis       redis:7-alpine        "docker-entrypoint.s…"   redis        5 seconds ago   Up 4 seconds   0.0.0.0:6379->6379/tcp
```

## 🏗️ Step 2: 启动 XQueue 服务

```bash
# 回到项目根目录
cd ..

# 方式1: 直接运行 Go 程序
go run cmd/server/main.go

# 方式2: 先构建再运行
go build -o xqueue cmd/server/main.go
./xqueue

# 方式3: 使用 Docker (如果您有 Dockerfile)
docker build -t xqueue .
docker run -p 8080:8080 xqueue
```

启动成功后，您应该看到类似下面的日志：
```json
{"level":"info","msg":"Starting XQueue Server","time":"2024-06-24T21:30:00+08:00"}
{"level":"info","msg":"Connected to Redis successfully","time":"2024-06-24T21:30:00+08:00"}
{"level":"info","msg":"Connected to MQTT broker successfully","time":"2024-06-24T21:30:00+08:00"}
{"level":"info","msg":"Starting HTTP server","port":"8080","time":"2024-06-24T21:30:00+08:00"}
```

## 🧪 Step 3: 测试基本功能

### 检查服务健康状态
```bash
curl http://localhost:8080/api/v1/health
```

响应示例：
```json
{
  "status": "ok",
  "timestamp": 1719234600,
  "version": "1.0.0"
}
```

### 提交第一个任务
```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "type": "example",
    "payload": {
      "message": "Hello, XQueue!",
      "user_id": "user123"
    },
    "priority": 1,
    "max_retry": 3
  }'
```

响应示例：
```json
{
  "id": "task_1719234600123456789",
  "type": "example",
  "payload": {
    "message": "Hello, XQueue!",
    "user_id": "user123"
  },
  "status": "pending",
  "priority": 1,
  "retry": 0,
  "max_retry": 3,
  "created_at": "2024-06-24T21:30:00.123456789+08:00",
  "updated_at": "2024-06-24T21:30:00.123456789+08:00"
}
```

### 查询任务状态
```bash
# 使用上面返回的 task_id
curl http://localhost:8080/api/v1/tasks/task_1719234600123456789
```

### 获取系统统计
```bash
curl http://localhost:8080/api/v1/stats
```

## 🎯 Step 4: 高级功能测试

### 并发控制任务
```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "type": "example",
    "payload": {
      "message": "Concurrent task"
    },
    "concurrency_key": "user_123",
    "max_concurrency": 2
  }'
```

### 高优先级任务
```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "type": "example",
    "payload": {
      "message": "High priority task"
    },
    "priority": 10,
    "queue_timeout": 60,
    "process_timeout": 30
  }'
```

### 批量提交任务
```bash
for i in {1..5}; do
  curl -X POST http://localhost:8080/api/v1/tasks \
    -H "Content-Type: application/json" \
    -d '{
      "type": "example",
      "payload": {
        "message": "Batch task #'$i'",
        "batch_id": "batch_001"
      },
      "priority": '$i'
    }'
  echo
done
```

## 📡 Step 5: 实时监控 (MQTT)

您可以使用 MQTT 客户端订阅任务状态更新：

### 使用 mosquitto_sub 命令行工具
```bash
# 订阅所有任务状态更新
mosquitto_sub -h localhost -t "task/status/#" -v

# 订阅特定任务
mosquitto_sub -h localhost -t "task/status/task_1719234600123456789" -v

# 订阅成功状态的任务
mosquitto_sub -h localhost -t "task/status/status/success" -v
```

### 使用 WebSocket (浏览器)
在浏览器控制台中运行：
```javascript
// 连接到 MQTT WebSocket
const client = new Paho.MQTT.Client("localhost", 9001, "client_" + Math.random());

client.onConnectionLost = function(responseObject) {
  console.log("Connection lost:", responseObject.errorMessage);
};

client.onMessageArrived = function(message) {
  console.log("Task update:", message.destinationName, message.payloadString);
};

client.connect({
  onSuccess: function() {
    console.log("Connected to MQTT broker");
    client.subscribe("task/status/#");
  }
});
```

## 🧪 Step 6: 运行完整测试套件

```bash
# 确保测试脚本有执行权限
chmod +x examples/test_api.sh

# 运行完整的 API 测试
./examples/test_api.sh
```

这个脚本将测试：
- 健康检查
- 任务提交
- 状态查询
- 任务取消
- 并发控制
- 优先级处理
- 批量操作
- 系统统计

## 🛠️ 故障排查

### 常见问题

1. **服务无法启动**
   ```bash
   # 检查端口占用
   lsof -i :8080
   
   # 检查 Redis 连接
   redis-cli -h localhost -p 6379 ping
   
   # 检查 MQTT 连接
   mosquitto_pub -h localhost -t "test" -m "hello"
   ```

2. **任务提交失败**
   ```bash
   # 检查服务日志
   tail -f logs/xqueue.log
   
   # 验证 JSON 格式
   echo '{"type":"example","payload":{}}' | jq .
   ```

3. **MQTT 通知不工作**
   ```bash
   # 测试 MQTT 连接
   mosquitto_sub -h localhost -t "test" &
   mosquitto_pub -h localhost -t "test" -m "hello"
   ```

### 性能优化

1. **调整 Redis 配置**
   - 增加内存限制
   - 启用持久化
   - 优化网络设置

2. **调整并发参数**
   - 修改 `max_concurrency` 设置
   - 调整工作池大小
   - 优化任务处理时间

## 🎉 恭喜！

您已经成功启动并测试了 XQueue 分布式任务队列系统！

### 下一步

- 查看 [API 文档](../README.md#api-文档) 了解更多接口
- 阅读 [架构设计](../doc/architecture.md) 了解系统原理
- 参考 [功能规划](../features_todo.md) 了解后续发展
- 加入社区讨论和贡献代码

### 有用的链接

- 📖 [完整文档](../README.md)
- 🏗️ [架构图](../doc/architecture.drawio)
- 📊 [时序图](../doc/sequence_diagram.drawio)
- 🐛 [问题反馈](https://github.com/your-org/xqueue/issues)
- 💬 [讨论区](https://github.com/your-org/xqueue/discussions) 