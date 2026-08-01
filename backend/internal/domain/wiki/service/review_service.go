package service

import (
	"context"
	"fmt"
	"log/slog"
)

// ReviewNotifyEnqueuer 复审通知入队（消费者定义，ISP）。
// 实现由 platform/asynq 适配：将 articleID 序列化为 TaskReviewNotify payload。
// wiki 域不依赖 asynq 包，保持领域纯净。
type ReviewNotifyEnqueuer interface {
	Enqueue(ctx context.Context, articleID int64) error
}

// ArticleOverdueMarker 复审逾期批量标记能力（消费者定义，ISP）。由 ArticleRepo 实现。
type ArticleOverdueMarker interface {
	MarkOverdue(ctx context.Context) ([]int64, error)
	// UnmarkOverdue 回滚单篇文章的逾期标记（通知入队失败时调用，下次扫描可重试）。
	UnmarkOverdue(ctx context.Context, id int64) error
}

// ReviewService 知识库复审服务：扫描 180 天复审逾期文章 + 入队通知（REQ-WIKI-017/018）。
// 由 asynq worker PeriodicTask 每日触发；通知系统未实现，当前以 slog 记录 + 入队 TaskReviewNotify 占位。
type ReviewService struct {
	repo   ArticleOverdueMarker
	notify ReviewNotifyEnqueuer
}

// NewReviewService 构造复审服务。
// notify 可为 nil（通知系统未实现时降级为仅 slog 记录）。
func NewReviewService(repo ArticleOverdueMarker, notify ReviewNotifyEnqueuer) *ReviewService {
	return &ReviewService{repo: repo, notify: notify}
}

// MarkOverdueArticles 扫描并标记 180 天复审逾期文章，对每条入队复审通知。
// 事务边界由 ArticleRepo.MarkOverdue 的单条 UPDATE SQL 保证（批量原子更新）；
// 通知入队失败仅记录日志，不阻塞扫描流程（与 Approve 入队向量化相同的最终一致性策略）。
func (s *ReviewService) MarkOverdueArticles(ctx context.Context) error {
	ids, err := s.repo.MarkOverdue(ctx)
	if err != nil {
		return fmt.Errorf("mark overdue articles: %w", err)
	}
	if len(ids) == 0 {
		return nil
	}
	slog.InfoContext(ctx, "wiki: review overdue scan",
		"overdue_count", len(ids), "article_ids", ids)
	for _, id := range ids {
		// ponytail: 通知系统未接入，当前 worker 端 handler 仅记录 slog 占位（Critical 1 修复说明），临时
		// 通知系统接入后，TaskReviewNotify handler 应在此处真正下发通知（站内信/邮件/推送），临时
		if s.notify != nil {
			if enqErr := s.notify.Enqueue(ctx, id); enqErr != nil {
				// REQ-WIKI-018 "必须发送"——入队失败时回滚 review_overdue 标志，下次扫描可重新选中并重试。
				// ponytail: 不引入 outbox 表——回滚标志让下次扫描兜底，覆盖 Redis 瞬时故障，折中；
				// 升级路径：outbox 表 + worker 兜底重试可同时支持更复杂的最终一致性场景。
				slog.ErrorContext(ctx, "wiki: enqueue review notify failed, reverting overdue flag",
					"article_id", id, "err", enqErr)
				if unmarkErr := s.repo.UnmarkOverdue(ctx, id); unmarkErr != nil {
					slog.ErrorContext(ctx, "wiki: unmark overdue failed, article will be skipped next scan",
						"article_id", id, "err", unmarkErr)
				}
				continue
			}
		} else {
			// 通知系统未注入：仅记录 slog 占位，避免静默丢失复审事件。
			slog.InfoContext(ctx, "wiki: review overdue (notify nil, log only)",
				"article_id", id)
		}
	}
	return nil
}
