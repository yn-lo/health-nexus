"""
Wiki 查询层 - 统一文章查询入口

基于 Guide.md 的查询治理原则：
- 高频查询必须集中管理
- 查询应与业务规则分层，而非散落在各处
- 必须识别核心读路径
- 区分"写正确"与"读高效"两种关注点
"""
import logging
from typing import List, Optional
from datetime import timedelta
from django.db import models
from django.db.models import F
from django.core.cache import cache
from django.utils import timezone
from apps.base.pagination import PaginatedResult
from apps.wiki.models import Article
from apps.auth.models import UserProfile

logger = logging.getLogger(__name__)


def _get_accessible_article_queryset(user):
    """返回有权限访问的文章 queryset
    
    匿名用户只能看公开科室的文章；
    认证用户可以看到公开科室 + 自己关联科室的文章。
    
    BR-BASE-05: 患者 = 本科室文章 + 已授权引用的公共文章
    """
    queryset = Article.objects.filter(
        status=Article.Status.PUBLISHED,
        is_deleted=False,
    ).select_related("department")

    if not user or not user.is_authenticated:
        return queryset.filter(department__is_public=True)

    try:
        patient = user.patient_profile
        patient_dept_ids = list(patient.departments.values_list('id', flat=True))
        if patient_dept_ids:
            from apps.wiki.reference.models import ArticleReferenceStatus
            return queryset.filter(
                models.Q(department__is_public=True)
                | models.Q(department_id__in=patient_dept_ids)
                | models.Q(
                    references_to__target_department_id__in=patient_dept_ids,
                    references_to__status=ArticleReferenceStatus.APPROVED,
                )
            )
        return queryset.filter(department__is_public=True)
    except Exception:
        logger.warning(
            "Failed to resolve patient profile for user=%s, falling back to public articles only",
            user.id if hasattr(user, 'id') else 'unknown',
        )
        return queryset.filter(department__is_public=True)


def get_published_articles_with_filters(user, dept_ids=None, dept_id=None, search_query=None, limit=20):
    queryset = _get_accessible_article_queryset(user)

    effective_dept_ids = dept_ids if dept_ids else ([int(dept_id)] if dept_id and dept_id != 'all' else [])
    if effective_dept_ids:
        queryset = queryset.filter(department_id__in=effective_dept_ids)

    if search_query:
        queryset = queryset.filter(
            models.Q(title__icontains=search_query) |
            models.Q(summary__icontains=search_query)
        )

    return queryset.select_related("department").distinct()


def get_published_articles_paginated(user, page=1, page_size=10, dept_ids=None, dept_id=None, search_query=None):
    queryset = _get_accessible_article_queryset(user)

    effective_dept_ids = dept_ids if dept_ids else ([int(dept_id)] if dept_id and dept_id != 'all' else [])
    if effective_dept_ids:
        queryset = queryset.filter(department_id__in=effective_dept_ids)

    if search_query:
        queryset = queryset.filter(
            models.Q(title__icontains=search_query) |
            models.Q(summary__icontains=search_query)
        )

    queryset = queryset.distinct()
    total_count = queryset.count()

    offset = (page - 1) * page_size
    items = list(queryset[offset:offset + page_size])

    return PaginatedResult(
        items=items,
        total_count=total_count,
        page=page,
        page_size=page_size,
    )


def get_article_with_access_check(user, article_id):
    """获取单篇文章，检查访问权限
    
    Args:
        user: 当前用户
        article_id: 文章 ID
    
    Returns:
        Article 对象或 None
    """
    queryset = _get_accessible_article_queryset(user)
    try:
        return queryset.get(id=article_id)
    except Article.DoesNotExist:
        return None


