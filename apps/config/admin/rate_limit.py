from django import forms
from django.contrib import admin
from unfold.admin import ModelAdmin
from unfold.widgets import UnfoldAdminSelectWidget, UnfoldAdminCheckboxSelectMultiple
from apps.config.models import RateLimitRule


class RateLimitRuleForm(forms.ModelForm):
    """限流规则增强表单 - HTTP 方法多选和时间窗口下拉"""
    
    HTTP_METHODS_CHOICES = [
        ('GET', 'GET - 查询请求'),
        ('POST', 'POST - 创建请求'),
        ('PUT', 'PUT - 更新请求'),
        ('DELETE', 'DELETE - 删除请求'),
        ('PATCH', 'PATCH - 部分更新请求'),
    ]
    
    WINDOW_CHOICES = [
        (60, '1分钟'),
        (300, '5分钟'),
        (900, '15分钟'),
        (3600, '1小时'),
        (86400, '1天'),
    ]

    methods = forms.MultipleChoiceField(
        label='请求方法',
        choices=HTTP_METHODS_CHOICES,
        widget=UnfoldAdminCheckboxSelectMultiple,
        required=False,
        help_text='选择需要限流的 HTTP 方法。不选表示对所有方法限流',
    )
    
    window = forms.ChoiceField(
        label='时间窗口',
        choices=WINDOW_CHOICES,
        widget=UnfoldAdminSelectWidget,
        help_text='限流统计的时间周期。例如 60 表示 60 秒内的请求数限制',
    )

    class Meta:
        model = RateLimitRule
        fields = '__all__'

    def clean_methods(self):
        return self.cleaned_data['methods'] if self.cleaned_data['methods'] else []

    def clean_window(self):
        return int(self.cleaned_data['window'])


@admin.register(RateLimitRule)
class RateLimitRuleAdmin(ModelAdmin):
    list_display = ['display_name', 'rule_type', 'path', 'limit', 'window_display', 'is_active']
    list_filter = ['rule_type', 'is_active']
    search_fields = ['name', 'path']
    list_editable = ['is_active']
    form = RateLimitRuleForm
    fieldsets = (
        ('规则定义', {
            'fields': ('name', 'rule_type'),
            'description': '限流规则的标识由系统定义，不可修改。全局限流：anonymous_global（匿名用户）、authenticated_global（登录用户）、anonymous_chat（匿名聊天）。短信限流：ip、phone、attempts。'
        }),
        ('限流条件', {
            'fields': ('path', 'methods', 'limit', 'window'),
            'description': '配置限流的次数和时间窗口。路径限流可配置 path，全局限流和短信限流的 path 应为空。'
        }),
        ('状态', {
            'fields': ('description', 'is_active'),
            'description': '规则的说明信息和启用状态。禁用后该规则将不生效，系统会回退到代码默认值。'
        }),
    )
    formfield_overrides = {
        RateLimitRule._meta.get_field('name'): {
            'help_text': '规则的唯一标识，由系统定义，不可修改。'
        },
        RateLimitRule._meta.get_field('path'): {
            'help_text': '需要限流的 API 路径。例如: /api/chat。全局限流和短信限流时留空。'
        },
        RateLimitRule._meta.get_field('limit'): {
            'help_text': '在时间窗口内允许的最大请求次数。'
        },
    }

    @admin.display(description='规则名称', ordering='name')
    def display_name(self, obj):
        return obj.display_name

    @admin.display(description='时间窗口', ordering='window')
    def window_display(self, obj):
        windows = {60: '1分钟', 300: '5分钟', 900: '15分钟', 3600: '1小时', 86400: '1天'}
        return windows.get(obj.window, f'{obj.window}秒')

    def get_readonly_fields(self, request, obj=None):
        """限流规则的所有标识字段都不可修改"""
        return ('name', 'rule_type', 'path')

    def has_add_permission(self, request):
        """禁止新增，限流规则集合由系统定义"""
        return False

    def has_delete_permission(self, request, obj=None):
        """禁止删除，限流规则集合由系统定义"""
        return False
