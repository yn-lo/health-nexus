package service

import (
	"context"
	"fmt"

	"health-nexus/internal/domain/base/entity"
)

// NotificationRepo 是通知业务需要的持久化能力（消费者定义，ISP）。
type NotificationRepo interface {
	Create(ctx context.Context, n *entity.Notification) error
	ListForRole(ctx context.Context, role string, deptID *int64, limit int) ([]*entity.Notification, error)
	MarkRead(ctx context.Context, id int64) error
	MarkAllRead(ctx context.Context, role string, deptID *int64) error
	UnreadCount(ctx context.Context, role string, deptID *int64) (int, error)
}

// NotificationService 站内通知业务服务：按角色+科室读取、标记已读、未读计数。
type NotificationService struct {
	repo NotificationRepo
}

// NewNotificationService 构造通知服务。
func NewNotificationService(repo NotificationRepo) *NotificationService {
	return &NotificationService{repo: repo}
}

// List 返回当前角色+科室可见的通知列表（未读优先），limit 限制返回条数。
func (s *NotificationService) List(
	ctx context.Context, role string, deptID *int64, limit int,
) ([]*entity.Notification, error) {
	items, err := s.repo.ListForRole(ctx, role, deptID, limit)
	if err != nil {
		return nil, fmt.Errorf("list notifications: %w", err)
	}
	return items, nil
}

// MarkRead 将单条通知标记为已读。
func (s *NotificationService) MarkRead(ctx context.Context, id int64) error {
	if err := s.repo.MarkRead(ctx, id); err != nil {
		return fmt.Errorf("mark notification read: %w", err)
	}
	return nil
}

// MarkAllRead 将当前角色+科室可见的全部未读通知标记为已读。
func (s *NotificationService) MarkAllRead(ctx context.Context, role string, deptID *int64) error {
	if err := s.repo.MarkAllRead(ctx, role, deptID); err != nil {
		return fmt.Errorf("mark all notifications read: %w", err)
	}
	return nil
}

// UnreadCount 返回当前角色+科室可见的未读通知数量。
func (s *NotificationService) UnreadCount(ctx context.Context, role string, deptID *int64) (int, error) {
	count, err := s.repo.UnreadCount(ctx, role, deptID)
	if err != nil {
		return 0, fmt.Errorf("count unread notifications: %w", err)
	}
	return count, nil
}
