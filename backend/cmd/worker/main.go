// Package main 是 asynq Worker 入口。
// 加载配置、初始化基础设施、注册 task handler、启动 asynq Server。
package main

import (
	"context"
	"crypto/sha256"
	"log/slog"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"health-nexus/internal/adapter"
	"health-nexus/internal/config"
	"health-nexus/internal/di"
	baseentity "health-nexus/internal/domain/base/entity"
	baserepo "health-nexus/internal/domain/base/repository"
	chatrepo "health-nexus/internal/domain/chat/repository"
	configrepo "health-nexus/internal/domain/config/repository"
	configservice "health-nexus/internal/domain/config/service"
	"health-nexus/internal/domain/wiki/repository"
	wikiservice "health-nexus/internal/domain/wiki/service"
	"health-nexus/internal/platform/asynq"
	"health-nexus/internal/platform/llm"
	"health-nexus/internal/shared/constants"

	asynqlib "github.com/hibiken/asynq"
)

// defaultWorkerConcurrency asynq worker 默认并发数。
const defaultWorkerConcurrency = 10

// outboxRelayInterval outbox 兜底投递的轮询间隔。
const outboxRelayInterval = 30 * time.Second

// shutdownTimeout worker 优雅关闭的等待超时。
const shutdownTimeout = 30 * time.Second

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config failed", "err", err)
		panic(err)
	}
	config.WarnIfDevSecrets(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 启动时自动应用数据库 schema + 种子（幂等：内容哈希未变则跳过；advisory lock 防止与 server 并发）
	if err := di.ApplySchema(ctx, cfg.Postgres.DSN); err != nil {
		slog.Error("run schema apply failed", "err", err)
		panic(err)
	}

	infra, err := di.NewInfrastructure(ctx, cfg)
	if err != nil {
		slog.Error("init infrastructure failed", "err", err)
		panic(err)
	}
	defer infra.Close()

	// ========== wiki 域：复审服务（Critical 1, REQ-WIKI-017/018） ==========
	articleRepo := repository.NewArticleRepo(infra.Pool)
	reviewNotifyEnqueuer := adapter.NewAsynqReviewNotifyEnqueuer(infra.AsynqClient)
	reviewSvc := wikiservice.NewReviewService(articleRepo, reviewNotifyEnqueuer)

	// ========== base 域：站内通知仓储（复审通知落库） ==========
	notifRepo := baserepo.NewNotificationRepo(infra.Pool)

	// ========== chat 域：危机事件仓储（危机通知落库） ==========
	crisisRepo := chatrepo.NewCrisisRepo(infra.Pool)

	// ========== wiki 域：向量化 handler（REQ-WIKI-012，Approve/Update 入队） ==========
	aesKey := sha256.Sum256([]byte(cfg.Security.EncryptionKey))
	vectorizeHandler := buildVectorizeHandler(ctx, cfg, infra, articleRepo, aesKey[:])

	// ========== wiki 域：outbox relay（向量化任务最终一致投递兜底） ==========
	// 写入侧快速路径 Enqueue（ArticleService 事务外）在 Redis 瞬时故障时丢失任务；
	// relay 周期扫描 vectorize_outbox 未处理记录重新入队，保证文章发布/更新后必然被向量化。
	outboxRepo := repository.NewOutboxRepo(infra.Pool)
	outboxEnqueuer := adapter.NewAsynqVectorizeEnqueuer(infra.AsynqClient)
	outboxRelay := adapter.NewOutboxRelay(outboxRepo, outboxEnqueuer)
	go outboxRelay.Start(ctx, outboxRelayInterval)

	srv := asynq.NewServer(cfg.Redis, defaultWorkerConcurrency)

	mux := buildTaskMux(reviewSvc, vectorizeHandler, articleRepo, notifRepo, crisisRepo)

	// Critical 1: PeriodicTask 调度器——每日 03:00 触发 TaskReviewOverdueScan。
	scheduler := asynqlib.NewScheduler(asynqlib.RedisClientOpt{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	}, nil)
	if _, err := scheduler.Register(asynq.DefaultReviewOverdueScanCron,
		asynqlib.NewTask(asynq.TaskReviewOverdueScan, nil)); err != nil {
		slog.Error("register periodic task failed", "err", err)
		panic(err)
	}

	slog.Info("asynq worker starting")
	go func() {
		if err := scheduler.Run(); err != nil {
			slog.Error("scheduler run error", "err", err)
		}
	}()
	if err := srv.Start(mux); err != nil {
		slog.Error("worker error", "err", err)
		panic(err)
	}

	<-ctx.Done()
	slog.Info("worker shutdown signal received")
	// 优雅关闭：srv.Shutdown + scheduler.Shutdown，30s 超时后强制退出。
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()
	done := make(chan struct{})
	go func() {
		srv.Shutdown()
		scheduler.Shutdown()
		close(done)
	}()
	select {
	case <-done:
		slog.Info("worker stopped gracefully")
	case <-shutdownCtx.Done():
		slog.Error("worker shutdown timed out, forcing exit")
	}
}

