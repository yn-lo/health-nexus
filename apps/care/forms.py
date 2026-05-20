from django import forms
from django.core.validators import MinValueValidator, MaxValueValidator
from django.utils.translation import gettext_lazy as _
from apps.care.models import PatientProfile, VitalSignRecord, LabTestRecord, ImagingRecord, MedicationRecord
import datetime


COMMON_DISEASES = [
    '高血压', '糖尿病', '冠心病', '慢阻肺', '脑卒中',
    '慢性肾病', '甲状腺疾病', '痛风', '哮喘', '其他',
]

COMMON_ALLERGIES = [
    '青霉素', '磺胺类', '头孢类', '阿司匹林', '碘造影剂',
    '花粉', '尘螨', '海鲜', '花生', '其他',
]


class BasicInfoForm(forms.ModelForm):
    """患者基础信息表单（姓名、性别、生日、联系方式等）"""

    class Meta:
        model = PatientProfile
        fields = ['name', 'birth_date', 'gender', 'phone', 'address', 'emergency_contact', 'emergency_phone']
        widgets = {
            'name': forms.TextInput(attrs={
                'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20',
            }),
            'birth_date': forms.DateInput(attrs={
                'type': 'date',
                'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20',
            }),
            'gender': forms.Select(attrs={
                'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20',
            }),
            'phone': forms.TextInput(attrs={
                'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20',
                'placeholder': '如：13800138000',
            }),
            'address': forms.TextInput(attrs={
                'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20',
                'placeholder': '如：北京市朝阳区XX路XX号',
            }),
            'emergency_contact': forms.TextInput(attrs={
                'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20',
                'placeholder': '如：张三（配偶）',
            }),
            'emergency_phone': forms.TextInput(attrs={
                'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20',
                'placeholder': '如：13900139000',
            }),
        }

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        if self.instance and self.instance.birth_date:
            self.fields['birth_date'].initial = self.instance.birth_date.strftime('%Y-%m-%d')


class PatientProfileH5Form(forms.ModelForm):
    """患者健康档案表单（身高体重、病史、过敏史等）"""

    class Meta:
        model = PatientProfile
        fields = [
            'height', 'weight', 'education', 'marital_status', 'occupation',
            'blood_type', 'medical_history_summary', 'allergies_summary',
        ]
        widgets = {
            'height': forms.NumberInput(attrs={
                'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20',
                'placeholder': '如：170',
                'step': '0.1',
            }),
            'weight': forms.NumberInput(attrs={
                'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20',
                'placeholder': '如：65',
                'step': '0.1',
            }),
            'education': forms.Select(attrs={
                'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20',
            }),
            'marital_status': forms.Select(attrs={
                'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20',
            }),
            'occupation': forms.TextInput(attrs={
                'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20',
                'placeholder': '如：教师、工程师',
            }),
            'blood_type': forms.Select(attrs={
                'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20',
            }),
            'medical_history_summary': forms.Textarea(attrs={
                'rows': 3,
                'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20',
            }),
            'allergies_summary': forms.Textarea(attrs={
                'rows': 2,
                'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20',
            }),
        }

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)


class VitalSignForm(forms.ModelForm):
    """V1 生命体征表单"""
    class Meta:
        model = VitalSignRecord
        fields = [
            'record_date', 'record_time', 'source',
            'systolic_bp', 'diastolic_bp', 'heart_rate', 'pulse',
            'temperature', 'respiratory_rate', 'blood_glucose', 'blood_oxygen',
            'weight', 'height', 'pain_score',
            'measurement_context', 'notes',
        ]
        widgets = {
            'record_date': forms.DateInput(attrs={'type': 'date', 'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20'}),
            'record_time': forms.TimeInput(attrs={'type': 'time', 'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20'}),
            'source': forms.TextInput(attrs={'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20', 'placeholder': _('如：家用血压计')}),
            'systolic_bp': forms.NumberInput(attrs={'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20', 'placeholder': '如：140'}),
            'diastolic_bp': forms.NumberInput(attrs={'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20', 'placeholder': '如：90'}),
            'heart_rate': forms.NumberInput(attrs={'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20', 'placeholder': '如：72'}),
            'pulse': forms.NumberInput(attrs={'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20', 'placeholder': '如：76'}),
            'temperature': forms.NumberInput(attrs={'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20', 'placeholder': '如：36.5', 'step': '0.1'}),
            'respiratory_rate': forms.NumberInput(attrs={'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20', 'placeholder': '如：16'}),
            'blood_glucose': forms.NumberInput(attrs={'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20', 'placeholder': '如：5.8', 'step': '0.1'}),
            'blood_oxygen': forms.NumberInput(attrs={'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20', 'placeholder': '如：98'}),
            'weight': forms.NumberInput(attrs={'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20', 'placeholder': '如：70.5', 'step': '0.1'}),
            'height': forms.NumberInput(attrs={'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20', 'placeholder': '如：175', 'step': '0.1'}),
            'pain_score': forms.NumberInput(attrs={'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20', 'placeholder': '0-10', 'min': '0', 'max': '10'}),
            'measurement_context': forms.Select(attrs={'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20'}),
            'notes': forms.Textarea(attrs={'rows': 2, 'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20'}),
        }

    def clean(self):
        cleaned_data = super().clean()
        # 至少需要一个指标有值
        indicator_fields = [
            'systolic_bp', 'diastolic_bp', 'heart_rate', 'pulse',
            'temperature', 'respiratory_rate', 'blood_glucose', 'blood_oxygen',
            'weight', 'height', 'pain_score',
        ]
        has_any_value = any(cleaned_data.get(field) for field in indicator_fields)
        if not has_any_value:
            raise forms.ValidationError(_('请至少填写一项体征指标'))
        return cleaned_data


