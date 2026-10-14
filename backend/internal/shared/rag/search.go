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
	// ResolveForPatient 校验患者可访问 selectedDeptID 并返回其对应的 Department：
	// nil 或 0 表示"全部科室"（不限检索范围，返回零值 Department）；>0 为具体科室，
	// 须在患者可见列表（所属科室 ∪ 公共科室）内，否则返回 403。
	// 返回的 Department 仅用于校验/语境提示，检索范围由调用方按零值判定为全库。
	ResolveForPatient(ctx context.Context, patientID int64, selectedDeptID *int64) (Department, error)
	// ResolveForAnonymous 校验匿名用户可选 selectedDeptID：仅允许公共且启用的科室；
	// nil 或 0 表示"全部科室"（不限定）；具体 id 不在公共科室列表中返回 403。
	// 匿名用户不得访问非公共科室（与患者端"公共科室"可见性一致）。
	ResolveForAnonymous(ctx context.Context, selectedDeptID *int64) (Department, error)
	// ResolveByID 按科室 id 精确取名，用于会话已锁定、续聊未携带 selected_dept_id 时回填科室名
	// （供统一理解与审查作为语境提示）。仅取名，不做可访问性校验；
	// 未找到返回零值 Department（ID=0），不返回错误。
	ResolveByID(ctx context.Context, deptID int64) (Department, error)
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
	// ContentRisk 文章来源的内容风险等级（high=用药/检查准备/高风险护理）。
	// 命中高风险资料时本轮答案必须完整生成后经语义审核再展示（REQ-CHAT-012~014）。
	ContentRisk string `json:"content_risk,omitempty"`
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