// buildVectorizeHandler 装配向量化 handler：加载 LLM embed client + RAG 配置提供者。
// 方案 C：与 server 端共用 adapter.ReloadAndSwap 装配，DB 配置优先，config.yaml fallback。
// handler 直接持有 swappable.Embed（*SwappableClient）：未配置时 Embed 返回
// ErrNotConfigured 触发 asynq 重试；配置变更经 Redis 通知热切换后，下一次重试即用新 client。
// 注意：不得 fallback 到 Chat client——chat 端点不提供 /embeddings，会打到错误地址（历史 bug 根因）。
// 支持热切换：通过 SwappableClient 包装，配置变更后无需重启 worker。
func buildVectorizeHandler(
	ctx context.Context, cfg *config.Config, infra *di.Infrastructure,
	articleRepo *repository.ArticleRepo, aesKey []byte,
) *adapter.VectorizeHandler {
	swappable := adapter.BuildSwappableClients()
	if err := adapter.ReloadAndSwap(ctx, swappable, infra.Pool, aesKey, cfg.LLM); err != nil {
		slog.Error("load llm clients for worker failed", "err", err)
		panic(err)
	}
	embedder := swappable.Embed
	// 订阅 Redis 频道，配置变更时自动热切换
	startWorkerLLMReloadSubscriber(ctx, infra, swappable, cfg.LLM, aesKey)

	chunkRepo := repository.NewChunkRepo(infra.Pool)
	configSvc := configservice.NewConfigService(
		configrepo.NewAIProviderRepo(infra.Pool),
		configrepo.NewSensitiveWordRepo(infra.Pool),
		configrepo.NewSafetyRuleRepo(infra.Pool),
		configrepo.NewRAGConfigRepo(infra.Pool),
		configrepo.NewPromptTemplateRepo(infra.Pool),
		configrepo.NewSafetyMessageRepo(infra.Pool),
		configrepo.NewConfigAuditLogRepo(infra.Pool),
		infra.TxMgr, aesKey, infra.Redis,
	)
	ragConfigProvider := adapter.NewConfigRAGConfigProvider(configSvc)
	return adapter.NewVectorizeHandler(articleRepo, chunkRepo, embedder, ragConfigProvider)
}

// startWorkerLLMReloadSubscriber 订阅 Redis 频道，收到 AI Provider 变更通知后重新加载 LLM 客户端并热切换。
func startWorkerLLMReloadSubscriber(
	ctx context.Context, infra *di.Infrastructure,
	sc *llm.SwappableClients, llmCfg config.LLMConfig, aesKey []byte,
) {
	if infra.Redis == nil {
		slog.Warn("llm: worker redis not available, hot-reload subscriber not started")
		return
	}
	channel := configservice.LLMReloadChannel()
	sub := infra.Redis.Subscribe(ctx, channel)
	const reloadTimeout = 30 * time.Second
	go func() {
		defer func() { _ = sub.Close() }()
		slog.Info("llm: worker hot-reload subscriber started", "channel", channel)
		ch := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				slog.Info("llm: worker hot-reload subscriber stopped")
				return
			case msg, ok := <-ch:
				if !ok {
					slog.Warn("llm: worker hot-reload subscriber channel closed")
					return
				}
				slog.Info("llm: worker received reload notification", "channel", msg.Channel)
				// 用订阅 ctx（收到关闭信号即取消），而非 Background，避免 goroutine 泄漏并跟随 worker 生命周期。
				reloadCtx, cancel := context.WithTimeout(ctx, reloadTimeout)
				if err := adapter.ReloadAndSwap(reloadCtx, sc, infra.Pool, aesKey, llmCfg); err != nil {
					slog.Error("llm: worker hot-reload failed", "err", err)
				}
				cancel()
			}
		}
	}()
}

