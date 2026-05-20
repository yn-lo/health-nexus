from django.contrib import admin
from unfold.admin import ModelAdmin
from apps.config.models import ConfigAuditLog


@admin.register(ConfigAuditLog)
class ConfigAuditLogAdmin(ModelAdmin):
    list_display = ['action_type', 'config_model', 'config_target', 'performed_by', 'created_at']
    list_filter = ['action_type', 'config_model', 'created_at']
    search_fields = ['config_model', 'config_target', 'performed_by']
    ordering = ['-created_at']
    readonly_fields = ['action_type', 'config_model', 'config_target', 'performed_by', 'old_values', 'new_values', 'created_at']

    fieldsets = (
        ('操作信息', {
            'fields': ('action_type', 'config_model', 'config_target', 'performed_by', 'created_at')
        }),
        ('修改前的值', {
            'fields': ('old_values',),
            'classes': ('collapse',),
        }),
        ('修改后的值', {
            'fields': ('new_values',),
            'classes': ('collapse',),
        }),
    )
