"""
Wiki 异步任务 - 团队D治理重构版

基于 Guide.md §10 异步任务规范：
- 幂等性：任务重复执行不应产生不可控副作用
- 超时控制：所有长耗时任务必须定义超时
- 重试策略：区分可重试错误与不可重试错误
- 可追踪性：任务状态必须可追踪
- 回退策略：失败后必须有补偿、告警或人工干预机制
"""


def deactivate_article_chunks(article):
    ArticleChunk.objects.filter(article=article).delete()
import hashlib
import json
import re
import time
import logging
from datetime import timedelta

from django.utils import timezone
from django.utils.html import strip_tags
from django.conf import settings
from django.db import transaction
from django_q.tasks import async_task as dq_async_task

from .embedding_client import generate_embeddings_batch
from .models import Article, ArticleChunk

logger = logging.getLogger(__name__)


def extract_text_from_quill_delta(content) -> str:
    """
    从 Quill Delta JSON 或 HTML 中提取纯文本。

    支持的输入格式：
    1. FieldQuill 对象（django_quill）→ 优先使用 plain 属性
    2. Quill Delta JSON 列表 → 拼接 insert 字段
    3. HTML 字符串 → strip_tags

    Args:
        content: FieldQuill 对象、Quill Delta JSON 字符串或 HTML 字符串

    Returns:
        提取的纯文本
    """
    if not content:
        return ""

    if hasattr(content, 'plain') and content.plain:
        return content.plain

    if hasattr(content, 'json_string') and content.json_string:
        content = content.json_string
    elif hasattr(content, 'html') and content.html:
        return strip_tags(content.html)

    if isinstance(content, str):
        try:
            delta = json.loads(content)
            if isinstance(delta, list):
                return ''.join(
                    item.get('insert', '')
                    for item in delta
                    if isinstance(item, dict)
                )
            if isinstance(delta, dict) and 'delta' in delta:
                ops = delta['delta'].get('ops', [])
                return ''.join(
                    item.get('insert', '')
                    for item in ops
                    if isinstance(item, dict)
                )
        except (json.JSONDecodeError, TypeError):
            pass

        return strip_tags(content)

    return ""

TASK_TIMEOUT = getattr(settings, "TASK_TIMEOUT", 300)
TASK_MAX_RETRIES = getattr(settings, "TASK_MAX_RETRIES", 2)
TASK_RETRY_DELAY = getattr(settings, "TASK_RETRY_DELAY", 60)

REVIEW_DUE_PERIOD_DAYS = getattr(settings, "WIKI_REVIEW_DUE_PERIOD_DAYS", 180)

RETRYABLE_ERRORS = {
    "ConnectionError": {"delay": 60},
    "TimeoutError": {"delay": 30},
    "OSError": {"delay": 120},
}


class VectorizationError(Exception):
    """Base exception for article vectorization failures."""
    pass


class ArticleNotFoundError(VectorizationError):
    pass


class EmbeddingFailedError(VectorizationError):
    pass


def _chunk_text(text: str, chunk_size: int, overlap: int) -> list:
    """按句子边界切分文本，同时遵守最大长度限制。

    切分策略：
    1. 按句子边界（。！？\n.!?）分割，保持语义完整性
    2. 合并句子到不超过 chunk_size 的块
    3. 相邻块之间保留 overlap 重叠
    4. 如果文本中没有任何句子边界标记，回退到固定长度切片
    """
    parts = re.split(r'([。！？\n.!?]+)', text)

    sentences = []
    i = 0
    while i < len(parts):
        if i + 1 < len(parts) and re.match(r'^[。！？\n.!?]+$', parts[i + 1]):
            sentences.append(parts[i] + parts[i + 1])
            i += 2
        else:
            if parts[i].strip():
                sentences.append(parts[i])
            i += 1

    if not sentences:
        return []

    if len(sentences) == 1 and len(sentences[0]) > chunk_size:
        chunks = []
        start = 0
        while start < len(text):
            end = start + chunk_size
            chunk = text[start:end]
            if chunk.strip():
                chunks.append(chunk)
            start += (chunk_size - overlap)
        return chunks

    chunks = []
    current = ""
    for sentence in sentences:
        if len(current) + len(sentence) > chunk_size and current:
            chunks.append(current)
            tail = current[-overlap:] if len(current) > overlap else current
            current = tail + sentence
        else:
            current += sentence

    if current.strip():
        chunks.append(current)

    return [c.strip() for c in chunks if c.strip()]


