from functools import wraps
from django.core.exceptions import PermissionDenied
from django.contrib import messages
from django.shortcuts import redirect
from apps.base.models import UserDepartment


def admin_required(view_func):
    @wraps(view_func)
    def _wrapped_view(request, *args, **kwargs):
        if not request.user.is_authenticated:
            raise PermissionDenied
        if not hasattr(request.user, 'is_admin') or not request.user.is_admin:
            raise PermissionDenied
        return view_func(request, *args, **kwargs)
    return _wrapped_view


def department_admin_required(dept_id_kwarg='dept_id'):
    """科室级管理员权限装饰器

    检查当前用户是否是指定科室的 ADMIN，或者是系统级 SUPER_ADMIN。
    用法: @department_admin_required('dept_id')
    """
    def decorator(view_func):
        @wraps(view_func)
        def _wrapped_view(request, *args, **kwargs):
            if not request.user.is_authenticated:
                raise PermissionDenied

            if request.user.role == 'SUPER_ADMIN':
                return view_func(request, *args, **kwargs)

            dept_id = kwargs.get(dept_id_kwarg)
            if not dept_id:
                raise PermissionDenied

            is_dept_admin = UserDepartment.objects.filter(
                user=request.user,
                department_id=dept_id,
                role=UserDepartment.Role.ADMIN,
            ).exists()

            if not is_dept_admin:
                if hasattr(request, '_messages'):
                    messages.error(request, "您不是该科室的管理员，无权执行此操作")
                    return redirect('departments:department_list')
                raise PermissionDenied

            return view_func(request, *args, **kwargs)
        return _wrapped_view
    return decorator
