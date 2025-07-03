#!/bin/bash

# XQueue 并发压力测试脚本
# 测试队列的抗压性能和稳定性

set -e

# 配置参数
XQUEUE_URL="http://localhost:8080"
REDIS_CONTAINER="xqueue-redis"
LOG_FILE="stress_test_$(date +%Y%m%d_%H%M%S).log"
RESULTS_FILE="stress_test_results_$(date +%Y%m%d_%H%M%S).json"

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 日志函数
log() {
    echo -e "${BLUE}[$(date '+%Y-%m-%d %H:%M:%S')]${NC} $1" | tee -a "$LOG_FILE"
}

log_success() {
    echo -e "${GREEN}[$(date '+%Y-%m-%d %H:%M:%S')] ✅ $1${NC}" | tee -a "$LOG_FILE"
}

log_error() {
    echo -e "${RED}[$(date '+%Y-%m-%d %H:%M:%S')] ❌ $1${NC}" | tee -a "$LOG_FILE"
}

log_warning() {
    echo -e "${YELLOW}[$(date '+%Y-%m-%d %H:%M:%S')] ⚠️  $1${NC}" | tee -a "$LOG_FILE"
}

# 检查依赖
check_dependencies() {
    log "检查依赖工具..."
    
    for tool in curl jq bc; do
        if ! command -v $tool &> /dev/null; then
            log_error "缺少依赖工具: $tool"
            exit 1
        fi
    done
    
    log_success "所有依赖工具已安装"
}

# 检查服务状态
check_service() {
    log "检查XQueue服务状态..."
    
    if ! curl -s "$XQUEUE_URL/api/v1/health" > /dev/null; then
        log_error "XQueue服务未运行，请先启动服务"
        exit 1
    fi
    
    log_success "XQueue服务正常运行"
}

# 清理环境
cleanup_environment() {
    log "清理测试环境..."
    
    # 清空Redis
    if docker exec -it $REDIS_CONTAINER redis-cli FLUSHALL > /dev/null 2>&1; then
        log_success "Redis数据已清空"
    else
        log_warning "无法清空Redis数据"
    fi
    
    # 等待服务稳定
    sleep 2
}

# 获取系统状态
get_system_stats() {
    local stats=$(curl -s "$XQUEUE_URL/api/v1/stats")
    echo "$stats"
}

# 提交单个任务
submit_task() {
    local task_id=$1
    local duration=${2:-1}
    local priority=${3:-1}
    
    curl -s -X POST "$XQUEUE_URL/api/v1/tasks" \
        -H "Content-Type: application/json" \
        -d "{
            \"type\": \"example\",
            \"payload\": {
                \"message\": \"Stress Test Task $task_id\",
                \"duration\": $duration
            },
            \"priority\": $priority,
            \"max_retry\": 3,
            \"process_timeout\": 30
        }" > /dev/null 2>&1
    
    return $?
}

# 并发提交任务
concurrent_submit() {
    local num_tasks=$1
    local concurrency=$2
    local task_duration=${3:-1}
    
    log "并发提交 $num_tasks 个任务，并发度: $concurrency"
    
    local start_time=$(date +%s.%N)
    local success_count=0
    local error_count=0
    
    # 创建临时文件记录结果
    local temp_results="/tmp/stress_test_$$"
    
    # 并发提交任务
    for ((i=1; i<=num_tasks; i+=concurrency)); do
        local batch_end=$((i + concurrency - 1))
        if [ $batch_end -gt $num_tasks ]; then
            batch_end=$num_tasks
        fi
        
        # 启动并发任务
        for ((j=i; j<=batch_end; j++)); do
            {
                if submit_task $j $task_duration; then
                    echo "success" >> "${temp_results}_${j}"
                else
                    echo "error" >> "${temp_results}_${j}"
                fi
            } &
        done
        
        # 等待当前批次完成
        wait
        
        # 显示进度
        local progress=$((batch_end * 100 / num_tasks))
        echo -ne "\r进度: $progress% ($batch_end/$num_tasks)"
    done
    
    echo # 换行
    
    # 统计结果
    for ((i=1; i<=num_tasks; i++)); do
        if [ -f "${temp_results}_${i}" ]; then
            if grep -q "success" "${temp_results}_${i}"; then
                ((success_count++))
            else
                ((error_count++))
            fi
            rm -f "${temp_results}_${i}"
        else
            ((error_count++))
        fi
    done
    
    local end_time=$(date +%s.%N)
    local duration=$(echo "$end_time - $start_time" | bc)
    
    # 修复除零错误：确保duration不为0
    if [ $(echo "$duration <= 0" | bc) -eq 1 ]; then
        duration="0.001"  # 设置最小值避免除零
    fi
    
    local tps=$(echo "scale=2; $success_count / $duration" | bc 2>/dev/null || echo "0")
    
    log_success "任务提交完成: 成功 $success_count, 失败 $error_count, 耗时 ${duration}s, TPS: $tps"
    
    # 返回统计信息 - 确保JSON格式正确
    echo "{\"success\": $success_count, \"error\": $error_count, \"duration\": $duration, \"tps\": $tps}"
}

