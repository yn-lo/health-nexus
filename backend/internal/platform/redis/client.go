// Package redis 封装 go-redis 客户端与基于 Redis 的分布式锁。
package redis

import (
	"github.com/redis/go-redis/v9"

	"health-nexus/internal/config"
)

// NewClient 创建 go-redis 客户端。
func NewClient(cfg config.RedisConfig) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
}
