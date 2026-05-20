"""
stats 领域视图层

职责: 渲染运营看板页面,调用 StatsService 获取数据
AC-STATS-04: 基于角色的权限过滤
"""
from datetime import timedelta
from django.shortcuts import render
from django.utils import timezone

from apps.auth.decorators import staff_member_required
from apps.auth.models import UserProfile
from apps.service_container import container


def _get_time_range(time_range: str, custom_start=None, custom_end=None):
    """根据时间范围返回 start_date 和 end_date"""
    today = timezone.now().date()

    ranges = {
        'today': (today, today),
        'week': (today - timedelta(days=6), today),
        'month': (today - timedelta(days=29), today),
        'custom': (custom_start, custom_end),
    }

    start, end = ranges.get(time_range, (today - timedelta(days=6), today))

    if start and end and end < start:
        raise ValueError("结束日期不能早于开始日期")

    return start, end


def _get_user_department_ids(user) -> list:
    """AC-STATS-04: 获取用户可见的科室ID列表"""
    if user.role == UserProfile.Role.SUPER_ADMIN:
        return []
    return list(user.departments.values_list('id', flat=True))


@staff_member_required
def stats_dashboard(request):
    """
    医护端统计看板
    入口: /stats/dashboard/
    AC-STATS-04: 医生/护士只能看本科室，超级管理员看全局
    """
    user = request.user
    time_range = request.GET.get('range', 'week')
    custom_start = request.GET.get('start')
    custom_end = request.GET.get('end')
    department_id = request.GET.get('department_id')

    try:
        start_date, end_date = _get_time_range(time_range, custom_start, custom_end)
    except ValueError:
        start_date, end_date = timezone.now().date() - timedelta(days=6), timezone.now().date()

    svc = container.stats_service

    user_dept_ids = _get_user_department_ids(user)

    if user.role != UserProfile.Role.SUPER_ADMIN:
        if not department_id:
            if user_dept_ids:
                department_id = user_dept_ids[0]
        elif int(department_id) not in user_dept_ids:
            department_id = user_dept_ids[0] if user_dept_ids else None

    coverage = svc.get_knowledge_coverage(start_date=start_date, end_date=end_date, department_id=department_id)
    feedback = svc.get_feedback_stats(start_date=start_date, end_date=end_date, department_id=department_id)
    activity = svc.get_user_activity(days=7)
    article_views = svc.get_article_view_stats(start_date=start_date, end_date=end_date, department_id=department_id)

    context = {
        'time_range': time_range,
        'start_date': start_date,
        'end_date': end_date,
        'department_id': department_id,
        'coverage': coverage,
        'feedback': feedback,
        'activity': activity,
        'article_views': article_views,
        'is_super_admin': user.role == UserProfile.Role.SUPER_ADMIN,
    }

    return render(request, 'stats/dashboard.html', context)
