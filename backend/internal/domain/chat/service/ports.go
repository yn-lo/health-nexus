package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"health-nexus/internal/domain/chat/entity"
)

// ConversationPort 会话仓储能力（消费者定义，ISP）。
type ConversationPort interface {
	Create(ctx context.Context, patientID int64, lockedDeptID *int64) (*entity.Conversation, error)
	GetByIDForPatient(ctx context.Context, id uuid.UUID, patientID int64) (*entity.Conversation, error)
	LockDept(ctx context.Context, id uuid.UUID, deptID int64) error
	UpdateTitleIfEmpty(ctx context.Context, id uuid.UUID, title string) error
	TouchLastMessageAt(ctx context.Context, id uuid.UUID) error
}

// MessagePort 消息仓储能力（消费者定义，ISP）。
type MessagePort interface {
	SaveUserMessage(ctx context.Context, convID uuid.UUID, content string) (*entity.Message, error)
	SaveAssistant(
		ctx context.Context, convID uuid.UUID, content, resultCode string, refs []entity.Reference,
	) (*entity.Message, error)
	SaveAssistantPlaceholder(ctx context.Context, convID uuid.UUID) (*entity.Message, error)
	// GetRecentHistory 返回最近 turns 轮对话（DESC）。excludeID 非 nil 时排除该消息——
	// 当前用户消息已先于历史加载持久化，不排除会导致 LLM 上下文出现重复提问。
	GetRecentHistory(ctx context.Context, convID uuid.UUID, turns int, excludeID *uuid.UUID) ([]*entity.Message, error)
	FinalizeAssistant(ctx context.Context, id uuid.UUID, content, resultCode string, refs []entity.Reference) error
}

// CrisisPort 危机事件仓储能力（消费者定义，ISP）。
type CrisisPort interface {
	Create(ctx context.Context, e *entity.CrisisEvent) (int64, error)
}

// TxRunner 事务执行能力（消费者定义，ISP）。*postgres.TxManager 实现此接口。
type TxRunner interface {
	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// LockProvider 分布式锁能力（消费者定义，ISP）。*redis.Locker 实现此接口。
type LockProvider interface {
	Lock(ctx context.Context, key string, ttl time.Duration) (func() error, error)
}
