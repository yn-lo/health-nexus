package entity

import "time"

// RAGConfig 对应 rag_configs 表（单例，id 固定为 1）。
type RAGConfig struct {
	ID                  int64
	ChunkSize           int
	ChunkOverlap        int
	MaxChunks           int
	TopK                int
	SimilarityThreshold float64
	RerankEnabled       bool
	RerankThreshold     float64
	OODThreshold        float64
	UpdatedAt           time.Time
}

// RAG 参数范围（REQ-CONFIG-007）。chunk_size/top_k 范围与 SQL CHECK 一致或更严。
const (
	ChunkSizeMin           = 200
	ChunkSizeMax           = 2000
	ChunkOverlapMin        = 0
	ChunkOverlapMax        = 500
	MaxChunksMin           = 1
	MaxChunksMax           = 50
	TopKMin                = 1
	TopKMax                = 50
	SimilarityThresholdMin = 0.0
	SimilarityThresholdMax = 1.0
	RerankThresholdMin     = 0.0
	RerankThresholdMax     = 1.0
	OODThresholdMin        = 0.0
	OODThresholdMax        = 0.5
)

// RAG 配置默认值（与 SQL DEFAULT 对齐）。
const (
	DefaultChunkSize           = 500
	DefaultChunkOverlap        = 50
	DefaultMaxChunks           = 10
	DefaultTopK                = 5
	DefaultSimilarityThreshold = 0.75
	DefaultRerankThreshold     = 0.5
	DefaultOODThreshold        = 0.3
)

// DefaultRAGConfig 是 RAG 配置缺失时的默认值（与 SQL DEFAULT 对齐）。
var DefaultRAGConfig = RAGConfig{
	ID:                  1,
	ChunkSize:           DefaultChunkSize,
	ChunkOverlap:        DefaultChunkOverlap,
	MaxChunks:           DefaultMaxChunks,
	TopK:                DefaultTopK,
	SimilarityThreshold: DefaultSimilarityThreshold,
	RerankEnabled:       false,
	RerankThreshold:     DefaultRerankThreshold,
	OODThreshold:        DefaultOODThreshold,
}
