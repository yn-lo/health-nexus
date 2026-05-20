"""
自定义错误页面处理器 - §4.4

实现 403/404/429/500 错误页面处理。
提供可直接通过 URL 访问的视图函数，供 E2E 测试使用。
"""
import logging
from django.shortcuts import render

logger = logging.getLogger(__name__)


def custom_handler404(request, exception=None):
    """404 页面未找到"""
    return render(request, 'errors/404.html', status=404)


def custom_handler403(request, exception=None):
    """403 权限错误"""
    username = 'anonymous'
    if hasattr(request, 'user') and request.user is not None:
        username = getattr(request.user, 'username', 'anonymous')
    logger.warning(f"403 Forbidden: {request.path} (user: {username})")
    return render(request, 'errors/403.html', status=403)


def custom_handler429(request, exception=None):
    """429 请求过于频繁"""
    retry_after = request.GET.get('retry_after', '60') if request.GET else '60'
    logger.warning(f"429 Too Many Requests: {request.path}")
    return render(request, 'errors/429.html', {'retry_after': retry_after}, status=429)


def custom_handler500(request):
    """500 服务器错误"""
    logger.error(f"500 Internal Server Error: {request.path}")
    return render(request, 'errors/500.html', status=500)


def error_page_404(request):
    """可直接访问的 404 页面预览（用于 E2E 测试）"""
    return custom_handler404(request)


def error_page_403(request):
    """可直接访问的 403 页面预览（用于 E2E 测试）"""
    return custom_handler403(request)


def error_page_429(request):
    """可直接访问的 429 页面预览（用于 E2E 测试）"""
    return custom_handler429(request)


def error_page_500(request):
    """可直接访问的 500 页面预览（用于 E2E 测试）"""
    return custom_handler500(request)