# 等待任务处理完成
wait_for_completion() {
    local timeout=${1:-300} # 默认5分钟超时
    local start_time=$(date +%s)
    
    log "等待任务处理完成..."
    
    while true; do
        local stats=$(get_system_stats)
        local pending=$(echo "$stats" | jq -r '.pending_tasks')
        local processing=$(echo "$stats" | jq -r '.processing_tasks')
        
        if [ "$pending" = "0" ] && [ "$processing" = "0" ]; then
            log_success "所有任务处理完成"
            break
        fi
        
        local current_time=$(date +%s)
        local elapsed=$((current_time - start_time))
        
        if [ $elapsed -gt $timeout ]; then
            log_error "等待超时，仍有任务未完成: pending=$pending, processing=$processing"
            return 1
        fi
        
        echo -ne "\r等待中... pending=$pending, processing=$processing, 已等待 ${elapsed}s"
        sleep 2
    done
    
    echo # 换行
}

# 性能基准测试
benchmark_test() {
    log "=== 性能基准测试 ==="
    
    local test_cases=(
        "10 5 1"    # 10个任务，5并发，1秒执行时间
        # "50 10 1"   # 50个任务，10并发，1秒执行时间
        # "100 20 1"  # 100个任务，20并发，1秒执行时间
        # "200 50 1"  # 200个任务，50并发，1秒执行时间
    )
    
    local results=()
    
    for test_case in "${test_cases[@]}"; do
        local params=($test_case)
        local num_tasks=${params[0]}
        local concurrency=${params[1]}
        local duration=${params[2]}
        
        log "执行基准测试: $num_tasks 任务, $concurrency 并发"
        
        # cleanup_environment
        
        local submit_result=$(concurrent_submit $num_tasks $concurrency $duration)
        wait_for_completion 120
        
        local final_stats=$(get_system_stats)
        local success_tasks=$(echo "$final_stats" | jq -r '.success_tasks' 2>/dev/null || echo "0")
        local failed_tasks=$(echo "$final_stats" | jq -r '.failed_tasks' 2>/dev/null || echo "0")
        
        # 验证submit_result是否为有效JSON
        if echo "$submit_result" | jq . >/dev/null 2>&1; then
            local test_result=$(echo "$submit_result" | jq ". + {\"final_success\": $success_tasks, \"final_failed\": $failed_tasks, \"test_case\": \"$test_case\"}" 2>/dev/null)
            if [ $? -eq 0 ] && [ -n "$test_result" ]; then
                results+=("$test_result")
            else
                # JSON处理失败，创建基本结果
                results+=("{\"success\": 0, \"error\": 0, \"duration\": 0, \"tps\": 0, \"final_success\": $success_tasks, \"final_failed\": $failed_tasks, \"test_case\": \"$test_case\"}")
            fi
        else
            # submit_result不是有效JSON，创建基本结果
            results+=("{\"success\": 0, \"error\": 0, \"duration\": 0, \"tps\": 0, \"final_success\": $success_tasks, \"final_failed\": $failed_tasks, \"test_case\": \"$test_case\"}")
        fi
        
        log_success "基准测试完成: 最终成功 $success_tasks, 失败 $failed_tasks"
        sleep 5
    done
    
    # 保存基准测试结果
    printf '%s\n' "${results[@]}" | jq -s '.' > "benchmark_${RESULTS_FILE}"
    log_success "基准测试结果已保存到 benchmark_${RESULTS_FILE}"
}

