package redis

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// lockTokenLen 是锁 token 的随机字节数。
const lockTokenLen = 16

// unlockTimeout 是释放锁操作的超时时间。
const unlockTimeout = 5 * time.Second

// unlockScript 用 Lua 脚本保证释放锁的原子性：仅当 key 对应的 value 等于 token 时才删除。
var unlockScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("DEL", KEYS[1])
else
    return 0
end
`)

// 锁相关错误。
var (
	ErrLockNotAcquired = errors.New("lock not acquired")
	ErrLockNotHeld     = errors.New("lock not held or held by another")
)

// Locker 基于 Redis SET NX EX 的分布式锁。
type Locker struct {
	client *redis.Client
}

// NewLocker 创建分布式锁。
func NewLocker(client *redis.Client) *Locker {
	return &Locker{client: client}
}

// Lock 获取分布式锁。返回的 unlock 函数用于释放锁。获取失败返回 ErrLockNotAcquired。
// Lock 时生成随机 token 作为 value，Unlock 时通过 Lua 脚本原子性地检查 token 再删除。
func (l *Locker) Lock(ctx context.Context, key string, ttl time.Duration) (func() error, error) {
	token, err := randomToken()
	if err != nil {
		return nil, fmt.Errorf("generate lock token: %w", err)
	}

	ok, err := l.client.SetNX(ctx, key, token, ttl).Result()
	if err != nil {
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	if !ok {
		return nil, ErrLockNotAcquired
	}

	unlock := func() error {
		uctx, cancel := context.WithTimeout(context.Background(), unlockTimeout)
		defer cancel()
		return l.Unlock(uctx, key, token)
	}
	return unlock, nil
}

// Unlock 释放锁。使用 Lua 脚本保证原子性：仅当 key 对应的 value 等于 token 时才删除。
// 若锁已过期或被其他客户端持有，返回 ErrLockNotHeld。
func (l *Locker) Unlock(ctx context.Context, key, token string) error {
	res, err := unlockScript.Run(ctx, l.client, []string{key}, token).Result()
	if err != nil {
		return fmt.Errorf("release lock: %w", err)
	}
	if n, ok := res.(int64); !ok || n == 0 {
		return ErrLockNotHeld
	}
	return nil
}

// randomToken 生成 lockTokenLen 字节随机数，base64 编码后返回。
func randomToken() (string, error) {
	b := make([]byte, lockTokenLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}
