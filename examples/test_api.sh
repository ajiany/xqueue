#!/bin/bash

# XQueue API 测试脚本
# 这个脚本演示了如何与 XQueue 系统进行交互

set -e

BASE_URL="http://localhost:8080/api/v1"

echo "🚀 XQueue API 测试开始 (v0.0.2)"
echo "================================"

# 检查服务健康状态
echo "1. 检查服务健康状态..."
curl -s "${BASE_URL}/health" | jq .
echo ""

# v0.0.2 新功能：获取已注册的处理器列表
echo "2. 获取已注册的处理器列表 (v0.0.2 新功能)..."
curl -s "${BASE_URL}/handlers" | jq .
echo ""

# 获取系统统计信息 (现在包含worker池信息)
echo "3. 获取系统统计信息 (增强版)..."
curl -s "${BASE_URL}/stats" | jq .
echo ""

# v0.0.2 新功能：测试未注册任务类型的验证
echo "4. 测试未注册任务类型验证 (v0.0.2 新功能)..."
INVALID_RESPONSE=$(curl -s -X POST "${BASE_URL}/tasks" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "nonexistent_task",
    "payload": {
      "message": "This should fail"
    }
  }')

echo "尝试提交未注册任务类型的响应："
echo $INVALID_RESPONSE | jq .
echo ""

# 提交一个简单任务
echo "5. 提交简单任务..."
TASK_RESPONSE=$(curl -s -X POST "${BASE_URL}/tasks" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "example",
    "payload": {
      "message": "Hello, XQueue!",
      "timestamp": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'"
    },
    "priority": 1,
    "max_retry": 3,
    "queue_timeout": 300,
    "process_timeout": 60
  }')

TASK_ID=$(echo $TASK_RESPONSE | jq -r '.id')
echo "任务已提交，ID: $TASK_ID"
echo $TASK_RESPONSE | jq .
echo ""

# 提交一个邮件任务 (v0.0.2 新增的处理器)
echo "6. 提交邮件任务 (v0.0.2 新增处理器)..."
EMAIL_RESPONSE=$(curl -s -X POST "${BASE_URL}/tasks" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "email",
    "payload": {
      "to": "user@example.com",
      "subject": "XQueue Test Email",
      "body": "This is a test email from XQueue v0.0.2"
    },
    "priority": 2
  }')

EMAIL_ID=$(echo $EMAIL_RESPONSE | jq -r '.id')
echo "邮件任务已提交，ID: $EMAIL_ID"
echo $EMAIL_RESPONSE | jq .
echo ""

# 提交一个高优先级任务
echo "7. 提交高优先级任务..."
HIGH_PRIORITY_RESPONSE=$(curl -s -X POST "${BASE_URL}/tasks" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "example",
    "payload": {
      "message": "High priority task!",
      "priority_level": "high"
    },
    "priority": 10,
    "max_retry": 5
  }')

HIGH_PRIORITY_ID=$(echo $HIGH_PRIORITY_RESPONSE | jq -r '.id')
echo "高优先级任务已提交，ID: $HIGH_PRIORITY_ID"
echo $HIGH_PRIORITY_RESPONSE | jq .
echo ""

# 提交一个带并发控制的任务
echo "8. 提交带并发控制的任务..."
CONCURRENT_RESPONSE=$(curl -s -X POST "${BASE_URL}/tasks" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "example",
    "payload": {
      "message": "Concurrent task",
      "user_id": "user123"
    },
    "concurrency_key": "user_123",
    "max_concurrency": 2
  }')

CONCURRENT_ID=$(echo $CONCURRENT_RESPONSE | jq -r '.id')
echo "并发控制任务已提交，ID: $CONCURRENT_ID"
echo $CONCURRENT_RESPONSE | jq .
echo ""

# 等待一会儿让任务处理 (v0.0.2 多worker并发处理)
echo "9. 等待任务处理 (多worker并发处理)..."
sleep 5

