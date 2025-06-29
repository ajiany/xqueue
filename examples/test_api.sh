#!/bin/bash

# XQueue API 测试脚本 (v0.0.3)
# 测试分布式任务队列的各项功能

BASE_URL="http://localhost:8080/api/v1"

echo "🚀 XQueue v0.0.3 API 功能测试"
echo "=============================="
echo ""

# 健康检查
echo "1. 健康检查..."
curl -s "${BASE_URL}/health" | jq .
echo ""

# 获取已注册的处理器
echo "2. 获取已注册的处理器..."
curl -s "${BASE_URL}/handlers" | jq .
echo ""

# v0.0.3 新增：获取并发限制配置
echo "3. 获取当前并发限制配置..."
curl -s "${BASE_URL}/concurrency" | jq .
echo ""

# v0.0.3 新增：设置并发限制
echo "4. 设置邮件任务并发限制为 2..."
curl -s -X POST "${BASE_URL}/concurrency" \
  -H "Content-Type: application/json" \
  -d '{
    "task_type": "email",
    "max_concurrency": 2
  }' | jq .
echo ""

# 确认并发限制已更新
echo "5. 确认并发限制已更新..."
curl -s "${BASE_URL}/concurrency" | jq .
echo ""

# 提交示例任务
echo "6. 提交示例任务..."
TASK_RESPONSE=$(curl -s -X POST "${BASE_URL}/tasks" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "example",
    "payload": {
      "message": "Hello, XQueue v0.0.3!",
      "test_version": "0.0.3"
    },
    "priority": 1
  }')

TASK_ID=$(echo $TASK_RESPONSE | jq -r '.id')
echo "示例任务已提交，ID: $TASK_ID"
echo $TASK_RESPONSE | jq .
echo ""

# 批量提交邮件任务测试并发控制
echo "7. 批量提交邮件任务测试并发控制 (限制为2个并发)..."
for i in {1..5}; do
  EMAIL_RESPONSE=$(curl -s -X POST "${BASE_URL}/tasks" \
    -H "Content-Type: application/json" \
    -d '{
      "type": "email",
      "payload": {
        "to": "test'$i'@example.com",
        "subject": "并发控制测试邮件 #'$i'",
        "body": "这是第'$i'个测试邮件，用于验证v0.0.3的并发控制功能"
      },
      "priority": '$i'
    }')
  
  EMAIL_ID=$(echo $EMAIL_RESPONSE | jq -r '.id')
  echo "邮件任务 #$i 已提交，ID: $EMAIL_ID"
done
echo ""

# 等待一会儿让任务处理
echo "8. 等待任务处理 (观察并发控制效果)..."
sleep 3

# 获取系统统计
echo "9. 获取系统统计..."
curl -s "${BASE_URL}/stats" | jq .
echo ""

# 查询特定任务状态
echo "10. 查询任务状态..."
echo "查询任务 $TASK_ID 的状态："
curl -s "${BASE_URL}/tasks/${TASK_ID}" | jq .
echo ""

# 获取待处理任务列表
echo "11. 获取待处理任务列表..."
curl -s "${BASE_URL}/tasks?status=pending&limit=10" | jq .
echo ""

# 获取处理中任务列表
echo "12. 获取处理中任务列表..."
curl -s "${BASE_URL}/tasks?status=processing&limit=10" | jq .
echo ""

# 获取已完成任务列表
echo "13. 获取已完成任务列表..."
curl -s "${BASE_URL}/tasks?status=success&limit=10" | jq .
echo ""

echo "✅ XQueue v0.0.3 API 测试完成！"
echo ""
echo "🔥 v0.0.3 新功能验证："
echo "   - ✅ 任务类型级别的全局并发控制"
echo "   - ✅ 并发限制配置管理API"
echo "   - ✅ 信号量令牌获取和释放"
echo "   - ✅ 批量任务并发限制验证"
echo ""
echo "📊 观察要点："
echo "   - 邮件任务应该最多同时处理2个"
echo "   - 示例任务应该最多同时处理3个"
echo "   - 超出限制的任务需要等待令牌释放"
echo ""
echo "🔧 使用说明："
echo "- 可以访问 http://localhost:8080/api/v1/handlers 查看已注册的处理器"
echo "- 可以访问 http://localhost:8080/api/v1/concurrency 查看并发限制配置"
echo "- 可以使用 WebSocket 连接 ws://localhost:9001 来接收实时通知" 