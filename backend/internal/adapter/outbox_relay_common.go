package adapter

import (
	"context"
	"log/slog"
	"time"
)

// outbox 投递骨架（vectorize_outbox / crisis_outbox 共用）。
//
// 两个 relay 的差异只有"查哪张表、投什么任务、日志前缀"，其余（批次上限、失败保留待重扫、
// 入队成功才标记已处理、定时循环）完全同构。抽到此处避免两份实现各自漂移。

// outboxBatchSize 单次扫描最多处理的 outbox 记录数。
const outboxBatchSize = 50

// outboxRecord 待投递记录的统一最小视图：主键 + 业务标识。
type outboxRecord struct {
	ID int64
	// Payload 业务标识（vectorize=article_id / crisis=event_id）。
	Payload int64
}

// deliverOutbox 逐条投递 outbox 记录，返回投递成功数。
// 入队失败不标记已处理（下次扫描重试）；标记失败不回退——已入队但标记失败会重复入队，
// 由下游（向量化 Worker / 危机通知落库）的幂等兜底。
// label 为日志前缀，payloadKey 为日志中业务标识的字段名。
func deliverOutbox(
	ctx context.Context,
	label, payloadKey string,
	records []outboxRecord,
	enqueue func(ctx context.Context, payload int64) error,
	markProcessed func(ctx context.Context, id int64) error,
) int {
	delivered := 0
	for _, rec := range records {
		if err := enqueue(ctx, rec.Payload); err != nil {
			slog.ErrorContext(ctx, label+": enqueue failed, will retry next scan",
				"outbox_id", rec.ID, payloadKey, rec.Payload, "err", err)
			continue
		}
		if markErr := markProcessed(ctx, rec.ID); markErr != nil {
			slog.ErrorContext(ctx, label+": mark processed failed", "outbox_id", rec.ID, "err", markErr)
		}
		delivered++
	}
	return delivered
}

// runRelayLoop 周期性执行一次 relay 扫描，直到 ctx 取消（阻塞）。
func runRelayLoop(
	ctx context.Context, label string, interval time.Duration, runOnce func(context.Context) (int, error),
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	slog.Info(label+" started", "interval", interval)
	for {
		select {
		case <-ctx.Done():
			slog.Info(label + " stopped")
			return
		case <-ticker.C:
			n, err := runOnce(ctx)
			if err != nil {
				slog.Error(label+" scan failed", "err", err)
			} else if n > 0 {
				slog.Info(label+" delivered", "count", n)
			}
		}
	}
}
