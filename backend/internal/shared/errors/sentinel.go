package errors

import stderrors "errors"

// ErrLockNotAcquired 分布式锁已被占用（获取失败）。
// 由平台层锁实现返回、业务层判定，避免业务层依赖具体 Redis 包。
var ErrLockNotAcquired = stderrors.New("lock not acquired")
