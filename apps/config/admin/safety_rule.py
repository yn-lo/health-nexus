from django import forms
from django.contrib import admin
from unfold.admin import ModelAdmin
from unfold.widgets import UnfoldAdminTextareaWidget
from apps.config.models import SafetyRule
import json


class SafetyRuleForm(forms.ModelForm):
    """安全规则增强表单 - 根据规则类型显示对应的 JSON 格式提示"""
    
    RULE_VALUE_HELP = {
        'DANGEROUS_OUTPUT': '请输入 JSON 数组格式，包含需要检测的正则表达式模式。示例: ["危险词1|危险词2", "正则模式.*"]',
        'SIMILARITY_THRESHOLD': '请输入 JSON 对象格式，包含阈值和度量方式。示例: {"threshold": 0.85, "metric": "cosine"}',
        'REJECTION_MESSAGE': '请输入 JSON 数组格式，包含多条拒答话术。示例: ["抱歉，我无法回答这个问题", "这超出了我的能力范围"]',
        'EMERGENCY_RESPONSE': '请输入 JSON 对象格式，包含紧急提示消息和建议动作。示例: {"message": "请立即就医或拨打120", "action": "紧急联系"}',
        'SAFETY_WARNING': '请输入 JSON 对象格式，包含警告内容和级别。示例: {"warning": "请注意，此信息仅供参考", "level": "medium"}',
    }

    rule_value_str = forms.CharField(
        label='规则值',
        widget=UnfoldAdminTextareaWidget,
        help_text='JSON 格式，请根据上方选择的规则类型填写对应格式',
    )

    class Meta:
        model = SafetyRule
        fields = '__all__'
        exclude = ('rule_value',)

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        if self.instance and self.instance.pk and self.instance.rule_type:
            help_text = self.RULE_VALUE_HELP.get(self.instance.rule_type, '')
            self.fields['rule_value_str'].help_text = help_text
            try:
                self.fields['rule_value_str'].initial = json.dumps(self.instance.rule_value, ensure_ascii=False, indent=2)
            except (TypeError, ValueError):
                self.fields['rule_value_str'].initial = ''
        else:
            self.fields['rule_value_str'].help_text = '请先选择规则类型，将显示对应的 JSON 格式示例'

    def clean_rule_value_str(self):
        value = self.cleaned_data['rule_value_str']
        try:
            return json.loads(value)
        except json.JSONDecodeError as e:
            raise forms.ValidationError(f'JSON 格式错误: {e}')

    def save(self, commit=True):
        instance = super().save(commit=False)
        instance.rule_value = self.cleaned_data['rule_value_str']
        if commit:
            instance.save()
        return instance


@admin.register(SafetyRule)
class SafetyRuleAdmin(ModelAdmin):
    list_display = ['rule_type', 'rule_key', 'is_active', 'updated_at']
    list_filter = ['rule_type', 'is_active']
    search_fields = ['rule_key', 'description']
    list_editable = ['is_active']
    form = SafetyRuleForm
    fieldsets = (
        ('规则定义', {
            'fields': ('rule_type', 'rule_key'),
            'description': '选择安全规则类型并为其命名。规则类型决定了规则的作用方式和值的格式。'
        }),
        ('规则值', {
            'fields': ('rule_value_str',),
            'description': '根据规则类型填写对应的 JSON 格式。请参考下方的格式说明。'
        }),
        ('附加信息', {
            'fields': ('description', 'is_active'),
            'description': '规则的说明信息和启用状态。'
        }),
    )
    formfield_overrides = {
        SafetyRule._meta.get_field('rule_type'): {
            'help_text': '危险输出模式：检测危险关键词；相似度阈值：设置最低相似度要求；拒答话术：配置拒答时的回复；紧急响应话术：配置紧急情况下的提示；安全警告话术：配置警告信息。'
        },
        SafetyRule._meta.get_field('rule_key'): {
            'help_text': '规则的唯一标识，建议使用英文和下划线。例如: suicide_keywords, medical_emergency_pattern。'
        },
    }

    def has_add_permission(self, request):
        """禁止新增，安全规则集合由系统定义"""
        return False

    def has_delete_permission(self, request, obj=None):
        """禁止删除，安全规则集合由系统定义"""
        return False
