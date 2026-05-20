"""
URL configuration for health_nexus project.
"""
from django.conf import settings
from django.conf.urls.static import static
from django.contrib import admin
from django.shortcuts import redirect
from django.urls import path, include
from django.views.generic import RedirectView

from .error_handlers import (
    custom_handler404, custom_handler403, custom_handler429, custom_handler500,
    error_page_404, error_page_403, error_page_429, error_page_500,
)
from apps.base.views import health_check, test_accounts_page
from apps.base.urls import departments_urls as department_urlpatterns
from apps.auth.urls import staff_urlpatterns

handler404 = custom_handler404
handler403 = custom_handler403
handler429 = custom_handler429
handler500 = custom_handler500


# 覆盖 Django Admin 登录重定向行为
_original_admin_login = admin.AdminSite.login


def _patched_admin_login(self, request, extra_context=None):
    """
    管理员登录成功后，固定跳转到 /admin/ 而不是遵循 next 参数或全局 LOGIN_REDIRECT_URL。
    防止管理员通过 next 参数被重定向到用户端/医护端页面。
    """
    # 清除 next 参数，防止 Django admin 遵循它
    if request.method == 'POST':
        # 在调用原始登录方法前，移除 POST 数据中的 next 参数
        if hasattr(request, 'POST') and 'next' in request.POST:
            request.POST = request.POST.copy()
            request.POST.pop('next', None)
        # 同时清除 GET 中的 next
        if hasattr(request, 'GET') and 'next' in request.GET:
            request.GET = request.GET.copy()
            request.GET.pop('next', None)
    
    response = _original_admin_login(self, request, extra_context)
    
    # 如果登录成功（302 重定向），强制跳转到 /admin/
    if response.status_code == 302:
        return redirect('/admin/')
    
    return response


admin.AdminSite.login = _patched_admin_login


# Admin Site Config
admin.site.site_header = "Health Nexus 健康宣教系统"
admin.site.site_title = "Health Nexus 后台管理"
admin.site.index_title = "系统管理面板"

urlpatterns = [
    path('health/', health_check, name='health-check'),
    path('', RedirectView.as_view(url='/chat/', permanent=False)),
    
    path('admin/', admin.site.urls),
    
    path('chat/', include('apps.chat.urls')),
    path('staff/', include((staff_urlpatterns, 'staff'), namespace='staff')),
    path('blog/', include('apps.wiki.urls')),
    path('profile/', include('apps.care.urls')),
    path('about/', include('apps.base.urls')),
    path('departments/', include((department_urlpatterns, 'departments'), namespace='departments')),
    path('stats/', include(('apps.stats.urls', 'stats'), namespace='stats')),
    
    path('accounts/', include('apps.auth.urls')),
    path('test-accounts/', test_accounts_page, name='test-accounts'),
    path('error/404/', error_page_404, name='error-404'),
    path('error/403/', error_page_403, name='error-403'),
    path('error/429/', error_page_429, name='error-429'),
    path('error/500/', error_page_500, name='error-500'),
]

if settings.DEBUG:
    urlpatterns += static(settings.MEDIA_URL, document_root=settings.MEDIA_ROOT)
    urlpatterns += static(settings.STATIC_URL, document_root=settings.STATIC_ROOT)
