// Package service 实现 config 域的业务逻辑（统一服务覆盖 6 个子模块）。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"health-nexus/internal/config"
	"health-nexus/internal/domain/config/entity"
	"health-nexus/internal/platform/postgres"
	"health-nexus/internal/shared/constants"
	"health-nexus/internal/shared/contextkeys"
)

// config 缓存 key（FIX-6：单例配置 cache-aside）。
const (
	cacheKeyRAGConfig      = "health-nexus:config:rag"
	cacheKeySafetyMessages = "health-nexus:config:safety_messages"
	cacheTTL               = 5 * time.Minute

	// LLMReloadChannel AI Provider 变更通知的 Redis 频道。
	// 订阅方（server/worker）收到通知后重新加载 LLM 客户端并热切换。
	llmReloadChannel = "health-nexus:llm:reload"
)

// ConfigService 系统配置统一服务，覆盖 6 个子模块：
// AI Provider / 敏感词 / 安全规则 / RAG / Prompt 模板 / 安全话术。
type ConfigService struct {
	aiProviderRepo     AIProviderPort
	sensitiveWordRepo  SensitiveWordPort
	safetyRuleRepo     SafetyRulePort
	ragConfigRepo      RAGConfigPort
	promptTemplateRepo PromptTemplatePort
	safetyMessageRepo  SafetyMessagePort
	auditLogRepo       AuditLogPort
	tx                 *postgres.TxManager
	aesKey             []byte
	redis              *goredis.Client
	llmCfg             config.LLMConfig // config.yaml fallback，供 GetConfigStatus 判断
}

// NewConfigService 创建 ConfigService。
// aesKey 必须是 32 字节（AES-256），由 di 层从 cfg.Security.EncryptionKey 派生 SHA-256 注入。
// redis 用于单例配置（RAGConfig/SafetyMessages）cache-aside，可为 nil（跳过缓存，回源 DB）。
// auditLogRepo 用于持久化变更审计日志（契约 §6 ConfigAuditLog）；tx 用于事务包裹批量写。
func NewConfigService(
	aiProviderRepo AIProviderPort,
	sensitiveWordRepo SensitiveWordPort,
	safetyRuleRepo SafetyRulePort,
	ragConfigRepo RAGConfigPort,
	promptTemplateRepo PromptTemplatePort,
	safetyMessageRepo SafetyMessagePort,
	auditLogRepo AuditLogPort,
	tx *postgres.TxManager,
	aesKey []byte,
	redis *goredis.Client,
) *ConfigService {
	return &ConfigService{
		aiProviderRepo:     aiProviderRepo,
		sensitiveWordRepo:  sensitiveWordRepo,
		safetyRuleRepo:     safetyRuleRepo,
		ragConfigRepo:      ragConfigRepo,
		promptTemplateRepo: promptTemplateRepo,
		safetyMessageRepo:  safetyMessageRepo,
		auditLogRepo:       auditLogRepo,
		tx:                 tx,
		aesKey:             aesKey,
		redis:              redis,
	}
}

// NewConfigServiceWithLLM 创建带 LLMConfig 的 ConfigService（供 DI 装配和测试使用）。
// llmCfg 用于 GetConfigStatus 判断 config.yaml fallback 是否有可用配置。
func NewConfigServiceWithLLM(
	aiProviderRepo AIProviderPort,
	sensitiveWordRepo SensitiveWordPort,
	safetyRuleRepo SafetyRulePort,
	ragConfigRepo RAGConfigPort,
	promptTemplateRepo PromptTemplatePort,
	safetyMessageRepo SafetyMessagePort,
	auditLogRepo AuditLogPort,
	tx *postgres.TxManager,
	aesKey []byte,
	redis *goredis.Client,
	llmCfg config.LLMConfig,
) *ConfigService {
	svc := NewConfigService(aiProviderRepo, sensitiveWordRepo, safetyRuleRepo,
		ragConfigRepo, promptTemplateRepo, safetyMessageRepo,
		auditLogRepo, tx, aesKey, redis)
	svc.llmCfg = llmCfg
	return svc
}

// audit 记录配置变更审计日志：slog.InfoContext 双写 + DB 持久化（契约 §6 ConfigAuditLog）。
// operator_id / operator_role 由 jwt_auth 中间件注入 ctx（int64 / string）。
// entityID 可为 nil（单例配置：RAGConfig / SafetyMessages）、int64 或 *int64。
// slogKV 是 slog 的 key/value 对，会同时进入 slog 日志与 DB changes JSONB。
// DB 持久化为 best-effort：失败仅 slog.Warn，不阻塞主流程。
func (s *ConfigService) audit(ctx context.Context, action, entityType string, entityID any, slogKV ...any) {
	// jwt_auth 写入 ctx 的 UserID 是 int64（直接断言；FromCtx 仅返回 string 不适用）。
	operatorID, _ := ctx.Value(contextkeys.UserID).(int64)
	operatorRole, _ := ctx.Value(contextkeys.UserRole).(string)

	var eid *int64
	switch v := entityID.(type) {
	case nil:
		// 单例配置：entity_id 列为 NULL
	case int64:
		eid = &v
	case *int64:
		eid = v
	}

	// 1. slog 双写（保留原有日志行为，operator_id 用数值化修复空串问题）
	args := []any{
		"action", action,
		"entity_type", entityType,
		"operator_id", operatorID,
		"entity_id", entityID,
	}
	args = append(args, slogKV...)
	slog.InfoContext(ctx, "config: audit", args...)

	// 2. DB 持久化（best-effort）
	if s.auditLogRepo == nil {
		return
	}
	changes := slogKVsToJSON(slogKV)
	if err := s.auditLogRepo.Create(ctx, &entity.ConfigAuditLog{
		Action:       action,
		EntityType:   entityType,
		EntityID:     eid,
		OperatorID:   operatorID,
		OperatorRole: operatorRole,
		Changes:      changes,
	}); err != nil {
		slog.WarnContext(ctx, "config: persist audit log failed",
			"err", err, "action", action, "entity_type", entityType)
	}
}

