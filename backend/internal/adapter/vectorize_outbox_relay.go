package adapter

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"health-nexus/internal/domain/wiki/repository"
	wikiservice "health-nexus/internal/domain/wiki/service"
)

// staleEmbeddingScanner 扫描"向量模型与当前模型不一致"的已发布文章（消费者定义，ISP）。
type staleEmbeddingScanner interface {
	ListArticleIDsWithStaleEmbedding(ctx context.Context, model string, limit int) ([]int64, error)
}

// staleRebuildBatchSize 单次扫描最多触发的重建文章数（避免一次性打爆向量化队列）。
const staleRebuildBatchSize = 20

// OutboxRelay 扫描 vectorize_outbox 表中未处理记录，投递到 asynq。
// 保证文章发布/更新/恢复后向量化任务最终一致投递。
// 另可选承担"Embedding 模型切换后全量重建"：切换模型后旧向量不可比，
// 依赖本扫描把模型不一致（含模型未知的历史切片）的文章重新入队。
type OutboxRelay struct {
	outbox   *repository.OutboxRepo
	enqueuer wikiservice.VectorizeEnqueuer
	stale    staleEmbeddingScanner // 可选：模型不一致扫描能力
	modelFn  func() string         // 可选：返回当前生效的 Embedding 模型名
}

func NewOutboxRelay(outbox *repository.OutboxRepo, enqueuer wikiservice.VectorizeEnqueuer) *OutboxRelay {
	return &OutboxRelay{outbox: outbox, enqueuer: enqueuer}
}

// EnableEmbeddingModelRebuild 启用 Embedding 模型切换后的自动重建：
// 周期性扫描向量模型与当前模型不一致的有效切片，重新入队向量化。
// 未调用时仅做 outbox 投递（保持原行为）。
func (r *OutboxRelay) EnableEmbeddingModelRebuild(scanner staleEmbeddingScanner, modelFn func() string) {
	r.stale = scanner
	r.modelFn = modelFn
}

// RunOnce 执行一次 outbox 扫描 + 投递 + 模型不一致重建。返回投递的记录数。
// 由定时任务或启动时调用。
func (r *OutboxRelay) RunOnce(ctx context.Context) (int, error) {
	records, err := r.outbox.ListPending(ctx, outboxBatchSize)
	if err != nil {
		return 0, fmt.Errorf("list pending outbox: %w", err)
	}
	batch := make([]outboxRecord, 0, len(records))
	for _, rec := range records {
		batch = append(batch, outboxRecord{ID: rec.ID, Payload: rec.ArticleID})
	}
	// Worker 的 DeactivateByArticle 是幂等的，重复入队可接受。
	delivered := deliverOutbox(ctx, "outbox", "article_id", batch,
		r.enqueuer.Enqueue, r.outbox.MarkProcessed)
	delivered += r.rebuildStaleVectors(ctx)
	return delivered, nil
}

// rebuildStaleVectors 把向量模型与当前模型不一致（含模型未知）的已发布文章重新入队。
// 已重建完成的文章不再命中扫描条件，因此该循环自然收敛。
func (r *OutboxRelay) rebuildStaleVectors(ctx context.Context) int {
	if r.stale == nil || r.modelFn == nil {
		return 0
	}
	model := r.modelFn()
	if model == "" {
		// 模型未知（未配置 Embedding）时不做判断，避免把全部切片误判为过期。
		return 0
	}
	ids, err := r.stale.ListArticleIDsWithStaleEmbedding(ctx, model, staleRebuildBatchSize)
	if err != nil {
		slog.ErrorContext(ctx, "outbox: list stale embedding articles failed", "err", err)
		return 0
	}
	delivered := 0
	for _, id := range ids {
		if err := r.enqueuer.Enqueue(ctx, id); err != nil {
			slog.ErrorContext(ctx, "outbox: enqueue stale embedding rebuild failed",
				"article_id", id, "err", err)
			continue
		}
		delivered++
	}
	if delivered > 0 {
		slog.InfoContext(ctx, "outbox: enqueued stale embedding rebuild",
			"count", delivered, "model", model)
	}
	return delivered
}

// Start 启动定时扫描（阻塞，直到 ctx 取消）。
func (r *OutboxRelay) Start(ctx context.Context, interval time.Duration) {
	runRelayLoop(ctx, "outbox relay", interval, r.RunOnce)
}
