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
		DialTimeout:     redisDialTimeout,
		ReadTimeout:     redisReadTimeout,
		WriteTimeout:    redisWriteTimeout,
		PoolSize:        redisPoolSize,
		MinIdleConns:    redisMinIdleConns,
		ConnMaxIdleTime: redisConnMaxIdleTime,
		MaxRetries:      redisMaxRetries,
		PoolTimeout:     redisPoolTimeout,
	})
}

// go-redis 连接默认参数（数字语义集中在常量，避免魔法值）。
const (
	redisDialTimeout     = 5 * time.Second
	redisReadTimeout     = 3 * time.Second
	redisWriteTimeout    = 3 * time.Second
	redisPoolSize        = 20
	redisMinIdleConns    = 5
	redisConnMaxIdleTime = 5 * time.Minute
	redisMaxRetries      = 3
	redisPoolTimeout     = 4 * time.Second
)
