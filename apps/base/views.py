from django.shortcuts import render, redirect, get_object_or_404
from django.contrib.auth.decorators import login_required
from django.contrib import messages
from django.http import JsonResponse, Http404
from django.conf import settings
from django.db import connection
from django.core.cache import cache
from .decorators import admin_required, department_admin_required
from apps.service_container import container
from .models import Department, UserDepartment, DepartmentAuditLog
from apps.auth.models import UserProfile

import time


def health_check(request):
    checks = {
        "status": "healthy",
        "timestamp": int(time.time()),
        "checks": {},
    }

    db_ok = _check_database()
    checks["checks"]["database"] = db_ok

    redis_ok = _check_redis()
    checks["checks"]["redis"] = redis_ok

    if not db_ok["ok"] or not redis_ok["ok"]:
        checks["status"] = "unhealthy"
        return JsonResponse(checks, status=503)

    return JsonResponse(checks, status=200)


def _check_database():
    try:
        connection.ensure_connection()
        with connection.cursor() as cursor:
            cursor.execute("SELECT 1")
            cursor.fetchone()
        return {"ok": True}
    except Exception as e:
        return {"ok": False, "error": str(e)}


def _check_redis():
    try:
        cache.set("_health_check", "ok", 5)
        value = cache.get("_health_check")
        if value == "ok":
            return {"ok": True}
        return {"ok": False, "error": "read/write mismatch"}
    except Exception as e:
        return {"ok": False, "error": str(e)}


def about_page(request):
    """关于项目页面"""
    return render(request, 'pages/about.html')


@login_required
def department_list(request):
    service = container.department_service
    user_depts = UserDepartment.objects.filter(
        user=request.user
    ).select_related('department')
    user_dept_info = [
        {
            'id': ud.department.id,
            'name': ud.department.name,
            'role': ud.role,
            'is_primary': ud.is_primary,
        }
        for ud in user_depts
    ]
    dept_ids = [d['id'] for d in user_dept_info]

    if not dept_ids and request.user.is_admin:
        departments = service.get_all_departments()
    else:
        departments = Department.objects.filter(id__in=dept_ids) if dept_ids else []

    page = int(request.GET.get('page', 1))
    limit = min(int(request.GET.get('limit', 20)), 100)
    total = len(departments) if isinstance(departments, list) else departments.count()
    departments = departments[(page - 1) * limit: page * limit]

    return render(request, 'departments/department_list.html', {
        'departments': departments,
        'user_departments': user_dept_info,
        'total': total,
        'page': page,
        'limit': limit,
        'has_next': page * limit < total,
        'has_prev': page > 1,
    })


@login_required
@admin_required
def department_create(request):
    service = container.department_service
    if request.method == 'POST':
        data = {
            'name': request.POST.get('name'),
            'tenant_code': request.POST.get('tenant_code', ''),
            'description': request.POST.get('description', ''),
            'config': {},
        }
        parent_id = request.POST.get('parent')
        if parent_id:
            parent = Department.objects.filter(id=parent_id).first()
            if parent:
                data['parent'] = parent
                data['parent_id'] = parent.id
        service.create_department(data, user=request.user)
        messages.success(request, '科室创建成功')
        return redirect('departments:department_list')
    return render(request, 'departments/department_form.html', {
        'parents': Department.objects.all(),
    })


@login_required
@admin_required
def department_edit(request, dept_id):
    service = container.department_service
    dept = get_object_or_404(Department, id=dept_id)
    if request.method == 'POST':
        data = {
            'name': request.POST.get('name'),
            'tenant_code': request.POST.get('tenant_code', ''),
            'description': request.POST.get('description', ''),
        }
        parent_id = request.POST.get('parent')
        if parent_id:
            parent = Department.objects.filter(id=parent_id).first()
            if parent and parent.id != dept.id:
                data['parent'] = parent
        else:
            data['parent'] = None
        service.update_department(dept_id, data, user=request.user)
        messages.success(request, '科室信息已更新')
        return redirect('departments:department_list')
    return render(request, 'departments/department_form.html', {
        'department': dept,
        'parents': Department.objects.exclude(id=dept.id),
    })


