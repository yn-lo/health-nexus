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
// 仅写入向量（embedding）；BM25(tv) 检索链路已移除，纯向量检索（REQ-WIKI-013）。
// embedding_model 记录向量所属模型，供检索侧过滤与模型切换后的重建识别。
func (r *ChunkRepo) Create(ctx context.Context, c *entity.ArticleChunk) error {
	const sql = `INSERT INTO article_chunks
		(article_id, chunk_index, content, content_hash, embedding, embedding_model, is_active, version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at`
	return postgres.Q(ctx, r.pool).QueryRow(ctx, sql,
		c.ArticleID, c.ChunkIndex, c.Content, c.ContentHash, c.Embedding, c.EmbeddingModel, c.IsActive, c.Version,
	).Scan(&c.ID, &c.CreatedAt)
}

// ListActiveByArticle 列出文章当前生效的切片（is_active=true），按 chunk_index 排序。
// 供检索服务（SearchService）与 Worker 重新切片时使用。
func (r *ChunkRepo) ListActiveByArticle(ctx context.Context, articleID int64) ([]*entity.ArticleChunk, error) {
	const sql = `SELECT id, article_id, chunk_index, content, content_hash, embedding, embedding_model,
		is_active, version, created_at
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
			&c.Embedding, &c.EmbeddingModel, &c.IsActive, &c.Version, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan article_chunk: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListArticleIDsWithStaleEmbedding 列出存在"向量模型与当前模型不一致"有效切片的文章 ID。
// 供 outbox relay 在 Embedding 模型切换后自动触发全量重建；embedding_model 为空串的历史切片
// 视为未知（迁移前数据），同样需要重建，因此一并返回。
func (r *ChunkRepo) ListArticleIDsWithStaleEmbedding(
	ctx context.Context, model string, limit int,
) ([]int64, error) {
	if model == "" || limit <= 0 {
		return nil, nil
	}
	const sql = `SELECT DISTINCT c.article_id
		FROM article_chunks c
		JOIN articles a ON a.id = c.article_id
		WHERE c.is_active = true
		  AND (c.embedding_model = '' OR c.embedding_model <> $1)
		  AND a.status = $2 AND a.is_deleted = false
		LIMIT $3`
	rows, err := postgres.Q(ctx, r.pool).Query(ctx, sql, model, constants.ArticleStatusPublished, limit)
	if err != nil {
		return nil, fmt.Errorf("list stale embedding articles: %w", err)
	}
	defer rows.Close()
	out := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan stale embedding article: %w", err)
		}
		out = append(out, id)
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
// ArticleTitle 由 JOIN articles 填充；Score 为向量相似度（1-cosine_distance）。
type ChunkSearchHit struct {
	entity.ArticleChunk
	ArticleTitle string
	Score        float64
	// ContentRisk 来源文章的内容风险等级（high=用药/检查准备/高风险护理），
	// 供 chat 域判定本轮答案是否必须经生成后语义审核（P1）。
	ContentRisk string
}

// chunkSearchColumns 检索 SQL 共用的 SELECT 列（含 article_title 与 score）。
// content 取**命中切片本身** c.content（与该行向量一一对应）——
// 不能返回 a.published_content（全文）：那样返回的内容与命中的向量不对应，
// 且全文重复占用上下文，长文章尾部的有效证据会被挤掉（P1）。
// 切片内容来源于审核通过时写入的版本（切片仅在 Approve/重建时写入，见 vectorize handler），
// 故命中的切片即"已审核版本"的切片；文章当前是否 pending 由可见性条件约束。
const chunkSearchColumns = `c.id, c.article_id, c.chunk_index, c.content, c.content_hash,
	c.embedding, c.embedding_model, c.is_active, c.version, c.created_at,
	COALESCE(a.title, ''), a.content_risk`

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
// embeddingModel 非空时仅返回同一模型生成的切片（另容忍空串：迁移前的老切片未记录模型，
// 由 outbox relay 触发重建后收敛为严格同模型），避免不同模型的向量在同一空间比较得出错误相似度。
// 始终排除空内容切片（c.content != ”），避免空切片混入候选。
// Score 为 1 - cosine_distance（OpenAI embedding 已归一化，相似度 ∈ [0,1]）。
func (r *ChunkRepo) SearchByVector(
	ctx context.Context, embedding []float32, topK int, deptIDs []int64,
	similarityThreshold float64, embeddingModel string,
) ([]ChunkSearchHit, error) {
	if topK <= 0 {
		return nil, nil
	}
	embeddingVector := pgvector.NewVector(embedding)
	args := []any{embeddingVector, topK}
	visSQL, _ := deptVisibilitySQL(deptIDs, len(args)+1, &args)

	// similarityThreshold > 0 时在 SQL 层加阈值过滤，减少低质候选回传
	thresholdSQL := ""
	if similarityThreshold > 0 {
		args = append(args, similarityThreshold)
		thresholdSQL = fmt.Sprintf(" AND 1 - (c.embedding <=> $1) >= $%d", len(args))
	}

	// 向量模型一致性过滤：不同 embed 模型产出的向量不可比。
	modelSQL := ""
	if embeddingModel != "" {
		args = append(args, embeddingModel)
		modelSQL = fmt.Sprintf(" AND (c.embedding_model = $%d OR c.embedding_model = '')", len(args))
	}

	// 已审核发布版可见性（P1）：
	//   - published：正常命中；
	//   - pending 且曾发布（published_at 非空）：这是"已发布文章被修改、正在重新审核"的状态，
	//     其有效切片仍是上一次审核通过的版本，应继续服务（新内容审核通过前不生效）；
	//   - 其它状态（draft/archived/deleted）的有效切片不存在或已失效，天然排除。
	// 绑定审核版本：切片 version 必须等于文章的 published_version（快照版本），
	// 避免并发重建写入的超前版本切片（新内容尚未审核）被检索命中。
	// 有效期与逾期策略：显式过期（valid_until）的资料一律退出检索；
	// 高风险（用药/检查准备等）且已逾期的资料同样退出（普通宣教逾期仅标记待复审，仍可命中）。
	sql := fmt.Sprintf(`SELECT %s,
		1 - (c.embedding <=> $1) AS score
		FROM article_chunks c
		JOIN articles a ON a.id = c.article_id
		WHERE c.is_active = true
		  AND c.content != ''
		  AND a.is_deleted = false
		  AND a.published_version > 0
		  AND c.version = a.published_version
		  AND (a.status = '%s' OR (a.status = '%s' AND a.published_at IS NOT NULL))
		  AND (a.valid_until IS NULL OR a.valid_until > now())
		  AND NOT (a.content_risk = '%s' AND a.review_overdue = true)
		  AND %s%s%s
		ORDER BY c.embedding <=> $1
		LIMIT $2`, chunkSearchColumns,
		constants.ArticleStatusPublished, constants.ArticleStatusPending, entity.ContentRiskHigh,
		visSQL, thresholdSQL, modelSQL)

	rows, err := postgres.Q(ctx, r.pool).Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("search chunks by vector: %w", err)
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
			&hit.Embedding, &hit.EmbeddingModel, &hit.IsActive, &hit.Version, &hit.CreatedAt,
			&hit.ArticleTitle, &hit.ContentRisk,
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