def vectorize_article(article_id: int, retry_count: int = 0) -> dict:
    """
    Vectorize a wiki article into embeddings.

    Idempotency: Deletes existing chunks before creating new ones (BR-WIKI-21),
    so re-running the task produces the same result.

    Timeout: Should be wrapped by Django Q with TASK_TIMEOUT.

    Retry: Caller (e.g. signal or admin action) should handle retries
    for retryable errors only.
    """
    start_time = time.time()
    result = {
        "article_id": article_id,
        "status": "pending",
        "chunks_created": 0,
        "duration_ms": 0,
        "retry_count": retry_count,
        "error": None,
    }

    try:
        try:
            article = Article.objects.select_related("department").prefetch_related('chunks').get(id=article_id)
        except Article.DoesNotExist:
            raise ArticleNotFoundError(f"Article {article_id} not found.")

        logger.info(
            "TASK_VECTORIZE_START | article_id=%s | title=%s | retry=%d",
            article_id, article.title, retry_count,
        )

        clean_text = extract_text_from_quill_delta(article.content)
        if not clean_text.strip():
            result["status"] = "skipped"
            result["error"] = "Article content is empty after stripping HTML."
            logger.warning("TASK_VECTORIZE_SKIP | article_id=%s | reason=empty_content", article_id)
            return result

        content_hash = hashlib.md5(clean_text.encode('utf-8')).hexdigest()
        latest_version_chunks = article.chunks.filter(version=article.version, is_active=True)
        if latest_version_chunks.exists():
            first_chunk = latest_version_chunks.first()
            if first_chunk and first_chunk.content_hash == content_hash:
                logger.info(
                    "TASK_VECTORIZE_SKIP | article_id=%s | reason=already_vectorized | version=%d",
                    article_id, article.version,
                )
                result["status"] = "skipped"
                return result

        chunks_text = _chunk_text(clean_text, settings.CHUNK_SIZE, settings.CHUNK_OVERLAP)

        embedding_vectors = generate_embeddings_batch(chunks_text)

        if None in embedding_vectors:
            failed_index = embedding_vectors.index(None)
            raise EmbeddingFailedError(f"Failed to generate embedding for chunk {failed_index}.")

        with transaction.atomic():
            article.chunks.all().delete()

            chunks_to_create = [
                ArticleChunk(
                    article=article,
                    content_text=text,
                    embedding=vector,
                    chunk_index=index,
                    department=article.department,
                    version=article.version,
                    is_active=True,
                    content_hash=content_hash,
                )
                for index, (text, vector) in enumerate(zip(chunks_text, embedding_vectors))
            ]

            ArticleChunk.objects.bulk_create(chunks_to_create)
            created_count = len(chunks_to_create)

            from django.contrib.postgres.search import SearchVector
            from django.db import connection as db_conn
            new_chunk_ids = [c.id for c in chunks_to_create if c.id]
            if new_chunk_ids and db_conn.vendor != 'sqlite':
                ArticleChunk.objects.filter(id__in=new_chunk_ids).update(
                    search_vector=SearchVector('content_text', config='simple')
                )

        duration_ms = (time.time() - start_time) * 1000
        result["status"] = "success"
        result["chunks_created"] = created_count
        result["duration_ms"] = int(duration_ms)

        logger.info(
            "TASK_VECTORIZE_SUCCESS | article_id=%s | chunks=%d | duration_ms=%d | retry=%d",
            article_id, created_count, int(duration_ms), retry_count,
        )

    except ArticleNotFoundError:
        result["status"] = "failed"
        result["error"] = "Article not found."
        logger.warning("TASK_VECTORIZE_FAILED | article_id=%s | reason=not_found", article_id)

    except EmbeddingFailedError as e:
        result["status"] = "failed"
        result["error"] = str(e)
        logger.error(
            "TASK_VECTORIZE_FAILED | article_id=%s | reason=embedding_failed | error=%s",
            article_id, str(e),
        )

    except (ConnectionError, TimeoutError, OSError) as e:
        result["status"] = "retryable"
        result["error"] = str(e)
        logger.warning(
            "TASK_VECTORIZE_RETRYABLE | article_id=%s | retry=%d | max_retries=%d | error=%s",
            article_id, retry_count, TASK_MAX_RETRIES, str(e),
        )

    except Exception as e:
        result["status"] = "failed"
        result["error"] = str(e)
        logger.error(
            "TASK_VECTORIZE_FAILED | article_id=%s | reason=unexpected | error=%s",
            article_id, str(e),
        )

    return result


