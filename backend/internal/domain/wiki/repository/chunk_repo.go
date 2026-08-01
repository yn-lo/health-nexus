package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"

	"health-nexus/internal/domain/wiki/entity"
	"health-nexus/internal/platform/postgres"
	"health-nexus/internal/shared/constants"
)

// ChunkRepo 文章切片仓储。
// 切片写入由 asynq Worker 异步调用（REQ-WIKI-012）；本任务只提供仓储能力，不实现切片逻辑。
type ChunkRepo struct {
	pool *pgxpool.Pool
}

// NewChunkRepo 构造切片仓储。
func NewChunkRepo(pool *pgxpool.Pool) *ChunkRepo {
	return &ChunkRepo{pool: pool}
}

// Create 插入切片。c.ID/c.CreatedAt 由 RETURNING 回填。
// embedding 与 tsv 由 Worker 生成后传入；tsvector 通过 bigram_tsvector(content) 计算（中文 bigram 分词）。
func (r *ChunkRepo) Create(ctx context.Context, c *entity.ArticleChunk) error {
	const sql = `INSERT INTO article_chunks
		(article_id, chunk_index, content, content_hash, embedding, tsv, is_active, version)
		VALUES ($1, $2, $3, $4, $5, bigram_tsvector($3), $6, $7)
		RETURNING id, created_at`
	return postgres.Q(ctx, r.pool).QueryRow(ctx, sql,
		c.ArticleID, c.ChunkIndex, c.Content, c.ContentHash, c.Embedding, c.IsActive, c.Version,
	).Scan(&c.ID, &c.CreatedAt)
}

