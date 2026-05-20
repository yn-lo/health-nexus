from django import forms
from django.contrib import admin
from unfold.admin import ModelAdmin
from apps.config.models import RAGConfig
from apps.config.admin.base import AuditTrackingAdminMixin


class RAGConfigForm(forms.ModelForm):
    """RAG 配置表单"""

    class Meta:
        model = RAGConfig
        fields = '__all__'

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        if 'category' in self.fields:
            self.fields['category'].help_text = '配置所属的功能分类'
        if 'config_value' in self.fields:
            self.fields['config_value'].help_text = '配置的数值，应为数字类型'
        if 'min_value' in self.fields:
            self.fields['min_value'].help_text = '可选：最小允许值，用于范围校验'
        if 'max_value' in self.fields:
            self.fields['max_value'].help_text = '可选：最大允许值，用于范围校验'


@admin.register(RAGConfig)
class RAGConfigAdmin(AuditTrackingAdminMixin, ModelAdmin):
    list_display = ['display_name', 'config_key', 'config_value', 'category', 'is_active']
    list_filter = ['category', 'is_active']
    search_fields = ['config_key', 'description']
    list_editable = ['is_active']
    ordering = ['category', 'config_key']
    form = RAGConfigForm
    fieldsets = (
        ('配置项', {
            'fields': ('config_key', 'config_value', 'category'),
            'description': 'RAG 配置的标识由系统定义，不可修改。仅可修改配置值和启用状态。'
        }),
        ('范围限制', {
            'fields': ('min_value', 'max_value'),
            'description': '可选：设置配置值的最小和最大值，用于校验输入范围'
        }),
        ('状态', {
            'fields': ('description', 'is_active'),
            'description': '配置的说明信息和启用状态'
        }),
    )

    @admin.display(description='配置名称', ordering='config_key')
    def display_name(self, obj):
        return obj.display_name

    def get_readonly_fields(self, request, obj=None):
        """RAG 配置的 config_key 不可修改"""
        return ('config_key', 'category')

    def has_add_permission(self, request):
        """禁止新增，RAG 配置集合由系统定义"""
        return False

    def has_delete_permission(self, request, obj=None):
        """禁止删除，RAG 配置集合由系统定义"""
        return False

    def save_model(self, request, obj, form, change):
        super().save_model(request, obj, form, change)