func buildTaskMux(
	reviewSvc *wikiservice.ReviewService,
	vectorizeHandler *adapter.VectorizeHandler,
	articleRepo *repository.ArticleRepo,
	notifRepo *baserepo.NotificationRepo,
	crisisRepo *chatrepo.CrisisRepo,
) *asynqlib.ServeMux {
	mux := asynqlib.NewServeMux()
	// Critical 1: 每日复审逾期扫描任务——由 Scheduler 触发，handler 调用 ReviewService.MarkOverdueArticles。
	mux.HandleFunc(asynq.TaskReviewOverdueScan, func(ctx context.Context, t *asynqlib.Task) error {
		slog.InfoContext(ctx, "wiki: review overdue scan task started")
		if err := reviewSvc.MarkOverdueArticles(ctx); err != nil {
			slog.ErrorContext(ctx, "wiki: review overdue scan task failed", "err", err)
			return err
		}
		return nil
	})
	// Critical 1: 单条复审通知任务——落库一条 REVIEW_PENDING 站内通知（面向科室管理员）。
	// payload 为 articleID 的十进制字符串；body 取文章标题（best-effort，取不到则为空）。
	mux.HandleFunc(asynq.TaskReviewNotify, func(ctx context.Context, t *asynqlib.Task) error {
		id, err := strconv.ParseInt(string(t.Payload()), 10, 64)
		if err != nil {
			slog.ErrorContext(ctx, "wiki: review notify task invalid payload",
				"payload", string(t.Payload()), "err", err)
			return err
		}
		body := ""
		if a, gerr := articleRepo.GetByID(ctx, id); gerr == nil {
			body = a.Title
		}
		refID := strconv.FormatInt(id, 10)
		// ponytail: 仅面向 DEPT_ADMIN 单角色落库一条，未向 SUPER_ADMIN/多科室扇出，简化；
		// 升级路径：按需为多个 recipient_role/recipient_dept_id 批量插入。
		n := &baseentity.Notification{
			RecipientRole: constants.RoleDeptAdmin,
			Type:          "REVIEW_PENDING",
			Title:         "文章待审核",
			Body:          body,
			RefID:         &refID,
		}
		if err := notifRepo.Create(ctx, n); err != nil {
			slog.ErrorContext(ctx, "wiki: review notify insert failed", "article_id", id, "err", err)
			return err
		}
		slog.InfoContext(ctx, "wiki: review notify created",
			"article_id", id, "notification_id", n.ID)
		return nil
	})
	// REQ-WIKI-012：文章审核通过/已发布内容更新后异步入队向量化。
	// payload 为 articleID 的十进制字符串；handler 内部完成切片+embedding+入库。
	mux.HandleFunc(asynq.TaskVectorizeArticle, vectorizeHandler.HandleVectorize)
	// 危机事件主动通知：查询事件获取科室，落库站内通知给 DEPT_ADMIN。
	mux.HandleFunc(asynq.TaskCrisisEvent, func(ctx context.Context, t *asynqlib.Task) error {
		eventID, err := strconv.ParseInt(string(t.Payload()), 10, 64)
		if err != nil {
			slog.ErrorContext(ctx, "chat: crisis notify task invalid payload",
				"payload", string(t.Payload()), "err", err)
			return err
		}
		ce, err := crisisRepo.GetByID(ctx, eventID)
		if err != nil {
			slog.ErrorContext(ctx, "chat: crisis notify get event failed", "event_id", eventID, "err", err)
			return err
		}
		// 仅向锁定科室的 DEPT_ADMIN 发送通知；未锁定科室的事件仅超管可见（通过列表查看）
		if ce.LockedDeptID <= 0 {
			slog.InfoContext(ctx, "chat: crisis event has no locked dept, skip notification", "event_id", eventID)
			return nil
		}
		deptID := ce.LockedDeptID
		refID := strconv.FormatInt(eventID, 10)
		n := &baseentity.Notification{
			RecipientRole:   constants.RoleDeptAdmin,
			RecipientDeptID: &deptID,
			Type:            "CRISIS_ALERT",
			Title:           "危机事件提醒",
			Body:            "患者表达了可能的自伤倾向，请及时处理",
			RefID:           &refID,
		}
		if err := notifRepo.Create(ctx, n); err != nil {
			slog.ErrorContext(ctx, "chat: crisis notify insert failed", "event_id", eventID, "err", err)
			return err
		}
		slog.InfoContext(ctx, "chat: crisis notification created",
			"event_id", eventID, "notification_id", n.ID, "dept_id", deptID)
		return nil
	})
	return mux
}
