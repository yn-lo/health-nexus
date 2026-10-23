// Package main 是 asynq Worker 入口。
// 加载配置、初始化基础设施、注册 task handler、启动 asynq Server。
package main

import (
	"context"
	"crypto/sha256"
	"log/slog"
	"os/signal"
	"syscall"
	"time"

	"health-nexus/internal/adapter"
	"health-nexus/internal/config"
	"health-nexus/internal/di"
	baserepo "health-nexus/internal/domain/base/repository"
	chatrepo "health-nexus/internal/domain/chat/repository"
	configservice "health-nexus/internal/domain/config/service"
	wikirepo "health-nexus/internal/domain/wiki/repository"
	wikiservice "health-nexus/internal/domain/wiki/service"
	"health-nexus/internal/platform/asynq"
	"health-nexus/internal/platform/llm"
	"health-nexus/internal/tasks"

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
	// env=development 时额外加载 seed.dev.sql 测试数据；生产环境数据库保持干净。
	if err := di.ApplySchema(ctx, cfg.Postgres.DSN, cfg.Env, cfg.Seed.AdminPassword); err != nil {
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
	articleRepo := wikirepo.NewArticleRepo(infra.Pool)
	reviewNotifyEnqueuer := adapter.NewAsynqReviewNotifyEnqueuer(infra.AsynqClient)
	reviewSvc := wikiservice.NewReviewService(articleRepo, reviewNotifyEnqueuer)

	// ========== base 域：站内通知仓储（复审通知落库） ==========
	notifRepo := baserepo.NewNotificationRepo(infra.Pool)

	// ========== chat 域：危机事件仓储（危机通知落库） ==========
	crisisRepo := chatrepo.NewCrisisRepo(infra.Pool)

	// ========== wiki 域：向量化 handler（REQ-WIKI-012，Approve/Update 入队） ==========
	aesKey := sha256.Sum256([]byte(cfg.Security.EncryptionKey))
	vectorizeHandler, embedClient := buildVectorizeHandler(ctx, cfg, infra, articleRepo, aesKey[:])

	// ========== outbox relay：向量化任务 / 危机通知的最终一致补投 ==========
	startOutboxRelays(ctx, infra, embedClient)

	srv := asynq.NewServer(cfg.Redis, defaultWorkerConcurrency)

	mux := tasks.NewMux(tasks.Deps{
		Review:        reviewSvc,
		Vectorize:     vectorizeHandler,
		Articles:      articleRepo,
		Notifications: notifRepo,
		Crisis:        crisisRepo,
	})

	// PeriodicTask 调度器：每日复审逾期扫描 + 危机事件超时升级扫描（详见 registerPeriodicTasks）。
	scheduler := asynqlib.NewScheduler(asynqlib.RedisClientOpt{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	}, nil)
	registerPeriodicTasks(scheduler)

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

// startOutboxRelays 启动两个 outbox relay 的后台补投循环：
//   - vectorize_outbox：文章发布/更新后的向量化任务。写入侧快速路径 Enqueue（事务外）在 Redis
//     瞬时故障时会丢任务，relay 周期扫描未处理记录重新入队；另承担 Embedding 模型切换后的全量重建
//     （模型不一致或未知的历史切片重新入队，检索侧按模型过滤，重建完成后收敛为严格同模型）。
//   - crisis_outbox：危机事件的医护通知任务。事件创建时已在同一事务写入 outbox，
//     SSE 链路快速入队失败时由 relay 补投，保证人工通知不会静默丢失。
func startOutboxRelays(ctx context.Context, infra *di.Infrastructure, embedClient *llm.SwappableClient) {
	outboxRepo := wikirepo.NewOutboxRepo(infra.Pool)
	outboxEnqueuer := adapter.NewAsynqVectorizeEnqueuer(infra.AsynqClient)
	outboxRelay := tasks.NewOutboxRelay(outboxRepo, outboxEnqueuer)
	outboxRelay.EnableEmbeddingModelRebuild(wikirepo.NewChunkRepo(infra.Pool), embedClient.EmbeddingModel)
	go outboxRelay.Start(ctx, outboxRelayInterval)

	crisisOutboxRepo := chatrepo.NewCrisisOutboxRepo(infra.Pool)
	crisisOutboxRelay := tasks.NewCrisisOutboxRelay(
		crisisOutboxRepo, adapter.NewAsynqCrisisNotifier(infra.AsynqClient))
	go crisisOutboxRelay.Start(ctx, outboxRelayInterval)
}

// registerPeriodicTasks 注册 asynq 周期任务：
//   - 每日 03:00 复审逾期扫描（Critical 1）；
//   - 每 5 分钟危机事件超时升级扫描（P1 人工闭环：超时未接单升级通知超管）。
func registerPeriodicTasks(scheduler *asynqlib.Scheduler) {
	if _, err := scheduler.Register(asynq.DefaultReviewOverdueScanCron,
		asynqlib.NewTask(asynq.TaskReviewOverdueScan, nil)); err != nil {
		slog.Error("register periodic task failed", "err", err)
		panic(err)
	}
	if _, err := scheduler.Register(asynq.DefaultCrisisEscalationCron,
		asynqlib.NewTask(asynq.TaskCrisisEscalationScan, nil)); err != nil {
		slog.Error("register crisis escalation task failed", "err", err)
		panic(err)
	}
}

// buildVectorizeHandler 装配向量化 handler：加载 LLM embed client + RAG 配置提供者。
// 方案 C：与 server 端共用 adapter.ReloadAndSwap 装配，DB 配置优先，config.yaml fallback。
// handler 直接持有 swappable.Embed（*SwappableClient）：未配置时 Embed 返回
// ErrNotConfigured 触发 asynq 重试；配置变更经 Redis 通知热切换后，下一次重试即用新 client。
// 注意：不得 fallback 到 Chat client——chat 端点不提供 /embeddings，会打到错误地址（历史 bug 根因）。
// 支持热切换：通过 SwappableClient 包装，配置变更后无需重启 worker。
// 返回 embed client 供 outbox relay 读取当前向量模型名（模型切换后的重建扫描使用）。
func buildVectorizeHandler(
	ctx context.Context, cfg *config.Config, infra *di.Infrastructure,
	articleRepo *wikirepo.ArticleRepo, aesKey []byte,
) (*tasks.VectorizeHandler, *llm.SwappableClient) {
	swappable := adapter.BuildSwappableClients()
	if err := adapter.ReloadAndSwap(ctx, swappable, infra.Pool, aesKey, cfg.LLM); err != nil {
		slog.Error("load llm clients for worker failed", "err", err)
		panic(err)
	}
	embedder := swappable.Embed
	// 订阅 Redis 频道，配置变更时自动热切换
	startWorkerLLMReloadSubscriber(ctx, infra, swappable, cfg.LLM, aesKey)

	chunkRepo := wikirepo.NewChunkRepo(infra.Pool)
	configSvc := di.NewConfigService(infra, aesKey, cfg.LLM)
	ragConfigProvider := adapter.NewConfigRAGConfigProvider(configSvc)
	return tasks.NewVectorizeHandler(articleRepo, chunkRepo, embedder, ragConfigProvider, infra.TxMgr), embedder
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
