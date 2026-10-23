package tasks

import (
	"context"
	"log/slog"
	"strconv"

	baseentity "health-nexus/internal/domain/base/entity"
	baserepo "health-nexus/internal/domain/base/repository"
	wikirepo "health-nexus/internal/domain/wiki/repository"
	"health-nexus/internal/shared/constants"
)

// HandleReviewNotify 落库一条面向科室管理员的 REVIEW_PENDING 站内通知。
// payload 为 articleID 的十进制字符串；body 取文章标题（best-effort，取不到则为空）。
func HandleReviewNotify(
	ctx context.Context, payload []byte,
	articles *wikirepo.ArticleRepo, notifications *baserepo.NotificationRepo,
) error {
	id, err := strconv.ParseInt(string(payload), 10, 64)
	if err != nil {
		slog.ErrorContext(ctx, "wiki: review notify task invalid payload",
			"payload", string(payload), "err", err)
		return err
	}
	body := ""
	if a, gerr := articles.GetByID(ctx, id); gerr == nil {
		body = a.Title
	}
	refID := strconv.FormatInt(id, 10)
	// ponytail: 仅面向 DEPT_ADMIN 单角色落库一条，未向 SUPER_ADMIN/多科室扇出，简化；
	// 升级路径：按需为多个 recipient_role/recipient_dept_id 批量插入。
	n := &baseentity.Notification{
		RecipientRole: constants.RoleDeptAdmin,
		Type:          "REVIEW_PENDING",
		Title:         "文章待审核",
		Body:          body,
		RefID:         &refID,
	}
	if err := notifications.Create(ctx, n); err != nil {
		slog.ErrorContext(ctx, "wiki: review notify insert failed", "article_id", id, "err", err)
		return err
	}
	slog.InfoContext(ctx, "wiki: review notify created", "article_id", id, "notification_id", n.ID)
	return nil
}
