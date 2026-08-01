package adapter

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"health-nexus/internal/domain/wiki/repository"
	wikiservice "health-nexus/internal/domain/wiki/service"
)

// OutboxRelay 扫描 vectorize_outbox 表中未处理记录，投递到 asynq。
// 保证文章发布/更新/恢复后向量化任务最终一致投递。
type OutboxRelay struct {
	outbox   *repository.OutboxRepo
	enqueuer wikiservice.VectorizeEnqueuer
}

func NewOutboxRelay(outbox *repository.OutboxRepo, enqueuer wikiservice.VectorizeEnqueuer) *OutboxRelay {
	return &OutboxRelay{outbox: outbox, enqueuer: enqueuer}
}

// RunOnce 执行一次 outbox 扫描 + 投递。返回投递的记录数。
// 由定时任务或启动时调用。
func (r *OutboxRelay) RunOnce(ctx context.Context) (int, error) {
	const batchSize = 50
	records, err := r.outbox.ListPending(ctx, batchSize)
	if err != nil {
		return 0, fmt.Errorf("list pending outbox: %w", err)
	}
	if len(records) == 0 {
		return 0, nil
	}

	delivered := 0
	for _, rec := range records {
		if err := r.enqueuer.Enqueue(ctx, rec.ArticleID); err != nil {
			slog.ErrorContext(ctx, "outbox: enqueue failed, will retry next scan",
				"outbox_id", rec.ID, "article_id", rec.ArticleID, "err", err)
			continue // 不标记为已处理，下次扫描重试
		}
		if markErr := r.outbox.MarkProcessed(ctx, rec.ID); markErr != nil {
			slog.ErrorContext(ctx, "outbox: mark processed failed",
				"outbox_id", rec.ID, "err", markErr)
			// 已入队但标记失败，下次扫描会重复入队——Worker 的 DeactivateByArticle 是幂等的，可接受
		}
		delivered++
	}
	return delivered, nil
}

// Start 启动定时扫描（阻塞，直到 ctx 取消）。
func (r *OutboxRelay) Start(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	slog.Info("outbox relay started", "interval", interval)
	for {
		select {
		case <-ctx.Done():
			slog.Info("outbox relay stopped")
			return
		case <-ticker.C:
			n, err := r.RunOnce(ctx)
			if err != nil {
				slog.Error("outbox relay scan failed", "err", err)
			} else if n > 0 {
				slog.Info("outbox relay delivered", "count", n)
			}
		}
	}
}
