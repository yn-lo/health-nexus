// Package tasks 承载 asynq 异步任务用例：文章向量化、复审/危机通知落库、超时升级与 outbox 补投。
// 任务 payload 与 asynq 绑定属于进程边界；业务编排（查什么、写什么、如何幂等）集中在本包，
// cmd/worker 只负责依赖装配与生命周期。
package tasks

import (
	"context"

	asynqlib "github.com/hibiken/asynq"

	baserepo "health-nexus/internal/domain/base/repository"
	chatrepo "health-nexus/internal/domain/chat/repository"
	wikirepo "health-nexus/internal/domain/wiki/repository"
	wikiservice "health-nexus/internal/domain/wiki/service"
	"health-nexus/internal/platform/asynq"
)

// Deps 任务处理所需依赖，由 cmd/worker 装配注入。
type Deps struct {
	Review        *wikiservice.ReviewService
	Vectorize     *VectorizeHandler
	Articles      *wikirepo.ArticleRepo
	Notifications *baserepo.NotificationRepo
	Crisis        *chatrepo.CrisisRepo
}

// NewMux 注册全部 asynq 任务处理器。
func NewMux(d Deps) *asynqlib.ServeMux {
	notifier := NewCrisisNotifier(d.Crisis, d.Notifications)
	mux := asynqlib.NewServeMux()

	mux.HandleFunc(asynq.TaskReviewOverdueScan, func(ctx context.Context, _ *asynqlib.Task) error {
		return d.Review.MarkOverdueArticles(ctx)
	})
	mux.HandleFunc(asynq.TaskReviewNotify, func(ctx context.Context, t *asynqlib.Task) error {
		return HandleReviewNotify(ctx, t.Payload(), d.Articles, d.Notifications)
	})
	mux.HandleFunc(asynq.TaskVectorizeArticle, d.Vectorize.HandleVectorize)
	mux.HandleFunc(asynq.TaskCrisisEvent, func(ctx context.Context, t *asynqlib.Task) error {
		return notifier.Notify(ctx, t.Payload())
	})
	mux.HandleFunc(asynq.TaskCrisisEscalationScan, func(ctx context.Context, _ *asynqlib.Task) error {
		return notifier.Escalate(ctx)
	})
	return mux
}
