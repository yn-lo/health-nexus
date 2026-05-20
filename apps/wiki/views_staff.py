import json
import logging
from django.shortcuts import render, redirect, get_object_or_404
from django.views.decorators.http import require_POST
from django.http import HttpResponseForbidden
from django.utils.html import strip_tags

from apps.auth.decorators import staff_member_required
from apps.auth.models import UserProfile
from apps.base.models import Department, UserDepartment
from apps.wiki.models import Article
from apps.wiki import queries as article_queries
from apps.service_container import container

logger = logging.getLogger(__name__)


def _html_to_quill_json(html_content):
    plain = strip_tags(html_content).strip()
    return json.dumps({
        "delta": {"ops": [{"insert": plain + "\n"}]},
        "html": html_content,
        "plain": plain,
    })


def _can_review(user):
    return user.can_review_articles()


def _get_user_primary_department(user):
    ud = UserDepartment.objects.filter(user=user, is_primary=True).first()
    if ud:
        return ud.department
    ud = UserDepartment.objects.filter(user=user).first()
    return ud.department if ud else None


@staff_member_required
def article_management(request):
    tab = request.GET.get('tab', 'mine')
    page = int(request.GET.get('page', 1))
    page_size = 10

    if tab == 'review':
        if not _can_review(request.user):
            result = article_queries.get_reviewable_articles_paginated(request.user, page=page, page_size=page_size)
            result.items = []
        else:
            result = article_queries.get_reviewable_articles_paginated(request.user, page=page, page_size=page_size)
        return render(request, "wiki/staff/article_management.html", {
            "tab": tab,
            "articles": result.items,
            "can_review": _can_review(request.user),
            "page": result.page,
            "total_count": result.total_count,
            "has_next": result.has_next,
            "has_prev": result.has_prev,
        })

    if tab == 'browse':
        dept_id = request.GET.get('dept', 'all')
        search_query = request.GET.get('q', '').strip()
        result = article_queries.get_staff_browsable_articles_paginated(
            request.user, page=page, page_size=page_size,
            dept_id=dept_id, search_query=search_query,
        )
        departments = Department.objects.filter(
            is_public=True
        ).order_by("name")
        return render(request, "wiki/staff/article_management.html", {
            "tab": tab,
            "articles": result.items,
            "departments": departments,
            "current_dept_id": dept_id,
            "search_query": search_query,
            "page": result.page,
            "total_count": result.total_count,
            "has_next": result.has_next,
            "has_prev": result.has_prev,
        })

    result = article_queries.get_user_articles_paginated(request.user, page=page, page_size=page_size)
    return render(request, "wiki/staff/article_management.html", {
        "tab": "mine",
        "articles": result.items,
        "page": result.page,
        "total_count": result.total_count,
        "has_next": result.has_next,
        "has_prev": result.has_prev,
    })


@staff_member_required
def article_create(request):
    user_departments = Department.objects.filter(
        user_departments__user=request.user
    ).order_by("name")

    if request.method == 'POST':
        title = request.POST.get('title', '').strip()
        content = request.POST.get('content', '').strip()
        department_id = request.POST.get('department')

        if not title or not content:
            return render(request, "wiki/staff/article_form.html", {
                "error": "标题和内容不能为空",
                "user_departments": user_departments,
                "form_data": request.POST,
            })

        if not department_id:
            dept = _get_user_primary_department(request.user)
            if not dept:
                return render(request, "wiki/staff/article_form.html", {
                    "error": "请选择所属科室",
                    "user_departments": user_departments,
                    "form_data": request.POST,
                })
        else:
            dept = get_object_or_404(Department, id=department_id)

        if not UserDepartment.objects.filter(
            user=request.user, department=dept
        ).exists():
            return HttpResponseForbidden("只能向自己所属科室提交文章")

        wiki_svc = container.wiki_service
        wiki_svc.create_article(
            title=title,
            content=_html_to_quill_json(content),
            author=request.user,
            department=dept,
        )

        return redirect("wiki:article_management")

    return render(request, "wiki/staff/article_form.html", {
        "user_departments": user_departments,
    })


@staff_member_required
def article_edit(request, pk):
    article = get_object_or_404(Article, pk=pk, is_deleted=False)

    if article.author != request.user:
        return HttpResponseForbidden("只能编辑自己撰写的文章")

    if article.status not in (Article.Status.DRAFT, Article.Status.PUBLISHED):
        return HttpResponseForbidden("仅草稿和已发布状态可编辑")

    user_departments = Department.objects.filter(
        user_departments__user=request.user
    ).order_by("name")

    if request.method == 'POST':
        title = request.POST.get('title', '').strip()
        content = request.POST.get('content', '').strip()
        department_id = request.POST.get('department')

        if not title or not content:
            return render(request, "wiki/staff/article_form.html", {
                "error": "标题和内容不能为空",
                "article": article,
                "user_departments": user_departments,
                "form_data": request.POST,
            })

        if department_id:
            dept = get_object_or_404(Department, id=department_id)
            if not UserDepartment.objects.filter(
                user=request.user, department=dept
            ).exists():
                return HttpResponseForbidden("只能选择自己所属科室")
        else:
            dept = article.department

        wiki_svc = container.wiki_service
        wiki_svc.update_article(
            article=article,
            user=request.user,
            title=title,
            content=_html_to_quill_json(content),
            department=dept,
        )

        return redirect("wiki:article_management")

    return render(request, "wiki/staff/article_form.html", {
        "article": article,
        "user_departments": user_departments,
    })


