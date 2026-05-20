from django.contrib import admin
from unfold.admin import ModelAdmin
from apps.config.models import BrandConfig


@admin.register(BrandConfig)
class BrandConfigAdmin(ModelAdmin):
    list_display = ['display_name', 'key', 'value', 'is_active']
    list_filter = ['is_active']
    search_fields = ['key', 'value']
    list_editable = ['is_active']
    ordering = ['key']
    formfield_overrides = {
        BrandConfig._meta.get_field('key'): {
            'help_text': '配置项的唯一标识，由系统定义，不可修改。'
        },
        BrandConfig._meta.get_field('value'): {
            'help_text': '配置的值。可以是文本、URL、颜色代码等。例如: "Health Nexus", "/static/logo.png"。'
        },
        BrandConfig._meta.get_field('is_active'): {
            'help_text': '启用/停用该配置。停用的配置不会在前端显示。'
        },
    }

    @admin.display(description='配置名称', ordering='key')
    def display_name(self, obj):
        return obj.display_name

    def get_readonly_fields(self, request, obj=None):
        """品牌配置的 key 不可修改"""
        return ('key',)

    def has_add_permission(self, request):
        """禁止新增，品牌配置集合由系统定义"""
        return False

    def has_delete_permission(self, request, obj=None):
        """禁止删除，品牌配置集合由系统定义"""
        return False
