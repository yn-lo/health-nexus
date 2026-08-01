package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"health-nexus/internal/domain/config/entity"
)

// RAGConfigRepo 对应 rag_configs 表（单例 id=1）。
type RAGConfigRepo struct {
	pool *pgxpool.Pool
}

// NewRAGConfigRepo 创建 RAGConfigRepo。
func NewRAGConfigRepo(pool *pgxpool.Pool) *RAGConfigRepo {
	return &RAGConfigRepo{pool: pool}
}

// Get 读取单例配置。未找到返回 ErrNotFound（service 层可用默认值兜底或返回 404）。
func (r *RAGConfigRepo) Get(ctx context.Context) (*entity.RAGConfig, error) {
	q := `SELECT id, chunk_size, chunk_overlap, max_chunks, top_k,
		similarity_threshold, rerank_enabled, rerank_threshold, diversity_factor, ood_threshold, updated_at
		FROM rag_configs WHERE id = 1`
	c := &entity.RAGConfig{}
	err := r.pool.QueryRow(ctx, q).Scan(
		&c.ID, &c.ChunkSize, &c.ChunkOverlap, &c.MaxChunks, &c.TopK,
		&c.SimilarityThreshold, &c.RerankEnabled, &c.RerankThreshold, &c.DiversityFactor, &c.OODThreshold, &c.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan rag_config: %w", err)
	}
	return c, nil
}

// Upsert 插入或更新单例配置（id=1）。
func (r *RAGConfigRepo) Upsert(ctx context.Context, c *entity.RAGConfig) error {
	q := `INSERT INTO rag_configs (id, chunk_size, chunk_overlap, max_chunks, top_k,
			similarity_threshold, rerank_enabled, rerank_threshold, diversity_factor, ood_threshold, updated_at)
		VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9, now())
		ON CONFLICT (id) DO UPDATE SET
			chunk_size = EXCLUDED.chunk_size,
			chunk_overlap = EXCLUDED.chunk_overlap,
			max_chunks = EXCLUDED.max_chunks,
			top_k = EXCLUDED.top_k,
			similarity_threshold = EXCLUDED.similarity_threshold,
			rerank_enabled = EXCLUDED.rerank_enabled,
			rerank_threshold = EXCLUDED.rerank_threshold,
			diversity_factor = EXCLUDED.diversity_factor,
			ood_threshold = EXCLUDED.ood_threshold,
			updated_at = now()
		RETURNING updated_at`
	return r.pool.QueryRow(ctx, q,
		c.ChunkSize, c.ChunkOverlap, c.MaxChunks, c.TopK,
		c.SimilarityThreshold, c.RerankEnabled, c.RerankThreshold, c.DiversityFactor, c.OODThreshold,
	).Scan(&c.UpdatedAt)
}
