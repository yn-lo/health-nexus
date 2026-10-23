package tasks

import (
	"context"
	"log/slog"
	"strconv"

	baseentity "health-nexus/internal/domain/base/entity"
	baserepo "health-nexus/internal/domain/base/repository"
	chatrepo "health-nexus/internal/domain/chat/repository"
	"health-nexus/internal/shared/constants"
)

// crisisEscalationBatchSize 单次危机超时升级扫描最多处理的记录数。
const crisisEscalationBatchSize = 50

// CrisisNotifier 危机事件的医护通知与超时升级用例。
type CrisisNotifier struct {
	crisis        *chatrepo.CrisisRepo
	notifications *baserepo.NotificationRepo
}

// NewCrisisNotifier 构造危机通知用例。
func NewCrisisNotifier(crisis *chatrepo.CrisisRepo, notifications *baserepo.NotificationRepo) *CrisisNotifier {
	return &CrisisNotifier{crisis: crisis, notifications: notifications}
}

// Notify 危机事件主动通知：查询事件获取科室，落库站内通知。
// 未锁定科室的事件投给超管兜底；通知按 (type, ref_id, 接收方) 幂等——
// 快速路径与 outbox relay 补投可能重复触发同一任务。
func (n *CrisisNotifier) Notify(ctx context.Context, payload []byte) error {
	eventID, err := strconv.ParseInt(string(payload), 10, 64)
	if err != nil {
		slog.ErrorContext(ctx, "chat: crisis notify task invalid payload",
			"payload", string(payload), "err", err)
		return err
	}
	ce, err := n.crisis.GetByID(ctx, eventID)
	if err != nil {
		slog.ErrorContext(ctx, "chat: crisis notify get event failed", "event_id", eventID, "err", err)
		return err
	}
	refID := strconv.FormatInt(eventID, 10)
	notification := &baseentity.Notification{
		Type:  "CRISIS_ALERT",
		Title: "危机事件提醒",
		Body:  "患者表达了可能的自伤倾向，请及时处理",
		RefID: &refID,
	}
	if ce.LockedDeptID > 0 {
		deptID := ce.LockedDeptID
		notification.RecipientRole = constants.RoleDeptAdmin
		notification.RecipientDeptID = &deptID
	} else {
		notification.RecipientRole = constants.RoleSuperAdmin
		notification.Body = "患者表达了可能的自伤倾向（会话未限定科室），请及时处理"
	}
	exists, err := n.notifications.ExistsForRef(
		ctx, notification.RecipientRole, notification.RecipientDeptID, notification.Type, refID)
	if err != nil {
		return err
	}
	if exists {
		slog.InfoContext(ctx, "chat: crisis notification already delivered, skip",
			"event_id", eventID, "role", notification.RecipientRole)
		return nil
	}
	if err := n.notifications.Create(ctx, notification); err != nil {
		slog.ErrorContext(ctx, "chat: crisis notify insert failed", "event_id", eventID, "err", err)
		return err
	}
	slog.InfoContext(ctx, "chat: crisis notification created",
		"event_id", eventID, "notification_id", notification.ID,
		"role", notification.RecipientRole, "dept_id", notification.RecipientDeptID)
	return nil
}

// Escalate 扫描超时未接单的危机事件，升级通知超管并标记已升级。
// 升级通知按 (type, ref_id) 幂等；MarkEscalated 返回 false 表示已被其他实例升级，不重复计数。
func (n *CrisisNotifier) Escalate(ctx context.Context) error {
	overdue, err := n.crisis.ListOverdueUnhandled(ctx, crisisEscalationBatchSize)
	if err != nil {
		return err
	}
	escalated := 0
	for _, o := range overdue {
		refID := strconv.FormatInt(o.ID, 10)
		notification := &baseentity.Notification{
			RecipientRole: constants.RoleSuperAdmin,
			Type:          "CRISIS_ESCALATED",
			Title:         "危机事件超时未处理",
			Body:          "有危机事件已超过接单时限仍未被处理，请立即跟进",
			RefID:         &refID,
		}
		exists, err := n.notifications.ExistsForRef(ctx, notification.RecipientRole, nil, notification.Type, refID)
		if err != nil {
			return err
		}
		if !exists {
			if cerr := n.notifications.Create(ctx, notification); cerr != nil {
				slog.ErrorContext(ctx, "chat: crisis escalation notify failed", "event_id", o.ID, "err", cerr)
				continue
			}
		}
		ok, merr := n.crisis.MarkEscalated(ctx, o.ID)
		if merr != nil {
			slog.ErrorContext(ctx, "chat: mark crisis escalated failed", "event_id", o.ID, "err", merr)
			continue
		}
		if ok {
			escalated++
			slog.WarnContext(ctx, "chat: crisis event escalated",
				"event_id", o.ID, "level", o.Level, "due_at", o.AcknowledgeDue)
		}
	}
	if escalated > 0 {
		slog.InfoContext(ctx, "chat: crisis escalation scan done", "escalated", escalated)
	}
	return nil
}
