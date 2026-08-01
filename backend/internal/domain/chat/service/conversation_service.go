package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"health-nexus/internal/domain/chat/entity"
	apperrors "health-nexus/internal/shared/errors"
)

// ConversationRepoPort 会话仓储能力（消费者定义，ISP）。*repository.ConversationRepo 实现此接口。
type ConversationRepoPort interface {
	ListByPatient(
		ctx context.Context, patientID int64, includeArchived bool, limit, offset int,
	) ([]*entity.Conversation, int64, error)
	GetByIDForPatient(
		ctx context.Context, id uuid.UUID, patientID int64,
	) (*entity.Conversation, error)
	Patch(
		ctx context.Context, id uuid.UUID, patientID int64,
		title *string, archived *bool,
	) (*entity.Conversation, error)
	Delete(ctx context.Context, id uuid.UUID, patientID int64) (int64, error)
}

// MessageRepoPort 消息仓储能力（消费者定义，ISP）。*repository.MessageRepo 实现此接口。
type MessageRepoPort interface {
	ListByConversation(ctx context.Context, convID uuid.UUID, before *uuid.UUID, limit int) ([]*entity.Message, error)
	UpdateFeedback(ctx context.Context, messageID uuid.UUID, patientID int64, feedback string) (int64, error)
}

// ConversationService 会话管理：列表 / 详情 / 修改 / 删除 / 消息回看。
// 全部为单语句操作，无需事务；删除会话的级联由 DB ON DELETE CASCADE 处理（消息）。
// 含危机事件的会话不可删除（ON DELETE RESTRICT），保护安全审计轨迹。
type ConversationService struct {
	conv ConversationRepoPort
	msg  MessageRepoPort
}

// ConversationResponse 会话响应 DTO（契约 §3.2~3.5）。
type ConversationResponse struct {
	ID            string  `json:"id"`
	Title         string  `json:"title"`
	LockedDeptID  *int64  `json:"locked_dept_id"`
	Archived      bool    `json:"archived"`
	LastMessageAt *string `json:"last_message_at"` // 尚无消息时为 null，避免零值时间 "0001-01-01T00:00:00Z"
	CreatedAt     string  `json:"created_at"`
}

// Reference 引用切片 DTO（契约 §3.6 references 字段）。
type Reference struct {
	ChunkID      string  `json:"chunk_id"`
	ArticleID    string  `json:"article_id"`
	ArticleTitle string  `json:"article_title"`
	Content      string  `json:"content"`
	Score        float64 `json:"score"`
}

// MessageResponse 消息响应 DTO（契约 §3.6）。
type MessageResponse struct {
	ID             string      `json:"id"`
	ConversationID string      `json:"conversation_id"`
	Role           string      `json:"role"`
	Content        string      `json:"content"`
	ResultCode     *string     `json:"result_code"`
	References     []Reference `json:"references"`
	Feedback       *string     `json:"feedback"`
	CreatedAt      string      `json:"created_at"`
}

func toConversationResponse(c *entity.Conversation) ConversationResponse {
	var lastMsgAt *string
	if !c.LastMessageAt.IsZero() {
		v := c.LastMessageAt.Format(timeRFC3339)
		lastMsgAt = &v
	}
	return ConversationResponse{
		ID:            c.ID.String(),
		Title:         c.Title,
		LockedDeptID:  c.LockedDeptID,
		Archived:      c.IsArchived,
		LastMessageAt: lastMsgAt,
		CreatedAt:     c.CreatedAt.Format(timeRFC3339),
	}
}

func toMessageResponse(m *entity.Message) MessageResponse {
	refs := make([]Reference, 0, len(m.ReferencedChunks))
	for _, r := range m.ReferencedChunks {
		refs = append(refs, Reference{
			ChunkID:      r.ChunkID,
			ArticleID:    r.ArticleID,
			ArticleTitle: r.ArticleTitle,
			Content:      r.Content,
			Score:        r.Score,
		})
	}
	var rc *string
	if m.ResultCode != "" {
		rc = &m.ResultCode
	}
	return MessageResponse{
		ID:             m.ID.String(),
		ConversationID: m.ConversationID.String(),
		Role:           m.Role,
		Content:        m.Content,
		ResultCode:     rc,
		References:     refs,
		Feedback:       m.Feedback,
		CreatedAt:      m.CreatedAt.Format(timeRFC3339),
	}
}

// NewConversationService 构造会话管理服务。
func NewConversationService(conv ConversationRepoPort, msg MessageRepoPort) *ConversationService {
	return &ConversationService{conv: conv, msg: msg}
}

