// Package asynq 封装 asynq 客户端、服务端与任务类型常量。
package asynq

import (
	"time"

	"github.com/hibiken/asynq"

	"health-nexus/internal/config"
)

// 任务类型常量。
const (
	TaskVectorizeArticle  = "wiki:vectorize_article"
	TaskCrisisEvent       = "chat:crisis_event"
	TaskReviewOverdueScan = "wiki:review_overdue_scan" // 每日扫描 180 天复审逾期文章（REQ-WIKI-017/018）
	TaskReviewNotify      = "review:notify"            // 单条复审通知（ponytail: 通知系统未实现，当前仅定义任务类型常量占位，临时）
)

// DefaultReviewOverdueScanCron 复审逾期扫描的默认 cron：每日 03:00 执行。
// ponytail: 直接字符串常量，避免引入新配置项；如需调整可后续挪到 config.Config。
const DefaultReviewOverdueScanCron = "0 3 * * *"

// DefaultMaxRetry 任务默认最大重试次数。
const DefaultMaxRetry = 5

// DefaultRetryDelay 任务默认重试间隔（由 Server Config RetryDelayFunc 使用）。
const DefaultRetryDelay = 30 * time.Second

// NewClient 创建 asynq 客户端。
func NewClient(cfg config.RedisConfig) *asynq.Client {
	return asynq.NewClient(asynq.RedisClientOpt{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
}

// NewServer 创建 asynq 服务端。
// concurrency 指定并发工作协程数。
func NewServer(cfg config.RedisConfig, concurrency int) *asynq.Server {
	return asynq.NewServer(
		asynq.RedisClientOpt{
			Addr:     cfg.Addr,
			Password: cfg.Password,
			DB:       cfg.DB,
		},
		asynq.Config{
			Concurrency: concurrency,
			RetryDelayFunc: func(n int, err error, task *asynq.Task) time.Duration {
				return DefaultRetryDelay
			},
		},
	)
}
