package semaphore

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

// RedisSemaphore 基于Redis的分布式信号量
type RedisSemaphore struct {
	client     *redis.Client
	key        string
	maxTokens  int64
	expiration time.Duration
}

// NewRedisSemaphore 创建新的Redis信号量
func NewRedisSemaphore(client *redis.Client, key string, maxTokens int64, expiration time.Duration) *RedisSemaphore {
	return &RedisSemaphore{
		client:     client,
		key:        key,
		maxTokens:  maxTokens,
		expiration: expiration,
	}
}

// Acquire 获取信号量令牌
func (s *RedisSemaphore) Acquire(ctx context.Context) (string, error) {
	token := uuid.New().String()

	// 使用Go的时间戳而不是Redis的TIME命令，避免可能的nil值
	now := time.Now().Unix()

	// Lua脚本确保原子性操作
	luaScript := `
		local key = KEYS[1]
		local token = ARGV[1]
		local max_tokens = tonumber(ARGV[2])
		local expiration = tonumber(ARGV[3])
		local current_time = tonumber(ARGV[4])
		
		-- 清理过期的令牌
		redis.call('ZREMRANGEBYSCORE', key, 0, current_time - expiration)
		
		-- 检查当前令牌数量
		local current_count = redis.call('ZCARD', key)
		if current_count < max_tokens then
			-- 添加新令牌
			redis.call('ZADD', key, current_time, token)
			redis.call('EXPIRE', key, expiration)
			return token
		else
			return "NO_TOKENS_AVAILABLE"
		end
	`

	result, err := s.client.Eval(ctx, luaScript, []string{s.key},
		token, s.maxTokens, int64(s.expiration.Seconds()), now).Result()

	if err != nil {
		return "", fmt.Errorf("failed to acquire semaphore: %w", err)
	}

	// 检查返回值
	if result == nil {
		return "", fmt.Errorf("no available tokens")
	}

	resultStr, ok := result.(string)
	if !ok {
		return "", fmt.Errorf("unexpected result type from semaphore script")
	}

	if resultStr == "NO_TOKENS_AVAILABLE" {
		return "", fmt.Errorf("no available tokens")
	}

	return resultStr, nil
}

// Release 释放信号量令牌
func (s *RedisSemaphore) Release(ctx context.Context, token string) error {
	luaScript := `
		local key = KEYS[1]
		local token = ARGV[1]
		return redis.call('ZREM', key, token)
	`

	_, err := s.client.Eval(ctx, luaScript, []string{s.key}, token).Result()
	if err != nil {
		return fmt.Errorf("failed to release semaphore token: %w", err)
	}

	return nil
}

// Count 获取当前令牌数量
func (s *RedisSemaphore) Count(ctx context.Context) (int64, error) {
	// 先清理过期令牌
	now := time.Now().Unix()
	expiredTime := now - int64(s.expiration.Seconds())

	err := s.client.ZRemRangeByScore(ctx, s.key, "0", fmt.Sprintf("%d", expiredTime)).Err()
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup expired tokens: %w", err)
	}

	count, err := s.client.ZCard(ctx, s.key).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get token count: %w", err)
	}

	return count, nil
}

// Available 获取可用令牌数量
func (s *RedisSemaphore) Available(ctx context.Context) (int64, error) {
	current, err := s.Count(ctx)
	if err != nil {
		return 0, err
	}

	return s.maxTokens - current, nil
}

// Cleanup 清理过期令牌
func (s *RedisSemaphore) Cleanup(ctx context.Context) error {
	now := time.Now().Unix()
	expiredTime := now - int64(s.expiration.Seconds())

	removed, err := s.client.ZRemRangeByScore(ctx, s.key, "0", fmt.Sprintf("%d", expiredTime)).Result()
	if err != nil {
		return fmt.Errorf("failed to cleanup expired tokens: %w", err)
	}

	if removed > 0 {
		// 可以记录日志
	}

	return nil
}
