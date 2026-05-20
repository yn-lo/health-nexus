from django.shortcuts import render, redirect
from django.http import Http404
import logging
from apps.base.models import Department
from apps.wiki import queries as article_queries
from apps.service_container import container

logger = logging.getLogger(__name__)


def _filter_public_dept_tree(tree):
    result = []
    for node in tree:
        if node.get('is_public'):
            filtered = dict(node)
            filtered['children'] = _filter_public_dept_tree(node.get('children', []))
            result.append(filtered)
        else:
            public_children = _filter_public_dept_tree(node.get('children', []))
            if public_children:
                filtered = dict(node)
                filtered['children'] = public_children
                result.append(filtered)
    return result


def blog_list(request):
    departments = Department.objects.filter(is_public=True).order_by("name")

    dept_service = container.department_service
    dept_tree = dept_service.get_department_tree()

    patient_dept_ids = []
    is_anonymous = not request.user.is_authenticated

    if not is_anonymous:
        try:
            patient = request.user.patient_profile
            patient_dept_ids = list(patient.departments.values_list('id', flat=True))
        except Exception as e:
            logger.warning("PATIENT_DEPT_FALLBACK | user=%s | reason=%s", request.user.id, str(e), exc_info=True)

    public_dept_ids = list(Department.objects.filter(is_public=True).values_list('id', flat=True))

    if is_anonymous:
        dept_tree = _filter_public_dept_tree(dept_tree)

    if 'dept' in request.GET:
        dept_param = request.GET.get("dept", "all")
        if dept_param and dept_param != "all":
            try:
                current_dept_ids = [int(x) for x in dept_param.split(",")]
            except (ValueError, AttributeError):
                current_dept_ids = []
        else:
            current_dept_ids = []
    else:
        current_dept_ids = list(set(patient_dept_ids) | set(public_dept_ids))

    search_query = request.GET.get("q", "").strip()
    current_sort = request.GET.get("sort", "newest")
    page = int(request.GET.get('page', 1))
    page_size = 10

    result = article_queries.get_published_articles_paginated(
        request.user, page=page, page_size=page_size,
        dept_ids=current_dept_ids, search_query=search_query,
    )

    articles = result.items
    if current_sort == "popular":
        articles = sorted(articles, key=lambda a: a.view_count, reverse=True)

    return render(
        request,
        "blog/list.html",
        {
            "departments": departments,
            "current_dept_ids": current_dept_ids,
            "articles": articles,
            "search_query": search_query,
            "dept_tree": dept_tree,
            "patient_dept_ids": [str(i) for i in patient_dept_ids],
            "public_dept_ids": [str(i) for i in public_dept_ids],
            "current_sort": current_sort,
            "page": result.page,
            "total_count": result.total_count,
            "has_next": result.has_next,
            "has_prev": result.has_prev,
        },
    )


def article_detail(request, pk):
    article = article_queries.get_article_with_access_check(request.user, pk)
    if not article:
        raise Http404("文章不存在或无权访问")

    if not request.user.is_authenticated and not request.session.session_key:
        request.session.cycle_key()
    session_key = request.session.session_key
    article_queries.increment_article_view_count(article.pk, user=request.user, session_key=session_key)

    content_html = article.content.html if hasattr(article.content, 'html') else str(article.content)

    related_articles = article_queries.get_published_articles_with_filters(
        request.user, dept_id=article.department_id, limit=20
    ).exclude(id=article.id)[:4]

    return render(
        request,
        "blog/detail.html",
        {
            "article": article,
            "content": content_html,
            "related_articles": related_articles,
        }
    )
