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

// LTrim 裁剪环到区间 [start, stop]；负数表示从尾部偏移（如 -N ~ -1 保留尾部 N 条）。
// 用于把匿名会话环限制为有界长度，避免持续使用无限增长。
func (r *RingStore) LTrim(ctx context.Context, key string, start, stop int64) error {
	return r.cli.LTrim(ctx, key, start, stop).Err()
}

// Expire 刷新环的 TTL。
func (r *RingStore) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return r.cli.Expire(ctx, key, ttl).Err()
}

// Del 删除环（匿名会话删除时清除服务端瞬态上下文）。
func (r *RingStore) Del(ctx context.Context, keys ...string) error {
	return r.cli.Del(ctx, keys...).Err()
}
