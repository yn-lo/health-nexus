// Package rag 定义 RAG 流程中的跨域接口与安全审查逻辑。
// 作为 shared 层的跨域契约包，由消费者（chat 域）定义接口，
// wiki / base / config 域实现并注入（ISP）。
// 安全审查逻辑（InputSafetyFilter / OutputSafetyFilter）也在此包，
// 因为它仅依赖 shared/constants，不耦合任何业务域。
package rag

import "context"

// Department 跨域科室 DTO。仅包含 chat 域需要的字段。
type Department struct {
	ID   int64
	Name string
}

// DepartmentResolver 跨域：base 域实现。
// 解析患者可访问的科室，用于会话锁定（REQ-CHAT-019）。
type DepartmentResolver interface {
	// ResolveForPatient 校验患者可访问 selectedDeptID（nil 表示用患者主科室），
	// 返回锁定的 Department。
	ResolveForPatient(ctx context.Context, patientID int64, selectedDeptID *int64) (Department, error)
}

// Chunk 检索命中的知识切片。对应 wiki 域 article_chunks。
// 字段对齐 GET /api/chat/conversations/{id}/messages 响应中的 references。
type Chunk struct {
	ChunkID      string  `json:"chunk_id"`
	ArticleID    string  `json:"article_id"`
	ArticleTitle string  `json:"article_title"`
	Content      string  `json:"content"`
	Score        float64 `json:"score"`
	VecScore     float64 `json:"vec_score,omitempty"` // 向量相似度分数（1 - cosine_distance），用于 OOD 检测
}

// SearchQuery 检索请求。
type SearchQuery struct {
	Query  string // 改写后的 Standalone Question
	DeptID *int64 // 限定科室范围；nil 表示不限定
	TopK   int    // 检索数量
}

// KnowledgeSearcher 跨域：wiki 域实现。
// 混合检索（向量 + BM25）+ 可选 Rerank（REQ-WIKI-014）。
type KnowledgeSearcher interface {
	SearchSimilarChunks(ctx context.Context, q SearchQuery) ([]Chunk, error)
}
