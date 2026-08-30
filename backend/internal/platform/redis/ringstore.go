package redis

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// RingStore 基于 Redis List 的瞬态会话消息环，用于匿名会话多轮上下文（TTL 自动过期）。
// 实现 chat.service.ringStore 接口（消费方定义，ISP），通过 run_mcp 注入到 chat 域。
type RingStore struct {
	cli *goredis.Client
}

// NewRingStore 构造 RingStore。
func NewRingStore(cli *goredis.Client) *RingStore {
	return &RingStore{cli: cli}
}

// RPush 追加值到环尾部。
func (r *RingStore) RPush(ctx context.Context, key string, values ...string) error {
	return r.cli.RPush(ctx, key, values).Err()
}

// LRange 读取环区间 [start, stop] 的值；负数表示从尾部偏移（如 -1 为最后一个元素）。
func (r *RingStore) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return r.cli.LRange(ctx, key, start, stop).Result()
}

// Expire 刷新环的 TTL。
func (r *RingStore) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return r.cli.Expire(ctx, key, ttl).Err()
}
