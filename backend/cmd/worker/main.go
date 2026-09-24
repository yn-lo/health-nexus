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

// crisisEscalationBatchSize 单次危机超时升级扫描最多处理的记录数。
const crisisEscalationBatchSize = 50

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
	vectorizeHandler, embedClient := buildVectorizeHandler(ctx, cfg, infra, articleRepo, aesKey[:])

	// ========== outbox relay：向量化任务 / 危机通知的最终一致补投 ==========
	startOutboxRelays(ctx, infra, embedClient)

	srv := asynq.NewServer(cfg.Redis, defaultWorkerConcurrency)

	mux := buildTaskMux(reviewSvc, vectorizeHandler, articleRepo, notifRepo, crisisRepo)

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
	outboxRepo := repository.NewOutboxRepo(infra.Pool)
	outboxEnqueuer := adapter.NewAsynqVectorizeEnqueuer(infra.AsynqClient)
	outboxRelay := adapter.NewOutboxRelay(outboxRepo, outboxEnqueuer)
	outboxRelay.EnableEmbeddingModelRebuild(repository.NewChunkRepo(infra.Pool), embedClient.EmbeddingModel)
	go outboxRelay.Start(ctx, outboxRelayInterval)

	crisisOutboxRepo := chatrepo.NewCrisisOutboxRepo(infra.Pool)
	crisisOutboxRelay := adapter.NewCrisisOutboxRelay(
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
	articleRepo *repository.ArticleRepo, aesKey []byte,
) (*adapter.VectorizeHandler, *llm.SwappableClient) {
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
	return adapter.NewVectorizeHandler(articleRepo, chunkRepo, embedder, ragConfigProvider, infra.TxMgr), embedder
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
	// 危机事件主动通知 + 超时升级扫描（实现见 handleCrisisNotify / handleCrisisEscalationScan）。
	mux.HandleFunc(asynq.TaskCrisisEvent, func(ctx context.Context, t *asynqlib.Task) error {
		return handleCrisisNotify(ctx, t, crisisRepo, notifRepo)
	})
	mux.HandleFunc(asynq.TaskCrisisEscalationScan, func(ctx context.Context, _ *asynqlib.Task) error {
		return handleCrisisEscalationScan(ctx, crisisRepo, notifRepo)
	})
	return mux
}

// handleCrisisNotify 危机事件主动通知：查询事件获取科室，落库站内通知。
// P1 修复：未锁定科室的事件不再跳过，改投超管兜底接收方（原实现仅超管在列表里自行发现）；
// 通知按 (type, ref_id, 接收方) 幂等——快速路径与 outbox relay 补投可能重复触发同一任务。
func handleCrisisNotify(
	ctx context.Context, t *asynqlib.Task,
	crisisRepo *chatrepo.CrisisRepo, notifRepo *baserepo.NotificationRepo,
) error {
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
	refID := strconv.FormatInt(eventID, 10)
	n := &baseentity.Notification{
		Type:  "CRISIS_ALERT",
		Title: "危机事件提醒",
		Body:  "患者表达了可能的自伤倾向，请及时处理",
		RefID: &refID,
	}
	if ce.LockedDeptID > 0 {
		deptID := ce.LockedDeptID
		n.RecipientRole = constants.RoleDeptAdmin
		n.RecipientDeptID = &deptID
	} else {
		// 未锁定科室：投给超管兜底，避免"通知发出但无接收方"。
		n.RecipientRole = constants.RoleSuperAdmin
		n.Body = "患者表达了可能的自伤倾向（会话未限定科室），请及时处理"
	}
	exists, xerr := notifRepo.ExistsForRef(ctx, n.RecipientRole, n.RecipientDeptID, n.Type, refID)
	if xerr != nil {
		return xerr
	}
	if exists {
		slog.InfoContext(ctx, "chat: crisis notification already delivered, skip",
			"event_id", eventID, "role", n.RecipientRole)
		return nil
	}
	if err := notifRepo.Create(ctx, n); err != nil {
		slog.ErrorContext(ctx, "chat: crisis notify insert failed", "event_id", eventID, "err", err)
		return err
	}
	slog.InfoContext(ctx, "chat: crisis notification created",
		"event_id", eventID, "notification_id", n.ID,
		"role", n.RecipientRole, "dept_id", n.RecipientDeptID)
	return nil
}

// handleCrisisEscalationScan 扫描超时未接单的危机事件，升级通知超管并标记已升级（P1 人工闭环）。
// 升级通知按 (type, ref_id) 幂等；MarkEscalated 返回 false 表示已被其他实例升级，不重复计数。
func handleCrisisEscalationScan(
	ctx context.Context, crisisRepo *chatrepo.CrisisRepo, notifRepo *baserepo.NotificationRepo,
) error {
	overdue, err := crisisRepo.ListOverdueUnhandled(ctx, crisisEscalationBatchSize)
	if err != nil {
		return err
	}
	escalated := 0
	for _, o := range overdue {
		refID := strconv.FormatInt(o.ID, 10)
		n := &baseentity.Notification{
			RecipientRole: constants.RoleSuperAdmin,
			Type:          "CRISIS_ESCALATED",
			Title:         "危机事件超时未处理",
			Body:          "有危机事件已超过接单时限仍未被处理，请立即跟进",
			RefID:         &refID,
		}
		exists, xerr := notifRepo.ExistsForRef(ctx, n.RecipientRole, nil, n.Type, refID)
		if xerr != nil {
			return xerr
		}
		if !exists {
			if cerr := notifRepo.Create(ctx, n); cerr != nil {
				slog.ErrorContext(ctx, "chat: crisis escalation notify failed", "event_id", o.ID, "err", cerr)
				continue
			}
		}
		ok, merr := crisisRepo.MarkEscalated(ctx, o.ID)
		if merr != nil {
			slog.ErrorContext(ctx, "chat: mark crisis escalated failed", "event_id", o.ID, "err", merr)
			continue
		}
		if ok {
			escalated++
			slog.WarnContext(ctx, "chat: crisis event escalated",
				"event_id", o.ID, "level", o.Level, "due_at", o.AcknowledgeDue)
		}
	}
	if escalated > 0 {
		slog.InfoContext(ctx, "chat: crisis escalation scan done", "escalated", escalated)
	}
	return nil
}
