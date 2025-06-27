package lock

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/sirupsen/logrus"
)

// RedisLock Redis分布式锁
type RedisLock struct {
	client *redis.Client
	logger *logrus.Logger
}

// NewRedisLock 创建Redis分布式锁
func NewRedisLock(client *redis.Client, logger *logrus.Logger) *RedisLock {
	return &RedisLock{
		client: client,
		logger: logger,
	}
}

// AcquireLock 获取锁
func (rl *RedisLock) AcquireLock(ctx context.Context, key string, value string, expiration time.Duration) (bool, error) {
	lockKey := fmt.Sprintf("lock:%s", key)

	// 使用 SET NX EX 命令获取锁
	result, err := rl.client.SetNX(ctx, lockKey, value, expiration).Result()
	if err != nil {
		rl.logger.WithError(err).WithField("lock_key", lockKey).Error("Failed to acquire lock")
		return false, err
	}

	if result {
		rl.logger.WithFields(logrus.Fields{
			"lock_key":   lockKey,
			"value":      value,
			"expiration": expiration,
		}).Debug("Lock acquired successfully")
	}

	return result, nil
}

// ReleaseLock 释放锁
func (rl *RedisLock) ReleaseLock(ctx context.Context, key string, value string) error {
	lockKey := fmt.Sprintf("lock:%s", key)

	// 使用Lua脚本确保只有锁的持有者才能释放锁
	luaScript := `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		else
			return 0
		end
	`

	result, err := rl.client.Eval(ctx, luaScript, []string{lockKey}, value).Result()
	if err != nil {
		rl.logger.WithError(err).WithField("lock_key", lockKey).Error("Failed to release lock")
		return err
	}

	if result.(int64) == 1 {
		rl.logger.WithFields(logrus.Fields{
			"lock_key": lockKey,
			"value":    value,
		}).Debug("Lock released successfully")
	} else {
		rl.logger.WithFields(logrus.Fields{
			"lock_key": lockKey,
			"value":    value,
		}).Warn("Lock was not released (may have expired or been released by another process)")
	}

	return nil
}

// ExtendLock 延长锁的过期时间
func (rl *RedisLock) ExtendLock(ctx context.Context, key string, value string, expiration time.Duration) (bool, error) {
	lockKey := fmt.Sprintf("lock:%s", key)

	// 使用Lua脚本确保只有锁的持有者才能延长锁
	luaScript := `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("EXPIRE", KEYS[1], ARGV[2])
		else
			return 0
		end
	`

	result, err := rl.client.Eval(ctx, luaScript, []string{lockKey}, value, int(expiration.Seconds())).Result()
	if err != nil {
		rl.logger.WithError(err).WithField("lock_key", lockKey).Error("Failed to extend lock")
		return false, err
	}

	success := result.(int64) == 1
	if success {
		rl.logger.WithFields(logrus.Fields{
			"lock_key":   lockKey,
			"value":      value,
			"expiration": expiration,
		}).Debug("Lock extended successfully")
	}

	return success, nil
}

// IsLocked 检查锁是否存在
func (rl *RedisLock) IsLocked(ctx context.Context, key string) (bool, error) {
	lockKey := fmt.Sprintf("lock:%s", key)

	exists, err := rl.client.Exists(ctx, lockKey).Result()
	if err != nil {
		return false, err
	}

	return exists > 0, nil
}

// GetLockValue 获取锁的值
func (rl *RedisLock) GetLockValue(ctx context.Context, key string) (string, error) {
	lockKey := fmt.Sprintf("lock:%s", key)

	value, err := rl.client.Get(ctx, lockKey).Result()
	if err != nil {
		if err == redis.Nil {
			return "", fmt.Errorf("lock not found")
		}
		return "", err
	}

	return value, nil
}

// CleanupExpiredLocks 清理过期的锁（可选的维护方法）
func (rl *RedisLock) CleanupExpiredLocks(ctx context.Context, pattern string) error {
	lockPattern := fmt.Sprintf("lock:%s", pattern)

	keys, err := rl.client.Keys(ctx, lockPattern).Result()
	if err != nil {
		return err
	}

	for _, key := range keys {
		// 检查键是否存在（Redis会自动清理过期键，但这里可以做额外检查）
		exists, err := rl.client.Exists(ctx, key).Result()
		if err != nil {
			continue
		}

		if exists == 0 {
			rl.logger.WithField("lock_key", key).Debug("Found expired lock key")
		}
	}

	return nil
}