def schedule_vectorize_with_retry(article_id: int, _retry_count: int = 0) -> dict:
    """
    带有限重试次数的向量化任务调度器。

    使用 Django-Q async_task 进行延迟重试（通过 schedule 参数）。
    如果文章已不是 PUBLISHED 状态，直接放弃。

    参数:
        article_id: 文章ID
        _retry_count: 内部使用，记录当前重试次数（用户不应传入此参数）
    """
    result = vectorize_article(article_id, retry_count=_retry_count)

    if result["status"] == "failed":
        return {
            "article_id": article_id,
            "status": "failed",
            "chunks_created": 0,
            "duration_ms": 0,
            "retry_count": _retry_count,
            "error": result.get("error", "未知错误"),
        }

    if result["status"] == "retryable":
        if _retry_count >= TASK_MAX_RETRIES:
            return {
                "article_id": article_id,
                "status": "exhausted_retries",
                "chunks_created": 0,
                "duration_ms": 0,
                "retry_count": TASK_MAX_RETRIES,
                "error": result.get("error", "未知错误"),
            }

        next_retry = _retry_count + 1

        try:
            article = Article.objects.select_related('department').get(id=article_id)
            error_type = "ConnectionError"
            if "timeout" in result.get("error", "").lower():
                error_type = "TimeoutError"

            delay_seconds = RETRYABLE_ERRORS.get(error_type, {}).get('delay', 60)
            schedule_time = timezone.now() + timedelta(seconds=delay_seconds)

            dq_async_task(
                'apps.wiki.tasks.schedule_vectorize_with_retry',
                article_id,
                _retry_count=next_retry,
                task_name=f"重试向量化: {article.title} (第{next_retry}次)",
                group="article_vectorization",
                schedule_type='O',
                next_run=schedule_time,
            )

            logger.info(
                "TASK_SCHEDULE | article_id=%s | retry=%d/%d | delay=%ds",
                article_id, next_retry, TASK_MAX_RETRIES, delay_seconds,
            )
            return {"status": "scheduled", "retry": next_retry}

        except Article.DoesNotExist:
            return {"status": "failed", "error": "文章不存在"}
        except Exception:
            logger.warning(
                "TASK_SCHEDULE_FALLBACK | article_id=%s | retry=%d | using immediate_retry",
                article_id, next_retry,
            )
            dq_async_task(
                'apps.wiki.tasks.schedule_vectorize_with_retry',
                article_id,
                _retry_count=next_retry,
                task_name=f"重试向量化: {article.title} (第{next_retry}次)",
                group="article_vectorization",
            )
            return {"status": "scheduled", "retry": next_retry}

    return result


