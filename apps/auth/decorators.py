from functools import wraps
from urllib.parse import urlencode

from django.contrib import messages
from django.contrib.auth import REDIRECT_FIELD_NAME
from django.contrib.auth.decorators import user_passes_test
from django.shortcuts import redirect, resolve_url
from django.utils.http import url_has_allowed_host_and_scheme


def _get_login_url_for_role(user=None):
    """根据用户角色返回合适的登录 URL 名称"""
    if user and user.is_authenticated:
        if user.is_admin:
            return '/admin/'
        elif user.can_access_staff_dashboard:
            return 'auth:staff_login'
    return 'auth:login'


def role_aware_login_required(function=None, redirect_field_name=REDIRECT_FIELD_NAME, login_url=None):
    """替代 @login_required 的装饰器：根据访问路径和用户角色智能选择登录入口

    未认证用户：
      - 访问 /staff/ 路径 → 医护登录页
      - 访问 /admin/ 路径 → 管理员登录页
      - 其他路径 → 统一登录页（默认患者端）

    已认证但角色不匹配：
      - 患者访问医护端 → 重定向到患者端首页
      - 医护访问患者端 → 重定向到医护端首页
    """
    actual_decorator = user_passes_test(
        lambda u: u.is_authenticated,
        login_url=login_url,
        redirect_field_name=redirect_field_name,
    )

    if function:
        @wraps(function)
        def wrapper(request, *args, **kwargs):
            if not request.user.is_authenticated:
                path = request.path
                if path.startswith('/staff/'):
                    target_url = 'auth:staff_login'
                elif path.startswith('/admin/'):
                    target_url = '/admin/login/'
                else:
                    target_url = 'auth:login'

                resolved = resolve_url(target_url)
                if redirect_field_name:
                    query_string = urlencode({redirect_field_name: request.get_full_path()})
                    return redirect(f'{resolved}?{query_string}')
                return redirect(target_url)

            user = request.user

            if request.path.startswith('/staff/') and not user.can_access_staff_dashboard:
                if user.is_patient:
                    messages.error(request, "患者请使用患者端登录")
                    return redirect('auth:patient_login')
                messages.error(request, "无权限访问此页面")
                return redirect('auth:login')

            if request.path.startswith('/admin/') and not user.is_admin:
                messages.error(request, "管理员请使用管理后台登录")
                return redirect('/admin/')

            return function(request, *args, **kwargs)

        return wrapper
    return actual_decorator


def staff_member_required(view_func):
    @wraps(view_func)
    def _wrapped_view(request, *args, **kwargs):
        if not request.user.is_authenticated:
            return redirect('auth:staff_login')

        if not request.user.can_access_staff_dashboard:
            if request.user.is_patient:
                messages.error(request, "患者请使用患者端登录")
                return redirect('auth:patient_login')
            messages.error(request, "无权限访问此页面")
            return redirect('auth:login')

        return view_func(request, *args, **kwargs)
    return _wrapped_view


def patient_only_required(view_func):
    @wraps(view_func)
    def _wrapped_view(request, *args, **kwargs):
        if not request.user.is_authenticated:
            return redirect('auth:patient_login')

        if request.user.can_access_staff_dashboard:
            messages.info(request, "医护人员请使用医护端")
            return redirect('staff:profile')

        return view_func(request, *args, **kwargs)
    return _wrapped_view
