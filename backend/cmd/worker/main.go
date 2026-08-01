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

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config failed", "err", err)
		panic(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 启动时自动执行数据库迁移（幂等，已执行的迁移不会重复；advisory lock 防止与 server 并发）
	if err := di.RunMigrations(ctx, cfg.Postgres.DSN, ""); err != nil {
		slog.Error("run migrations failed", "err", err)
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

	// ========== wiki 域：向量化 handler（REQ-WIKI-012，Approve/Update 入队） ==========
	aesKey := sha256.Sum256([]byte(cfg.Security.EncryptionKey))
	vectorizeHandler := buildVectorizeHandler(ctx, cfg, infra, articleRepo, aesKey[:])

	srv := asynq.NewServer(cfg.Redis, defaultWorkerConcurrency)

	mux := buildTaskMux(reviewSvc, vectorizeHandler, articleRepo, notifRepo)

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
	srv.Shutdown()
	scheduler.Shutdown()
	slog.Info("worker stopped")
}

// buildVectorizeHandler 装配向量化 handler：加载 LLM embed client + RAG 配置提供者。
// 方案 C：与 server 端共用 adapter.ReloadAndSwap 装配，DB 配置优先，config.yaml fallback。
// 未配置时 embedder=nil，VectorizeHandler 调用 Embed 时触发重试（asynq 自动重试）。
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
	if !embedder.IsReady() {
		embedder = swappable.Chat
	}
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
				reloadCtx, cancel := context.WithTimeout(context.Background(), reloadTimeout)
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
	// 阶段 2 集成时注册 chat 域 task handler：
	// mux.HandleFunc(asynq.TaskCrisisEvent, chatHandler.HandleCrisisEvent)
	return mux
}
