package service

import (
	"context"

	"health-nexus/internal/domain/config/entity"
	"health-nexus/internal/shared/pagination"
)

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

// PromptTemplatePort Prompt 模板仓储能力。
type PromptTemplatePort interface {
	List(
		ctx context.Context, promptType string, isActive *bool, p pagination.Params,
	) ([]*entity.PromptTemplate, int64, error)
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
type AuditLogPort interface {
	Create(ctx context.Context, l *entity.ConfigAuditLog) error
	ListByEntity(
		ctx context.Context, entityType string, entityID int64, page, pageSize int,
	) ([]*entity.ConfigAuditLog, int, error)
}
