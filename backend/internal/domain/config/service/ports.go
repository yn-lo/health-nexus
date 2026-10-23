package service

import (
	"context"
	"time"

	"health-nexus/internal/domain/config/entity"
	"health-nexus/internal/shared/pagination"
)

// TxRunner 事务执行能力（消费者定义，ISP）。*postgres.TxManager 实现此接口。
type TxRunner interface {
	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// CacheClient 单例配置 cache-aside 与变更广播能力（消费者定义，ISP）。
// *redis.Cache 实现此接口；为 nil 时跳过缓存与热重载通知。
type CacheClient interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
	Publish(ctx context.Context, channel, message string) error
}

// AIProviderPort AI 提供商仓储能力（消费者定义，ISP）。
type AIProviderPort interface {
	List(ctx context.Context, providerType string, isActive *bool) ([]*entity.AIProvider, error)
	Get(ctx context.Context, id int64) (*entity.AIProvider, error)
	Create(ctx context.Context, p *entity.AIProvider) error
	Update(ctx context.Context, p *entity.AIProvider) error
	Delete(ctx context.Context, id int64) error
	CurrentEmbeddingDimension(ctx context.Context) (int, error)
	HasVectorizedChunks(ctx context.Context) (bool, error)
	AlignEmbeddingDimension(ctx context.Context, dim int) error
}

// SensitiveWordPort 敏感词仓储能力。
//
//nolint:dupl // SensitiveWordPort 与 SafetyRulePort 为不同域的 CRUD 接口，结构相近但语义独立，合并成泛型过度设计
type SensitiveWordPort interface {
	List(ctx context.Context, category string, p pagination.Params) ([]*entity.SensitiveWord, int64, error)
	Get(ctx context.Context, id int64) (*entity.SensitiveWord, error)
	Create(ctx context.Context, w *entity.SensitiveWord) error
	Update(ctx context.Context, w *entity.SensitiveWord) error
	Delete(ctx context.Context, id int64) error
}

// SafetyRulePort 安全规则仓储能力。
//
//nolint:dupl // 同上：SafetyRulePort 为安全规则域 CRUD 接口，与 SensitiveWordPort 结构相近但语义独立
type SafetyRulePort interface {
	List(ctx context.Context, category string, p pagination.Params) ([]*entity.SafetyRule, int64, error)
	Get(ctx context.Context, id int64) (*entity.SafetyRule, error)
	Create(ctx context.Context, r *entity.SafetyRule) error
	Update(ctx context.Context, r *entity.SafetyRule) error
	Delete(ctx context.Context, id int64) error
}

// RAGConfigPort RAG 配置仓储能力。
type RAGConfigPort interface {
	Get(ctx context.Context) (*entity.RAGConfig, error)
	Upsert(ctx context.Context, c *entity.RAGConfig) error
}

// BrandConfigPort 品牌配置仓储能力。
type BrandConfigPort interface {
	Get(ctx context.Context) (*entity.BrandConfig, error)
	Upsert(ctx context.Context, c *entity.BrandConfig) error
}

// PromptTemplatePort Prompt 模板仓储能力。
// GetActive 是「哪个版本生效」的唯一查询入口（唯一索引 uq_prompt_templates_active_per_type 与之对应），
// 管理端展示与运行时调用共用同一结果，禁止在别处二次实现选择逻辑。
type PromptTemplatePort interface {
	List(
		ctx context.Context, promptType string, isActive *bool, p pagination.Params,
	) ([]*entity.PromptTemplate, int64, error)
	GetActive(ctx context.Context, promptType string) (*entity.PromptTemplate, error)
	Create(ctx context.Context, t *entity.PromptTemplate) error
	UpdateContentAndActive(
		ctx context.Context, id int64, content *string, isActive *bool,
	) (*entity.PromptTemplate, error)
	Delete(ctx context.Context, id int64) error
}

// SafetyMessagePort 安全话术仓储能力。
type SafetyMessagePort interface {
	ListAll(ctx context.Context) ([]*entity.SafetyMessage, error)
	Upsert(ctx context.Context, msgType, content string) error
}

// AuditLogPort 配置审计日志仓储能力。
// entityID 为 nil 表示不按实体 ID 过滤；指向 0 表示只查单例配置（entity_id IS NULL）；
// 指向正整数表示按该实体 ID 过滤。
type AuditLogPort interface {
	Create(ctx context.Context, l *entity.ConfigAuditLog) error
	ListByEntity(
		ctx context.Context, entityType string, entityID *int64, page, pageSize int,
	) ([]*entity.ConfigAuditLog, int, error)
}
