from django import forms
from django.contrib import admin, messages
from unfold.admin import ModelAdmin
from apps.config.models import SystemConfig
from apps.config.admin.base import AuditTrackingAdminMixin


@admin.register(SystemConfig)
class SystemConfigAdmin(AuditTrackingAdminMixin, ModelAdmin):
    list_display = ['category', 'display_name', 'config_key', 'config_value_short', 'requires_restart', 'updated_at']
    list_filter = ['category', 'requires_restart']
    search_fields = ['config_key', 'description']
    ordering = ['category', 'config_key']
    fieldsets = (
        ('基本信息', {
            'fields': ('category', 'config_key', 'description'),
            'description': '选择配置分类和键名。系统配置的标识由系统定义，不可修改。'
        }),
        ('配置值', {
            'fields': ('config_value',),
            'description': 'JSON 格式值，例如: {"timeout": 300}, "string_value", true, 123'
        }),
        ('选项', {
            'fields': ('is_encrypted', 'requires_restart'),
            'description': '配置行为选项'
        }),
    )

    @admin.display(description='配置值', ordering='config_value')
    def config_value_short(self, obj):
        """截断显示配置值"""
        val = str(obj.config_value)
        if len(val) > 50:
            return val[:47] + '...'
        return val

    def get_readonly_fields(self, request, obj=None):
        """系统配置的 config_key 不可修改"""
        return ('config_key', 'category')

    def has_add_permission(self, request):
        """禁止新增，系统配置集合由系统定义"""
        return False

    def has_delete_permission(self, request, obj=None):
        """禁止删除，系统配置集合由系统定义"""
        return False

    def save_model(self, request, obj, form, change):
        super().save_model(request, obj, form, change)
        if obj.requires_restart:
            messages.warning(request, "此配置需重启服务才能生效")
