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
	IsActive    bool
	Version     int
	CreatedAt   time.Time
}
