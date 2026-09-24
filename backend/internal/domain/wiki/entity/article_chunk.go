package entity

import (
	"time"

	"github.com/pgvector/pgvector-go"
)

// ArticleChunk 文章切片，对应 article_chunks 表。
// 切片与向量化由 asynq Worker 异步处理（REQ-WIKI-012/013），本任务只定义实体与仓储骨架。
// Embedding 为 pgvector 向量；检索仅走向量路（pure vector，REQ-WIKI-013）。
type ArticleChunk struct {
	ID          int64
	ArticleID   int64
	ChunkIndex  int
	Content     string
	ContentHash string
	Embedding   pgvector.Vector
	// EmbeddingModel 生成本行向量的模型标识。模型切换后旧切片据此被识别并重建，
	// 检索侧按模型过滤，避免不同模型的向量混用（P0：模型版本一致性）。
	// 空串表示未知（历史数据/实现未暴露模型名），检索侧不过滤。
	EmbeddingModel string
	IsActive       bool
	Version        int
	CreatedAt      time.Time
}