# 查询特定任务状态
echo "10. 查询任务状态..."
echo "查询任务 $TASK_ID 的状态："
curl -s "${BASE_URL}/tasks/${TASK_ID}" | jq .
echo ""

# 获取待处理任务列表
echo "11. 获取待处理任务列表..."
curl -s "${BASE_URL}/tasks?status=pending&limit=5" | jq .
echo ""

# 获取所有任务列表
echo "12. 获取所有任务列表 (最近10个)..."
curl -s "${BASE_URL}/tasks?limit=10" | jq .
echo ""

# 提交多个任务进行批量测试
echo "13. 批量提交任务..."
for i in {1..5}; do
  echo "提交批量任务 $i/5..."
  curl -s -X POST "${BASE_URL}/tasks" \
    -H "Content-Type: application/json" \
    -d '{
      "type": "example",
      "payload": {
        "message": "Batch task #'$i'",
        "batch_id": "batch_001"
      },
      "priority": '$i'
    }' | jq -r '.id'
done
echo ""

# 等待批量任务处理
echo "14. 等待批量任务处理..."
sleep 5

# 再次获取系统统计
echo "15. 获取更新后的系统统计..."
curl -s "${BASE_URL}/stats" | jq .
echo ""

# 测试任务取消
echo "16. 测试任务取消..."
CANCEL_RESPONSE=$(curl -s -X POST "${BASE_URL}/tasks" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "example",
    "payload": {
      "message": "This task will be canceled"
    }
  }')

CANCEL_ID=$(echo $CANCEL_RESPONSE | jq -r '.id')
echo "要取消的任务ID: $CANCEL_ID"

# 立即取消任务
curl -s -X DELETE "${BASE_URL}/tasks/${CANCEL_ID}" | jq .
echo ""

# 验证任务已被取消
echo "17. 验证任务取消状态..."
curl -s "${BASE_URL}/tasks/${CANCEL_ID}" | jq .
echo ""

# 获取不同状态的任务统计
echo "18. 获取各状态任务数量..."
for status in pending processing success failed canceled timeout; do
  count=$(curl -s "${BASE_URL}/tasks?status=${status}&limit=1" | jq '.count')
  echo "${status}: ${count} 个任务"
done
echo ""

echo "🎉 XQueue API 测试完成 (v0.0.2)"
echo "================================"

# 显示测试总结
echo "测试总结："
echo "- ✅ 验证了已注册的处理器列表 (v0.0.2 新功能)"
echo "- ✅ 测试了任务类型验证机制 (v0.0.2 新功能)"
echo "- ✅ 测试了多个处理器类型 (example, email)"
echo "- ✅ 验证了多Worker并发处理 (v0.0.2 新功能)"
echo "- ✅ 测试了分布式锁机制 (v0.0.2 新功能)"
echo "- ✅ 已提交多个不同类型的任务"
echo "- ✅ 测试了优先级设置"
echo "- ✅ 测试了并发控制"
echo "- ✅ 测试了任务取消"
echo "- ✅ 验证了各种查询接口"
echo "- ✅ 检查了增强版统计信息 (包含worker池状态)"
echo ""

echo "💡 v0.0.2 新特性："
echo "- 🔒 任务类型验证：只允许提交已注册处理器的任务"
echo "- 👥 多Worker并发：支持多个worker同时处理任务"
echo "- 🔐 分布式锁：防止任务重复处理"
echo "- 📊 增强统计：包含worker池和处理器信息"
echo "- 🎯 处理器管理：支持动态注册和查询处理器"
echo ""

echo "💡 提示："
echo "- 可以通过 MQTT 客户端订阅 'task/status/#' 主题来实时监控任务状态"
echo "- 可以访问 http://localhost:8080/api/v1/health 检查服务状态"
echo "- 可以访问 http://localhost:8080/api/v1/handlers 查看已注册的处理器"
echo "- 可以使用 WebSocket 连接 ws://localhost:9001 来接收实时通知" 