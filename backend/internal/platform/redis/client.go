// Package redis 封装 go-redis 客户端与基于 Redis 的分布式锁。
package redis

import (
	"time"

	"github.com/redis/go-redis/v9"

	"health-nexus/internal/config"
)

// NewClient 创建 go-redis 客户端（含连接池与超时配置）。
func NewClient(cfg config.RedisConfig) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:            cfg.Addr,
		Password:        cfg.Password,
		DB:              cfg.DB,
		DialTimeout:     5 * time.Second,
		ReadTimeout:     3 * time.Second,
		WriteTimeout:    3 * time.Second,
		PoolSize:        20,
		MinIdleConns:    5,
		ConnMaxIdleTime: 5 * time.Minute,
		MaxRetries:      3,
		PoolTimeout:     4 * time.Second,
	})
}
