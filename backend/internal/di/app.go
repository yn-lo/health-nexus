// Package di 手写依赖注入（替代 google/wire）。
// NewApp 顺序构造所有基础设施 + 5 个域的 handler/service/repository。
package di

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"health-nexus/internal/adapter"
	"health-nexus/internal/config"
	"health-nexus/internal/domain/auth/handler"
	authrepo "health-nexus/internal/domain/auth/repository"
	authservice "health-nexus/internal/domain/auth/service"
	basehandler "health-nexus/internal/domain/base/handler"
	baserepo "health-nexus/internal/domain/base/repository"
	baseservice "health-nexus/internal/domain/base/service"
	chathandler "health-nexus/internal/domain/chat/handler"
	chatrepo "health-nexus/internal/domain/chat/repository"
	chatservice "health-nexus/internal/domain/chat/service"
	confighandler "health-nexus/internal/domain/config/handler"
	configrepo "health-nexus/internal/domain/config/repository"
	configservice "health-nexus/internal/domain/config/service"
	wikihandler "health-nexus/internal/domain/wiki/handler"
	wikirepo "health-nexus/internal/domain/wiki/repository"
	wikiservice "health-nexus/internal/domain/wiki/service"
	"health-nexus/internal/middleware"
	"health-nexus/internal/platform/asynq"
	"health-nexus/internal/platform/llm"
	"health-nexus/internal/platform/logger"
	"health-nexus/internal/platform/postgres"
	"health-nexus/internal/platform/redis"
	"health-nexus/internal/shared/rag"

	asynqlib "github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

// App 完整应用容器，持有所有基础设施和域 handler。
type App struct {
	Infra         *Infrastructure
	Auth          *handler.AuthHandler
	Base          *basehandler.DepartmentHandler
	Notifications *basehandler.NotificationHandler
	Wiki          *wikihandler.Router
	Chat          http.Handler
	Config        *confighandler.ConfigHandler
}

// Infrastructure 基础设施容器。
// LLM 客户端不在此处——worker 不使用 LLM，但共享 NewInfrastructure 装配流程（R8-Config-3 修复）。
// LLM 客户端由 NewApp 单独创建，仅供 chat 域使用。
type Infrastructure struct {
	Pool        *pgxpool.Pool
	Redis       *goredis.Client
	AsynqClient *asynqlib.Client
	TxMgr       *postgres.TxManager
	Locker      *redis.Locker
	Auth        *middleware.Authenticator
	RateLimiter *middleware.RateLimiter
	Logger      *slog.Logger
	Cfg         *config.Config
}

// NewInfrastructure 装配基础设施。
// 任意后续步骤失败时通过 infra.Close() 关闭已创建的资源（pool/redis/asynq），
// 避免连接泄漏。success=true 时由返回的 Infrastructure 接管生命周期。
// LLM 客户端不在此处创建——worker 共享此装配流程但不需要 LLM，避免强迫 worker 配置 LLM_API_KEY。
func NewInfrastructure(ctx context.Context, cfg *config.Config) (*Infrastructure, error) {
	log := logger.New("logs")

	pool, err := postgres.NewPool(ctx, cfg.Postgres)
	if err != nil {
		return nil, fmt.Errorf("init postgres: %w", err)
	}
	infra := &Infrastructure{Pool: pool}
	success := false
	defer func() {
		if !success {
			infra.Close()
		}
	}()

	infra.Redis = redis.NewClient(cfg.Redis)
	infra.AsynqClient = asynq.NewClient(cfg.Redis)
	infra.TxMgr = postgres.NewTxManager(pool)
	infra.Locker = redis.NewLocker(infra.Redis)
	infra.RateLimiter = middleware.NewRateLimiter(infra.Redis, cfg.Server.TrustedProxies)

	auth, err := middleware.NewAuthenticator(cfg.JWT)
	if err != nil {
		return nil, fmt.Errorf("init jwt auth: %w", err)
	}
	infra.Auth = auth
	infra.Logger = log
	infra.Cfg = cfg

	success = true
	return infra, nil
}