@staff_member_required
@require_POST
def article_submit_review(request, pk):
    article = get_object_or_404(Article, pk=pk, is_deleted=False)

    if article.author != request.user:
        return HttpResponseForbidden("只能提交自己撰写的文章")

    wiki_svc = container.wiki_service
    wiki_svc.submit_for_review(article, request.user)

    return redirect("wiki:article_management")


@staff_member_required
def article_review_list(request):
    if not _can_review(request.user):
        return HttpResponseForbidden("仅医护和管理员可以审核文章")

    page = int(request.GET.get('page', 1))
    page_size = 10
    result = article_queries.get_reviewable_articles_paginated(request.user, page=page, page_size=page_size)

    return render(request, "wiki/staff/review_list.html", {
        "articles": result.items,
        "page": result.page,
        "total_count": result.total_count,
        "has_next": result.has_next,
        "has_prev": result.has_prev,
    })


@staff_member_required
@require_POST
def article_review_action(request):
    if not _can_review(request.user):
        return HttpResponseForbidden("仅医护和管理员可以审核文章")

    article_id = request.POST.get("article_id")
    action = request.POST.get("action")

    article = get_object_or_404(
        Article, id=article_id, status=Article.Status.PENDING, is_deleted=False
    )

    if request.user.role != UserProfile.Role.SUPER_ADMIN:
        user_dept_ids = request.user.user_departments.values_list(
            'department_id', flat=True
        )
        if article.department_id not in user_dept_ids:
            return HttpResponseForbidden("只能审核本科室文章")

    if action not in ("approve", "reject"):
        return HttpResponseForbidden("无效操作")

    wiki_svc = container.wiki_service
    wiki_svc.review_article(
        article, request.user, action, reason=request.POST.get("reason", ""),
    )

    return redirect("wiki:article_management")


@staff_member_required
def re_review_list(request):
    if not _can_review(request.user):
        return HttpResponseForbidden("仅医护和管理员可以复审文章")

    page = int(request.GET.get('page', 1))
    page_size = 10

    qs = Article.objects.filter(
        review_overdue=True,
        status=Article.Status.PUBLISHED,
        is_deleted=False,
    ).select_related('author', 'department').order_by('review_due_date')

    if request.user.role != UserProfile.Role.SUPER_ADMIN:
        user_dept_ids = request.user.user_departments.values_list('department_id', flat=True)
        qs = qs.filter(department_id__in=user_dept_ids)

    total_count = qs.count()
    start = (page - 1) * page_size
    items = list(qs[start:start + page_size])
    has_next = start + page_size < total_count
    has_prev = page > 1

    return render(request, "wiki/staff/review_list.html", {
        "articles": items,
        "tab": "re_review",
        "page": page,
        "total_count": total_count,
        "has_next": has_next,
        "has_prev": has_prev,
    })


@staff_member_required
@require_POST
def re_review_action(request):
    if not _can_review(request.user):
        return HttpResponseForbidden("仅医护和管理员可以复审文章")

    article_id = request.POST.get("article_id")
    action = request.POST.get("action")

    article = get_object_or_404(
        Article, id=article_id, review_overdue=True,
        status=Article.Status.PUBLISHED, is_deleted=False,
    )

    if request.user.role != UserProfile.Role.SUPER_ADMIN:
        user_dept_ids = request.user.user_departments.values_list('department_id', flat=True)
        if article.department_id not in user_dept_ids:
            return HttpResponseForbidden("只能复审本科室文章")

    if action not in ("approve", "reject"):
        return HttpResponseForbidden("无效操作")

    wiki_svc = container.wiki_service
    wiki_svc.re_review_article(
        article, request.user, action, reason=request.POST.get("reason", ""),
    )

    return redirect("wiki:re_review_list")


@staff_member_required
@require_POST
def article_delete_draft(request, pk):
    article = get_object_or_404(Article, pk=pk, is_deleted=False)

    if article.author != request.user:
        return HttpResponseForbidden("只能删除自己的草稿")

    if article.status not in (Article.Status.DRAFT, Article.Status.PENDING):
        return HttpResponseForbidden("仅草稿和待审核状态可删除")

    wiki_svc = container.wiki_service
    wiki_svc.delete_draft(article, request.user)

    return redirect("wiki:article_management")