@login_required
@admin_required
def department_delete(request, dept_id):
    service = container.department_service
    service.delete_department(dept_id, user=request.user)
    messages.success(request, '科室已删除')
    return redirect('departments:department_list')


@login_required
@department_admin_required('dept_id')
def department_member_add(request, dept_id):
    service = container.department_service
    dept = get_object_or_404(Department, id=dept_id)
    if request.method == 'POST':
        user_id = request.POST.get('user_id')
        role = request.POST.get('role', UserDepartment.Role.MEMBER)
        is_primary = request.POST.get('is_primary') == 'on'
        service.add_member(dept_id, user_id, role, is_primary, actor=request.user)
        messages.success(request, '成员已添加')
        return redirect('departments:department_list')
    users = UserProfile.objects.exclude(
        id__in=UserDepartment.objects.filter(
            department_id=dept_id
        ).values_list('user_id', flat=True)
    )
    page = int(request.GET.get('page', 1))
    limit = min(int(request.GET.get('limit', 20)), 100)
    total = users.count()
    users = users[(page - 1) * limit: page * limit]
    return render(request, 'departments/department_members.html', {
        'department': dept,
        'users': users,
        'total': total,
        'page': page,
        'limit': limit,
        'has_next': page * limit < total,
        'has_prev': page > 1,
    })


@login_required
@department_admin_required('dept_id')
def department_member_remove(request, dept_id, user_id):
    service = container.department_service
    service.remove_member(dept_id, user_id, actor=request.user)
    messages.success(request, '成员已移除')
    return redirect('departments:department_list')


@login_required
@department_admin_required('dept_id')
def department_config(request, dept_id):
    service = container.department_service
    dept = get_object_or_404(Department, id=dept_id)
    if request.method == 'POST':
        import json
        config_data = request.POST.get('config')
        if config_data:
            try:
                config = json.loads(config_data)
            except (json.JSONDecodeError, TypeError):
                config = {'welcome_msg': config_data}
        else:
            config = {}
            welcome_msg = request.POST.get('welcome_msg', '').strip()
            if welcome_msg:
                config['welcome_msg'] = welcome_msg
            ai_enabled = request.POST.get('ai_enabled')
            if ai_enabled:
                config['ai_enabled'] = ai_enabled == 'true' or ai_enabled == 'on'
        service.update_department(dept_id, {'config': config}, user=request.user)
        messages.success(request, '配置已更新')
        return redirect('departments:department_config', dept_id=dept_id)
    return render(request, 'departments/department_config.html', {
        'department': dept,
    })


@login_required
@department_admin_required('dept_id')
def department_references(request, dept_id):
    dept = get_object_or_404(Department, id=dept_id)
    ref_service = container.article_reference_service
    
    if request.method == 'POST':
        action = request.POST.get('action')
        article_id = request.POST.get('article_id')
        
        if action == 'request':
            success, msg = ref_service.reference_article(
                article_id, dept_id, user=request.user
            )
            if success:
                messages.success(request, msg)
            else:
                messages.error(request, msg)
        elif action == 'approve':
            success, msg = ref_service.approve_reference(
                article_id, dept_id, user=request.user
            )
            if success:
                messages.success(request, msg)
            else:
                messages.error(request, msg)
        elif action == 'reject':
            reason = request.POST.get('reason', '')
            success, msg = ref_service.reject_reference(
                article_id, dept_id, reason=reason, user=request.user
            )
            if success:
                messages.success(request, msg)
            else:
                messages.error(request, msg)
        
        return redirect('departments:department_references', dept_id=dept_id)
    
    available_articles = ref_service.get_available_public_articles(dept_id)
    from apps.wiki.reference.models import ArticleReference, ArticleReferenceStatus
    all_refs = ArticleReference.objects.filter(
        target_department=dept
    ).select_related('source_article', 'authorized_by').order_by('-created_at')

    page = int(request.GET.get('page', 1))
    limit = min(int(request.GET.get('limit', 20)), 100)
    total = all_refs.count()
    all_refs = all_refs[(page - 1) * limit: page * limit]

    pending_refs = [r for r in all_refs if r.status == ArticleReferenceStatus.PENDING]
    approved_refs = [r for r in all_refs if r.status == ArticleReferenceStatus.APPROVED]
    rejected_refs = [r for r in all_refs if r.status == ArticleReferenceStatus.REJECTED]

    return render(request, 'departments/department_references.html', {
        'department': dept,
        'available_articles': available_articles,
        'pending_refs': pending_refs,
        'approved_refs': approved_refs,
        'rejected_refs': rejected_refs,
        'total': total,
        'page': page,
        'limit': limit,
        'has_next': page * limit < total,
        'has_prev': page > 1,
    })