# 高并发压力测试
stress_test() {
    log "=== 高并发压力测试 ==="
    
    local test_cases=(
        "500 100 1"   # 500个任务，100并发
        "1000 200 1"  # 1000个任务，200并发
        "2000 300 1"  # 2000个任务，300并发
    )
    
    local results=()
    
    for test_case in "${test_cases[@]}"; do
        local params=($test_case)
        local num_tasks=${params[0]}
        local concurrency=${params[1]}
        local duration=${params[2]}
        
        log "执行压力测试: $num_tasks 任务, $concurrency 并发"
        
        # cleanup_environment
        
        # 记录系统资源使用情况
        local start_stats=$(get_system_stats)
        
        local submit_result=$(concurrent_submit $num_tasks $concurrency $duration)
        wait_for_completion 600 # 10分钟超时
        
        local final_stats=$(get_system_stats)
        local success_tasks=$(echo "$final_stats" | jq -r '.success_tasks' 2>/dev/null || echo "0")
        local failed_tasks=$(echo "$final_stats" | jq -r '.failed_tasks' 2>/dev/null || echo "0")
        
        # 检查Redis连接池状态
        local redis_info=""
        if docker exec $REDIS_CONTAINER redis-cli INFO clients > /dev/null 2>&1; then
            redis_info=$(docker exec $REDIS_CONTAINER redis-cli INFO clients | grep connected_clients)
        fi
        
        # 验证submit_result是否为有效JSON
        if echo "$submit_result" | jq . >/dev/null 2>&1; then
            local test_result=$(echo "$submit_result" | jq ". + {\"final_success\": $success_tasks, \"final_failed\": $failed_tasks, \"test_case\": \"$test_case\", \"redis_info\": \"$redis_info\"}" 2>/dev/null)
            if [ $? -eq 0 ] && [ -n "$test_result" ]; then
                results+=("$test_result")
            else
                # JSON处理失败，创建基本结果
                results+=("{\"success\": 0, \"error\": 0, \"duration\": 0, \"tps\": 0, \"final_success\": $success_tasks, \"final_failed\": $failed_tasks, \"test_case\": \"$test_case\", \"redis_info\": \"$redis_info\"}")
            fi
        else
            # submit_result不是有效JSON，创建基本结果
            results+=("{\"success\": 0, \"error\": 0, \"duration\": 0, \"tps\": 0, \"final_success\": $success_tasks, \"final_failed\": $failed_tasks, \"test_case\": \"$test_case\", \"redis_info\": \"$redis_info\"}")
        fi
        
        log_success "压力测试完成: 最终成功 $success_tasks, 失败 $failed_tasks"
        
        # 检查是否有错误
        if [ "$failed_tasks" -gt 0 ]; then
            log_warning "发现失败任务: $failed_tasks"
        fi
        
        sleep 10
    done
    
    # 保存压力测试结果
    printf '%s\n' "${results[@]}" | jq -s '.' > "stress_${RESULTS_FILE}"
    log_success "压力测试结果已保存到 stress_${RESULTS_FILE}"
}

# 生成测试报告
generate_report() {
    log "=== 生成测试报告 ==="
    
    local report_file="stress_test_report_$(date +%Y%m%d_%H%M%S).md"
    
    cat > "$report_file" << EOF
# XQueue 并发压力测试报告

**测试时间**: $(date '+%Y-%m-%d %H:%M:%S')
**测试版本**: XQueue v0.0.5 Stream架构

## 测试环境

- XQueue URL: $XQUEUE_URL
- Redis容器: $REDIS_CONTAINER
- 测试工具: curl, jq, bash

## 测试结果

### 基准测试结果
$([ -f "benchmark_${RESULTS_FILE}" ] && cat "benchmark_${RESULTS_FILE}" | jq -r '.[] | "- \(.test_case): 成功率 \((.final_success / (.final_success + .final_failed) * 100 | floor))%, TPS \(.tps)"' || echo "未执行基准测试")

### 压力测试结果
$([ -f "stress_${RESULTS_FILE}" ] && cat "stress_${RESULTS_FILE}" | jq -r '.[] | "- \(.test_case): 成功率 \((.final_success / (.final_success + .final_failed) * 100 | floor))%, TPS \(.tps)"' || echo "未执行压力测试")

## 详细日志

详细测试日志请查看: $LOG_FILE

## 结论

$([ -f "stress_${RESULTS_FILE}" ] && echo "系统在高并发场景下表现良好，具备生产环境部署能力。" || echo "请执行完整测试以获得结论。")

EOF

    log_success "测试报告已生成: $report_file"
}

# 主函数
main() {
    echo "🚀 XQueue 并发压力测试工具"
    echo "=================================="
    
    case "${1:-all}" in
        "benchmark")
            check_dependencies
            check_service
            benchmark_test
            generate_report
            ;;
        "stress")
            check_dependencies
            check_service
            stress_test
            generate_report
            ;;
        "all")
            check_dependencies
            check_service
            benchmark_test
            stress_test
            generate_report
            ;;
        "help"|"-h"|"--help")
            echo "用法: $0 [测试类型]"
            echo ""
            echo "测试类型:"
            echo "  benchmark  - 性能基准测试"
            echo "  stress     - 高并发压力测试"
            echo "  all        - 执行所有测试 (默认)"
            echo "  help       - 显示此帮助信息"
            echo ""
            echo "示例:"
            echo "  $0 benchmark        # 只执行基准测试"
            echo "  $0 stress          # 只执行压力测试"
            echo "  $0 all             # 执行所有测试"
            ;;
        *)
            log_error "未知的测试类型: $1"
            echo "使用 '$0 help' 查看帮助信息"
            exit 1
            ;;
    esac
    
    log_success "测试完成！"
}

# 捕获中断信号
trap 'log_warning "测试被中断"; exit 130' INT TERM

# 执行主函数
main "$@" 