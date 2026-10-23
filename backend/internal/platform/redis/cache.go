package redis

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Cache 实现 config.service.CacheClient：单例配置 cache-aside 与变更广播。
type Cache struct {
	cli *goredis.Client
}

// NewCache 构造 Cache。
func NewCache(cli *goredis.Client) *Cache {
	return &Cache{cli: cli}
}

// Get 读取缓存值；key 不存在时返回 redis.Nil。
func (c *Cache) Get(ctx context.Context, key string) ([]byte, error) {
	return c.cli.Get(ctx, key).Bytes()
}

// Set 写入缓存值。
func (c *Cache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return c.cli.Set(ctx, key, value, ttl).Err()
}

// Del 删除缓存 key。
func (c *Cache) Del(ctx context.Context, keys ...string) error {
	return c.cli.Del(ctx, keys...).Err()
}

// Publish 发布通知消息。
func (c *Cache) Publish(ctx context.Context, channel, message string) error {
	return c.cli.Publish(ctx, channel, message).Err()
}
