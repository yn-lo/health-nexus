// Package repository 实现 config 域的数据访问层（手写 SQL + pgx）。
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"health-nexus/internal/domain/config/entity"
	"health-nexus/internal/platform/postgres"
)

// AIProviderRepo 对应 ai_providers 表。
type AIProviderRepo struct {
	pool *pgxpool.Pool
}

// NewAIProviderRepo 创建 AIProviderRepo。
func NewAIProviderRepo(pool *pgxpool.Pool) *AIProviderRepo {
	return &AIProviderRepo{pool: pool}
}

// ErrNotFound 通用"未找到"哨兵错误，service 层翻译为 404。
var ErrNotFound = errors.New("record not found")

const aiProviderColumns = `id, name, provider_type, api_url, api_key_encrypted, api_key_masked,
	model_name, dimension, parameters, is_active, created_at, updated_at`

// List 按 provider_type 和 is_active 过滤。providerType 空表示不过滤；isActive nil 表示不过滤。
func (r *AIProviderRepo) List(ctx context.Context, providerType string, isActive *bool) ([]*entity.AIProvider, error) {
	q := "SELECT " + aiProviderColumns + " FROM ai_providers WHERE 1=1"
	args := []any{}
	if providerType != "" {
		args = append(args, providerType)
		q += " AND provider_type = $" + fmt.Sprintf("%d", len(args))
	}
	if isActive != nil {
		args = append(args, *isActive)
		q += " AND is_active = $" + fmt.Sprintf("%d", len(args))
	}
	q += " ORDER BY created_at DESC"

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query ai_providers: %w", err)
	}
	defer rows.Close()

	var out []*entity.AIProvider
	for rows.Next() {
		p, err := scanAIProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Get 按 ID 查询单条。未找到返回 (nil, ErrNotFound)。
func (r *AIProviderRepo) Get(ctx context.Context, id int64) (*entity.AIProvider, error) {
	q := "SELECT " + aiProviderColumns + " FROM ai_providers WHERE id = $1"
	row := r.pool.QueryRow(ctx, q, id)
	p, err := scanAIProvider(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

// Create 插入新 AI 提供商。p.ID/p.CreatedAt/p.UpdatedAt 由 RETURNING 回填。
func (r *AIProviderRepo) Create(ctx context.Context, p *entity.AIProvider) error {
	q := `INSERT INTO ai_providers
		(name, provider_type, api_url, api_key_encrypted, api_key_masked,
		 model_name, dimension, parameters, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, created_at, updated_at`
	return r.pool.QueryRow(ctx, q,
		p.Name, p.ProviderType, p.APIURL, p.APIKeyEncrypted, p.APIKeyMasked,
		p.ModelName, p.Dimension, p.Parameters, p.IsActive,
	).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
}

// Update 全量更新（service 层负责合并 patch）。
func (r *AIProviderRepo) Update(ctx context.Context, p *entity.AIProvider) error {
	q := `UPDATE ai_providers SET
		name = $2, provider_type = $3, api_url = $4, api_key_encrypted = $5, api_key_masked = $6,
		model_name = $7, dimension = $8, parameters = $9, is_active = $10, updated_at = now()
		WHERE id = $1
		RETURNING created_at, updated_at`
	tag, err := r.pool.Exec(ctx, q,
		p.ID, p.Name, p.ProviderType, p.APIURL, p.APIKeyEncrypted, p.APIKeyMasked,
		p.ModelName, p.Dimension, p.Parameters, p.IsActive,
	)
	if err != nil {
		return fmt.Errorf("update ai_provider: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete 按 ID 删除。未找到返回 ErrNotFound。
func (r *AIProviderRepo) Delete(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, "DELETE FROM ai_providers WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete ai_provider: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ============ 方案 C：embedding 维度动态对齐 ============

// ErrEmbeddingDimChangeBlocked 已有向量化切片，禁止更改 embedding 维度。
// service 层翻译为 409 CONFIG_EMBEDDING_DIM_CHANGE_BLOCKED。
var ErrEmbeddingDimChangeBlocked = errors.New("embedding dimension change blocked by existing vectors")

// CurrentEmbeddingDimension 返回 article_chunks.embedding 当前的 vector(N) 维度。
// 列不存在或类型不是 vector 时返回 (0, nil)。
// ponytail: 直接查 pg_attribute + format_type，不依赖 pgvector 元数据表，简化，pgvector 0.5+ 兼容。
func (r *AIProviderRepo) CurrentEmbeddingDimension(ctx context.Context) (int, error) {
	const q = `SELECT coalesce(
		(SELECT (regexp_match(format_type(atttypid, atttypmod), 'vector\((\d+)\)'))[1]::int
		FROM pg_attribute
		WHERE attrelid = 'article_chunks'::regclass AND attname = 'embedding'),
		0)`
	var dim int
	if err := r.pool.QueryRow(ctx, q).Scan(&dim); err != nil {
		return 0, fmt.Errorf("query embedding dimension: %w", err)
	}
	return dim, nil
}

// HasVectorizedChunks 报告 article_chunks 是否存在非 NULL 的 embedding。
// ponytail: 用 EXISTS 子查询，扫描到第一行即返回，O(1)，简化。
func (r *AIProviderRepo) HasVectorizedChunks(ctx context.Context) (bool, error) {
	const q = `SELECT EXISTS (SELECT 1 FROM article_chunks WHERE embedding IS NOT NULL)`
	var has bool
	if err := r.pool.QueryRow(ctx, q).Scan(&has); err != nil {
		return false, fmt.Errorf("check vectorized chunks: %w", err)
	}
	return has, nil
}

// AlignEmbeddingDimension 把 article_chunks.embedding 列类型对齐到 dim 维。
// 已有非 NULL embedding 时返回 ErrEmbeddingDimChangeBlocked（service 层应在调用前自检）。
// DDL 步骤：
//  1. ALTER COLUMN TYPE vector(dim) USING NULL（清空旧向量，避免维度不匹配导致 ALTER 失败）
//  2. DROP + CREATE hnsw 索引（hnsw 索引与列维度绑定，必须重建）
//
// ponytail: USING NULL 等于清空所有已向量化的切片——这是方案 C 的"切换维度即清空"语义，折中，
// 调用方（ConfigService）已先 HasVectorizedChunks 拦截，到这里一定无向量；保留 USING NULL 仅作防御兜底。
// 上限：DDL 不可回滚（无事务），失败时列类型可能已变但索引未重建——ConfigService 调用方应记 slog 告警。
// 升级路径：若要支持零停机切换，需新建 vector(N) 列 + 双写 + 切换读列 + DROP 旧列。
//
// 注：pgvector 的 vector(N) 中 N 不接受参数占位符（$1），必须字面整数；dim 已被校验为 1-16000，
// 用 fmt.Sprintf 拼接安全（无注入风险）。
func (r *AIProviderRepo) AlignEmbeddingDimension(ctx context.Context, dim int) error {
	if dim <= 0 || dim > 16000 {
		return fmt.Errorf("invalid embedding dimension %d (must be 1-16000)", dim)
	}
	// 防御：DDL 前 final check，避免 service 层 TOCTOU 竞态。
	has, err := r.HasVectorizedChunks(ctx)
	if err != nil {
		return err
	}
	if has {
		return ErrEmbeddingDimChangeBlocked
	}
	// 1. ALTER COLUMN TYPE（pgvector 不接受 vector($1)，必须字面整数；dim 已校验 1-16000）
	if _, err := r.pool.Exec(ctx,
		fmt.Sprintf(`ALTER TABLE article_chunks ALTER COLUMN embedding TYPE vector(%d) USING NULL`, dim),
	); err != nil {
		return fmt.Errorf("alter embedding column to vector(%d): %w", dim, err)
	}
	// 2. 重建 hnsw 索引（先 DROP IF EXISTS，再 CREATE INDEX；不使用 IF NOT EXISTS 以避免索引残留）
	if _, err := r.pool.Exec(ctx,
		`DROP INDEX IF EXISTS idx_article_chunks_embedding`,
	); err != nil {
		return fmt.Errorf("drop hnsw index: %w", err)
	}
	if _, err := r.pool.Exec(ctx,
		`CREATE INDEX idx_article_chunks_embedding
			ON article_chunks USING hnsw (embedding vector_cosine_ops)
			WITH (m = 16, ef_construction = 64)`,
	); err != nil {
		return fmt.Errorf("create hnsw index for vector(%d): %w", dim, err)
	}
	return nil
}

func scanAIProvider(s postgres.Scanner) (*entity.AIProvider, error) {
	p := &entity.AIProvider{}
	err := s.Scan(
		&p.ID, &p.Name, &p.ProviderType, &p.APIURL, &p.APIKeyEncrypted, &p.APIKeyMasked,
		&p.ModelName, &p.Dimension, &p.Parameters, &p.IsActive, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan ai_provider: %w", err)
	}
	return p, nil
}
