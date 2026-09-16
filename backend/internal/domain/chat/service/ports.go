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
// turnID 标识"本轮生成"：同一轮的 user / assistant 消息共享，供幂等重放按轮定位结果。
type MessagePort interface {
	SaveUserMessage(ctx context.Context, convID, turnID uuid.UUID, content string) (*entity.Message, error)
	SaveAssistant(
		ctx context.Context, convID uuid.UUID, content, resultCode string,
		refs []entity.Reference, turnID uuid.UUID,
	) (*entity.Message, error)
	SaveAssistantPlaceholder(ctx context.Context, convID, turnID uuid.UUID) (*entity.Message, error)
	// GetRecentHistory 返回最近 turns 轮对话（DESC）。excludeID 非 nil 时排除该消息——
	// 当前用户消息已先于历史加载持久化，不排除会导致 LLM 上下文出现重复提问。
	GetRecentHistory(ctx context.Context, convID uuid.UUID, turns int, excludeID *uuid.UUID) ([]*entity.Message, error)
	FinalizeAssistant(ctx context.Context, id uuid.UUID, content, resultCode string, refs []entity.Reference) error
	// ListByTurn 按轮次列出本轮全部消息（幂等重放定位本轮结果）；无记录返回空切片。
	ListByTurn(ctx context.Context, convID, turnID uuid.UUID) ([]*entity.Message, error)
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

// TurnRegistry 请求幂等登记能力（消费者定义，ISP）。*redis.TurnRegistry 实现此接口。
// key 由调用方按身份作用域拼接，实现不感知身份。
type TurnRegistry interface {
	// Put 写入/覆盖登记值（ttl 后自动过期）。
	Put(ctx context.Context, key, value string, ttl time.Duration) error
	// Lookup 读取登记值；不存在返回 ("", false, nil)。
	Lookup(ctx context.Context, key string) (string, bool, error)
	// Delete 删除登记（未真正开始的请求不应阻塞后续重试）。
	Delete(ctx context.Context, key string) error
}
