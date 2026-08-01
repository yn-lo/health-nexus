package entity

import (
	"time"

	"github.com/google/uuid"
)

// Message 消息实体，对应 messages 表。
// Role 取值见 constants.MessageRoleUser/Assistant。
// ResultCode 取值见 constants.Result*（user 消息留空）。
type Message struct {
	ID               uuid.UUID
	ConversationID   uuid.UUID
	Role             string
	Content          string
	ResultCode       string
	ReferencedChunks []Reference
	Feedback         *string `json:"feedback"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Reference 引用切片，存储在 messages.referenced_chunks JSONB。
// 对应契约 GET /api/chat/conversations/{id}/messages 响应中的 references 字段。
type Reference struct {
	ChunkID      string  `json:"chunk_id"`
	ArticleID    string  `json:"article_id"`
	ArticleTitle string  `json:"article_title"`
	Content      string  `json:"content"`
	Score        float64 `json:"score"`
}