// slogKVsToJSON 把 slog 的 key/value 对打包成 JSONB 字节。无法序列化的值用 fmt.Sprint 兜底。
func slogKVsToJSON(kvs []any) []byte {
	if len(kvs) == 0 {
		return nil
	}
	m := make(map[string]any, len(kvs)/2)
	for i := 0; i+1 < len(kvs); i += 2 {
		k, ok := kvs[i].(string)
		if !ok {
			k = strconv.Itoa(i)
		}
		m[k] = kvs[i+1]
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	return b
}

// invalidateCache 删除指定缓存 key。Redis 不可用时静默跳过（ponytail：缓存失败不影响正确性）。
func (s *ConfigService) invalidateCache(ctx context.Context, keys ...string) {
	if s.redis == nil {
		return
	}
	s.redis.Del(ctx, keys...)
}

// LLMReloadChannel 返回 LLM 热重载通知的 Redis 频道名。
// 供 DI 层订阅使用。
func LLMReloadChannel() string {
	return llmReloadChannel
}

// notifyLLMReload 发布 AI Provider 变更通知。
// 订阅方（server/worker 进程）收到通知后从 DB 重新加载 LLM 客户端并热切换。
// Redis 不可用时静默跳过（仅影响热切换时效性，下次启动仍会加载最新配置）。
func (s *ConfigService) notifyLLMReload(ctx context.Context) {
	if s.redis == nil {
		slog.WarnContext(ctx, "config: redis not available, LLM hot-reload notification skipped")
		return
	}
	if err := s.redis.Publish(ctx, llmReloadChannel, "reload").Err(); err != nil {
		slog.WarnContext(ctx, "config: failed to publish LLM reload notification", "err", err)
	}
}

// ============ Config Status ============

// GetConfigStatus 返回各 LLM 模块的配置状态（DB 优先 → config.yaml fallback）。
// 供管理端工作台展示黄色预警：未配置的模块显示提示信息。
func (s *ConfigService) GetConfigStatus(ctx context.Context) (*ConfigStatusResponse, error) {
	active := true
	all, err := s.aiProviderRepo.List(ctx, "", &active)
	if err != nil {
		return nil, fmt.Errorf("list active ai_providers: %w", err)
	}

	byType := make(map[string]bool, 4)
	for _, p := range all {
		byType[p.ProviderType] = true
	}

	return &ConfigStatusResponse{
		LLM: s.resolveProviderStatus(
			byType, constants.ProviderTypeLLM, s.llmCfg.APIKey != "", msgLLM,
		),
		Embedding: s.resolveProviderStatus(
			byType, constants.ProviderTypeEmbedding, s.hasYAMLEmbeddingKey(), msgEmbedding,
		),
		Rerank: s.resolveProviderStatus(
			byType, constants.ProviderTypeRerank, s.hasYAMLRerankKey(), msgRerank,
		),
		Rewrite: s.resolveProviderStatus(
			byType, constants.ProviderTypeRewrite, s.hasYAMLRewriteKey(), msgRewrite,
		),
	}, nil
}

// 未配置时的提示信息。
const (
	msgLLM       = "主聊天模型未配置，请在管理后台添加 LLM 提供商"
	msgEmbedding = "向量模型未配置，检索和向量化功能不可用"
	msgRerank    = "重排模型未配置，检索质量会下降"
	msgRewrite   = "查询改写模型未配置，将回退到主聊天模型"
)

func (s *ConfigService) resolveProviderStatus(
	byType map[string]bool,
	providerType string,
	yamlFallback bool,
	notConfiguredMsg string,
) ProviderStatus {
	if byType[providerType] {
		return ProviderStatus{Configured: true}
	}
	if yamlFallback {
		return ProviderStatus{Configured: true}
	}
	return ProviderStatus{Configured: false, Message: notConfiguredMsg}
}

// hasYAMLEmbeddingKey 判断 config.yaml 中 embedding 配置是否可用（含回退到主 api_key）。
func (s *ConfigService) hasYAMLEmbeddingKey() bool {
	if s.llmCfg.Embedding.APIKey != "" {
		return true
	}
	return s.llmCfg.APIKey != ""
}

// hasYAMLRerankKey 判断 config.yaml 中 rerank 配置是否可用（含回退到主 api_key）。
func (s *ConfigService) hasYAMLRerankKey() bool {
	if s.llmCfg.Rerank.APIKey != "" {
		return true
	}
	// rerank 还需要 model
	if s.llmCfg.APIKey != "" && s.llmCfg.Rerank.Model != "" {
		return true
	}
	return false
}

// hasYAMLRewriteKey 判断 config.yaml 中 rewrite 配置是否可用（含回退到主 api_key）。
func (s *ConfigService) hasYAMLRewriteKey() bool {
	if s.llmCfg.Rewrite.APIKey != "" {
		return true
	}
	return s.llmCfg.APIKey != ""
}