class LabTestForm(forms.ModelForm):
    """V1 检验报告表单"""
    data_json = forms.CharField(
        label=_("检验指标 (JSON)"),
        required=False,
        widget=forms.Textarea(attrs={
            'rows': 5,
            'placeholder': '{"WBC": 6.5, "HGB": 145, "PLT": 200}',
            'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 text-sm focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20',
        }),
        help_text=_("JSON 格式"),
    )

    class Meta:
        model = LabTestRecord
        fields = ['test_date', 'source', 'lab_type', 'notes']
        widgets = {
            'test_date': forms.DateInput(attrs={'type': 'date', 'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20'}),
            'source': forms.TextInput(attrs={'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20', 'placeholder': _('如：北京协和医院')}),
            'lab_type': forms.Select(attrs={'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20'}),
            'notes': forms.Textarea(attrs={'rows': 2, 'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20'}),
        }

    def clean_data_json(self):
        import json
        raw = self.cleaned_data.get('data_json', '')
        if raw:
            try:
                return json.loads(raw)
            except json.JSONDecodeError:
                raise forms.ValidationError(_("JSON 格式不正确"))
        return {}

    def clean(self):
        cleaned_data = super().clean()
        lab_type = cleaned_data.get('lab_type')
        data = cleaned_data.get('data_json', {})
        if lab_type and lab_type != 'other' and not data:
            raise forms.ValidationError({'data_json': _('请填写检验指标数据')})
        return cleaned_data


class ImagingForm(forms.ModelForm):
    """V1 影像检查表单"""
    class Meta:
        model = ImagingRecord
        fields = ['exam_date', 'source', 'imaging_type', 'body_part', 'findings', 'conclusion', 'notes']
        widgets = {
            'exam_date': forms.DateInput(attrs={'type': 'date', 'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20'}),
            'source': forms.TextInput(attrs={'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20', 'placeholder': _('如：北京协和医院')}),
            'imaging_type': forms.Select(attrs={'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20'}),
            'body_part': forms.TextInput(attrs={'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20', 'placeholder': _('如：胸部')}),
            'findings': forms.Textarea(attrs={'rows': 4, 'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20'}),
            'conclusion': forms.Textarea(attrs={'rows': 2, 'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20'}),
            'notes': forms.Textarea(attrs={'rows': 2, 'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20'}),
        }


class MedicationRecordForm(forms.ModelForm):
    """V1 用药记录表单（手动录入）"""
    class Meta:
        model = MedicationRecord
        fields = ['medication_name', 'dosage', 'frequency', 'route', 'start_date', 'end_date', 'status', 'prescriber', 'notes']
        widgets = {
            'medication_name': forms.TextInput(attrs={
                'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20',
                'placeholder': _('如：二甲双胍、阿莫西林'),
            }),
            'dosage': forms.TextInput(attrs={
                'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20',
                'placeholder': _('如：500mg'),
            }),
            'frequency': forms.TextInput(attrs={
                'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20',
                'placeholder': _('如：每日2次'),
            }),
            'route': forms.Select(attrs={
                'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20',
            }),
            'start_date': forms.DateInput(attrs={
                'type': 'date',
                'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20',
            }),
            'end_date': forms.DateInput(attrs={
                'type': 'date',
                'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20',
            }),
            'status': forms.Select(attrs={
                'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20',
            }),
            'prescriber': forms.TextInput(attrs={
                'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20',
                'placeholder': _('如：王医生、北京协和医院'),
            }),
            'notes': forms.Textarea(attrs={
                'rows': 2,
                'class': 'w-full rounded-xl border border-slate-300 bg-slate-50 px-4 py-3 focus:border-sky-500 focus:ring-2 focus:ring-sky-500/20',
            }),
        }

    def clean_medication_name(self):
        name = self.cleaned_data.get('medication_name', '').strip()
        if not name:
            raise forms.ValidationError(_("药品名称不能为空"))
        return name
