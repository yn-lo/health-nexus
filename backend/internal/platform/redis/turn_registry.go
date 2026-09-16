package redis

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// TurnRegistry 请求幂等登记（Redis 实现）：request_id → 本轮引用。
// 同一 request_id 重复提交时据此区分"进行中（拒绝）/已完成（回放权威结果）"，
// 避免网络重试产生重复生成。key 由调用方按身份作用域拼接，跨身份无法命中。
// 实现 chat.service.TurnRegistry 接口（消费方定义，ISP）。
type TurnRegistry struct {
	cli *goredis.Client
}

// NewTurnRegistry 构造 TurnRegistry。
func NewTurnRegistry(cli *goredis.Client) *TurnRegistry {
	return &TurnRegistry{cli: cli}
}

// Put 写入/覆盖登记值。
func (r *TurnRegistry) Put(ctx context.Context, key, value string, ttl time.Duration) error {
	return r.cli.Set(ctx, key, value, ttl).Err()
}

// Lookup 读取登记值；不存在返回 ("", false, nil)。
func (r *TurnRegistry) Lookup(ctx context.Context, key string) (value string, found bool, err error) {
	val, gerr := r.cli.Get(ctx, key).Result()
	if gerr == goredis.Nil {
		return "", false, nil
	}
	if gerr != nil {
		return "", false, gerr
	}
	return val, true, nil
}

// Delete 删除登记（申请锁失败等未真正开始的请求，不应阻塞后续重试）。
func (r *TurnRegistry) Delete(ctx context.Context, key string) error {
	return r.cli.Del(ctx, key).Err()
}