// List 会话列表。includeArchived=true 时包含已归档会话。
func (s *ConversationService) List(
	ctx context.Context, patientID int64, includeArchived bool, limit, offset int,
) ([]*ConversationResponse, int64, error) {
	items, total, err := s.conv.ListByPatient(ctx, patientID, includeArchived, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*ConversationResponse, 0, len(items))
	for _, c := range items {
		resp := toConversationResponse(c)
		out = append(out, &resp)
	}
	return out, total, nil
}

// Get 取会话详情。不存在或不属于该患者返回 AppError(404)。
func (s *ConversationService) Get(ctx context.Context, id uuid.UUID, patientID int64) (*ConversationResponse, error) {
	conv, err := s.conv.GetByIDForPatient(ctx, id, patientID)
	if err != nil {
		return nil, fmt.Errorf("get conversation: %w", err)
	}
	if conv == nil {
		return nil, apperrors.NotFound("CHAT_CONVERSATION_NOT_FOUND", "会话不存在或不属于当前用户")
	}
	resp := toConversationResponse(conv)
	return &resp, nil
}

// PatchInput 修改会话请求。指针字段为 nil 表示不更新；全 nil 返回 422。
type PatchInput struct {
	Title    *string
	Archived *bool
}

// Patch 修改会话标题/归档状态。返回更新后的 DTO。
func (s *ConversationService) Patch(
	ctx context.Context, id uuid.UUID, patientID int64, in PatchInput,
) (*ConversationResponse, error) {
	if in.Title == nil && in.Archived == nil {
		return nil, apperrors.Validation("CHAT_PATCH_EMPTY", "至少需要修改一个字段")
	}
	conv, err := s.conv.Patch(ctx, id, patientID, in.Title, in.Archived)
	if err != nil {
		return nil, fmt.Errorf("patch conversation: %w", err)
	}
	if conv == nil {
		return nil, apperrors.NotFound("CHAT_CONVERSATION_NOT_FOUND", "会话不存在或不属于当前用户")
	}
	resp := toConversationResponse(conv)
	return &resp, nil
}

// Delete 删除会话。不存在或不属于该患者返回 AppError(404)。
func (s *ConversationService) Delete(ctx context.Context, id uuid.UUID, patientID int64) error {
	rows, err := s.conv.Delete(ctx, id, patientID)
	if err != nil {
		// FK RESTRICT：含危机事件的会话不可删除，保护安全审计轨迹
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return apperrors.Conflict("CHAT_CRISIS_EVENT_EXISTS", "该会话包含危机事件记录，不可删除")
		}
		return fmt.Errorf("delete conversation: %w", err)
	}
	if rows == 0 {
		return apperrors.NotFound("CHAT_CONVERSATION_NOT_FOUND", "会话不存在或不属于当前用户")
	}
	return nil
}

// ListMessages 取会话消息列表（游标分页）。
// before 为 nil 时从最新开始；limit 控制单页大小（契约默认 50，最大 200）。
// 会话不存在或不属于该患者返回 AppError(404)。
func (s *ConversationService) ListMessages(
	ctx context.Context, convID uuid.UUID, patientID int64, before *uuid.UUID, limit int,
) ([]*MessageResponse, error) {
	// 先校验会话存在 + 所属
	if _, err := s.Get(ctx, convID, patientID); err != nil {
		return nil, err
	}
	msgs, err := s.msg.ListByConversation(ctx, convID, before, limit)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	// repo 按 created_at DESC 返回（游标分页语义）；前端按时间升序渲染，此处反转。
	// 分页加载更旧消息时同样反转后拼接到当前页之前，整体仍为升序。
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	out := make([]*MessageResponse, 0, len(msgs))
	for _, m := range msgs {
		resp := toMessageResponse(m)
		out = append(out, &resp)
	}
	return out, nil
}

// Feedback 记录消息反馈（up/down）。
// 消息不存在或不属于该患者返回 AppError(404)。
func (s *ConversationService) Feedback(ctx context.Context, messageID uuid.UUID, patientID int64, feedback string) error {
	rows, err := s.msg.UpdateFeedback(ctx, messageID, patientID, feedback)
	if err != nil {
		return fmt.Errorf("update feedback: %w", err)
	}
	if rows == 0 {
		return apperrors.NotFound("CHAT_MESSAGE_NOT_FOUND", "消息不存在或不属于当前用户")
	}
	return nil
}
