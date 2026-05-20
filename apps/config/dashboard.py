"""
配置总览 Dashboard - 展示所有配置的实际运行值
"""
from django.contrib import admin
from django.contrib.admin.views.decorators import staff_member_required
from django.shortcuts import render
from django.urls import path
from django.utils.safestring import mark_safe
from django.utils.translation import gettext_lazy as _
from functools import wraps

from apps.config.models import (
    LLMProviderConfig,
    EmbeddingProviderConfig,
    BrandConfig,
    RAGConfig,
    RateLimitRule,
    SafetyRule,
    SensitiveWord,
    SystemConfig,
)


def _dashboard_context(request):
    """构建配置总览的上下文"""
    return {
        **admin.site.each_context(request),
        'title': '配置总览',
        'llm_configs': LLMProviderConfig.objects.all().values('pk', 'name', 'model_name', 'provider', 'api_key', 'base_url', 'is_active'),
        'embedding_configs': EmbeddingProviderConfig.objects.all().values('pk', 'name', 'model_name', 'provider', 'api_key', 'base_url', 'dimensions', 'is_active'),
        'brand_configs': BrandConfig.objects.all().order_by('key'),
        'rag_configs': RAGConfig.objects.all().order_by('category', 'config_key'),
        'system_configs': SystemConfig.objects.all().order_by('category', 'config_key'),
        'safety_rules': SafetyRule.objects.all().order_by('rule_type', 'rule_key'),
        'sensitive_words': SensitiveWord.objects.all().order_by('category', 'word'),
        'rate_limit_rules': RateLimitRule.objects.all().order_by('rule_type', 'name'),
    }


def _config_dashboard_view(request):
    """配置总览页面视图"""
    context = _dashboard_context(request)
    return render(request, 'admin/config/config_dashboard.html', context)


def _wrap_dashboard_view(admin_site):
    """包装视图函数以应用 admin_site 权限"""
    wrapped = staff_member_required(_config_dashboard_view)
    return wraps(_config_dashboard_view)(
        lambda r: admin_site.admin_view(wrapped)(r)
    )


_original_get_urls = admin.AdminSite.get_urls


def _patched_get_urls(self):
    """扩展 AdminSite.get_urls 以添加配置总览视图"""
    urls = _original_get_urls(self)

    custom_urls = [
        path(
            'config-dashboard/',
            _wrap_dashboard_view(self),
            name='config-dashboard',
        ),
    ]
    return custom_urls + urls


_original_index = admin.AdminSite.index


def _patched_index(self, request, extra_context=None):
    """扩展 Admin 首页，添加配置总览入口卡片"""
    extra_context = extra_context or {}
    extra_context['config_dashboard_url'] = 'admin:config-dashboard'
    return _original_index(self, request, extra_context)


admin.AdminSite.get_urls = _patched_get_urls
admin.AdminSite.index = _patched_index