// Close 释放基础设施资源。
func (i *Infrastructure) Close() {
	if i.Pool != nil {
		i.Pool.Close()
	}
	if i.Redis != nil {
		_ = i.Redis.Close()
	}
	if i.AsynqClient != nil {
		_ = i.AsynqClient.Close()
	}
}

// NewApp 装配完整应用：基础设施 + 5 个域。
func NewApp(ctx context.Context, cfg *config.Config) (*App, error) {
	infra, err := NewInfrastructure(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// 任意后续步骤失败时关闭已创建的基础设施资源（连接池/Redis/asynq），
	// 避免进程残留或重启时连接泄漏。success=true 时由调用方（main 的 defer app.Infra.Close()）接管。
	success := false
	defer func() {
		if !success {
			infra.Close()
		}
	}()

	// ========== base 域 ==========
	deptRepo := baserepo.NewDepartmentRepo(infra.Pool)
	deptSvc := baseservice.NewDepartmentService(deptRepo, infra.TxMgr)
	deptHandler := basehandler.NewDepartmentHandler(deptSvc, infra.Auth)

	notifRepo := baserepo.NewNotificationRepo(infra.Pool)
	notifSvc := baseservice.NewNotificationService(notifRepo)
	notifHandler := basehandler.NewNotificationHandler(notifSvc, infra.Auth)

	// ========== auth 域 ==========
	userRepo := authrepo.NewUserRepo(infra.Pool)
	inviteRepo := authrepo.NewInviteRepo(infra.Pool)
	tokenIssuer, err := authservice.NewTokenIssuer(cfg.JWT)
	if err != nil {
		return nil, fmt.Errorf("init token issuer: %w", err)
	}
	authSvc := authservice.NewAuthService(userRepo, inviteRepo, tokenIssuer, infra.Auth, infra.Redis, cfg)
	authHandler := handler.NewAuthHandler(authSvc)

	// ========== config 域 ==========
	configH, configSvc, aesKey, err := buildConfigDomain(infra, cfg)
	if err != nil {
		return nil, err
	}

	// ========== wiki 域 ==========
	chunkRepo := wikirepo.NewChunkRepo(infra.Pool)
	wikiRouter := buildWikiRouter(infra, deptRepo, userRepo, chunkRepo)

	// ========== chat 域 ==========
	chatRouter, err := buildChatRouter(ctx, infra, cfg.LLM, aesKey, deptRepo, configSvc, chunkRepo)
	if err != nil {
		return nil, err
	}

	success = true
	return &App{
		Infra:         infra,
		Auth:          authHandler,
		Base:          deptHandler,
		Notifications: notifHandler,
		Wiki:          wikiRouter,
		Chat:          chatRouter,
		Config:        configH,
	}, nil
}

// buildConfigDomain 装配 config 域：7 个 repository + ConfigService + ConfigHandler。
// 返回 AES-GCM 字段级加密 key（供 chat 域加载 LLM 客户端时解密 API Key）。
func buildConfigDomain(
	infra *Infrastructure, cfg *config.Config,
) (*confighandler.ConfigHandler, *configservice.ConfigService, []byte, error) {
	// AES-GCM 字段级加密 key（API Key 等）：从 Security.EncryptionKey 派生 SHA-256。
	// 必须显式配置——空 key 会派生出可预测的 sha256("")，等于无加密。
	if cfg.Security.EncryptionKey == "" {
		return nil, nil, nil, fmt.Errorf(
			"security.encryption_key must be set (env: HEALTH_NEXUS_SECURITY_ENCRYPTION_KEY)")
	}
	aesKey := sha256.Sum256([]byte(cfg.Security.EncryptionKey))

	aiProviderRepo := configrepo.NewAIProviderRepo(infra.Pool)
	sensitiveWordRepo := configrepo.NewSensitiveWordRepo(infra.Pool)
	safetyRuleRepo := configrepo.NewSafetyRuleRepo(infra.Pool)
	ragConfigRepo := configrepo.NewRAGConfigRepo(infra.Pool)
	promptTemplateRepo := configrepo.NewPromptTemplateRepo(infra.Pool)
	safetyMessageRepo := configrepo.NewSafetyMessageRepo(infra.Pool)
	configAuditLogRepo := configrepo.NewConfigAuditLogRepo(infra.Pool)

	configSvc := configservice.NewConfigServiceWithLLM(
		aiProviderRepo, sensitiveWordRepo, safetyRuleRepo,
		ragConfigRepo, promptTemplateRepo, safetyMessageRepo,
		configAuditLogRepo, infra.TxMgr,
		aesKey[:], infra.Redis, cfg.LLM,
	)
	return confighandler.NewConfigHandler(configSvc), configSvc, aesKey[:], nil
}

func buildWikiRouter(
	infra *Infrastructure,
	deptRepo *baserepo.DepartmentRepo,
	userRepo *authrepo.UserRepo,
	chunkRepo *wikirepo.ChunkRepo,
) *wikihandler.Router {
	articleRepo := wikirepo.NewArticleRepo(infra.Pool)
	referenceRepo := wikirepo.NewReferenceRepo(infra.Pool)
	auditLogRepo := wikirepo.NewAuditLogRepo(infra.Pool)
	outboxRepo := wikirepo.NewOutboxRepo(infra.Pool)
	vectorizeEnqueuer := adapter.NewAsynqVectorizeEnqueuer(infra.AsynqClient)
	deptLookup := adapter.NewBaseDepartmentLookup(deptRepo)
	applicantRoleResolver := adapter.NewAuthApplicantRoleResolver(userRepo)
	referenceSvc := wikiservice.NewReferenceService(
		referenceRepo, articleRepo, deptLookup, auditLogRepo, applicantRoleResolver, infra.TxMgr,
	)
	articleSvc := wikiservice.NewArticleService(
		articleRepo, auditLogRepo, chunkRepo, infra.TxMgr, vectorizeEnqueuer, outboxRepo, referenceSvc,
	)
	return wikihandler.NewRouter(
		wikihandler.NewPublicHandler(articleSvc),
		wikihandler.NewStaffArticleHandler(articleSvc),
		wikihandler.NewReferenceHandler(referenceSvc, articleSvc),
		infra.Auth,
	)
}

func buildChatRouter(
	ctx context.Context,
	infra *Infrastructure,
	llmCfg config.LLMConfig,
	aesKey []byte,
	deptRepo *baserepo.DepartmentRepo,
	configSvc *configservice.ConfigService,
	chunkRepo *wikirepo.ChunkRepo,
) (http.Handler, error) {
	conversationRepo := chatrepo.NewConversationRepo(infra.Pool)
	messageRepo := chatrepo.NewMessageRepo(infra.Pool)
	crisisRepo := chatrepo.NewCrisisRepo(infra.Pool)
	deptResolver := adapter.NewBaseDepartmentResolver(deptRepo)
	safetyRuleProvider := adapter.NewConfigSafetyRuleProvider(configSvc)
	promptProvider := adapter.NewConfigSystemPromptProvider(configSvc)

	// 可热切换的 LLM 客户端容器
	swappable := adapter.BuildSwappableClients()
	if err := adapter.ReloadAndSwap(ctx, swappable, infra.Pool, aesKey, llmCfg); err != nil {
		return nil, fmt.Errorf("load llm clients: %w", err)
	}

	// 订阅 Redis 频道，配置变更时自动热切换
	startLLMReloadSubscriber(ctx, infra, swappable, llmCfg, aesKey)

	// 注入 SwappableClient（实现 llm.Streamer/Embedder/Rewriter/Reranker 接口）
	llmClient := swappable.Chat
	embedClient := swappable.Embed
	rerankClient := swappable.Rerank
	rewriteClient := swappable.Rewrite

	var _ rag.LLMSafetyChecker = (*llm.LLMSafetyChecker)(nil)
	// LLMSafetyChecker 通过 provider 函数每次审查时取当前 swappable.Chat 快照——
	// 热切换后安全审查自动跟随新 client，也避免"启动未配置则永远不启用"。
	llmSafetyChecker := llm.NewLLMSafetyChecker(func() *llm.Client { return swappable.Chat.Load() })
	inputSafety := rag.NewDefaultInputSafetyFilter(safetyRuleProvider, llmSafetyChecker)
	outputSafety := rag.NewDefaultOutputSafetyFilter(safetyRuleProvider)
	ragConfigProvider := adapter.NewConfigRAGConfigProvider(configSvc)
	knowledgeSearcher := wikiservice.NewSearchService(
		chunkRepo, embedClient, rerankClient, ragConfigProvider,
	)
	// rewriter 直接注入动态的 swappable.Rewrite / swappable.Chat：
	// 依赖 SwappableClient 原子取当前 Client，管理员后续配置专用改写模型时热切换即生效，
	// 无需启动时快照决定（此前 `if !IsReady() 回退主 chat` 使专用模型后配置永不生效）。
	crisisNotifier := adapter.NewAsynqCrisisNotifier(infra.AsynqClient)
	// 匿名会话瞬态上下文环（Redis List，12h TTL 自动过期，无需清理任务）。
	ring := redis.NewRingStore(infra.Redis)
	chatSvc := chatservice.NewChatSendService(
		deptResolver, inputSafety, outputSafety, knowledgeSearcher,
		rewriteClient, llmClient, llmClient,
		conversationRepo, messageRepo, crisisRepo, crisisNotifier,
		infra.Locker, infra.TxMgr, ring, promptProvider,
	)
	convSvc := chatservice.NewConversationService(conversationRepo, messageRepo)
	crisisSvc := chatservice.NewCrisisService(crisisRepo)
	return chathandler.NewRouter(
		infra.Auth,
		infra.RateLimiter,
		infra.Cfg.RateLimit,
		chathandler.NewStreamHandler(chatSvc),
		chathandler.NewConversationHandler(convSvc),
		chathandler.NewCrisisHandler(crisisSvc),
	), nil
}

// startLLMReloadSubscriber 订阅 Redis 频道，收到 AI Provider 变更通知后重新加载 LLM 客户端并热切换。
// 当 ctx 取消时自动退出。Redis 不可用时静默跳过（仅影响热切换时效性）。
func startLLMReloadSubscriber(
	ctx context.Context,
	infra *Infrastructure,
	sc *llm.SwappableClients,
	llmCfg config.LLMConfig,
	aesKey []byte,
) {
	if infra.Redis == nil {
		slog.Warn("llm: redis not available, hot-reload subscriber not started")
		return
	}
	channel := configservice.LLMReloadChannel()
	sub := infra.Redis.Subscribe(ctx, channel)
	// 热重载超时：DB 查询 + LLM 客户端构造应在 30s 内完成。
	const reloadTimeout = 30 * time.Second
	go func() {
		defer func() { _ = sub.Close() }()
		slog.Info("llm: hot-reload subscriber started", "channel", channel)
		ch := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				slog.Info("llm: hot-reload subscriber stopped")
				return
			case msg, ok := <-ch:
				if !ok {
					slog.Warn("llm: hot-reload subscriber channel closed")
					return
				}
				slog.Info("llm: received reload notification", "channel", msg.Channel)
				// 用订阅 ctx（收到关闭信号即取消），而非 Background，避免 goroutine 泄漏并跟随服务生命周期。
				reloadCtx, cancel := context.WithTimeout(ctx, reloadTimeout)
				if err := adapter.ReloadAndSwap(reloadCtx, sc, infra.Pool, aesKey, llmCfg); err != nil {
					slog.Error("llm: hot-reload failed", "err", err)
				}
				cancel()
			}
		}
	}()
}
