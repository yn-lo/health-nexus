package service

import (
	"context"
	"errors"
	"fmt"

	"health-nexus/internal/domain/chat/entity"
	"health-nexus/internal/domain/chat/repository"
	"health-nexus/internal/shared/constants"
	apperrors "health-nexus/internal/shared/errors"
)

// timeRFC3339 时间格式常量（与契约 §0.1 对齐）。
const timeRFC3339 = "2006-01-02T15:04:05Z07:00"

// CrisisListItem 危机事件列表 DTO（handler 层消费，断绝 handler→repository 依赖）。
type CrisisListItem struct {
	ID               int64
	PatientID        int64
	PatientName      string
	ConversationID   string
	TriggeredContent string
	MatchedKeywords  []string
	Level            string
	IsHandled        bool
	HandlerID        *int64
	HandledAt        *string
	HandleNote       string
	CreatedAt        string
}

// CrisisRepoPort 危机事件仓储能力（消费者定义，ISP）。*repository.CrisisRepo 实现此接口。
type CrisisRepoPort interface {
	GetByID(ctx context.Context, id int64) (*entity.CrisisEvent, error)
	List(
		ctx context.Context, filter repository.CrisisFilter, limit, offset int,
	) ([]*repository.CrisisListRow, int64, error)
	MarkHandled(ctx context.Context, id, handlerID int64, note string) (bool, error)
}

// CrisisService 危机事件管理：医护端列表 + 处理。
type CrisisService struct {
	crisis CrisisRepoPort
}

// NewCrisisService 构造危机事件服务。
func NewCrisisService(crisis CrisisRepoPort) *CrisisService {
	return &CrisisService{crisis: crisis}
}

// CrisisActor 危机事件操作者上下文。
type CrisisActor struct {
	UserID int64
	Role   string
	DeptID int64
}

// List 危机事件列表。level/handled 为空/nil 表示不过滤。
// 数据隔离：非超管仅查看本科室（按会话锁定科室过滤）的危机事件。
func (s *CrisisService) List(
	ctx context.Context, level string, handled *bool, actor CrisisActor, limit, offset int,
) ([]*CrisisListItem, int64, error) {
	filter := repository.CrisisFilter{Level: level, Handled: handled}
	if actor.Role != constants.RoleSuperAdmin {
		filter.DeptID = actor.DeptID
	}
	rows, total, err := s.crisis.List(ctx, filter, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*CrisisListItem, 0, len(rows))
	for _, r := range rows {
		item := &CrisisListItem{
			ID:               r.CrisisEvent.ID,
			PatientID:        r.CrisisEvent.PatientID,
			PatientName:      r.PatientName,
			ConversationID:   r.CrisisEvent.ConversationID.String(),
			TriggeredContent: r.CrisisEvent.TriggeredContent,
			MatchedKeywords:  r.CrisisEvent.MatchedKeywords,
			Level:            r.CrisisEvent.Level,
			IsHandled:        r.CrisisEvent.IsHandled,
			HandleNote:       r.CrisisEvent.HandleNote,
			CreatedAt:        r.CrisisEvent.CreatedAt.Format(timeRFC3339),
		}
		if r.CrisisEvent.HandlerID != nil {
			item.HandlerID = r.CrisisEvent.HandlerID
		}
		if r.CrisisEvent.HandledAt != nil {
			s := r.CrisisEvent.HandledAt.Format(timeRFC3339)
			item.HandledAt = &s
		}
		if item.MatchedKeywords == nil {
			item.MatchedKeywords = []string{}
		}
		out = append(out, item)
	}
	return out, total, nil
}

// Handle 标记危机事件为已处理。
// 不存在返回 AppError(404)；已处理返回 AppError(409)。
func (s *CrisisService) Handle(ctx context.Context, eventID, handlerID int64, note string) error {
	// 先校验存在（404 优先于 409）。D-MED-05: 用 errors.Is 判断哨兵错误，对齐 wiki repo 模式。
	existing, err := s.crisis.GetByID(ctx, eventID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return apperrors.NotFound("CHAT_CRISIS_NOT_FOUND", "危机事件不存在")
		}
		return fmt.Errorf("get crisis event: %w", err)
	}
	if existing.IsHandled {
		return apperrors.Conflict("CHAT_CRISIS_ALREADY_HANDLED", "危机事件已处理")
	}
	ok, err := s.crisis.MarkHandled(ctx, eventID, handlerID, note)
	if err != nil {
		return fmt.Errorf("mark crisis handled: %w", err)
	}
	if !ok {
		// 并发场景：已被其他请求处理
		return apperrors.Conflict("CHAT_CRISIS_ALREADY_HANDLED", "危机事件已处理")
	}
	return nil
}
