from django import forms
from django.contrib import admin, messages
from unfold.admin import ModelAdmin
from apps.config.models import PromptTemplate
from apps.config.admin.base import AuditTrackingAdminMixin


class PromptTemplateForm(forms.ModelForm):

    class Meta:
        model = PromptTemplate
        fields = '__all__'

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        if 'content' in self.fields:
            self.fields['content'].help_text = '支持变量替换，如 {{patient_context}}、{{department}}'


@admin.register(PromptTemplate)
class PromptTemplateAdmin(AuditTrackingAdminMixin, ModelAdmin):
    list_display = ['name', 'is_default', 'is_active', 'updated_at']
    list_filter = ['is_default', 'is_active']
    search_fields = ['name', 'description']
    list_editable = ['is_active']
    form = PromptTemplateForm
    fieldsets = (
        ('模板信息', {
            'fields': ('name', 'description'),
            'description': '设置模板的名称和说明'
        }),
        ('模板内容', {
            'fields': ('content',),
            'description': 'Prompt 模板内容。支持变量替换：{{patient_context}}（患者上下文）、{{department}}（科室信息）'
        }),
        ('状态', {
            'fields': ('is_default', 'is_active'),
            'description': '活跃模板：设为活跃后，AI 问答将使用此模板。同一时刻只能有一个活跃模板。'
        }),
    )

    def has_module_permission(self, request):
        return request.user.is_superuser

    def has_view_permission(self, request, obj=None):
        return request.user.is_superuser

    def has_add_permission(self, request):
        return request.user.is_superuser

    def has_change_permission(self, request, obj=None):
        return request.user.is_superuser

    def has_delete_permission(self, request, obj=None):
        return request.user.is_superuser

    def save_model(self, request, obj, form, change):
        super().save_model(request, obj, form, change)
        if obj.is_default:
            messages.info(request, f"模板「{obj.name}」已设为活跃模板")
