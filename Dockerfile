# 构建阶段
FROM golang:1.21-alpine AS builder

# 设置工作目录
WORKDIR /app

# 安装必要的系统工具
RUN apk add --no-cache git ca-certificates tzdata

# 复制 go mod 文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 构建应用
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o xqueue cmd/server/main.go

# 运行阶段
FROM alpine:latest

# 安装 ca-certificates 和 curl (用于健康检查)
RUN apk --no-cache add ca-certificates curl

# 创建非 root 用户
RUN addgroup -g 1001 -S xqueue && \
    adduser -u 1001 -S xqueue -G xqueue

# 设置工作目录
WORKDIR /app

# 从构建阶段复制二进制文件
COPY --from=builder /app/xqueue .
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# 设置时区
ENV TZ=Asia/Shanghai

# 改变文件所有者
RUN chown -R xqueue:xqueue /app

# 切换到非 root 用户
USER xqueue

# 暴露端口
EXPOSE 8080

# 健康检查
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/api/v1/health || exit 1

# 启动应用
CMD ["./xqueue"] 