def increment_article_view_count(article_id: int, user=None, session_key: str = None) -> None:
    """增加文章阅读量（同一天同一用户/会话只+1）"""
    if user and user.is_authenticated:
        today = timezone.now().date().isoformat()
        cache_key = f"article_view:user:{user.id}:{article_id}:{today}"
    elif session_key:
        today = timezone.now().date().isoformat()
        cache_key = f"article_view:session:{session_key}:{article_id}:{today}"
    else:
        # 无 session 时按文章+日期全局去重（粗略统计）
        today = timezone.now().date().isoformat()
        cache_key = f"article_view:global:{article_id}:{today}"

    if cache.get(cache_key):
        return

    cache.set(cache_key, True, timeout=int(timedelta(days=1).total_seconds()))
    Article.objects.filter(id=article_id).update(view_count=F("view_count") + 1)


def get_user_articles(user):
    return Article.objects.filter(
        author=user,
        is_deleted=False,
    ).select_related("department").order_by("-created_at")


def get_user_articles_paginated(user, page=1, page_size=10):
    qs = get_user_articles(user)
    total_count = qs.count()
    offset = (page - 1) * page_size
    items = list(qs[offset:offset + page_size])
    return PaginatedResult(items=items, total_count=total_count, page=page, page_size=page_size)


def get_reviewable_articles(user):
    queryset = Article.objects.filter(
        status=Article.Status.PENDING,
        is_deleted=False,
    ).select_related("department", "author")

    if user.role == UserProfile.Role.SUPER_ADMIN:
        return queryset.order_by("-created_at")

    if user.role in (UserProfile.Role.DOCTOR, UserProfile.Role.NURSE, UserProfile.Role.DEPT_ADMIN):
        user_dept_ids = user.user_departments.values_list('department_id', flat=True)
        return queryset.filter(department_id__in=user_dept_ids).order_by("-created_at")

    return queryset.none()


def get_reviewable_articles_paginated(user, page=1, page_size=10):
    qs = get_reviewable_articles(user)
    total_count = qs.count()
    offset = (page - 1) * page_size
    items = list(qs[offset:offset + page_size])
    return PaginatedResult(items=items, total_count=total_count, page=page, page_size=page_size)


def get_staff_browsable_articles(user, dept_ids=None, dept_id=None, search_query=None, limit=20):
    user_dept_ids = list(
        user.user_departments.values_list('department_id', flat=True)
    )

    queryset = Article.objects.filter(
        status=Article.Status.PUBLISHED,
        is_deleted=False,
    ).select_related("department").filter(
        models.Q(department__is_public=True)
        | models.Q(department_id__in=user_dept_ids)
    )

    effective_dept_ids = dept_ids if dept_ids else ([int(dept_id)] if dept_id and dept_id != 'all' else [])
    if effective_dept_ids:
        queryset = queryset.filter(department_id__in=effective_dept_ids)

    if search_query:
        queryset = queryset.filter(
            models.Q(title__icontains=search_query)
            | models.Q(summary__icontains=search_query)
        )

    return queryset.order_by("-created_at")[:limit]


def get_staff_browsable_articles_paginated(user, page=1, page_size=10, dept_ids=None, dept_id=None, search_query=None):
    user_dept_ids = list(
        user.user_departments.values_list('department_id', flat=True)
    )

    queryset = Article.objects.filter(
        status=Article.Status.PUBLISHED,
        is_deleted=False,
    ).select_related("department").filter(
        models.Q(department__is_public=True)
        | models.Q(department_id__in=user_dept_ids)
    )

    effective_dept_ids = dept_ids if dept_ids else ([int(dept_id)] if dept_id and dept_id != 'all' else [])
    if effective_dept_ids:
        queryset = queryset.filter(department_id__in=effective_dept_ids)

    if search_query:
        queryset = queryset.filter(
            models.Q(title__icontains=search_query)
            | models.Q(summary__icontains=search_query)
        )

    queryset = queryset.order_by("-created_at")
    total_count = queryset.count()
    offset = (page - 1) * page_size
    items = list(queryset[offset:offset + page_size])
    return PaginatedResult(items=items, total_count=total_count, page=page, page_size=page_size)
