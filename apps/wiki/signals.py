from django.db.models.signals import post_save, pre_save
from django.dispatch import receiver
from django.utils import timezone
from datetime import timedelta
from django_q.tasks import async_task
from django.conf import settings
from .models import Article

REVIEW_DUE_PERIOD_DAYS = getattr(settings, "WIKI_REVIEW_DUE_PERIOD_DAYS", 180)


def _get_change_summary(old, new, fields):
    changes = []
    for field in fields:
        old_val = getattr(old, field, None)
        new_val = getattr(new, field, None)
        if old_val != new_val:
            old_str = str(old_val)[:50]
            new_str = str(new_val)[:50]
            changes.append(f"{field}: {old_str!r} → {new_str!r}")
    return "; ".join(changes) if changes else ""


@receiver(post_save, sender=Article)
def article_post_save(sender, instance, created, **kwargs):
    """
    文章保存后触发向量化：
    1. 状态为 PUBLISHED 且 _should_vectorize=True 时触发。
    2. _should_vectorize 由 pre_save 信号根据内容变更和状态变更计算。
    3. 为了不阻塞主线程，使用 Django-Q 异步执行。
    4. 如果 _skip_vectorization=True，跳过向量化（用于 bulk 操作等场景）。
    """
    if getattr(instance, '_skip_vectorization', False):
        return

    if instance.status == 'PUBLISHED' and getattr(instance, '_should_vectorize', True):
        async_task(
            'apps.wiki.tasks.schedule_vectorize_with_retry',
            instance.id,
            task_name=f"向量化文章: {instance.title}",
            group="article_vectorization"
        )


@receiver(pre_save, sender=Article)
def article_pre_save_handler(sender, instance, **kwargs):
    """
    文章保存前处理：
    1. BR-WIKI-08: 已发布文章每次保存 version+1
    2. BR-WIKI-09: 状态变更时记录审计日志
    3. BR-WIKI-12: 被引用文章被下架或软删除后，相关引用自动失效
    4. BR-BASE-12: 公共文章更新后已引用自动失效
    5. 首次发布时自动设置 review_due_date
    6. 计算 _should_vectorize 标志，供 post_save 信号决定是否触发向量化
    """
    if not instance.pk:
        instance._should_vectorize = (instance.status == Article.Status.PUBLISHED)
        if instance.status == Article.Status.PUBLISHED and not instance.review_due_date:
            instance.review_due_date = timezone.now().date() + timedelta(days=REVIEW_DUE_PERIOD_DAYS)
        return

    try:
        old = Article.objects.get(pk=instance.pk)
    except Article.DoesNotExist:
        instance._should_vectorize = (instance.status == Article.Status.PUBLISHED)
        return

    instance._should_vectorize = False
    content_changed = _is_article_content_changed(old, instance)

    if instance.status == Article.Status.PUBLISHED:
        if old.status != Article.Status.PUBLISHED:
            instance._should_vectorize = True
            if not instance.review_due_date:
                instance.review_due_date = timezone.now().date() + timedelta(days=REVIEW_DUE_PERIOD_DAYS)
        elif content_changed:
            instance._should_vectorize = True

    if content_changed and instance.status == Article.Status.PUBLISHED and old.status == Article.Status.PUBLISHED:
        instance.version = old.version + 1
        _invalidate_answer_cache()

    if old.status != instance.status:
        from apps.wiki.models import ArticleAuditLog

        if instance._audit_changed_by:
            content_changed = _is_article_content_changed(old, instance)
            if content_changed:
                old_snapshot = type(instance)()
                old_snapshot.title = old.title
                old_snapshot.content = str(old.content)[:100] if old.content else ''
                old_snapshot.department_id = old.department_id
                change_summary = _get_change_summary(old_snapshot, instance, ['title', 'content', 'department'])
            else:
                change_summary = f"状态变更: {old.status} → {instance.status}"

            ArticleAuditLog.objects.create(
                article=instance,
                old_status=old.status,
                new_status=instance.status,
                changed_by=instance._audit_changed_by,
                reason=instance._audit_reason or '',
                change_summary=change_summary,
            )

    was_published = old.status == Article.Status.PUBLISHED and not old.is_deleted
    is_now_invalid = instance.status in (Article.Status.ARCHIVED,) or instance.is_deleted

    if was_published and is_now_invalid:
        from django.utils import timezone as tz
        from apps.wiki.reference.models import ArticleReference, ArticleReferenceStatus
        ArticleReference.objects.filter(
            source_article=instance,
            status=ArticleReferenceStatus.APPROVED,
        ).update(
            status=ArticleReferenceStatus.INVALIDATED,
            invalidated_at=tz.now(),
            invalidated_reason="文章已被下架或删除",
        )
        _invalidate_answer_cache()

    if content_changed and instance.status == Article.Status.PUBLISHED:
        _invalidate_approved_references(instance)


def _is_article_content_changed(old, new) -> bool:
    """检查文章内容是否发生了变化。"""
    try:
        old_content = getattr(old.content, 'plain', None)
        if old_content is None:
            old_content = str(old.content) if old.content else ''
    except Exception:
        old_content = str(old.content) if old.content else ''
    
    try:
        new_content = getattr(new.content, 'plain', None)
        if new_content is None:
            new_content = str(new.content) if new.content else ''
    except Exception:
        new_content = str(new.content) if new.content else ''
    
    return old_content != new_content


def _invalidate_approved_references(article):
    """将已发布文章的所有 APPROVED 引用标记为 INVALIDATED。"""
    from django.utils import timezone
    from apps.wiki.reference.models import ArticleReference, ArticleReferenceStatus
    
    now = timezone.now()
    ArticleReference.objects.filter(
        source_article=article,
        status=ArticleReferenceStatus.APPROVED,
    ).update(
        status=ArticleReferenceStatus.INVALIDATED,
        invalidated_at=now,
        invalidated_reason='源文章内容已更新'
    )


def _invalidate_answer_cache():
    """使所有 AI 回答缓存失效，知识库更新后调用。"""
    try:
        from apps.chat.services.answer_cache_service import AnswerCacheService
        AnswerCacheService.invalidate_by_knowledge_version()
    except Exception:
        pass


def record_article_audit_log(article, user, reason=''):
    """
    辅助方法：在调用代码中设置审计信息，由 pre_save signal 自动记录。
    """
    article._audit_changed_by = user
    article._audit_reason = reason


Article._audit_changed_by = None
Article._audit_reason = ''
Article._should_vectorize = True
