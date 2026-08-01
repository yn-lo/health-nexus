package adapter

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	appconfig "health-nexus/internal/config"
	"health-nexus/internal/domain/config/entity"
	"health-nexus/internal/domain/config/repository"
	"health-nexus/internal/platform/llm"
	"health-nexus/internal/shared/constants"
)

// BuildSwappableClients 构造 4 个 SwappableClient（初始均未配置）。
// 启动时先调用此函数创建容器，再调用 ReloadAndSwap 从 DB 加载并 Swap。
func BuildSwappableClients() *llm.SwappableClients {
	return &llm.SwappableClients{
		Chat:    llm.NewSwappableClient(nil),
		Embed:   llm.NewSwappableClient(nil),
		Rerank:  llm.NewSwappableClient(nil),
		Rewrite: llm.NewSwappableClient(nil),
	}
}

// ReloadAndSwap 从 DB 重新加载 active provider，构造 LLM 客户端，原子替换到 SwappableClients。
// 用于启动时初始加载 + 配置变更后热切换。
// 加载顺序：DB 优先 -> config.yaml fallback。
//
// ConfigService 在 Create/Update/Delete AI Provider 后发布 Redis 通知，
// DI 层订阅通知后调用本函数热切换。
// 解密 API Key 用 cfg.Security.EncryptionKey 派生的 aesKey（与 ConfigService 共用）。
func ReloadAndSwap(
	ctx context.Context,
	sc *llm.SwappableClients,
	pool *pgxpool.Pool,
	aesKey []byte,
	fallback appconfig.LLMConfig,
) error {
	aiRepo := repository.NewAIProviderRepo(pool)
	active := true
	all, err := aiRepo.List(ctx, "", &active)
	if err != nil {
		return fmt.Errorf("list active ai_providers: %w", err)
	}

	byType := make(map[string]*entity.AIProvider, 4)
	for _, p := range all {
		if _, exists := byType[p.ProviderType]; !exists {
			byType[p.ProviderType] = p
		}
	}

	chat, err := buildClientWithFallback(
		byType, constants.ProviderTypeLLM, aesKey, fallback, llm.NewClient, "chat",
	)
	if err != nil {
		return err
	}
	sc.Chat.Swap(chat)

	embed, err := buildClientWithFallback(
		byType, constants.ProviderTypeEmbedding, aesKey, fallback, llm.NewEmbeddingClient, "embed",
	)
	if err != nil {
		return err
	}
	sc.Embed.Swap(embed)

	rerank, err := buildClientWithFallback(
		byType, constants.ProviderTypeRerank, aesKey, fallback, llm.NewRerankClient, "rerank",
	)
	if err != nil {
		return err
	}
	sc.Rerank.Swap(rerank)

	// Rewrite 降级策略：无专用 rewrite provider 时复用 chat client（rewrite 本质是轻量 LLM 调用，
	// 无需独立 API Key/Endpoint；rewrite.go 内部 RewriteModel 为空时自动回退 ChatModel）。
	var rewrite *llm.Client
	if _, hasRewrite := byType[constants.ProviderTypeRewrite]; hasRewrite {
		if rewrite, err = buildClientWithFallback(
			byType, constants.ProviderTypeRewrite, aesKey, fallback, llm.NewRewriteClient, "rewrite",
		); err != nil {
			return err
		}
	} else {
		rewrite = chat
		slog.Info("llm: no dedicated rewrite provider, reusing chat client")
	}
	sc.Rewrite.Swap(rewrite)

	slog.Info("llm: hot-reload completed",
		"chat_ready", chat != nil && chat.IsReady(),
		"embed_ready", embed != nil && embed.IsReady(),
		"rerank_ready", rerank != nil && rerank.IsReady(),
		"rewrite_ready", rewrite != nil && rewrite.IsReady(),
	)
	return nil
}