// ListActiveByArticle 列出文章当前生效的切片（is_active=true），按 chunk_index 排序。
// 供检索服务（SearchService）与 Worker 重新切片时使用。
func (r *ChunkRepo) ListActiveByArticle(ctx context.Context, articleID int64) ([]*entity.ArticleChunk, error) {
	const sql = `SELECT id, article_id, chunk_index, content, content_hash, embedding, is_active, version, created_at
		FROM article_chunks
		WHERE article_id = $1 AND is_active = true
		ORDER BY chunk_index`
	rows, err := postgres.Q(ctx, r.pool).Query(ctx, sql, articleID)
	if err != nil {
		return nil, fmt.Errorf("list article_chunks: %w", err)
	}
	defer rows.Close()

	out := make([]*entity.ArticleChunk, 0)
	for rows.Next() {
		c := &entity.ArticleChunk{}
		if err := rows.Scan(&c.ID, &c.ArticleID, &c.ChunkIndex, &c.Content, &c.ContentHash,
			&c.Embedding, &c.IsActive, &c.Version, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan article_chunk: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeactivateByArticle 将指定文章当前生效切片标记为失效（is_active=false）。
// REQ-WIKI-016：新版本发布后旧版本切片失效。Worker 重新切片前调用。
// 返回受影响行数。
func (r *ChunkRepo) DeactivateByArticle(ctx context.Context, articleID int64) (int64, error) {
	const sql = `UPDATE article_chunks SET is_active = false WHERE article_id = $1 AND is_active = true`
	tag, err := postgres.Q(ctx, r.pool).Exec(ctx, sql, articleID)
	if err != nil {
		return 0, fmt.Errorf("deactivate article_chunks: %w", err)
	}
	return tag.RowsAffected(), nil
}

// DeleteInactiveByArticle 物理删除指定文章的已失效切片（is_active=false）。
// Worker 写入新切片前调用，避免旧版本切片无限堆积。
// 返回受影响行数。
func (r *ChunkRepo) DeleteInactiveByArticle(ctx context.Context, articleID int64) (int64, error) {
	const sql = `DELETE FROM article_chunks WHERE article_id = $1 AND is_active = false`
	tag, err := postgres.Q(ctx, r.pool).Exec(ctx, sql, articleID)
	if err != nil {
		return 0, fmt.Errorf("delete inactive article_chunks: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ChunkSearchHit 检索命中结果（含文章标题与相关性分数）。
// ArticleTitle 由 JOIN articles 填充；Score 为该路检索的原始分数（向量=1-cosine_distance，BM25=ts_rank）。
type ChunkSearchHit struct {
	entity.ArticleChunk
	ArticleTitle string
	Score        float64
}

// chunkSearchColumns 检索 SQL 共用的 SELECT 列（含 article_title 与 score）。
const chunkSearchColumns = `c.id, c.article_id, c.chunk_index, c.content, c.content_hash,
	c.embedding, c.is_active, c.version, c.created_at,
	COALESCE(a.title, '')`

// deptVisibilitySQL 生成科室可见性子条件：deptIDs 为 nil/空时不限制，非空时
// 允许文章所属科室命中 OR 文章被 approved 引用授权给该科室。
// 占位符从 startArg 开始，append deptIDs + approved status 到 args。
// 返回新增的 SQL 片段与下一个可用占位符序号。
func deptVisibilitySQL(deptIDs []int64, startArg int, args *[]any) (sql string, nextArg int) {
	if len(deptIDs) == 0 {
		return "1=1", startArg
	}
	// $N=deptIDs 数组（同一参数在 a.department_id 与 r.target_dept_id 复用），$N+1=approved 状态。
	*args = append(*args, deptIDs, constants.ReferenceStatusApproved)
	return fmt.Sprintf(
		`(a.department_id = ANY($%d) OR EXISTS (
			SELECT 1 FROM article_references r
			WHERE r.article_id = c.article_id
			  AND r.target_dept_id = ANY($%d)
			  AND r.status = $%d))`,
		startArg, startArg, startArg+1,
	), startArg + 2
}

// SearchByVector HNSW 向量检索 topK 候选（REQ-WIKI-013）。
// embedding 为查询向量；deptIDs 为 nil 时不限制科室可见性，非 nil 时按"本科室 + 已授权引用"过滤。
// similarityThreshold > 0 时在 SQL 层预过滤相似度（1 - cosine_distance），减少回传与上层重复计算。
// 始终排除空内容切片（c.content != ''），避免空切片混入候选。
// Score 为 1 - cosine_distance（OpenAI embedding 已归一化，相似度 ∈ [0,1]）。
func (r *ChunkRepo) SearchByVector(
	ctx context.Context, embedding []float32, topK int, deptIDs []int64, similarityThreshold float64,
) ([]ChunkSearchHit, error) {
	if topK <= 0 {
		return nil, nil
	}
	embeddingVector := pgvector.NewVector(embedding)
	args := []any{embeddingVector, topK}
	visSQL, nextArg := deptVisibilitySQL(deptIDs, len(args)+1, &args)
	args = append(args, constants.ArticleStatusPublished)

	// similarityThreshold > 0 时在 SQL 层加阈值过滤，减少低质候选回传
	thresholdSQL := ""
	if similarityThreshold > 0 {
		args = append(args, similarityThreshold)
		thresholdSQL = fmt.Sprintf(" AND 1 - (c.embedding <=> $1) >= $%d", len(args))
	}

	sql := fmt.Sprintf(`SELECT %s,
		1 - (c.embedding <=> $1) AS score
		FROM article_chunks c
		JOIN articles a ON a.id = c.article_id
		WHERE c.is_active = true
		  AND c.content != ''
		  AND a.is_deleted = false
		  AND a.status = $%d
		  AND %s%s
		ORDER BY c.embedding <=> $1
		LIMIT $2`, chunkSearchColumns, nextArg, visSQL, thresholdSQL)

	rows, err := postgres.Q(ctx, r.pool).Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("search chunks by vector: %w", err)
	}
	defer rows.Close()
	return scanChunkSearchHits(rows)
}

// SearchByFullText BM25 全文检索 topK 候选（REQ-WIKI-013）。
// query 为 bigram_tsquery 输入（中文 bigram 分词 + OR 语义）；deptIDs 为 nil 时不限制科室可见性。
// Score 为 ts_rank（PostgreSQL 全文检索相关性，量纲与向量相似度不同，由调用方在 RRF 融合时归一化）。
func (r *ChunkRepo) SearchByFullText(
	ctx context.Context, query string, topK int, deptIDs []int64,
) ([]ChunkSearchHit, error) {
	if topK <= 0 || query == "" {
		return nil, nil
	}
	args := []any{query, topK}
	visSQL, nextArg := deptVisibilitySQL(deptIDs, len(args)+1, &args)
	args = append(args, constants.ArticleStatusPublished)

	sql := fmt.Sprintf(`SELECT %s,
		ts_rank(c.tsv, bigram_tsquery($1)) AS score
		FROM article_chunks c
		JOIN articles a ON a.id = c.article_id
		WHERE c.is_active = true
		  AND a.is_deleted = false
		  AND a.status = $%d
		  AND %s
		  AND c.tsv @@ bigram_tsquery($1)
		ORDER BY ts_rank(c.tsv, bigram_tsquery($1)) DESC
		LIMIT $2`, chunkSearchColumns, nextArg, visSQL)

	rows, err := postgres.Q(ctx, r.pool).Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("search chunks by fulltext: %w", err)
	}
	defer rows.Close()
	return scanChunkSearchHits(rows)
}

// scanChunkSearchHits 扫描检索结果行。score 列附加在末尾。
func scanChunkSearchHits(rows pgx.Rows) ([]ChunkSearchHit, error) {
	out := make([]ChunkSearchHit, 0)
	for rows.Next() {
		var hit ChunkSearchHit
		if err := rows.Scan(
			&hit.ID, &hit.ArticleID, &hit.ChunkIndex, &hit.Content, &hit.ContentHash,
			&hit.Embedding, &hit.IsActive, &hit.Version, &hit.CreatedAt,
			&hit.ArticleTitle,
			&hit.Score,
		); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				break
			}
			return nil, fmt.Errorf("scan chunk search hit: %w", err)
		}
		out = append(out, hit)
	}
	return out, rows.Err()
}
