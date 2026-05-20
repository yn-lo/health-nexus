from django.contrib import messages
from django.shortcuts import redirect
from django.urls import reverse
from django.utils.translation import gettext_lazy as _


class SingleSessionMiddleware:
    """单设备单会话中间件，新登录会使旧会话失效"""

    CACHE_PREFIX = 'session_key:'
    CACHE_TTL = 300  # 5 minutes

    def __init__(self, get_response):
        self.get_response = get_response

    def _get_stored_session_key(self, user_id):
        """从 Redis 缓存获取 stored session key，未命中则查数据库"""
        from django.core.cache import cache
        from apps.auth.models import UserProfile

        cache_key = f'{self.CACHE_PREFIX}{user_id}'
        stored_key = cache.get(cache_key)
        if stored_key is not None:
            return stored_key

        stored_key = UserProfile.objects.filter(
            pk=user_id
        ).values_list('current_session_key', flat=True).first()

        cache.set(cache_key, stored_key, self.CACHE_TTL)
        return stored_key

    @staticmethod
    def invalidate_session_cache(user_id):
        """清除指定用户的 session key 缓存"""
        from django.core.cache import cache
        cache_key = f'{SingleSessionMiddleware.CACHE_PREFIX}{user_id}'
        cache.delete(cache_key)

    def __call__(self, request):
        if request.user.is_authenticated:
            current_key = request.session.session_key
            stored_key = self._get_stored_session_key(request.user.pk)
            exempt_paths = ['/accounts/patient-login/', '/accounts/staff-login/', '/admin/login/', '/static/']

            if stored_key and current_key != stored_key and not any(request.path.startswith(p) for p in exempt_paths):
                messages.info(request, _("您的账号已在其他设备登录，请重新登录"))
                from django.contrib.auth import logout as auth_logout
                auth_logout(request)
                return redirect('auth:login')

            session_role = request.session.get('user_role')
            if session_role and session_role != request.user.role:
                user_role = request.user.role
                messages.info(request, _("会话状态异常，请重新登录"))
                from django.contrib.auth import logout as auth_logout
                auth_logout(request)
                if user_role == 'PATIENT':
                    return redirect('auth:patient_login')
                elif user_role in ('DOCTOR', 'NURSE', 'SUPER_ADMIN', 'DEPT_ADMIN'):
                    return redirect('auth:staff_login')
                return redirect('auth:login')

        return self.get_response(request)


class TermsAgreementMiddleware:
    def __init__(self, get_response):
        self.get_response = get_response

    def __call__(self, request):
        if (
            request.user.is_authenticated
            and not request.user.agreed_terms
            and request.user.is_patient
        ):
            exempt_paths = [
                reverse('auth:terms_agreement'),
                reverse('auth:logout'),
                reverse('auth:password_change'),
                '/admin/',
                '/static/',
                '/accounts/bind/',
            ]
            if not any(request.path.startswith(p) for p in exempt_paths):
                return redirect('auth:terms_agreement')

        return self.get_response(request)


class RoleBasedRouteMiddleware:
    """基于角色的路由中间件，拦截特定路径的非授权访问"""

    def __init__(self, get_response):
        self.get_response = get_response

    def __call__(self, request):
        response = self.get_response(request)
        return response

    def process_view(self, request, view_func, view_args, view_kwargs):
        path = request.path

        if request.user.is_authenticated and request.user.is_patient:
            if path.startswith('/admin/') and not path.startswith('/admin/login/'):
                messages.error(request, _("患者无权访问管理后台"))
                return redirect('/chat/')

        if request.user.is_authenticated and request.user.is_admin:
            user_side_paths = ['/chat/', '/profile/', '/blog/', '/about/']
            staff_paths = ['/blog/staff/']
            if any(path.startswith(p) for p in staff_paths):
                pass
            elif any(path.startswith(p) for p in user_side_paths):
                messages.info(request, "管理员请使用管理后台访问")
                return redirect('/admin/')

        if path.startswith('/staff/'):
            if not request.user.is_authenticated:
                return None

            if request.user.is_patient:
                messages.error(request, "患者请使用患者端登录")
                return redirect('/chat/')

            if not request.user.can_access_staff_dashboard:
                messages.error(request, "无权限访问此页面")
                return redirect('auth:login')

        if path.startswith('/accounts/'):
            if not request.user.is_authenticated:
                return None

            if path.startswith('/accounts/bind/'):
                return None

            if request.user.is_patient:
                return None

            staff_allowed_paths = [
                '/accounts/password-change/',
                '/accounts/password-reset/',
                '/accounts/logout/',
                '/accounts/staff-login/',
                '/accounts/login/',
            ]
            if any(path.startswith(p) for p in staff_allowed_paths):
                return None

            messages.error(request, "请通过医护端访问")
            return redirect('staff:dashboard')

        return None


class RolePermissionMiddleware:
    def __init__(self, get_response):
        self.get_response = get_response

    def __call__(self, request):
        request.user_role = None
        request.is_medical_staff = False
        request.is_admin = False

        if request.user.is_authenticated:
            request.user_role = request.user.role
            request.is_medical_staff = request.user.is_medical_staff
            request.is_admin = request.user.is_admin

        return self.get_response(request)