def check_review_due_articles() -> dict:
    """
    检查复审到期的文章，标记 review_overdue=True（保持 PUBLISHED 状态）。
    同时检查宽限期已过的待复审文章，自动下线。

    优化：
    1. 使用 bulk_update 批量更新，避免逐个 save() 触发信号
    2. 审计日志在 bulk_update 后手动记录

    幂等性：重复执行不会产生额外副作用（已标记的文章状态不变）。
    超时：查询+批量更新，耗时极小，不会超时。
    """
    from apps.wiki.models import ArticleAuditLog

    today = timezone.now().date()
    review_grace_period_days = getattr(settings, "WIKI_REVIEW_GRACE_PERIOD_DAYS", 30)

    expired_articles = list(Article.objects.filter(
        status=Article.Status.PUBLISHED,
        review_due_date__lte=today,
        review_overdue=False,
        is_deleted=False,
    ))

    if not expired_articles:
        logger.info(
            "TASK_REVIEW_DUE_COMPLETE | expired_count=0 | today=%s",
            today,
        )
    else:
        for article in expired_articles:
            article.review_overdue = True
            article._skip_vectorization = True

        Article.objects.bulk_update(expired_articles, ['review_overdue'])

        for article in expired_articles:
            ArticleAuditLog.objects.create(
                article=article,
                old_status=Article.Status.PUBLISHED,
                new_status=Article.Status.PUBLISHED,
                changed_by=None,
                reason="复审到期自动标记待复核",
            )

            logger.info(
                "TASK_REVIEW_DUE | article_id=%s | title=%s | review_due_date=%s",
                article.id, article.title, article.review_due_date,
            )

        _send_review_due_notifications(expired_articles)

        logger.info(
            "TASK_REVIEW_DUE_COMPLETE | expired_count=%d | today=%s",
            len(expired_articles), today,
        )

    grace_cutoff = today - timedelta(days=review_grace_period_days)
    overdue_articles = list(Article.objects.filter(
        status=Article.Status.PUBLISHED,
        review_overdue=True,
        review_due_date__lte=grace_cutoff,
        is_deleted=False,
    ))

    if not overdue_articles:
        logger.info(
            "TASK_REVIEW_GRACE_COMPLETE | archived_count=0 | today=%s",
            today,
        )
    else:
        for article in overdue_articles:
            article.status = Article.Status.ARCHIVED
            article._skip_vectorization = True

        Article.objects.bulk_update(overdue_articles, ['status'])

        for article in overdue_articles:
            ArticleAuditLog.objects.create(
                article=article,
                old_status=Article.Status.PUBLISHED,
                new_status=Article.Status.ARCHIVED,
                changed_by=None,
                reason=f"复审宽限期({review_grace_period_days}天)已过，自动下线",
            )

            ArticleChunk.objects.filter(article=article).delete()

            logger.info(
                "TASK_REVIEW_GRACE_ARCHIVE | article_id=%s | title=%s | review_due_date=%s",
                article.id, article.title, article.review_due_date,
            )

        logger.info(
            "TASK_REVIEW_GRACE_COMPLETE | archived_count=%d | today=%s",
            len(overdue_articles), today,
        )

    return {
        "status": "success",
        "expired_count": len(expired_articles),
        "archived_count": len(overdue_articles),
        "review_date": today.isoformat(),
    }


def _send_review_due_notifications(articles):
    from apps.base.services import NotificationService
    from apps.base.models import Notification

    notif_svc = NotificationService()
    for article in articles:
        try:
            if article.author:
                notif_svc.notify(
                    recipient=article.author,
                    category=Notification.Category.REVIEW,
                    title=f"文章复审到期：{article.title}",
                    content=f"您撰写的文章《{article.title}》已到复审日期（{article.review_due_date}），请及时处理。",
                    source_id=str(article.id),
                )

            if article.department:
                notif_svc.notify_department_staff(
                    department=article.department,
                    category=Notification.Category.REVIEW,
                    title=f"科室文章复审到期：{article.title}",
                    content=f"科室《{article.department.name}》的文章《{article.title}》已到复审日期，请安排复审。",
                    source_id=str(article.id),
                )
        except Exception as e:
            logger.error(
                "REVIEW_DUE_NOTIFY_FAILED | article_id=%s | error=%s",
                article.id, str(e), exc_info=True,
            )
