"""
Wiki 服务层 - 文章业务逻辑的唯一承载层

基于 BR-ARCH-01/BR-ARCH-02: 视图层禁止跨服务层直接操作 ORM
"""
import logging
from typing import Optional

from apps.wiki.models import Article, ArticleAuditLog

logger = logging.getLogger(__name__)


def _safe_str(val) -> str:
    """安全地将值转换为字符串，处理 Quill 字段的多种格式"""
    if val is None:
        return ''
    try:
        result = str(val)
        if isinstance(result, dict):
            return result.get('html', '')[:50] if isinstance(result, dict) else str(result)
        return result[:50] if len(result) > 50 else result
    except (TypeError, AttributeError):
        return ''


def _get_change_summary(old: Article, new: Article, fields: list[str]) -> str:
    """计算变更内容摘要。"""
    changes = []
    for field in fields:
        old_val = getattr(old, field, None)
        new_val = getattr(new, field, None)
        if old_val != new_val:
            old_str = _safe_str(old_val)
            new_str = _safe_str(new_val)
            changes.append(f"{field}: {old_str!r} → {new_str!r}")
    return "; ".join(changes) if changes else ""


class WikiService:
    """文章全生命周期服务"""

    @staticmethod
    def _get_article_content_summary(content) -> str:
        """从文章内容中提取摘要，处理 Quill 字段的多种格式"""
        if not content:
            return ''
        try:
            content_str = str(content)
            if isinstance(content_str, dict):
                return content_str.get('html', '')[:100] if isinstance(content_str, dict) else ''
            return content_str[:100]
        except (TypeError, AttributeError):
            return ''

    def create_article(
        self,
        title: str,
        content: str,
        author,
        department,
        source_type: str = 'MANUAL',
    ) -> Article:
        """创建文章（默认草稿状态）"""
        article = Article.objects.create(
            title=title,
            content=content,
            author=author,
            department=department,
            status=Article.Status.DRAFT,
            source_type=source_type,
        )

        ArticleAuditLog.objects.create(
            article=article,
            old_status=None,
            new_status=Article.Status.DRAFT,
            changed_by=author,
        )

        logger.info("Article created: id=%s, title=%s, author=%s", article.id, title, author.username)
        return article

    def update_article(
        self,
        article: Article,
        user,
        title: Optional[str] = None,
        content: Optional[str] = None,
        department=None,
    ) -> Article:
        """更新文章

        草稿：直接编辑保存。
        已发布：编辑后状态变为 DRAFT，需重新走审核流程（原 PUBLISHED 版本仍可检索）。
        """
        if article.status == Article.Status.PUBLISHED:
            old_status = article.status
            old_title = article.title
            old_content = self._get_article_content_summary(article.content)
            article.status = Article.Status.DRAFT
            article.review_overdue = False

            if title is not None:
                article.title = title
            if content is not None:
                article.content = content
            if department is not None:
                article.department = department

            article.save(update_fields=['status', 'review_overdue', 'title', 'content', 'department'])

            old_snapshot = type(article)()
            old_snapshot.title = old_title
            old_snapshot.content = old_content
            ArticleAuditLog.objects.create(
                article=article,
                old_status=old_status,
                new_status=Article.Status.DRAFT,
                changed_by=user,
                reason="编辑已发布文章，回退为草稿待重新审核",
                change_summary=_get_change_summary(
                    old_snapshot, article, ['title', 'content', 'department']
                ),
            )

            logger.info(
                "Published article edited, reverted to draft: id=%s, editor=%s",
                article.id, user.username,
            )
            return article

        if article.status != Article.Status.DRAFT:
            raise PermissionError("仅草稿和已发布状态可编辑")

        old_title = article.title
        old_content = WikiService._get_article_content_summary(article.content)
        old_department_id = article.department_id

        if title is not None:
            article.title = title
        if content is not None:
            article.content = content
        if department is not None:
            article.department = department

        article.save()

        old_snapshot = type(article)()
        old_snapshot.title = old_title
        old_snapshot.content = old_content
        old_snapshot.department_id = old_department_id
        ArticleAuditLog.objects.create(
            article=article,
            old_status=Article.Status.DRAFT,
            new_status=Article.Status.DRAFT,
            changed_by=user,
            reason="编辑草稿",
            change_summary=_get_change_summary(
                old_snapshot, article, ['title', 'content', 'department']
            ),
        )

        logger.info("Article updated: id=%s, editor=%s", article.id, user.username)
        return article

    def submit_for_review(self, article: Article, user) -> Article:
        """提交文章审核"""
        if article.author != user:
            raise PermissionError("只能提交自己撰写的文章")

        if article.status != Article.Status.DRAFT:
            raise PermissionError("仅草稿状态可提交审核")

        old_status = article.status
        article.status = Article.Status.PENDING
        article.save(update_fields=["status"])

        ArticleAuditLog.objects.create(
            article=article,
            old_status=old_status,
            new_status=Article.Status.PENDING,
            changed_by=user,
        )

        logger.info("Article submitted for review: id=%s, author=%s", article.id, user.username)
        return article

    def review_article(
        self,
        article: Article,
        reviewer,
        action: str,
        reason: str = "",
    ) -> Article:
        """审核文章（通过/驳回）"""
        if article.status != Article.Status.PENDING:
            raise PermissionError("只能审核待审核文章")

        if action == "approve":
            new_status = Article.Status.PUBLISHED
        elif action == "reject":
            new_status = Article.Status.DRAFT
        else:
            raise ValueError(f"无效审核操作: {action}")

        old_status = article.status
        article.status = new_status

        if action == "approve":
            article.save(update_fields=["status"])
        else:
            article.save(update_fields=["status"])

        ArticleAuditLog.objects.create(
            article=article,
            old_status=old_status,
            new_status=new_status,
            changed_by=reviewer,
            reason=reason,
        )

        logger.info(
            "Article reviewed: id=%s, action=%s, reviewer=%s",
            article.id, action, reviewer.username,
        )
        return article

    def delete_draft(self, article: Article, user) -> Article:
        """删除草稿或待审核文章（仅作者可删除，不可恢复）"""
        if article.author != user:
            raise PermissionError("只能删除自己的草稿")

        if article.status not in (Article.Status.DRAFT, Article.Status.PENDING):
            raise PermissionError("仅草稿和待审核状态可删除")

        ArticleAuditLog.objects.create(
            article=article,
            old_status=article.status,
            new_status="DELETED",
            changed_by=user,
            reason="作者主动删除草稿",
        )

        article.is_deleted = True
        article.save(update_fields=["is_deleted"])

        logger.info("Draft deleted: id=%s, by=%s", article.id, user.username)
        return article

    def soft_delete_article(self, article: Article, user) -> Article:
        """软删除文章"""
        article.is_deleted = True
        article.save(update_fields=["is_deleted"])

        ArticleAuditLog.objects.create(
            article=article,
            old_status=article.status,
            new_status="DELETED",
            changed_by=user,
        )

        logger.info("Article soft deleted: id=%s, by=%s", article.id, user.username)
        return article

    def restore_article(self, article: Article, user) -> Article:
        """恢复已删除文章"""
        if not article.is_deleted:
            raise PermissionError("只能恢复已删除文章")

        article.is_deleted = False
        article.save(update_fields=["is_deleted"])

        ArticleAuditLog.objects.create(
            article=article,
            old_status="DELETED",
            new_status=article.status,
            changed_by=user,
        )

        logger.info("Article restored: id=%s, by=%s", article.id, user.username)
        return article

    def re_review_article(
        self,
        article: Article,
        reviewer,
        action: str,
        reason: str = "",
    ) -> Article:
        if not article.review_overdue:
            raise PermissionError("仅待复审文章可执行复审操作")

        if article.status != Article.Status.PUBLISHED:
            raise PermissionError("仅已发布的待复审文章可执行复审操作")

        if action == "approve":
            article.review_overdue = False
            from django.utils import timezone
            from datetime import timedelta
            from django.conf import settings
            period = getattr(settings, "WIKI_REVIEW_DUE_PERIOD_DAYS", 180)
            article.review_due_date = timezone.now().date() + timedelta(days=period)
            article.save(update_fields=["review_overdue", "review_due_date"])

            ArticleAuditLog.objects.create(
                article=article,
                old_status=Article.Status.PUBLISHED,
                new_status=Article.Status.PUBLISHED,
                changed_by=reviewer,
                reason="复审通过" + (f"：{reason}" if reason else ""),
            )

            logger.info(
                "Article re-review approved: id=%s, reviewer=%s",
                article.id, reviewer.username,
            )
        elif action == "reject":
            article.status = Article.Status.ARCHIVED
            article.review_overdue = False
            article.save(update_fields=["status", "review_overdue"])

            from apps.wiki.tasks import deactivate_article_chunks
            deactivate_article_chunks(article)

            ArticleAuditLog.objects.create(
                article=article,
                old_status=Article.Status.PUBLISHED,
                new_status=Article.Status.ARCHIVED,
                changed_by=reviewer,
                reason="复审拒绝，文章下线" + (f"：{reason}" if reason else ""),
            )

            logger.info(
                "Article re-review rejected, archived: id=%s, reviewer=%s",
                article.id, reviewer.username,
            )
        else:
            raise ValueError(f"无效复审操作: {action}")

        return article

    def change_article_status(self, article: Article, user, new_status: str) -> Article:
        """管理员直接修改文章状态

        仅用于 Django Admin，绕过 submit_for_review / review_article 的权限限制。
        审计日志由 pre_save signal 自动记录。
        """
        old_status = article.status
        if old_status == new_status:
            return article

        article.status = new_status

        from apps.wiki.signals import record_article_audit_log
        record_article_audit_log(article, user, f"管理员修改状态: {old_status} → {new_status}")

        update_fields = ['status']
        if new_status == Article.Status.PUBLISHED and not article.review_due_date:
            from django.utils import timezone
            from datetime import timedelta
            from django.conf import settings
            period = getattr(settings, "WIKI_REVIEW_DUE_PERIOD_DAYS", 180)
            article.review_due_date = timezone.now().date() + timedelta(days=period)
            update_fields.append('review_due_date')

        article.save(update_fields=update_fields)

        if new_status == Article.Status.ARCHIVED:
            from apps.wiki.tasks import deactivate_article_chunks
            deactivate_article_chunks(article)

        logger.info(
            "Article status changed by admin: id=%s, %s → %s, by=%s",
            article.id, old_status, new_status, user.username,
        )
        return article