@login_required
@department_admin_required('dept_id')
def department_audit_logs(request, dept_id):
    dept = get_object_or_404(Department, id=dept_id)
    audit_service = container.audit_log_service
    
    page = int(request.GET.get('page', 1))
    limit = min(int(request.GET.get('limit', 20)), 100)
    action_type = request.GET.get('action_type', '')
    
    logs_qs = DepartmentAuditLog.objects.filter(department=dept)
    
    if action_type:
        logs_qs = logs_qs.filter(action_type=action_type)
    
    total = logs_qs.count()
    logs = logs_qs[(page - 1) * limit: page * limit]
    
    return render(request, 'departments/department_audit_logs.html', {
        'department': dept,
        'logs': logs,
        'total': total,
        'page': page,
        'limit': limit,
        'action_type': action_type,
        'has_next': page * limit < total,
        'has_prev': page > 1,
    })


@login_required
@department_admin_required('dept_id')
def department_members_list(request, dept_id):
    dept = get_object_or_404(Department, id=dept_id)
    service = container.department_service
    
    members = service.get_members(dept_id)

    role_order = {
        UserDepartment.Role.ADMIN: 0,
        UserDepartment.Role.MEMBER: 1,
        UserDepartment.Role.VIEWER: 2,
    }
    members.sort(key=lambda m: role_order.get(m['role'], 99))

    page = int(request.GET.get('page', 1))
    limit = min(int(request.GET.get('limit', 20)), 100)
    total = len(members)
    members = members[(page - 1) * limit: page * limit]

    return render(request, 'departments/department_members_list.html', {
        'department': dept,
        'members': members,
        'total': total,
        'page': page,
        'limit': limit,
        'has_next': page * limit < total,
        'has_prev': page > 1,
    })


@login_required
@admin_required
def department_tree(request):
    service = container.department_service
    tree = service.get_department_tree()
    return render(request, 'departments/department_tree.html', {
        'tree': tree,
    })


@login_required
@admin_required
def user_audit_logs(request, user_id):
    user = get_object_or_404(UserProfile, id=user_id)
    
    page = int(request.GET.get('page', 1))
    limit = min(int(request.GET.get('limit', 20)), 100)

    logs_qs = DepartmentAuditLog.objects.filter(
        performed_by_id=user_id
    ).order_by('-created_at')

    total = logs_qs.count()
    logs = logs_qs[(page - 1) * limit: page * limit]
    
    return render(request, 'departments/user_audit_logs.html', {
        'target_user': user,
        'logs': logs,
        'total': total,
        'page': page,
        'limit': limit,
        'has_next': page * limit < total,
        'has_prev': page > 1,
    })


def test_accounts_page(request):
    if not getattr(settings, 'SHOW_TEST_ACCOUNTS_PAGE', False):
        raise Http404("页面不存在")
    test_accounts = getattr(settings, 'TEST_ACCOUNTS', {})
    return render(request, 'test_accounts.html', {'accounts': test_accounts})
